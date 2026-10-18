package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

// Career mentors offer two things. Mock interviews are ordinary bookings
// (kind "mentor", the interview type in service) plus a scorecard the mentor
// fills in afterwards. Resume reviews have no time slot: the student uploads
// a resume, the mentor accepts and delivers written feedback within the
// turnaround they promised.

// ── Interview feedback ──────────────────────────────────────────────────────

func textLen(s string, min, max int) bool {
	n := len([]rune(s))
	return n >= min && n <= max
}

func (s *server) SaveInterviewFeedback(ctx context.Context, req *marketplacev1.SaveInterviewFeedbackRequest) (*marketplacev1.InterviewFeedback, error) {
	strengths := strings.TrimSpace(req.Strengths)
	improvements := strings.TrimSpace(req.Improvements)
	resources := strings.TrimSpace(req.Resources)
	switch {
	case !slices.Contains(verdicts, req.Verdict):
		return nil, grpcx.Invalid("choose a verdict")
	case !textLen(strengths, 10, 2000):
		return nil, grpcx.Invalid("strengths must be 10–2000 characters")
	case !textLen(improvements, 10, 2000):
		return nil, grpcx.Invalid("what to improve must be 10–2000 characters")
	case len([]rune(resources)) > 1000:
		return nil, grpcx.Invalid("resources must be at most 1000 characters")
	}
	scores := map[string]int32{}
	for _, sc := range req.Scores {
		if !slices.Contains(feedbackCriteria, sc.Criterion) {
			return nil, grpcx.Invalid("unknown criterion %q", sc.Criterion)
		}
		if sc.Score < 1 || sc.Score > 5 {
			return nil, grpcx.Invalid("scores must be 1–5")
		}
		scores[sc.Criterion] = sc.Score
	}
	if len(scores) != len(feedbackCriteria) {
		return nil, grpcx.Invalid("score every criterion")
	}

	var kind, provider, student, status string
	var start time.Time
	err := s.db.QueryRow(ctx, `SELECT kind, provider_id, student_id, status, start_at FROM bookings WHERE id = $1`, req.BookingId).
		Scan(&kind, &provider, &student, &status, &start)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("booking not found")
		}
		return nil, grpcx.Internal(err)
	}
	switch {
	case provider != req.ActorId && student != req.ActorId:
		return nil, grpcx.NotFound("booking not found")
	case provider != req.ActorId:
		return nil, grpcx.Denied("only the mentor gives feedback")
	case kind != "mentor":
		return nil, grpcx.Precondition("feedback is for mock interviews")
	case status != "completed" && !(status == "accepted" && !s.now().Before(start)):
		return nil, grpcx.Precondition("you can add feedback once the interview has started")
	}

	raw, _ := json.Marshal(scores)
	var created, updated time.Time
	if err := s.db.QueryRow(ctx, `INSERT INTO interview_feedback (booking_id, scores, verdict, strengths, improvements, resources)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (booking_id) DO UPDATE SET scores = EXCLUDED.scores, verdict = EXCLUDED.verdict, strengths = EXCLUDED.strengths,
			improvements = EXCLUDED.improvements, resources = EXCLUDED.resources, updated_at = now()
		RETURNING created_at, updated_at`,
		req.BookingId, raw, req.Verdict, strengths, improvements, resources).Scan(&created, &updated); err != nil {
		return nil, grpcx.Internal(err)
	}
	if created.Equal(updated) {
		s.notify(ctx, student, "interview_feedback", "Your mock interview feedback is ready",
			"Your mentor scored your interview and wrote what to work on next.", "/bookings/"+req.BookingId, true)
	}
	return s.GetInterviewFeedback(ctx, &marketplacev1.GetBookingRequest{Id: req.BookingId, ActorId: req.ActorId})
}

func (s *server) GetInterviewFeedback(ctx context.Context, req *marketplacev1.GetBookingRequest) (*marketplacev1.InterviewFeedback, error) {
	f := &marketplacev1.InterviewFeedback{BookingId: req.Id}
	var raw []byte
	var provider, student string
	var created, updated time.Time
	err := s.db.QueryRow(ctx, `SELECT f.scores, f.verdict, f.strengths, f.improvements, f.resources, f.created_at, f.updated_at,
			b.provider_id, b.student_id
		FROM interview_feedback f JOIN bookings b ON b.id = f.booking_id WHERE f.booking_id = $1`, req.Id).
		Scan(&raw, &f.Verdict, &f.Strengths, &f.Improvements, &f.Resources, &created, &updated, &provider, &student)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("no feedback yet")
		}
		return nil, grpcx.Internal(err)
	}
	if !req.ActorIsAdmin && req.ActorId != provider && req.ActorId != student {
		return nil, grpcx.NotFound("no feedback yet")
	}
	var scores map[string]int32
	_ = json.Unmarshal(raw, &scores)
	// In catalog order, so every scorecard reads the same way.
	for _, c := range feedbackCriteria {
		if v, ok := scores[c]; ok {
			f.Scores = append(f.Scores, &marketplacev1.Score{Criterion: c, Score: v})
		}
	}
	f.CreatedAt, f.UpdatedAt = created.Unix(), updated.Unix()
	return f, nil
}

