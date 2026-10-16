package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	"github.com/yourmate/backend/pkg/auth"
	"github.com/yourmate/backend/pkg/config"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
	"github.com/yourmate/backend/pkg/mailer"
)

// Sign-in rules. These are the defaults; each one can be overridden with the
// environment variable named in loadSettings (see .env.example).
var (
	otpTTL             = 10 * time.Minute // how long an emailed code works
	otpMaxAttempts     = 5                // wrong guesses allowed per code
	otpResendGap       = 60 * time.Second // wait before another code can be sent
	otpMaxPerHour      = 5                // codes per person per hour
	loginFailureWindow = 15 * time.Minute // failed logins are counted over this window
	loginMaxFailures   = 10               // failed logins before the account is paused
)

func loadSettings() {
	otpTTL = config.Duration("OTP_TTL", otpTTL)
	otpMaxAttempts = config.Int("OTP_MAX_ATTEMPTS", otpMaxAttempts)
	otpResendGap = config.Duration("OTP_RESEND_GAP", otpResendGap)
	otpMaxPerHour = config.Int("OTP_MAX_PER_HOUR", otpMaxPerHour)
	loginFailureWindow = config.Duration("LOGIN_FAILURE_WINDOW", loginFailureWindow)
	loginMaxFailures = config.Int("LOGIN_MAX_FAILURES", loginMaxFailures)
}

func minutes(d time.Duration) string {
	if m := int(d.Minutes()); m != 1 {
		return fmt.Sprintf("%d minutes", m)
	}
	return "1 minute"
}

type server struct {
	identityv1.UnimplementedIdentityServiceServer
	db         *pgxpool.Pool
	signer     *auth.Signer
	mail       *mailer.Mailer
	refreshTTL time.Duration
	appURL     string
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	if len(s) > 254 || !strings.Contains(s, "@") {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".")
}

func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < 8 {
		return grpcx.Invalid("password must be at least 8 characters")
	}
	if len(p) > 72 {
		return grpcx.Invalid("password must be at most 72 bytes")
	}
	return nil
}

