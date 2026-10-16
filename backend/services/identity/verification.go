package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

const verificationColumns = `id, user_id, document_key, status, coalesce(reviewer_id::text, ''), reason, created_at, reviewed_at`

func scanVerification(row pgx.Row) (*identityv1.Verification, error) {
	v := &identityv1.Verification{}
	var created time.Time
	var reviewed *time.Time
	if err := row.Scan(&v.Id, &v.UserId, &v.DocumentKey, &v.Status, &v.ReviewerId, &v.Reason, &created, &reviewed); err != nil {
		return nil, err
	}
	v.CreatedAt = created.Unix()
	v.ReviewedAt = db.Unix(reviewed)
	return v, nil
}

func (s *server) SubmitVerification(ctx context.Context, req *identityv1.SubmitVerificationRequest) (*identityv1.Verification, error) {
	if !strings.HasPrefix(req.DocumentKey, "verifications/") {
		return nil, grpcx.Invalid("upload the document first")
	}
	u, err := s.loadUser(ctx, req.ActorId)
	if err != nil {
		return nil, err
	}
	if u.IdVerified {
		return nil, grpcx.Precondition("your student ID is already verified")
	}
	if !u.EmailVerified {
		return nil, grpcx.Precondition("verify your email first")
	}
	v, err := scanVerification(s.db.QueryRow(ctx, `INSERT INTO verifications (user_id, document_key) VALUES ($1, $2) RETURNING `+verificationColumns,
		req.ActorId, req.DocumentKey))
	if db.IsUniqueViolation(err, "verifications_one_pending_idx") {
		return nil, grpcx.Exists("your student ID is already waiting for review")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return v, nil
}

func (s *server) GetLatestVerification(ctx context.Context, req *identityv1.UserIdRequest) (*identityv1.Verification, error) {
	v, err := scanVerification(s.db.QueryRow(ctx, `SELECT `+verificationColumns+` FROM verifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, req.UserId))
	if db.IsNoRows(err) {
		return &identityv1.Verification{UserId: req.UserId, Status: "none"}, nil
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return v, nil
}

func (s *server) ListVerifications(ctx context.Context, req *identityv1.ListVerificationsRequest) (*identityv1.ListVerificationsResponse, error) {
	status := req.Status
	if status == "" {
		status = "pending"
	}
	limit, offset := db.Page(req.Page, req.PageSize, 25, 100)
	out := &identityv1.ListVerificationsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM verifications WHERE status = $1`, status).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	// Oldest first for the pending queue, newest first for history.
	order := "created_at ASC"
	if status != "pending" {
		order = "reviewed_at DESC NULLS LAST"
	}
	rows, err := s.db.Query(ctx, `SELECT `+verificationColumns+` FROM verifications WHERE status = $1 ORDER BY `+order+` LIMIT $2 OFFSET $3`,
		status, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		v, err := scanVerification(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Verifications = append(out.Verifications, v)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

func (s *server) ReviewVerification(ctx context.Context, req *identityv1.ReviewRequest) (*identityv1.Verification, error) {
	reason := strings.TrimSpace(req.Reason)
	if !req.Approve && reason == "" {
		return nil, grpcx.Invalid("give a reason so the student knows what to fix")
	}
	status := "rejected"
	if req.Approve {
		status = "approved"
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	v, err := scanVerification(tx.QueryRow(ctx, `UPDATE verifications SET status = $2, reviewer_id = $3, reason = $4, reviewed_at = now()
		WHERE id = $1 AND status = 'pending' RETURNING `+verificationColumns, req.Id, status, req.ReviewerId, reason))
	if db.IsNoRows(err) {
		return nil, grpcx.Precondition("this verification was already reviewed")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if req.Approve {
		if _, err := tx.Exec(ctx, `UPDATE users SET id_verified = true, updated_at = now() WHERE id = $1`, v.UserId); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}

	if u, err := s.loadUser(ctx, v.UserId); err == nil {
		body := fmt.Sprintf("Hi %s,\n\nYour student ID is verified. You can now publish a tutor or guide profile and book sessions:\n%s/dashboard\n", u.FirstName, s.appURL)
		if !req.Approve {
			body = fmt.Sprintf("Hi %s,\n\nWe couldn't verify the student ID you uploaded.\n\nReason: %s\n\nYou can upload a clearer photo here:\n%s/verify\n", u.FirstName, reason, s.appURL)
		}
		s.mail.SendAsync(u.Email, "Your student ID verification", body)
	}
	return v, nil
}
