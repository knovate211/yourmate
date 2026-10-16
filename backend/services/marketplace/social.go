package main

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

// ── Messages ────────────────────────────────────────────────────────────────

func (s *server) bookingParties(ctx context.Context, bookingID string) (provider, student, status string, err error) {
	err = s.db.QueryRow(ctx, `SELECT provider_id, student_id, status FROM bookings WHERE id = $1`, bookingID).Scan(&provider, &student, &status)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return "", "", "", grpcx.NotFound("booking not found")
		}
		return "", "", "", grpcx.Internal(err)
	}
	return provider, student, status, nil
}

func (s *server) ListMessages(ctx context.Context, req *marketplacev1.ListMessagesRequest) (*marketplacev1.ListMessagesResponse, error) {
	provider, student, _, err := s.bookingParties(ctx, req.BookingId)
	if err != nil {
		return nil, err
	}
	if !req.ActorIsAdmin && req.ActorId != provider && req.ActorId != student {
		return nil, grpcx.NotFound("booking not found")
	}
	rows, err := s.db.Query(ctx, `SELECT id, booking_id, sender_id, body, created_at FROM messages
		WHERE booking_id = $1 AND created_at > $2 ORDER BY created_at LIMIT 500`, req.BookingId, time.Unix(req.After, 0))
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &marketplacev1.ListMessagesResponse{}
	for rows.Next() {
		m := &marketplacev1.Message{}
		var created time.Time
		if err := rows.Scan(&m.Id, &m.BookingId, &m.SenderId, &m.Body, &created); err != nil {
			return nil, grpcx.Internal(err)
		}
		m.CreatedAt = created.Unix()
		out.Messages = append(out.Messages, m)
	}
	return out, rows.Err()
}

