package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

// Interview experiences: a senior writes up a company's process round by
// round. An admin approves each one before it's published, as with the
// campus feed, because a false "they asked X" misleads a whole batch.

const experienceColumns = `id, author_id, campus_id, company, role, year, outcome, rounds, tips, anonymous, status, review_reason, created_at`

func scanExperience(row pgx.Row, a *communityv1.Actor) (*communityv1.InterviewExperience, error) {
	e := &communityv1.InterviewExperience{}
	var rounds []byte
	var created time.Time
	var year int16
	if err := row.Scan(&e.Id, &e.AuthorId, &e.CampusId, &e.Company, &e.Role, &year, &e.Outcome, &rounds, &e.Tips, &e.Anonymous,
		&e.Status, &e.ReviewReason, &created); err != nil {
		return nil, err
	}
	e.Year, e.CreatedAt = int32(year), created.Unix()
	_ = json.Unmarshal(rounds, &e.Rounds)
	e.Mine = a != nil && a.Id == e.AuthorId
	// Anonymous posts hide who wrote them from everyone but the author and admins.
	if e.Anonymous && !e.Mine && (a == nil || !a.IsAdmin) {
		e.AuthorId = ""
	}
	return e, nil
}

var outcomes = []string{"selected", "rejected", "waiting"}

func (s *server) CreateExperience(ctx context.Context, req *communityv1.CreateExperienceRequest) (*communityv1.InterviewExperience, error) {
	a := req.Actor
	if err := requireUser(a); err != nil {
		return nil, err
	}
	if !a.CampusVerified {
		return nil, grpcx.Precondition("VERIFICATION_REQUIRED: verify your campus email or student ID to share an interview experience")
	}
	company := strings.TrimSpace(req.Company)
	role := strings.TrimSpace(req.Role)
	tips := strings.TrimSpace(req.Tips)
	n := func(s string) int { return utf8.RuneCountInString(s) }
	switch {
	case n(company) < 2 || n(company) > 60:
		return nil, grpcx.Invalid("company must be 2–60 characters")
	case n(role) < 2 || n(role) > 80:
		return nil, grpcx.Invalid("role must be 2–80 characters")
	case req.Year < 2000 || int(req.Year) > time.Now().Year()+1:
		return nil, grpcx.Invalid("year is out of range")
	case !contains(outcomes, req.Outcome):
		return nil, grpcx.Invalid("outcome must be selected, rejected or waiting")
	case n(tips) > 2000:
		return nil, grpcx.Invalid("tips must be at most 2000 characters")
	case len(req.Rounds) == 0 || len(req.Rounds) > 10:
		return nil, grpcx.Invalid("describe 1–10 rounds")
	}
	rounds := make([]*communityv1.ExperienceRound, 0, len(req.Rounds))
	for _, r := range req.Rounds {
		name, details := strings.TrimSpace(r.Name), strings.TrimSpace(r.Details)
		if n(name) < 2 || n(name) > 60 || n(details) < 10 || n(details) > 3000 {
			return nil, grpcx.Invalid("each round needs a name (2–60 characters) and details (10–3000)")
		}
		rounds = append(rounds, &communityv1.ExperienceRound{Name: name, Details: details})
	}
	raw, _ := json.Marshal(rounds)

	var pending int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM interview_experiences WHERE author_id = $1 AND status = 'pending'`, a.Id).Scan(&pending); err != nil {
		return nil, grpcx.Internal(err)
	}
	if pending >= maxPendingFeedPosts {
		return nil, grpcx.TooMany("you have %d experiences waiting for review; wait for those first", pending)
	}
	e, err := scanExperience(s.db.QueryRow(ctx, `INSERT INTO interview_experiences (author_id, campus_id, company, role, year, outcome, rounds, tips, anonymous)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+experienceColumns,
		a.Id, a.CampusId, company, role, req.Year, req.Outcome, raw, tips, req.Anonymous), a)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return e, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *server) ListExperiences(ctx context.Context, req *communityv1.ListExperiencesRequest) (*communityv1.ListExperiencesResponse, error) {
	a := req.Actor
	if err := requireUser(a); err != nil {
		return nil, err
	}
	limit, offset := db.Page(req.Page, req.PageSize, 10, 50)
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	switch {
	case req.Mine:
		add("author_id = $%d", a.Id)
	case a.IsAdmin && req.Status != "":
		add("status = $%d", req.Status)
	default:
		where = append(where, "status = 'published'")
	}
	if c := strings.TrimSpace(req.Company); c != "" {
		add("lower(company) = lower($%d)", c)
	}
	if req.CampusId != "" {
		add("campus_id = $%d::uuid", req.CampusId)
	}
	if len(where) == 0 {
		where = []string{"true"}
	}
	cond := strings.Join(where, " AND ")
	out := &communityv1.ListExperiencesResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM interview_experiences WHERE `+cond, args...).Scan(&out.Total); err != nil {
		if strings.Contains(err.Error(), "invalid input syntax") {
			return out, nil
		}
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM interview_experiences WHERE %s ORDER BY year DESC, created_at DESC LIMIT $%d OFFSET $%d`,
		experienceColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanExperience(rows, a)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Experiences = append(out.Experiences, e)
	}
	return out, rows.Err()
}