// ── Resume reviews ──────────────────────────────────────────────────────────

const resumeColumns = `r.id, r.provider_profile_id, r.reviewer_id, r.student_id, r.target_role, r.notes, r.file_key, r.file_name,
	r.price_minor, r.currency, r.turnaround_days, r.status, r.status_reason, coalesce(r.cancelled_by::text, ''),
	r.due_at, r.accepted_at, r.delivered_at, coalesce(r.overall_score, 0), r.summary, r.sections, r.feedback_file_key,
	EXISTS (SELECT 1 FROM reviews v WHERE v.resume_review_id = r.id), r.created_at, r.updated_at, r.pins`

type pin struct {
	Page    int32   `json:"page"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Section string  `json:"section"`
	Comment string  `json:"comment"`
}

type sectionNote struct {
	Section string `json:"section"`
	Rating  int32  `json:"rating"`
	Comment string `json:"comment"`
}

func unixOrZero(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

func scanResume(row pgx.Row) (*marketplacev1.ResumeReview, error) {
	r := &marketplacev1.ResumeReview{}
	var due, accepted, delivered *time.Time
	var created, updated time.Time
	var turnaround, score int16
	var sections, pins []byte
	if err := row.Scan(&r.Id, &r.ProviderProfileId, &r.ReviewerId, &r.StudentId, &r.TargetRole, &r.Notes, &r.FileKey, &r.FileName,
		&r.PriceMinor, &r.Currency, &turnaround, &r.Status, &r.StatusReason, &r.CancelledBy,
		&due, &accepted, &delivered, &score, &r.Summary, &sections, &r.FeedbackFileKey,
		&r.Reviewed, &created, &updated, &pins); err != nil {
		return nil, err
	}
	r.TurnaroundDays, r.OverallScore = int32(turnaround), int32(score)
	r.DueAt, r.AcceptedAt, r.DeliveredAt = unixOrZero(due), unixOrZero(accepted), unixOrZero(delivered)
	r.CreatedAt, r.UpdatedAt = created.Unix(), updated.Unix()
	var notes []sectionNote
	_ = json.Unmarshal(sections, &notes)
	for _, n := range notes {
		r.Sections = append(r.Sections, &marketplacev1.SectionNote{Section: n.Section, Rating: n.Rating, Comment: n.Comment})
	}
	var ps []pin
	_ = json.Unmarshal(pins, &ps)
	for _, p := range ps {
		r.Pins = append(r.Pins, &marketplacev1.Pin{Page: p.Page, X: p.X, Y: p.Y, Section: p.Section, Comment: p.Comment})
	}
	return r, nil
}

func (s *server) resumeByID(ctx context.Context, q pgx.Row) (*marketplacev1.ResumeReview, error) {
	r, err := scanResume(q)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("resume review not found")
		}
		return nil, grpcx.Internal(err)
	}
	return r, nil
}

func (s *server) loadResume(ctx context.Context, id string) (*marketplacev1.ResumeReview, error) {
	return s.resumeByID(ctx, s.db.QueryRow(ctx, `SELECT `+resumeColumns+` FROM resume_reviews r WHERE r.id = $1`, id))
}

func (s *server) CreateResumeReview(ctx context.Context, req *marketplacev1.CreateResumeReviewRequest) (*marketplacev1.ResumeReview, error) {
	role := strings.TrimSpace(req.TargetRole)
	notes := strings.TrimSpace(req.Notes)
	name := strings.TrimSpace(req.FileName)
	switch {
	case !textLen(role, 2, 100):
		return nil, grpcx.Invalid("say which role you're applying for (2–100 characters)")
	case len([]rune(notes)) > 1000:
		return nil, grpcx.Invalid("notes must be at most 1000 characters")
	case !strings.HasPrefix(req.FileKey, "resumes/"):
		return nil, grpcx.Invalid("attach your resume")
	}
	if name == "" || len([]rune(name)) > 200 {
		name = "resume.pdf"
	}

	student, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: req.ActorId})
	if err != nil {
		return nil, err
	}
	switch {
	case student.Status != "active":
		return nil, grpcx.Denied("your account is suspended")
	case !campusVerified(student):
		return nil, grpcx.Precondition("VERIFICATION_REQUIRED: verify your campus email or student ID before requesting a review")
	}

	var p struct {
		userID, kind, campusID, currency, status string
		resume                                   bool
		price                                    int64
		turnaround                               int16
	}
	err = s.db.QueryRow(ctx, `SELECT user_id, kind, campus_id, currency, status, resume_reviews, resume_price_minor, resume_turnaround_days
		FROM provider_profiles WHERE id = $1`, req.ProviderProfileId).Scan(
		&p.userID, &p.kind, &p.campusID, &p.currency, &p.status, &p.resume, &p.price, &p.turnaround)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("profile not found")
		}
		return nil, grpcx.Internal(err)
	}
	switch {
	case p.kind != "mentor" || !p.resume:
		return nil, grpcx.Precondition("this profile doesn't review resumes")
	case p.status != "active":
		return nil, grpcx.Precondition("this mentor isn't taking requests right now")
	case p.userID == req.ActorId:
		return nil, grpcx.Invalid("you can't review your own resume")
	}
	if blocked, err := s.isBlocked(ctx, req.ActorId, p.userID); err != nil {
		return nil, grpcx.Internal(err)
	} else if blocked {
		return nil, grpcx.Denied("you can't send a request to this person")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialise a student's requests so the open-request cap holds.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('resume:' || $1))`, req.ActorId); err != nil {
		return nil, grpcx.Internal(err)
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resume_reviews WHERE student_id = $1 AND status IN ('requested', 'accepted')`,
		req.ActorId).Scan(&open); err != nil {
		return nil, grpcx.Internal(err)
	}
	if open >= resumeMaxOpen {
		return nil, grpcx.TooMany("you have %d resume reviews in progress; wait for one to finish or cancel a request", open)
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO resume_reviews (provider_profile_id, reviewer_id, student_id, campus_id, target_role, notes,
			file_key, file_name, price_minor, currency, turnaround_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		req.ProviderProfileId, p.userID, req.ActorId, p.campusID, role, notes, req.FileKey, name, p.price, p.currency, p.turnaround).Scan(&id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	s.notify(ctx, p.userID, "resume_requested", "New resume review request from "+displayName(student),
		fmt.Sprintf("%s wants feedback on their resume for %s. Accept or decline within %s.", displayName(student), role, humanDuration(resumeRequestTTL)),
		"/resume-reviews/"+id, true)
	return s.loadResume(ctx, id)
}

func (s *server) GetResumeReview(ctx context.Context, req *marketplacev1.GetResumeReviewRequest) (*marketplacev1.ResumeReview, error) {
	r, err := s.loadResume(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if !req.ActorIsAdmin && r.StudentId != req.ActorId && r.ReviewerId != req.ActorId {
		return nil, grpcx.NotFound("resume review not found")
	}
	return r, nil
}

func (s *server) ListResumeReviews(ctx context.Context, req *marketplacev1.ListResumeReviewsRequest) (*marketplacev1.ListResumeReviewsResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 20, 100)
	args := []any{req.ActorId}
	where := []string{"(r.student_id = $1 OR r.reviewer_id = $1)"}
	switch req.As {
	case "student":
		where[0] = "r.student_id = $1"
	case "reviewer":
		where[0] = "r.reviewer_id = $1"
	}
	if len(req.Statuses) > 0 {
		args = append(args, req.Statuses)
		where = append(where, fmt.Sprintf("r.status = ANY($%d)", len(args)))
	}
	cond := strings.Join(where, " AND ")
	out := &marketplacev1.ListResumeReviewsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM resume_reviews r WHERE `+cond, args...).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	// Open work first, then the most recent.
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM resume_reviews r WHERE %s
		ORDER BY (r.status IN ('requested', 'accepted')) DESC, r.created_at DESC LIMIT $%d OFFSET $%d`,
		resumeColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanResume(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Reviews = append(out.Reviews, r)
	}
	return out, rows.Err()
}

func (s *server) TransitionResumeReview(ctx context.Context, req *marketplacev1.TransitionResumeReviewRequest) (*marketplacev1.ResumeReview, error) {
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) > 500 {
		return nil, grpcx.Invalid("reason must be at most 500 characters")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	r, err := s.resumeByID(ctx, tx.QueryRow(ctx, `SELECT `+resumeColumns+` FROM resume_reviews r WHERE r.id = $1 FOR UPDATE`, req.Id))
	if err != nil {
		return nil, err
	}
	isReviewer, isStudent := r.ReviewerId == req.ActorId, r.StudentId == req.ActorId
	if !isReviewer && !isStudent {
		return nil, grpcx.NotFound("resume review not found")
	}
	now := s.now()
	overdue := r.Status == "accepted" && now.Unix() > r.DueAt

	var sql string
	args := []any{r.Id, reason}
	switch req.Action {
	case "accept":
		if !isReviewer {
			return nil, grpcx.Denied("only the mentor can accept")
		}
		if r.Status != "requested" {
			return nil, grpcx.Precondition("only new requests can be accepted")
		}
		args = append(args, now, now.Add(time.Duration(r.TurnaroundDays)*24*time.Hour))
		sql = `status = 'accepted', status_reason = $2, accepted_at = $3, due_at = $4`
	case "decline":
		if !isReviewer {
			return nil, grpcx.Denied("only the mentor can decline")
		}
		if r.Status != "requested" {
			return nil, grpcx.Precondition("only new requests can be declined")
		}
		args = append(args, now)
		sql = `status = 'declined', status_reason = $2, closed_at = $3`
	case "cancel":
		switch {
		case r.Status != "requested" && r.Status != "accepted":
			return nil, grpcx.Precondition("this review can no longer be cancelled")
		case isStudent && r.Status == "accepted" && !overdue:
			return nil, grpcx.Precondition("your mentor is working on it; you can cancel if it goes past the due date")
		case isReviewer && r.Status == "accepted" && reason == "":
			return nil, grpcx.Invalid("tell the student why you're cancelling")
		}
		args = append(args, now, req.ActorId)
		sql = `status = 'cancelled', status_reason = $2, closed_at = $3, cancelled_by = $4`
	default:
		return nil, grpcx.Invalid("unknown action %q", req.Action)
	}
	if _, err := tx.Exec(ctx, `UPDATE resume_reviews SET `+sql+`, updated_at = now() WHERE id = $1`, args...); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}

	link := "/resume-reviews/" + r.Id
	reasonText := ""
	if reason != "" {
		reasonText = " Reason: " + reason
	}
	switch req.Action {
	case "accept":
		s.notify(ctx, r.StudentId, "resume_accepted", "Your resume review was accepted",
			fmt.Sprintf("Your mentor will send feedback within %d days. Pay the agreed price (%s) directly as arranged.",
				r.TurnaroundDays, formatMoney(r.PriceMinor, r.Currency)), link, true)
	case "decline":
		s.notify(ctx, r.StudentId, "resume_declined", "Your resume review request was declined",
			"The mentor can't take it on right now."+reasonText+" Try another mentor.", link, true)
	case "cancel":
		other := r.ReviewerId
		if isReviewer {
			other = r.StudentId
		}
		s.notify(ctx, other, "resume_cancelled", "A resume review was cancelled", "The resume review for "+r.TargetRole+" was cancelled."+reasonText, link, true)
	}
	return s.loadResume(ctx, r.Id)
}

func (s *server) DeliverResumeReview(ctx context.Context, req *marketplacev1.DeliverResumeReviewRequest) (*marketplacev1.ResumeReview, error) {
	summary := strings.TrimSpace(req.Summary)
	switch {
	case req.OverallScore < 1 || req.OverallScore > 10:
		return nil, grpcx.Invalid("overall score must be 1–10")
	case !textLen(summary, 30, 3000):
		return nil, grpcx.Invalid("the summary must be 30–3000 characters")
	case req.FeedbackFileKey != "" && !strings.HasPrefix(req.FeedbackFileKey, "resumes/"):
		return nil, grpcx.Invalid("invalid feedback file")
	}
	var notes []sectionNote
	for _, n := range req.Sections {
		comment := strings.TrimSpace(n.Comment)
		switch {
		case !slices.Contains(resumeSections, n.Section):
			return nil, grpcx.Invalid("unknown section %q", n.Section)
		case slices.ContainsFunc(notes, func(x sectionNote) bool { return x.Section == n.Section }):
			return nil, grpcx.Invalid("each section only once")
		case n.Rating < 1 || n.Rating > 5:
			return nil, grpcx.Invalid("section ratings must be 1–5")
		case !textLen(comment, 1, 1500):
			return nil, grpcx.Invalid("each section comment must be 1–1500 characters")
		}
		notes = append(notes, sectionNote{Section: n.Section, Rating: n.Rating, Comment: comment})
	}
	if len(notes) < 3 {
		return nil, grpcx.Invalid("comment on at least 3 sections")
	}
	var pins []pin
	if len(req.Pins) > 40 {
		return nil, grpcx.Invalid("at most 40 pinned comments")
	}
	for _, p := range req.Pins {
		comment := strings.TrimSpace(p.Comment)
		switch {
		case p.Page < 1 || p.Page > 50 || p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1:
			return nil, grpcx.Invalid("a pinned comment is off the page")
		case p.Section != "" && !slices.Contains(resumeSections, p.Section):
			return nil, grpcx.Invalid("unknown section %q", p.Section)
		case !textLen(comment, 1, 1500):
			return nil, grpcx.Invalid("each pinned comment must be 1–1500 characters")
		}
		pins = append(pins, pin{Page: p.Page, X: p.X, Y: p.Y, Section: p.Section, Comment: comment})
	}
	if pins == nil {
		pins = []pin{}
	}
	rawPins, _ := json.Marshal(pins)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	r, err := s.resumeByID(ctx, tx.QueryRow(ctx, `SELECT `+resumeColumns+` FROM resume_reviews r WHERE r.id = $1 FOR UPDATE`, req.Id))
	if err != nil {
		return nil, err
	}
	switch {
	case r.ReviewerId != req.ActorId && r.StudentId != req.ActorId:
		return nil, grpcx.NotFound("resume review not found")
	case r.ReviewerId != req.ActorId:
		return nil, grpcx.Denied("only the mentor can deliver feedback")
	case r.Status != "accepted":
		return nil, grpcx.Precondition("accept the request before delivering feedback")
	}
	raw, _ := json.Marshal(notes)
	now := s.now()
	if _, err := tx.Exec(ctx, `UPDATE resume_reviews SET status = 'delivered', overall_score = $2, summary = $3, sections = $4,
			feedback_file_key = $5, delivered_at = $6, closed_at = $6, pins = $7, updated_at = now() WHERE id = $1`,
		r.Id, req.OverallScore, summary, raw, req.FeedbackFileKey, now, rawPins); err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE provider_profiles SET resume_reviews_delivered = resume_reviews_delivered + 1 WHERE id = $1`,
		r.ProviderProfileId); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	s.notify(ctx, r.StudentId, "resume_delivered", "Your resume feedback is ready",
		fmt.Sprintf("Your mentor scored your resume %d/10 and left notes section by section.", req.OverallScore), "/resume-reviews/"+r.Id, true)
	return s.loadResume(ctx, r.Id)
}