func (s *server) SendMessage(ctx context.Context, req *marketplacev1.SendMessageRequest) (*marketplacev1.Message, error) {
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		return nil, grpcx.Invalid("messages must be 1–2000 characters")
	}
	provider, student, status, err := s.bookingParties(ctx, req.BookingId)
	if err != nil {
		return nil, err
	}
	if req.ActorId != provider && req.ActorId != student {
		return nil, grpcx.NotFound("booking not found")
	}
	if status != "requested" && status != "accepted" && status != "completed" {
		return nil, grpcx.Precondition("this conversation is closed")
	}
	other := provider
	if req.ActorId == provider {
		other = student
	}
	if blocked, err := s.isBlocked(ctx, req.ActorId, other); err != nil {
		return nil, grpcx.Internal(err)
	} else if blocked {
		return nil, grpcx.Denied("you can't message this person")
	}
	m := &marketplacev1.Message{}
	var created time.Time
	if err := s.db.QueryRow(ctx, `INSERT INTO messages (booking_id, sender_id, body) VALUES ($1, $2, $3) RETURNING id, booking_id, sender_id, body, created_at`,
		req.BookingId, req.ActorId, body).Scan(&m.Id, &m.BookingId, &m.SenderId, &m.Body, &created); err != nil {
		return nil, grpcx.Internal(err)
	}
	m.CreatedAt = created.Unix()

	// One unread "new message" notification per conversation is enough.
	link := "/bookings/" + req.BookingId
	var pending bool
	_ = s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM notifications WHERE user_id = $1 AND type = 'message' AND link = $2 AND read_at IS NULL)`,
		other, link).Scan(&pending)
	if !pending {
		preview := body
		if len([]rune(preview)) > 120 {
			preview = string([]rune(preview)[:120]) + "…"
		}
		s.notify(ctx, other, "message", "New message about your session", preview, link, false)
	}
	return m, nil
}

// ── Reviews ─────────────────────────────────────────────────────────────────

const reviewColumns = `id, coalesce(booking_id::text, ''), reviewer_id, provider_profile_id, rating, comment, hidden, created_at,
	coalesce(resume_review_id::text, '')`

func scanReview(row pgx.Row) (*marketplacev1.Review, error) {
	r := &marketplacev1.Review{}
	var created time.Time
	if err := row.Scan(&r.Id, &r.BookingId, &r.ReviewerId, &r.ProviderProfileId, &r.Rating, &r.Comment, &r.Hidden, &created, &r.ResumeReviewId); err != nil {
		return nil, err
	}
	r.CreatedAt = created.Unix()
	return r, nil
}

func refreshRating(ctx context.Context, tx pgx.Tx, profileID string) error {
	_, err := tx.Exec(ctx, `UPDATE provider_profiles p SET
		rating_avg = coalesce((SELECT round(avg(rating)::numeric, 2)::float8 FROM reviews WHERE provider_profile_id = p.id AND NOT hidden), 0),
		rating_count = (SELECT count(*) FROM reviews WHERE provider_profile_id = p.id AND NOT hidden)
		WHERE p.id = $1`, profileID)
	return err
}

func (s *server) CreateReview(ctx context.Context, req *marketplacev1.CreateReviewRequest) (*marketplacev1.Review, error) {
	comment := strings.TrimSpace(req.Comment)
	if req.Rating < 1 || req.Rating > 5 {
		return nil, grpcx.Invalid("rating must be 1–5 stars")
	}
	if len(comment) > 1000 {
		return nil, grpcx.Invalid("review must be at most 1000 characters")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var student, profileID, status, provider string
	var bookingID, resumeID any
	what, done := "session", "completed"
	if req.ResumeReviewId != "" {
		what, done, resumeID = "resume review", "delivered", req.ResumeReviewId
		err = tx.QueryRow(ctx, `SELECT student_id, provider_profile_id, status, reviewer_id FROM resume_reviews WHERE id = $1 FOR UPDATE`,
			req.ResumeReviewId).Scan(&student, &profileID, &status, &provider)
	} else {
		bookingID = req.BookingId
		err = tx.QueryRow(ctx, `SELECT student_id, provider_profile_id, status, provider_id FROM bookings WHERE id = $1 FOR UPDATE`,
			req.BookingId).Scan(&student, &profileID, &status, &provider)
	}
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("%s not found", what)
		}
		return nil, grpcx.Internal(err)
	}
	if student != req.ActorId {
		return nil, grpcx.Denied("only the student can review this %s", what)
	}
	if status != done {
		return nil, grpcx.Precondition("you can review once the %s is %s", what, done)
	}
	r, err := scanReview(tx.QueryRow(ctx, `INSERT INTO reviews (booking_id, resume_review_id, reviewer_id, provider_profile_id, rating, comment)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+reviewColumns, bookingID, resumeID, req.ActorId, profileID, req.Rating, comment))
	if db.IsUniqueViolation(err, "") {
		return nil, grpcx.Exists("you already reviewed this %s", what)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := refreshRating(ctx, tx, profileID); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	s.notify(ctx, provider, "review", "You received a new review", strings.Repeat("★", int(r.Rating))+" "+comment, "/dashboard/profile", false)
	return r, nil
}

func (s *server) ListReviews(ctx context.Context, req *marketplacev1.ListReviewsRequest) (*marketplacev1.ListReviewsResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 10, 50)
	out := &marketplacev1.ListReviewsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM reviews WHERE provider_profile_id = $1 AND ($2 OR NOT hidden)`,
		req.ProviderProfileId, req.IncludeHidden).Scan(&out.Total); err != nil {
		if strings.Contains(err.Error(), "invalid input syntax") {
			return out, nil
		}
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE provider_profile_id = $1 AND ($2 OR NOT hidden)
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`, req.ProviderProfileId, req.IncludeHidden, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Reviews = append(out.Reviews, r)
	}
	return out, rows.Err()
}