func (s *server) GetExperience(ctx context.Context, req *communityv1.ExperienceRef) (*communityv1.InterviewExperience, error) {
	if err := requireUser(req.Actor); err != nil {
		return nil, err
	}
	e, err := scanExperience(s.db.QueryRow(ctx, `SELECT `+experienceColumns+` FROM interview_experiences WHERE id = $1`, req.Id), req.Actor)
	if err != nil {
		return nil, notFound(err, "experience")
	}
	if e.Status != "published" && !e.Mine && !req.Actor.IsAdmin {
		return nil, grpcx.NotFound("experience not found")
	}
	return e, nil
}

func (s *server) ReviewExperience(ctx context.Context, req *communityv1.ReviewExperienceRequest) (*communityv1.InterviewExperience, error) {
	status := "rejected"
	if req.Approve {
		status = "published"
	}
	e, err := scanExperience(s.db.QueryRow(ctx, `UPDATE interview_experiences SET status = $2, review_reason = $3, updated_at = now()
		WHERE id = $1 RETURNING `+experienceColumns, req.Id, status, strings.TrimSpace(req.Reason)), &communityv1.Actor{IsAdmin: true})
	if err != nil {
		return nil, notFound(err, "experience")
	}
	return e, nil
}

func (s *server) DeleteExperience(ctx context.Context, req *communityv1.ExperienceRef) (*communityv1.Empty, error) {
	if err := requireUser(req.Actor); err != nil {
		return nil, err
	}
	q, args := `DELETE FROM interview_experiences WHERE id = $1 AND author_id = $2`, []any{req.Id, req.Actor.Id}
	if req.Actor.IsAdmin {
		q, args = `DELETE FROM interview_experiences WHERE id = $1`, []any{req.Id}
	}
	tag, err := s.db.Exec(ctx, q, args...)
	if err != nil {
		return nil, notFound(err, "experience")
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("experience not found")
	}
	return &communityv1.Empty{}, nil
}

func (s *server) ExperienceCompanies(ctx context.Context, _ *communityv1.Empty) (*communityv1.ExperienceCompaniesResponse, error) {
	rows, err := s.db.Query(ctx, `SELECT mode() WITHIN GROUP (ORDER BY company), count(*) FROM interview_experiences
		WHERE status = 'published' GROUP BY lower(company) ORDER BY count(*) DESC, 1 LIMIT 30`)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ExperienceCompaniesResponse{}
	for rows.Next() {
		c := &communityv1.CompanyExperienceCount{}
		if err := rows.Scan(&c.Name, &c.Experiences); err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Companies = append(out.Companies, c)
	}
	return out, rows.Err()
}
