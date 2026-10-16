// Command community runs the community gRPC service: the moderated campus
// feed, abuse reports and contact inquiries.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/config"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
	"github.com/yourmate/backend/pkg/mailer"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type server struct {
	communityv1.UnimplementedCommunityServiceServer
	db         *pgxpool.Pool
	mail       *mailer.Mailer
	staffEmail string
	hub        *hub
}

func main() {
	grpcx.SetupLogging("community", config.Bool("DEBUG", false))
	ctx := context.Background()

	loadSettings()

	pool, err := db.Open(ctx, config.MustString("DATABASE_URL"), "community", int32(config.Int("DB_MAX_CONNS", 5)))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	migrations, _ := fs.Sub(migrationFiles, "migrations")
	if err := db.Migrate(ctx, pool, "community", migrations); err != nil {
		log.Fatal(err)
	}

	s := &server{
		hub:        newHub(),
		db:         pool,
		staffEmail: config.String("STAFF_EMAIL", ""),
		mail: &mailer.Mailer{
			Host:     config.String("SMTP_HOST", ""),
			Port:     config.Int("SMTP_PORT", 1025),
			Username: config.String("SMTP_USERNAME", ""),
			Password: config.String("SMTP_PASSWORD", ""),
			From:     config.MustString("MAIL_FROM"),
		},
	}
	go s.listen(ctx, pool)

	srv := grpcx.NewServer(config.MustString("INTERNAL_TOKEN"))
	communityv1.RegisterCommunityServiceServer(srv, s)
	if err := grpcx.Serve(srv, config.String("GRPC_ADDR", ":50073")); err != nil {
		slog.Error("server stopped", "err", err)
	}
}

func notFound(err error, what string) error {
	if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
		return grpcx.NotFound("%s not found", what)
	}
	return grpcx.Internal(err)
}

// ── Posts ───────────────────────────────────────────────────────────────────

const postColumns = `id, campus_id, author_id, type, title, body, image_key, event_at, location, status, review_reason, created_at`

func scanPost(row pgx.Row) (*communityv1.Post, error) {
	p := &communityv1.Post{}
	var eventAt *time.Time
	var created time.Time
	if err := row.Scan(&p.Id, &p.CampusId, &p.AuthorId, &p.Type, &p.Title, &p.Body, &p.ImageKey, &eventAt, &p.Location,
		&p.Status, &p.ReviewReason, &created); err != nil {
		return nil, err
	}
	p.EventAt = db.Unix(eventAt)
	p.CreatedAt = created.Unix()
	return p, nil
}

func (s *server) CreatePost(ctx context.Context, req *communityv1.CreatePostRequest) (*communityv1.Post, error) {
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	location := strings.TrimSpace(req.Location)
	switch {
	case !slices.Contains(postTypes, req.Type):
		return nil, grpcx.Invalid("type must be event, notice, place or tip")
	case len(title) < 5 || len(title) > 120:
		return nil, grpcx.Invalid("title must be 5–120 characters")
	case len(body) < 10 || len(body) > 4000:
		return nil, grpcx.Invalid("post must be 10–4000 characters")
	case len(location) > 200:
		return nil, grpcx.Invalid("location is too long")
	case req.Type == "event" && req.EventAt == 0:
		return nil, grpcx.Invalid("events need a date and time")
	}
	var pending int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM posts WHERE author_id = $1 AND status = 'pending'`, req.ActorId).Scan(&pending); err != nil {
		return nil, grpcx.Internal(err)
	}
	if pending >= maxPendingFeedPosts {
		return nil, grpcx.TooMany("you have %d posts waiting for review; wait for them to be approved", pending)
	}
	var eventAt *time.Time
	if req.EventAt > 0 {
		t := time.Unix(req.EventAt, 0)
		eventAt = &t
	}
	p, err := scanPost(s.db.QueryRow(ctx, `INSERT INTO posts (campus_id, author_id, type, title, body, image_key, event_at, location)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+postColumns,
		req.CampusId, req.ActorId, req.Type, title, body, req.ImageKey, eventAt, location))
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return p, nil
}