func (s *server) GetReview(ctx context.Context, req *marketplacev1.GetReviewRequest) (*marketplacev1.Review, error) {
	r, err := scanReview(s.db.QueryRow(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE id = $1`, req.Id))
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("review not found")
		}
		return nil, grpcx.Internal(err)
	}
	return r, nil
}

func (s *server) SetReviewHidden(ctx context.Context, req *marketplacev1.SetReviewHiddenRequest) (*marketplacev1.Review, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	r, err := scanReview(tx.QueryRow(ctx, `UPDATE reviews SET hidden = $2 WHERE id = $1 RETURNING `+reviewColumns, req.Id, req.Hidden))
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("review not found")
		}
		return nil, grpcx.Internal(err)
	}
	if err := refreshRating(ctx, tx, r.ProviderProfileId); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return r, nil
}

// ── Notifications ───────────────────────────────────────────────────────────

// notify stores an in-app notification and optionally emails it. Failures
// are logged, never returned: a lost notification must not undo the action.
func (s *server) notify(ctx context.Context, userID, typ, title, body, link string, email bool) {
	if _, err := s.db.Exec(ctx, `INSERT INTO notifications (user_id, type, title, body, link) VALUES ($1, $2, $3, $4, $5)`,
		userID, typ, title, body, link); err != nil {
		grpcx.Internal(err) //nolint:errcheck // logs
	}
	if !email {
		return
	}
	u, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: userID})
	if err != nil {
		return
	}
	text := "Hi " + u.FirstName + ",\n\n" + body + "\n"
	if link != "" {
		text += "\nOpen Your Mate: " + s.appURL + link + "\n"
	}
	s.mail.SendAsync(u.Email, title, text)
}

func (s *server) Notify(ctx context.Context, req *marketplacev1.NotifyRequest) (*marketplacev1.Empty, error) {
	if req.UserId == "" || req.Title == "" {
		return nil, grpcx.Invalid("user and title are required")
	}
	s.notify(ctx, req.UserId, req.Type, req.Title, req.Body, req.Link, req.Email)
	return &marketplacev1.Empty{}, nil
}

func (s *server) ListNotifications(ctx context.Context, req *marketplacev1.ListNotificationsRequest) (*marketplacev1.ListNotificationsResponse, error) {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	out := &marketplacev1.ListNotificationsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, req.UserId).Scan(&out.UnreadCount); err != nil {
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT id, user_id, type, title, body, link, read_at IS NOT NULL, created_at FROM notifications
		WHERE user_id = $1 AND (NOT $2 OR read_at IS NULL) ORDER BY created_at DESC LIMIT $3`, req.UserId, req.UnreadOnly, limit)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		n := &marketplacev1.Notification{}
		var created time.Time
		if err := rows.Scan(&n.Id, &n.UserId, &n.Type, &n.Title, &n.Body, &n.Link, &n.Read, &created); err != nil {
			return nil, grpcx.Internal(err)
		}
		n.CreatedAt = created.Unix()
		out.Notifications = append(out.Notifications, n)
	}
	return out, rows.Err()
}