func (s *server) ClaimExpiredResumeFiles(ctx context.Context, _ *marketplacev1.Empty) (*marketplacev1.FileKeys, error) {
	rows, err := s.db.Query(ctx, `WITH old AS (
			SELECT id, file_key, feedback_file_key FROM resume_reviews
			WHERE closed_at < $1 AND (file_key <> '' OR feedback_file_key <> '') LIMIT 500 FOR UPDATE SKIP LOCKED)
		UPDATE resume_reviews r SET file_key = '', feedback_file_key = '' FROM old WHERE r.id = old.id
		RETURNING old.file_key, old.feedback_file_key`, s.now().Add(-resumeFileRetention))
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &marketplacev1.FileKeys{}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, grpcx.Internal(err)
		}
		for _, k := range []string{a, b} {
			if k != "" {
				out.Keys = append(out.Keys, k)
			}
		}
	}
	return out, rows.Err()
}

// expireResumeRequests closes requests the mentor never answered.
func (s *server) expireResumeRequests(ctx context.Context, now time.Time) {
	rows, err := s.db.Query(ctx, `UPDATE resume_reviews SET status = 'expired', closed_at = $1, updated_at = now()
		WHERE status = 'requested' AND created_at <= $2 RETURNING id, student_id`, now, now.Add(-resumeRequestTTL))
	if err != nil {
		slog.Error("expire resume reviews", "err", err)
		return
	}
	type expired struct{ id, student string }
	var list []expired
	for rows.Next() {
		var e expired
		if rows.Scan(&e.id, &e.student) == nil {
			list = append(list, e)
		}
	}
	rows.Close()
	for _, e := range list {
		s.notify(ctx, e.student, "resume_expired", "Your resume review request expired",
			"The mentor didn't answer in time. Try another mentor.", "/resume-reviews/"+e.id, false)
	}
}