func (s *server) ListPosts(ctx context.Context, req *communityv1.ListPostsRequest) (*communityv1.ListPostsResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 20, 50)
	status := req.Status
	if status == "" {
		status = "published"
	}
	where := []string{}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if status != "all" {
		add("status = $%d", status)
	}
	if req.CampusId != "" {
		add("campus_id = $%d::uuid", req.CampusId)
	}
	if req.Type != "" {
		add("type = $%d", req.Type)
	}
	if req.AuthorId != "" {
		add("author_id = $%d::uuid", req.AuthorId)
	}
	cond := "true"
	if len(where) > 0 {
		cond = strings.Join(where, " AND ")
	}
	order := "created_at DESC"
	if status == "pending" {
		order = "created_at ASC"
	}
	out := &communityv1.ListPostsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM posts WHERE `+cond, args...).Scan(&out.Total); err != nil {
		if strings.Contains(err.Error(), "invalid input syntax") {
			return out, nil
		}
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM posts WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		postColumns, cond, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Posts = append(out.Posts, p)
	}
	return out, rows.Err()
}

func (s *server) GetPost(ctx context.Context, req *communityv1.GetPostRequest) (*communityv1.Post, error) {
	p, err := scanPost(s.db.QueryRow(ctx, `SELECT `+postColumns+` FROM posts WHERE id = $1`, req.Id))
	if err != nil {
		return nil, notFound(err, "post")
	}
	return p, nil
}

func (s *server) ReviewPost(ctx context.Context, req *communityv1.ReviewPostRequest) (*communityv1.Post, error) {
	status := "rejected"
	if req.Approve {
		status = "published"
	}
	p, err := scanPost(s.db.QueryRow(ctx, `UPDATE posts SET status = $2, review_reason = $3 WHERE id = $1 RETURNING `+postColumns,
		req.Id, status, strings.TrimSpace(req.Reason)))
	if err != nil {
		return nil, notFound(err, "post")
	}
	return p, nil
}

func (s *server) DeletePost(ctx context.Context, req *communityv1.DeletePostRequest) (*communityv1.Empty, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if req.ActorIsAdmin {
		tag, err = s.db.Exec(ctx, `DELETE FROM posts WHERE id = $1`, req.Id)
	} else {
		tag, err = s.db.Exec(ctx, `DELETE FROM posts WHERE id = $1 AND author_id = $2`, req.Id, req.ActorId)
	}
	if err != nil {
		return nil, notFound(err, "post")
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("post not found")
	}
	return &communityv1.Empty{}, nil
}

// ── Reports ─────────────────────────────────────────────────────────────────

const reportColumns = `id, reporter_id, target_type, target_id, reason, details, status, resolution_note, created_at, resolved_at`

func scanReport(row pgx.Row) (*communityv1.Report, error) {
	r := &communityv1.Report{}
	var created time.Time
	var resolved *time.Time
	if err := row.Scan(&r.Id, &r.ReporterId, &r.TargetType, &r.TargetId, &r.Reason, &r.Details, &r.Status, &r.ResolutionNote,
		&created, &resolved); err != nil {
		return nil, err
	}
	r.CreatedAt = created.Unix()
	r.ResolvedAt = db.Unix(resolved)
	return r, nil
}

func (s *server) CreateReport(ctx context.Context, req *communityv1.CreateReportRequest) (*communityv1.Report, error) {
	details := strings.TrimSpace(req.Details)
	switch {
	case !slices.Contains([]string{"user", "provider", "review", "post", "booking", "community", "community_post"}, req.TargetType):
		return nil, grpcx.Invalid("unknown report target")
	case !slices.Contains(reportReasons, req.Reason):
		return nil, grpcx.Invalid("choose a reason")
	case len(details) > 2000:
		return nil, grpcx.Invalid("details must be at most 2000 characters")
	case req.Reason == "other" && details == "":
		return nil, grpcx.Invalid("tell us what happened")
	}
	r, err := scanReport(s.db.QueryRow(ctx, `INSERT INTO reports (reporter_id, target_type, target_id, reason, details)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+reportColumns, req.ReporterId, req.TargetType, req.TargetId, req.Reason, details))
	if db.IsUniqueViolation(err, "") {
		return nil, grpcx.Exists("you already reported this; our team is looking into it")
	}
	if err != nil {
		return nil, notFound(err, "target")
	}
	if s.staffEmail != "" {
		s.mail.SendAsync(s.staffEmail, "New report: "+req.Reason+" ("+req.TargetType+")",
			fmt.Sprintf("Target: %s %s\nReason: %s\n\n%s\n", req.TargetType, req.TargetId, req.Reason, details))
	}
	return r, nil
}