func (s *server) MarkNotificationsRead(ctx context.Context, req *marketplacev1.MarkNotificationsReadRequest) (*marketplacev1.Empty, error) {
	var err error
	if len(req.Ids) == 0 {
		_, err = s.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, req.UserId)
	} else {
		_, err = s.db.Exec(ctx, `UPDATE notifications SET read_at = now() WHERE user_id = $1 AND id = ANY($2::uuid[]) AND read_at IS NULL`, req.UserId, req.Ids)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

// ── Blocks ──────────────────────────────────────────────────────────────────

func (s *server) BlockUser(ctx context.Context, req *marketplacev1.BlockRequest) (*marketplacev1.Empty, error) {
	if req.ActorId == req.UserId {
		return nil, grpcx.Invalid("you can't block yourself")
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO blocks (user_id, blocked_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, req.ActorId, req.UserId); err != nil {
		return nil, grpcx.Invalid("user not found")
	}
	// Pending requests between the two are withdrawn.
	if _, err := s.db.Exec(ctx, `UPDATE bookings SET status = 'cancelled', cancelled_by = $1, status_reason = 'Cancelled after a block', updated_at = now()
		WHERE status = 'requested' AND ((provider_id = $1 AND student_id = $2) OR (provider_id = $2 AND student_id = $1))`, req.ActorId, req.UserId); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

func (s *server) UnblockUser(ctx context.Context, req *marketplacev1.BlockRequest) (*marketplacev1.Empty, error) {
	if _, err := s.db.Exec(ctx, `DELETE FROM blocks WHERE user_id = $1 AND blocked_id = $2`, req.ActorId, req.UserId); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

func (s *server) ListBlocks(ctx context.Context, req *marketplacev1.UserRequest) (*marketplacev1.ListBlocksResponse, error) {
	rows, err := s.db.Query(ctx, `SELECT blocked_id FROM blocks WHERE user_id = $1 ORDER BY created_at DESC`, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &marketplacev1.ListBlocksResponse{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, grpcx.Internal(err)
		}
		out.UserIds = append(out.UserIds, id)
	}
	return out, rows.Err()
}

// ── Stats ───────────────────────────────────────────────────────────────────

func (s *server) Stats(ctx context.Context, _ *marketplacev1.Empty) (*marketplacev1.MarketplaceStats, error) {
	st := &marketplacev1.MarketplaceStats{}
	var decided, accepted, finished, completed int64
	// One pass over each table instead of a subquery per number.
	err := s.db.QueryRow(ctx, `SELECT p.tutors, p.guides, p.mentors, p.drafts,
			b.total, b.requested, b.accepted, b.completed, b.last7d, b.decided, b.accepted_or_done, b.finished,
			r.avg, r.n, rr.delivered
		FROM (SELECT count(*) FILTER (WHERE kind = 'tutor' AND status = 'active') AS tutors,
				count(*) FILTER (WHERE kind = 'guide' AND status = 'active') AS guides,
				count(*) FILTER (WHERE kind = 'mentor' AND status = 'active') AS mentors,
				count(*) FILTER (WHERE status = 'draft') AS drafts
			FROM provider_profiles) p,
		(SELECT count(*) AS total,
				count(*) FILTER (WHERE status = 'requested') AS requested,
				count(*) FILTER (WHERE status = 'accepted') AS accepted,
				count(*) FILTER (WHERE status = 'completed') AS completed,
				count(*) FILTER (WHERE created_at > now() - interval '7 days') AS last7d,
				-- accept rate: of the requests the provider answered (or let expire), how many were accepted
				count(*) FILTER (WHERE status IN ('accepted', 'completed', 'declined', 'expired')) AS decided,
				count(*) FILTER (WHERE status IN ('accepted', 'completed')) AS accepted_or_done,
				-- completion rate: of the sessions that ended one way or another, how many happened
				count(*) FILTER (WHERE status IN ('completed', 'cancelled')) AS finished
			FROM bookings) b,
		(SELECT coalesce(avg(rating) FILTER (WHERE NOT hidden), 0)::float8 AS avg, count(*) AS n FROM reviews) r,
		(SELECT count(*) FILTER (WHERE status = 'delivered') AS delivered FROM resume_reviews) rr`).Scan(
		&st.ActiveTutors, &st.ActiveGuides, &st.ActiveMentors, &st.DraftProfiles, &st.BookingsTotal, &st.BookingsRequested,
		&st.BookingsAccepted, &st.BookingsCompleted, &st.BookingsLast_7D, &decided, &accepted, &finished,
		&st.AverageRating, &st.Reviews, &st.ResumeReviewsDelivered)
	completed = st.BookingsCompleted
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if decided > 0 {
		st.AcceptRate = float64(accepted) / float64(decided)
	}
	if finished > 0 {
		st.CompletionRate = float64(completed) / float64(finished)
	}

	rows, err := s.db.Query(ctx, `SELECT s.id, s.name,
			(SELECT count(*) FROM bookings b WHERE b.subject_id = s.id) AS bookings,
			(SELECT count(*) FROM provider_subjects ps JOIN provider_profiles p ON p.id = ps.profile_id
			  WHERE ps.subject_id = s.id AND p.status = 'active') AS tutors
		FROM subjects s ORDER BY bookings DESC, tutors DESC, s.name LIMIT 10`)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		d := &marketplacev1.SubjectDemand{}
		if err := rows.Scan(&d.SubjectId, &d.Name, &d.Bookings, &d.Tutors); err != nil {
			return nil, grpcx.Internal(err)
		}
		st.TopSubjects = append(st.TopSubjects, d)
	}
	return st, rows.Err()
}
