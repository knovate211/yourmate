package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

const userColumns = `id, email, first_name, last_name, campus_id, student_id_no, gender, phone, avatar_key, bio,
	email_verified, campus_email_verified, id_verified, is_admin, status, created_at`

func scanUser(row pgx.Row) (*identityv1.User, error) {
	u := &identityv1.User{}
	var isAdmin bool
	var created time.Time
	if err := row.Scan(&u.Id, &u.Email, &u.FirstName, &u.LastName, &u.CampusId, &u.StudentIdNo, &u.Gender, &u.Phone,
		&u.AvatarKey, &u.Bio, &u.EmailVerified, &u.CampusEmailVerified, &u.IdVerified, &isAdmin, &u.Status, &created); err != nil {
		return nil, err
	}
	u.Roles = []string{"user"}
	if isAdmin {
		u.Roles = append(u.Roles, "admin")
	}
	u.CreatedAt = created.Unix()
	return u, nil
}

func (s *server) loadUser(ctx context.Context, id string) (*identityv1.User, error) {
	u, err := scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("user not found")
		}
		return nil, grpcx.Internal(err)
	}
	return u, nil
}

func (s *server) GetUser(ctx context.Context, req *identityv1.UserIdRequest) (*identityv1.User, error) {
	return s.loadUser(ctx, req.UserId)
}

func (s *server) GetUsers(ctx context.Context, req *identityv1.GetUsersRequest) (*identityv1.GetUsersResponse, error) {
	if len(req.UserIds) == 0 {
		return &identityv1.GetUsersResponse{}, nil
	}
	if len(req.UserIds) > 500 {
		return nil, grpcx.Invalid("at most 500 ids per call")
	}
	rows, err := s.db.Query(ctx, `SELECT `+userColumns+` FROM users WHERE id = ANY($1::uuid[])`, req.UserIds)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &identityv1.GetUsersResponse{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Users = append(out.Users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

func (s *server) UpdateProfile(ctx context.Context, req *identityv1.UpdateProfileRequest) (*identityv1.User, error) {
	fields := map[string]struct {
		value string
		max   int
	}{
		"first_name":    {strings.TrimSpace(req.FirstName), 60},
		"last_name":     {strings.TrimSpace(req.LastName), 60},
		"gender":        {strings.TrimSpace(req.Gender), 30},
		"phone":         {strings.TrimSpace(req.Phone), 30},
		"bio":           {strings.TrimSpace(req.Bio), 1000},
		"avatar_key":    {req.AvatarKey, 200},
		"student_id_no": {strings.TrimSpace(req.StudentIdNo), 40},
	}
	var sets []string
	args := []any{req.ActorId}
	for _, name := range req.UpdateMask {
		f, ok := fields[name]
		if !ok {
			return nil, grpcx.Invalid("unknown field %q", name)
		}
		if len(f.value) > f.max {
			return nil, grpcx.Invalid("%s is too long (max %d characters)", strings.ReplaceAll(name, "_", " "), f.max)
		}
		if name == "first_name" && f.value == "" {
			return nil, grpcx.Invalid("first name is required")
		}
		if slices.Contains(sets, name) {
			continue
		}
		args = append(args, f.value)
		sets = append(sets, fmt.Sprintf("%s = $%d", name, len(args)))
	}
	if len(sets) > 0 {
		tag, err := s.db.Exec(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = $1`, args...)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		if tag.RowsAffected() == 0 {
			return nil, grpcx.NotFound("user not found")
		}
	}
	return s.loadUser(ctx, req.ActorId)
}

func (s *server) ListUsers(ctx context.Context, req *identityv1.ListUsersRequest) (*identityv1.ListUsersResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 25, 100)
	where := []string{"true"}
	var args []any
	if q := strings.TrimSpace(req.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		where = append(where, fmt.Sprintf("(email LIKE $%[1]d OR lower(first_name || ' ' || last_name) LIKE $%[1]d OR lower(student_id_no) LIKE $%[1]d)", len(args)))
	}
	if req.CampusId != "" {
		args = append(args, req.CampusId)
		where = append(where, fmt.Sprintf("campus_id = $%d", len(args)))
	}
	cond := strings.Join(where, " AND ")

	out := &identityv1.ListUsersResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE `+cond, args...).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM users WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		userColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Users = append(out.Users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

func (s *server) SetUserStatus(ctx context.Context, req *identityv1.SetUserStatusRequest) (*identityv1.User, error) {
	if req.Status != "active" && req.Status != "suspended" {
		return nil, grpcx.Invalid("status must be active or suspended")
	}
	tag, err := s.db.Exec(ctx, `UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, req.UserId, req.Status)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("user not found")
	}
	if req.Status == "suspended" {
		// Existing access tokens expire within minutes; refresh is cut now.
		if _, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, req.UserId); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	return s.loadUser(ctx, req.UserId)
}

func (s *server) SetUserAdmin(ctx context.Context, req *identityv1.SetUserAdminRequest) (*identityv1.User, error) {
	if !req.Admin {
		var admins int
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM users WHERE is_admin AND id <> $1`, req.UserId).Scan(&admins); err != nil {
			return nil, grpcx.Internal(err)
		}
		if admins == 0 {
			return nil, grpcx.Precondition("can't remove the last admin")
		}
	}
	tag, err := s.db.Exec(ctx, `UPDATE users SET is_admin = $2, updated_at = now() WHERE id = $1`, req.UserId, req.Admin)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("user not found")
	}
	return s.loadUser(ctx, req.UserId)
}

func (s *server) Stats(ctx context.Context, _ *identityv1.Empty) (*identityv1.IdentityStats, error) {
	st := &identityv1.IdentityStats{}
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM users),
		(SELECT count(*) FROM users WHERE email_verified),
		(SELECT count(*) FROM users WHERE campus_email_verified OR id_verified),
		(SELECT count(*) FROM verifications WHERE status = 'pending'),
		(SELECT count(*) FROM campuses WHERE status = 'active'),
		(SELECT count(*) FROM campuses WHERE status = 'pending'),
		(SELECT count(*) FROM users WHERE created_at > now() - interval '7 days')`).Scan(
		&st.Users, &st.EmailVerified, &st.CampusVerified, &st.PendingVerifications, &st.Campuses, &st.PendingCampuses, &st.SignupsLast_7D)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return st, nil
}