func hashPassword(p string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(h), err
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// otpHash binds the code to the user so equal codes never share a hash.
func otpHash(userID, code string) string { return sha(userID + ":" + code) }

func (s *server) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	email := normalizeEmail(req.Email)
	first := strings.TrimSpace(req.FirstName)
	last := strings.TrimSpace(req.LastName)
	switch {
	case !validEmail(email):
		return nil, grpcx.Invalid("enter a valid email address")
	case first == "" || len(first) > 60 || len(last) > 60:
		return nil, grpcx.Invalid("first name is required (max 60 characters)")
	case req.CampusId == "":
		return nil, grpcx.Invalid("choose your campus")
	case len(req.StudentIdNo) > 40 || len(req.Phone) > 30 || len(req.Gender) > 30:
		return nil, grpcx.Invalid("a profile field is too long")
	}
	if err := validatePassword(req.Password); err != nil {
		return nil, err
	}

	var campusStatus string
	if err := s.db.QueryRow(ctx, `SELECT status FROM campuses WHERE id = $1`, req.CampusId).Scan(&campusStatus); err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.Invalid("campus not found")
		}
		return nil, grpcx.Internal(err)
	}
	if campusStatus == "rejected" {
		return nil, grpcx.Invalid("that campus is not available; choose another or request it again")
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		return nil, grpcx.Internal(err)
	}

	var userID string
	var verified bool
	err = s.db.QueryRow(ctx, `SELECT id, email_verified FROM users WHERE email = $1`, email).Scan(&userID, &verified)
	switch {
	case err == nil && verified:
		return nil, grpcx.Exists("an account with this email already exists; log in instead")
	case err == nil:
		// An unverified account can be claimed again by whoever controls the
		// inbox, so a typo'd or abandoned signup never locks the address.
		_, err = s.db.Exec(ctx, `UPDATE users SET password_hash = $2, first_name = $3, last_name = $4,
			campus_id = $5, student_id_no = $6, gender = $7, phone = $8, updated_at = now() WHERE id = $1`,
			userID, hash, first, last, req.CampusId, strings.TrimSpace(req.StudentIdNo), strings.TrimSpace(req.Gender), strings.TrimSpace(req.Phone))
	case db.IsNoRows(err):
		err = s.db.QueryRow(ctx, `INSERT INTO users (email, password_hash, first_name, last_name, campus_id, student_id_no, gender, phone)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			email, hash, first, last, req.CampusId, strings.TrimSpace(req.StudentIdNo), strings.TrimSpace(req.Gender), strings.TrimSpace(req.Phone)).Scan(&userID)
		if db.IsUniqueViolation(err, "") {
			return nil, grpcx.Exists("an account with this email already exists")
		}
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}

	if err := s.sendOTP(ctx, userID, email, first, "verify", true); err != nil {
		return nil, err
	}
	return &identityv1.RegisterResponse{UserId: userID, Email: email}, nil
}

// sendOTP issues a fresh code, enforcing a resend gap and an hourly cap.
func (s *server) sendOTP(ctx context.Context, userID, email, name, purpose string, skipGap bool) error {
	var lastAt *time.Time
	var hourCount int
	if err := s.db.QueryRow(ctx, `SELECT max(created_at), count(*) FILTER (WHERE created_at > now() - interval '1 hour')
		FROM email_otps WHERE user_id = $1 AND purpose = $2`, userID, purpose).Scan(&lastAt, &hourCount); err != nil {
		return grpcx.Internal(err)
	}
	if hourCount >= otpMaxPerHour {
		return grpcx.TooMany("too many codes requested; try again in an hour")
	}
	if !skipGap && lastAt != nil && time.Since(*lastAt) < otpResendGap {
		return grpcx.TooMany("wait %d seconds before requesting another code", int((otpResendGap-time.Since(*lastAt)).Seconds())+1)
	}

	code, err := newOTP()
	if err != nil {
		return grpcx.Internal(err)
	}
	// Older codes stop working as soon as a new one is issued.
	if _, err := s.db.Exec(ctx, `UPDATE email_otps SET consumed_at = now() WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL`, userID, purpose); err != nil {
		return grpcx.Internal(err)
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO email_otps (user_id, purpose, code_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, purpose, otpHash(userID, code), time.Now().Add(otpTTL)); err != nil {
		return grpcx.Internal(err)
	}

	subject, body := "Your Your Mate verification code", fmt.Sprintf(
		"Hi %s,\n\nYour verification code is %s. It expires in %s.\n\nIf you didn't sign up for Your Mate, ignore this email.\n", name, code, minutes(otpTTL))
	if purpose == "reset" {
		subject, body = "Reset your Your Mate password", fmt.Sprintf(
			"Hi %s,\n\nUse the code %s to reset your password. It expires in %s.\n\nIf you didn't ask for this, you can ignore this email; your password is unchanged.\n", name, code, minutes(otpTTL))
	}
	if err := s.mail.Send(email, subject, body); err != nil {
		slog.Error("otp email failed", "err", err)
		return grpcx.Precondition("we couldn't send the email right now; try again shortly")
	}
	return nil
}

// checkOTP validates and consumes the latest code for the purpose.
func (s *server) checkOTP(ctx context.Context, tx pgx.Tx, userID, purpose, code string) error {
	var id, hash string
	var attempts int
	var expires time.Time
	err := tx.QueryRow(ctx, `SELECT id, code_hash, attempts, expires_at FROM email_otps
		WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 1 FOR UPDATE`,
		userID, purpose).Scan(&id, &hash, &attempts, &expires)
	if db.IsNoRows(err) {
		return grpcx.Invalid("code is invalid or expired; request a new one")
	}
	if err != nil {
		return grpcx.Internal(err)
	}
	if time.Now().After(expires) || attempts >= otpMaxAttempts {
		return grpcx.Invalid("code is invalid or expired; request a new one")
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(otpHash(userID, strings.TrimSpace(code)))) != 1 {
		if _, err := tx.Exec(ctx, `UPDATE email_otps SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return grpcx.Internal(err)
		}
		return errWrongCode
	}
	if _, err := tx.Exec(ctx, `UPDATE email_otps SET consumed_at = now() WHERE id = $1`, id); err != nil {
		return grpcx.Internal(err)
	}
	return nil
}

var errWrongCode = grpcx.Invalid("that code is not correct")