func (s *server) ListReports(ctx context.Context, req *communityv1.ListReportsRequest) (*communityv1.ListReportsResponse, error) {
	status := req.Status
	if status == "" {
		status = "open"
	}
	limit, offset := db.Page(req.Page, req.PageSize, 25, 100)
	out := &communityv1.ListReportsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM reports WHERE status = $1`, status).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT `+reportColumns+` FROM reports WHERE status = $1 ORDER BY created_at LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Reports = append(out.Reports, r)
	}
	return out, rows.Err()
}

func (s *server) ResolveReport(ctx context.Context, req *communityv1.ResolveReportRequest) (*communityv1.Report, error) {
	if req.Status != "resolved" && req.Status != "dismissed" {
		return nil, grpcx.Invalid("status must be resolved or dismissed")
	}
	r, err := scanReport(s.db.QueryRow(ctx, `UPDATE reports SET status = $2, resolution_note = $3, resolved_at = now()
		WHERE id = $1 RETURNING `+reportColumns, req.Id, req.Status, strings.TrimSpace(req.Note)))
	if err != nil {
		return nil, notFound(err, "report")
	}
	return r, nil
}

// ── Contact ─────────────────────────────────────────────────────────────────

func (s *server) CreateInquiry(ctx context.Context, req *communityv1.CreateInquiryRequest) (*communityv1.Inquiry, error) {
	name := strings.TrimSpace(req.Name)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	msg := strings.TrimSpace(req.Message)
	if _, err := mail.ParseAddress(email); err != nil || len(email) > 254 {
		return nil, grpcx.Invalid("enter a valid email address")
	}
	if name == "" || len(name) > 100 || len(msg) < 10 || len(msg) > 4000 {
		return nil, grpcx.Invalid("name is required and the message must be 10–4000 characters")
	}
	var recent int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM inquiries WHERE email = $1 AND created_at > now() - interval '1 hour'`, email).Scan(&recent); err != nil {
		return nil, grpcx.Internal(err)
	}
	if recent >= contactPerHour {
		return nil, grpcx.TooMany("we've got your messages; we'll reply soon")
	}
	in := &communityv1.Inquiry{}
	var created time.Time
	if err := s.db.QueryRow(ctx, `INSERT INTO inquiries (name, email, message) VALUES ($1, $2, $3) RETURNING id, name, email, message, created_at`,
		name, email, msg).Scan(&in.Id, &in.Name, &in.Email, &in.Message, &created); err != nil {
		return nil, grpcx.Internal(err)
	}
	in.CreatedAt = created.Unix()
	if s.staffEmail != "" {
		s.mail.SendAsync(s.staffEmail, "Contact form: "+name, fmt.Sprintf("From: %s <%s>\n\n%s\n", name, email, msg))
	}
	return in, nil
}

func (s *server) ListInquiries(ctx context.Context, req *communityv1.ListInquiriesRequest) (*communityv1.ListInquiriesResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 25, 100)
	out := &communityv1.ListInquiriesResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM inquiries`).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT id, name, email, message, created_at FROM inquiries ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		in := &communityv1.Inquiry{}
		var created time.Time
		if err := rows.Scan(&in.Id, &in.Name, &in.Email, &in.Message, &created); err != nil {
			return nil, grpcx.Internal(err)
		}
		in.CreatedAt = created.Unix()
		out.Inquiries = append(out.Inquiries, in)
	}
	return out, rows.Err()
}

func (s *server) Stats(ctx context.Context, _ *communityv1.Empty) (*communityv1.CommunityStats, error) {
	st := &communityv1.CommunityStats{}
	if err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM posts WHERE status = 'published'),
		(SELECT count(*) FROM posts WHERE status = 'pending'),
		(SELECT count(*) FROM reports WHERE status = 'open'),
		(SELECT count(*) FROM inquiries)`).Scan(&st.PublishedPosts, &st.PendingPosts, &st.OpenReports, &st.Inquiries); err != nil {
		return nil, grpcx.Internal(err)
	}
	return st, nil
}