// withOTP runs fn in a transaction after the code checks out. A wrong code
// still commits so the attempt counter sticks.
func (s *server) withOTP(ctx context.Context, userID, purpose, code string, fn func(pgx.Tx) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.checkOTP(ctx, tx, userID, purpose, code); err != nil {
		if err == errWrongCode {
			if cerr := tx.Commit(ctx); cerr != nil {
				return grpcx.Internal(cerr)
			}
		}
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return grpcx.Internal(err)
	}
	return nil
}

func (s *server) VerifyEmail(ctx context.Context, req *identityv1.VerifyEmailRequest) (*identityv1.AuthResponse, error) {
	email := normalizeEmail(req.Email)
	var userID string
	if err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		if db.IsNoRows(err) {
			return nil, grpcx.Invalid("code is invalid or expired; request a new one")
		}
		return nil, grpcx.Internal(err)
	}
	err := s.withOTP(ctx, userID, "verify", req.Code, func(tx pgx.Tx) error {
		// The email proves campus membership only when the campus is active
		// (its domains were reviewed by an admin).
		_, err := tx.Exec(ctx, `UPDATE users u SET email_verified = true, updated_at = now(),
			campus_email_verified = u.campus_email_verified OR (c.status = 'active' AND domain_matches(u.email, c.email_domains))
			FROM campuses c WHERE c.id = u.campus_id AND u.id = $1`, userID)
		if err != nil {
			return grpcx.Internal(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	s.mail.SendAsync(u.Email, "Welcome to Your Mate", fmt.Sprintf(
		"Hi %s,\n\nYour email is verified. Find a tutor or campus guide, or start earning by offering your own skills:\n%s\n", u.FirstName, s.appURL))
	return s.issueSession(ctx, u, req.UserAgent)
}

func (s *server) ResendOtp(ctx context.Context, req *identityv1.EmailRequest) (*identityv1.Empty, error) {
	email := normalizeEmail(req.Email)
	var userID, name string
	var verified bool
	err := s.db.QueryRow(ctx, `SELECT id, first_name, email_verified FROM users WHERE email = $1`, email).Scan(&userID, &name, &verified)
	if db.IsNoRows(err) || (err == nil && verified) {
		return &identityv1.Empty{}, nil // don't reveal which emails exist
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := s.sendOTP(ctx, userID, email, name, "verify", false); err != nil {
		return nil, err
	}
	return &identityv1.Empty{}, nil
}

func (s *server) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.AuthResponse, error) {
	email := normalizeEmail(req.Email)
	var failures int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM login_failures WHERE email = $1 AND at > $2`,
		email, time.Now().Add(-loginFailureWindow)).Scan(&failures); err != nil {
		return nil, grpcx.Internal(err)
	}
	if failures >= loginMaxFailures {
		return nil, grpcx.TooMany("too many failed attempts; try again in %s or reset your password", minutes(loginFailureWindow))
	}

	var userID, hash string
	err := s.db.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&userID, &hash)
	if err != nil && !db.IsNoRows(err) {
		return nil, grpcx.Internal(err)
	}
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		if _, err := s.db.Exec(ctx, `INSERT INTO login_failures (email) VALUES ($1)`, email); err != nil {
			return nil, grpcx.Internal(err)
		}
		return nil, grpcx.Unauthenticated("email or password is incorrect")
	}
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Status == "suspended" {
		return nil, grpcx.Denied("this account is suspended; contact support")
	}
	if !u.EmailVerified {
		return nil, grpcx.Precondition("EMAIL_NOT_VERIFIED: verify your email to continue")
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM login_failures WHERE email = $1`, email); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.issueSession(ctx, u, req.UserAgent)
}

func (s *server) issueSession(ctx context.Context, u *identityv1.User, userAgent string) (*identityv1.AuthResponse, error) {
	access, accessExp, err := s.signer.Sign(u.Id, u.CampusId, u.Roles)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, grpcx.Internal(err)
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	refreshExp := time.Now().Add(s.refreshTTL)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO refresh_tokens (user_id, token_hash, user_agent, expires_at) VALUES ($1, $2, $3, $4)`,
		u.Id, sha(refresh), userAgent, refreshExp); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &identityv1.AuthResponse{
		AccessToken:      access,
		AccessExpiresAt:  accessExp.Unix(),
		RefreshToken:     refresh,
		RefreshExpiresAt: refreshExp.Unix(),
		User:             u,
	}, nil
}

func (s *server) Refresh(ctx context.Context, req *identityv1.RefreshRequest) (*identityv1.AuthResponse, error) {
	if req.RefreshToken == "" {
		return nil, grpcx.Unauthenticated("no session")
	}
	var id, userID, userAgent string
	var expires time.Time
	var revoked *time.Time
	err := s.db.QueryRow(ctx, `SELECT id, user_id, user_agent, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1`,
		sha(req.RefreshToken)).Scan(&id, &userID, &userAgent, &expires, &revoked)
	if db.IsNoRows(err) {
		return nil, grpcx.Unauthenticated("session expired; log in again")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if revoked != nil {
		// A rotated token came back: someone else has a copy. End every session.
		slog.Warn("refresh token reuse detected", "user_id", userID)
		if _, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
			return nil, grpcx.Internal(err)
		}
		return nil, grpcx.Unauthenticated("session expired; log in again")
	}
	if time.Now().After(expires) {
		return nil, grpcx.Unauthenticated("session expired; log in again")
	}
	tag, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.Unauthenticated("session expired; log in again") // lost a concurrent rotation
	}
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u.Status == "suspended" {
		return nil, grpcx.Denied("this account is suspended; contact support")
	}
	return s.issueSession(ctx, u, userAgent)
}

func (s *server) Logout(ctx context.Context, req *identityv1.RefreshRequest) (*identityv1.Empty, error) {
	if req.RefreshToken != "" {
		if _, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, sha(req.RefreshToken)); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	return &identityv1.Empty{}, nil
}

func (s *server) RequestPasswordReset(ctx context.Context, req *identityv1.EmailRequest) (*identityv1.Empty, error) {
	email := normalizeEmail(req.Email)
	var userID, name string
	err := s.db.QueryRow(ctx, `SELECT id, first_name FROM users WHERE email = $1 AND status = 'active'`, email).Scan(&userID, &name)
	if db.IsNoRows(err) {
		return &identityv1.Empty{}, nil // don't reveal which emails exist
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := s.sendOTP(ctx, userID, email, name, "reset", false); err != nil {
		return nil, err
	}
	return &identityv1.Empty{}, nil
}

func (s *server) ResetPassword(ctx context.Context, req *identityv1.ResetPasswordRequest) (*identityv1.Empty, error) {
	if err := validatePassword(req.NewPassword); err != nil {
		return nil, err
	}
	email := normalizeEmail(req.Email)
	var userID string
	if err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		if db.IsNoRows(err) {
			return nil, grpcx.Invalid("code is invalid or expired; request a new one")
		}
		return nil, grpcx.Internal(err)
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	err = s.withOTP(ctx, userID, "reset", req.Code, func(tx pgx.Tx) error {
		// Receiving the reset code also proves the inbox, so the email counts as verified.
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, email_verified = true, updated_at = now() WHERE id = $1`, userID, hash); err != nil {
			return grpcx.Internal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID); err != nil {
			return grpcx.Internal(err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM login_failures WHERE email = $1`, email); err != nil {
			return grpcx.Internal(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &identityv1.Empty{}, nil
}

func (s *server) ChangePassword(ctx context.Context, req *identityv1.ChangePasswordRequest) (*identityv1.Empty, error) {
	if err := validatePassword(req.NewPassword); err != nil {
		return nil, err
	}
	var hash string
	if err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, req.ActorId).Scan(&hash); err != nil {
		if db.IsNoRows(err) {
			return nil, grpcx.NotFound("user not found")
		}
		return nil, grpcx.Internal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		return nil, grpcx.Invalid("current password is incorrect")
	}
	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, req.ActorId, newHash); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &identityv1.Empty{}, nil
}

// bootstrapAdmin makes sure the configured admin account exists, so a fresh
// deployment has someone who can approve campuses and verifications.
func (s *server) bootstrapAdmin(ctx context.Context, email, password, campusSlug string) error {
	email = normalizeEmail(email)
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		_, err := s.db.Exec(ctx, `UPDATE users SET is_admin = true WHERE email = $1`, email)
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users (email, password_hash, first_name, last_name, campus_id, email_verified, is_admin)
		SELECT $1, $2, 'Your Mate', 'Admin', id, true, true FROM campuses WHERE slug = $3`, email, hash, campusSlug)
	if err == nil {
		slog.Info("created admin account", "email", email)
	}
	return err
}
