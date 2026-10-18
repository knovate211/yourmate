package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

// Placement prep: the live interview room, the mentors' question bank, and
// the numbers behind the prep hub and a student's progress page.

// ── Rooms ───────────────────────────────────────────────────────────────────

// roomWindow is when the booking's room is open: a little before the start,
// so both sides can check their camera, until a while after the end.
func roomWindow(b *marketplacev1.Booking) (opens, closes time.Time) {
	return time.Unix(b.StartAt, 0).Add(-roomOpensBefore), time.Unix(b.EndAt, 0).Add(roomClosesAfter)
}

func (s *server) partyBooking(ctx context.Context, bookingID, actorID string, admin bool) (*marketplacev1.Booking, error) {
	b, err := s.bookingByID(ctx, s.db.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings b WHERE b.id = $1`, bookingID))
	if err != nil {
		return nil, err
	}
	if !admin && b.StudentId != actorID && b.ProviderId != actorID {
		return nil, grpcx.NotFound("booking not found")
	}
	return b, nil
}

// roomOpen reports why the room can't be used right now, or nil.
func (s *server) roomOpen(b *marketplacev1.Booking) error {
	opens, closes := roomWindow(b)
	now := s.now()
	switch {
	case b.Mode != "online":
		return grpcx.Precondition("ROOM_IN_PERSON: this session is in person, so it has no video room")
	case b.Status != "accepted" && b.Status != "completed":
		return grpcx.Precondition("ROOM_NOT_ACCEPTED: the room opens once the session is accepted")
	case now.Before(opens):
		return grpcx.Precondition("ROOM_NOT_OPEN: the room opens %s before the start, at %s", humanDuration(roomOpensBefore),
			formatWhen(opens, s.bookingTimezone(context.Background(), b.ProviderProfileId)))
	case now.After(closes):
		return grpcx.Precondition("ROOM_CLOSED: the room closed %s after the session ended", humanDuration(roomClosesAfter))
	}
	return nil
}

func (s *server) OpenRoom(ctx context.Context, req *marketplacev1.RoomRequest) (*marketplacev1.RoomAccess, error) {
	b, err := s.partyBooking(ctx, req.BookingId, req.ActorId, false)
	if err != nil {
		return nil, err
	}
	if err := s.roomOpen(b); err != nil {
		return nil, err
	}
	if blocked, err := s.isBlocked(ctx, b.StudentId, b.ProviderId); err != nil {
		return nil, grpcx.Internal(err)
	} else if blocked {
		return nil, grpcx.Denied("you can't join a session with this person")
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO interview_rooms (booking_id) VALUES ($1) ON CONFLICT DO NOTHING`, b.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	opens, closes := roomWindow(b)
	role := "student"
	if b.ProviderId == req.ActorId {
		role = "provider"
	}
	return &marketplacev1.RoomAccess{Booking: b, Role: role, OpensAt: opens.Unix(), ClosesAt: closes.Unix()}, nil
}

func (s *server) GetRoom(ctx context.Context, req *marketplacev1.RoomRequest) (*marketplacev1.Room, error) {
	b, err := s.partyBooking(ctx, req.BookingId, req.ActorId, req.ActorIsAdmin)
	if err != nil {
		return nil, err
	}
	opens, closes := roomWindow(b)
	r := &marketplacev1.Room{BookingId: b.Id, Language: "python", Whiteboard: "[]", OpensAt: opens.Unix(), ClosesAt: closes.Unix()}
	var questionID *string
	var whiteboard, scores []byte
	err = s.db.QueryRow(ctx, `SELECT problem, question_id::text, language, code, code_state, whiteboard, draft_scores
		FROM interview_rooms WHERE booking_id = $1`, b.Id).Scan(&r.Problem, &questionID, &r.Language, &r.Code, &r.CodeState, &whiteboard, &scores)
	switch {
	case db.IsNoRows(err):
	case err != nil:
		return nil, grpcx.Internal(err)
	default:
		r.Exists = true
		if questionID != nil {
			r.QuestionId = *questionID
		}
		r.Whiteboard = string(whiteboard)
		// The mentor's in-room scores are their private draft.
		if req.ActorId == b.ProviderId {
			var m map[string]int32
			_ = json.Unmarshal(scores, &m)
			for _, c := range feedbackCriteria {
				if v, ok := m[c]; ok {
					r.DraftScores = append(r.DraftScores, &marketplacev1.Score{Criterion: c, Score: v})
				}
			}
		}
	}
	rows, err := s.db.Query(ctx, `SELECT user_id, joined_at, left_at FROM room_presence WHERE booking_id = $1 ORDER BY joined_at`, b.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		p := &marketplacev1.Presence{}
		var joined time.Time
		var left *time.Time
		if err := rows.Scan(&p.UserId, &joined, &left); err != nil {
			return nil, grpcx.Internal(err)
		}
		p.JoinedAt, p.LeftAt = joined.Unix(), unixOrZero(left)
		r.Presence = append(r.Presence, p)
	}
	return r, rows.Err()
}

const (
	maxCodeChars      = 100_000
	maxCodeState      = 1 << 20
	maxWhiteboardSize = 2 << 20
	maxProblemChars   = 10_000
)

func (s *server) SaveRoom(ctx context.Context, req *marketplacev1.SaveRoomRequest) (*marketplacev1.Empty, error) {
	b, err := s.partyBooking(ctx, req.BookingId, req.ActorId, false)
	if err != nil {
		return nil, err
	}
	if err := s.roomOpen(b); err != nil {
		return nil, err
	}
	isProvider := b.ProviderId == req.ActorId
	sets := []string{}
	args := []any{b.Id}
	set := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Code != nil {
		if len([]rune(*req.Code)) > maxCodeChars {
			return nil, grpcx.Invalid("the code is too long to save")
		}
		set("code", *req.Code)
	}
	if req.CodeState != nil {
		if len(req.CodeState) > maxCodeState {
			return nil, grpcx.Invalid("the editor state is too large to save")
		}
		set("code_state", req.CodeState)
	}
	if req.Language != nil {
		if !slices.Contains(codeLanguages, *req.Language) {
			return nil, grpcx.Invalid("unknown language %q", *req.Language)
		}
		set("language", *req.Language)
	}
	if req.Whiteboard != nil {
		if len(*req.Whiteboard) > maxWhiteboardSize {
			return nil, grpcx.Invalid("the whiteboard is too large to save")
		}
		var elements []json.RawMessage
		if err := json.Unmarshal([]byte(*req.Whiteboard), &elements); err != nil {
			return nil, grpcx.Invalid("the whiteboard must be a JSON array")
		}
		set("whiteboard", *req.Whiteboard)
	}
	if req.Problem != nil || req.QuestionId != nil || req.SetDraftScores {
		if !isProvider {
			return nil, grpcx.Denied("only the %s sets the problem and scores", b.Kind)
		}
	}
	if req.Problem != nil {
		if len([]rune(*req.Problem)) > maxProblemChars {
			return nil, grpcx.Invalid("the problem must be at most %d characters", maxProblemChars)
		}
		set("problem", strings.TrimSpace(*req.Problem))
	}
	if req.QuestionId != nil {
		var q any
		if *req.QuestionId != "" {
			var ok bool
			if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM questions WHERE id = $1::uuid)`, *req.QuestionId).Scan(&ok); err != nil || !ok {
				return nil, grpcx.Invalid("unknown question")
			}
			q = *req.QuestionId
		}
		set("question_id", q)
	}
	if req.SetDraftScores {
		m := map[string]int32{}
		for _, sc := range req.DraftScores {
			if !slices.Contains(feedbackCriteria, sc.Criterion) || sc.Score < 1 || sc.Score > 5 {
				return nil, grpcx.Invalid("scores must be 1–5 for a known criterion")
			}
			m[sc.Criterion] = sc.Score
		}
		raw, _ := json.Marshal(m)
		set("draft_scores", raw)
	}
	if len(sets) == 0 {
		return &marketplacev1.Empty{}, nil
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO interview_rooms (booking_id) VALUES ($1) ON CONFLICT DO NOTHING`, b.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE interview_rooms SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE booking_id = $1`, args...); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

// RecordPresence logs joins and leaves reported by the video server.
func (s *server) RecordPresence(ctx context.Context, req *marketplacev1.PresenceEvent) (*marketplacev1.Empty, error) {
	b, err := s.partyBooking(ctx, req.BookingId, req.UserId, false)
	if err != nil {
		return nil, err
	}
	at := time.Unix(req.At, 0)
	switch req.Event {
	case "joined":
		_, err = s.db.Exec(ctx, `INSERT INTO room_presence (booking_id, user_id, joined_at) VALUES ($1, $2, $3)`, b.Id, req.UserId, at)
	case "left":
		_, err = s.db.Exec(ctx, `UPDATE room_presence SET left_at = $3 WHERE id = (
			SELECT id FROM room_presence WHERE booking_id = $1 AND user_id = $2 AND left_at IS NULL ORDER BY joined_at DESC LIMIT 1)`,
			b.Id, req.UserId, at)
	default:
		return nil, grpcx.Invalid("event must be joined or left")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

// ── Mentor notes ────────────────────────────────────────────────────────────

const maxNotesPerRoom = 200

func scanNote(row pgx.Row) (*marketplacev1.MentorNote, error) {
	n := &marketplacev1.MentorNote{}
	var created time.Time
	if err := row.Scan(&n.Id, &n.BookingId, &n.AtSeconds, &n.Body, &n.Tone, &created); err != nil {
		return nil, err
	}
	n.CreatedAt = created.Unix()
	return n, nil
}

func (s *server) AddMentorNote(ctx context.Context, req *marketplacev1.AddMentorNoteRequest) (*marketplacev1.MentorNote, error) {
	body := strings.TrimSpace(req.Body)
	tone := req.Tone
	if tone == "" {
		tone = "note"
	}
	switch {
	case !textLen(body, 1, 500):
		return nil, grpcx.Invalid("a note must be 1–500 characters")
	case tone != "plus" && tone != "minus" && tone != "note":
		return nil, grpcx.Invalid("tone must be plus, minus or note")
	case req.AtSeconds < 0 || req.AtSeconds > 6*3600:
		return nil, grpcx.Invalid("the time is out of range")
	}
	b, err := s.partyBooking(ctx, req.BookingId, req.ActorId, false)
	if err != nil {
		return nil, err
	}
	if b.ProviderId != req.ActorId {
		return nil, grpcx.Denied("notes are for the %s", b.Kind)
	}
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM mentor_notes WHERE booking_id = $1`, b.Id).Scan(&count); err != nil {
		return nil, grpcx.Internal(err)
	}
	if count >= maxNotesPerRoom {
		return nil, grpcx.TooMany("at most %d notes per session", maxNotesPerRoom)
	}
	n, err := scanNote(s.db.QueryRow(ctx, `INSERT INTO mentor_notes (booking_id, author_id, at_seconds, body, tone)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, booking_id, at_seconds, body, tone, created_at`, b.Id, req.ActorId, req.AtSeconds, body, tone))
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return n, nil
}

func (s *server) ListMentorNotes(ctx context.Context, req *marketplacev1.RoomRequest) (*marketplacev1.MentorNotes, error) {
	b, err := s.partyBooking(ctx, req.BookingId, req.ActorId, req.ActorIsAdmin)
	if err != nil {
		return nil, err
	}
	out := &marketplacev1.MentorNotes{}
	if b.ProviderId != req.ActorId && !req.ActorIsAdmin {
		return out, nil // private to the mentor
	}
	rows, err := s.db.Query(ctx, `SELECT id, booking_id, at_seconds, body, tone, created_at FROM mentor_notes
		WHERE booking_id = $1 ORDER BY at_seconds, created_at`, b.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Notes = append(out.Notes, n)
	}
	return out, rows.Err()
}

func (s *server) DeleteMentorNote(ctx context.Context, req *marketplacev1.DeleteMentorNoteRequest) (*marketplacev1.Empty, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM mentor_notes WHERE id = $1::uuid AND author_id = $2`, req.Id, req.ActorId)
	if err != nil && !strings.Contains(err.Error(), "invalid input syntax") {
		return nil, grpcx.Internal(err)
	}
	if err != nil || tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("note not found")
	}
	return &marketplacev1.Empty{}, nil
}

// ── Question bank ───────────────────────────────────────────────────────────

const questionColumns = `id, author_id, title, body, interview_type, difficulty, companies, created_at, updated_at`

func scanQuestion(row pgx.Row) (*marketplacev1.Question, error) {
	q := &marketplacev1.Question{}
	var created, updated time.Time
	if err := row.Scan(&q.Id, &q.AuthorId, &q.Title, &q.Body, &q.InterviewType, &q.Difficulty, &q.Companies, &created, &updated); err != nil {
		return nil, err
	}
	q.CreatedAt, q.UpdatedAt = created.Unix(), updated.Unix()
	return q, nil
}

// requireMentor: the bank is for mentors only, so students can't read the
// questions they'll be asked.
func (s *server) requireMentor(ctx context.Context, userID string) error {
	var ok bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM provider_profiles WHERE user_id = $1 AND kind = 'mentor' AND status <> 'suspended')`,
		userID).Scan(&ok); err != nil {
		return grpcx.Internal(err)
	}
	if !ok {
		return grpcx.Denied("MENTOR_REQUIRED: the question bank is for career mentors")
	}
	return nil
}

var difficulties = []string{"easy", "medium", "hard"}

func (s *server) SaveQuestion(ctx context.Context, req *marketplacev1.SaveQuestionRequest) (*marketplacev1.Question, error) {
	if err := s.requireMentor(ctx, req.ActorId); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	switch {
	case !textLen(title, 3, 150):
		return nil, grpcx.Invalid("the title must be 3–150 characters")
	case !textLen(body, 10, maxProblemChars):
		return nil, grpcx.Invalid("the problem must be 10–%d characters", maxProblemChars)
	case !slices.Contains(interviewTypes, req.InterviewType):
		return nil, grpcx.Invalid("choose an interview type")
	case !slices.Contains(difficulties, req.Difficulty):
		return nil, grpcx.Invalid("difficulty must be easy, medium or hard")
	}
	companies, err := cleanList(req.Companies, 6, 60, "company")
	if err != nil {
		return nil, err
	}
	if companies == nil {
		companies = []string{}
	}
	var row pgx.Row
	if req.Id == "" {
		row = s.db.QueryRow(ctx, `INSERT INTO questions (author_id, title, body, interview_type, difficulty, companies)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+questionColumns, req.ActorId, title, body, req.InterviewType, req.Difficulty, companies)
	} else {
		row = s.db.QueryRow(ctx, `UPDATE questions SET title = $3, body = $4, interview_type = $5, difficulty = $6, companies = $7, updated_at = now()
			WHERE id = $1::uuid AND author_id = $2 RETURNING `+questionColumns, req.Id, req.ActorId, title, body, req.InterviewType, req.Difficulty, companies)
	}
	q, err := scanQuestion(row)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("question not found, or not yours")
		}
		return nil, grpcx.Internal(err)
	}
	return q, nil
}

func (s *server) ListQuestions(ctx context.Context, req *marketplacev1.ListQuestionsRequest) (*marketplacev1.ListQuestionsResponse, error) {
	if err := s.requireMentor(ctx, req.ActorId); err != nil {
		return nil, err
	}
	limit, offset := db.Page(req.Page, req.PageSize, 20, 100)
	where := []string{"true"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if req.InterviewType != "" {
		add("interview_type = $%d", req.InterviewType)
	}
	if req.Difficulty != "" {
		add("difficulty = $%d", req.Difficulty)
	}
	if c := strings.TrimSpace(req.Company); c != "" {
		add("EXISTS (SELECT 1 FROM unnest(companies) c WHERE lower(c) LIKE $%d)", "%"+strings.ToLower(c)+"%")
	}
	if q := strings.TrimSpace(req.Query); q != "" {
		add("(lower(title) LIKE $%[1]d OR lower(body) LIKE $%[1]d)", "%"+strings.ToLower(q)+"%")
	}
	if req.MineOnly {
		add("author_id = $%d", req.ActorId)
	}
	cond := strings.Join(where, " AND ")
	out := &marketplacev1.ListQuestionsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM questions WHERE `+cond, args...).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM questions WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		questionColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Questions = append(out.Questions, q)
	}
	return out, rows.Err()
}

func (s *server) GetQuestion(ctx context.Context, req *marketplacev1.GetQuestionRequest) (*marketplacev1.Question, error) {
	q, err := scanQuestion(s.db.QueryRow(ctx, `SELECT `+questionColumns+` FROM questions WHERE id = $1::uuid`, req.Id))
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("question not found")
		}
		return nil, grpcx.Internal(err)
	}
	if err := s.requireMentor(ctx, req.ActorId); err != nil {
		return nil, err
	}
	return q, nil
}

func (s *server) DeleteQuestion(ctx context.Context, req *marketplacev1.GetQuestionRequest) (*marketplacev1.Empty, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM questions WHERE id = $1::uuid AND author_id = $2`, req.Id, req.ActorId)
	if err != nil && !strings.Contains(err.Error(), "invalid input syntax") {
		return nil, grpcx.Internal(err)
	}
	if err != nil || tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("question not found, or not yours")
	}
	return &marketplacev1.Empty{}, nil
}

// ── Hub: next slot, reply speed, available soon ─────────────────────────────

// nextSlot returns the provider's first open hour-long (or shortest allowed)
// slot before `until`, or zero.
func (s *server) nextSlot(ctx context.Context, userID, timezone string, until time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, nil
	}
	from := s.now().Add(leadTime)
	if !until.After(from) {
		return time.Time{}, nil
	}
	ws, err := s.loadWindows(ctx, userID)
	if err != nil || len(ws) == 0 {
		return time.Time{}, err
	}
	busy, err := s.busyIntervals(ctx, s.db, []string{userID}, from, until)
	if err != nil {
		return time.Time{}, err
	}
	dur := time.Duration(slices.Min(sessionDurations)) * time.Minute
	if slices.Contains(sessionDurations, 60) {
		dur = time.Hour
	}
	slots := computeSlots(ws, loc, from, until, dur, busy)
	if len(slots) == 0 {
		return time.Time{}, nil
	}
	return slots[0].Start, nil
}

func (s *server) GetProviderInsights(ctx context.Context, req *marketplacev1.ProviderInsightsRequest) (*marketplacev1.ProviderInsights, error) {
	out := &marketplacev1.ProviderInsights{}
	if len(req.ProfileIds) == 0 {
		return out, nil
	}
	if len(req.ProfileIds) > 60 {
		return nil, grpcx.Invalid("at most 60 profiles at a time")
	}
	days := req.WithinDays
	if days <= 0 || days > int32(maxWin) {
		days = 7
	}
	rows, err := s.db.Query(ctx, `SELECT p.id, p.user_id, p.timezone, p.status,
			-- Median minutes to answer a session or resume request over the last 90 days, once there are 3 answers.
			(SELECT CASE WHEN count(*) >= 3 THEN percentile_cont(0.5) WITHIN GROUP (ORDER BY m) END FROM (
				SELECT extract(epoch FROM b.answered_at - b.created_at) / 60 AS m FROM bookings b
				WHERE b.provider_profile_id = p.id AND b.answered_at IS NOT NULL AND b.created_at > now() - interval '90 days'
				UNION ALL
				SELECT extract(epoch FROM coalesce(r.accepted_at, r.closed_at) - r.created_at) / 60 FROM resume_reviews r
				WHERE r.provider_profile_id = p.id AND r.status IN ('accepted', 'delivered', 'declined')
				  AND coalesce(r.accepted_at, r.closed_at) IS NOT NULL AND r.created_at > now() - interval '90 days') answers)
		FROM provider_profiles p WHERE p.id = ANY($1::uuid[])`, req.ProfileIds)
	if err != nil {
		if strings.Contains(err.Error(), "invalid input syntax") {
			return out, nil
		}
		return nil, grpcx.Internal(err)
	}
	type row struct {
		id, user, tz, status string
		reply                *float64
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.user, &r.tz, &r.status, &r.reply); err != nil {
			rows.Close()
			return nil, grpcx.Internal(err)
		}
		list = append(list, r)
	}
	rows.Close()
	until := s.now().Add(time.Duration(days) * 24 * time.Hour)
	for _, r := range list {
		in := &marketplacev1.ProviderInsight{ProfileId: r.id}
		if r.reply != nil {
			in.ReplyMinutes = int32(*r.reply + 0.5)
			if in.ReplyMinutes == 0 {
				in.ReplyMinutes = 1
			}
		}
		if r.status == "active" {
			next, err := s.nextSlot(ctx, r.user, r.tz, until)
			if err != nil {
				return nil, err
			}
			if !next.IsZero() {
				in.NextSlotAt = next.Unix()
			}
		}
		out.Insights = append(out.Insights, in)
	}
	return out, nil
}

func (s *server) AvailableSoon(ctx context.Context, req *marketplacev1.AvailableSoonRequest) (*marketplacev1.AvailableSoonResponse, error) {
	hours := req.WithinHours
	if hours <= 0 || hours > 72 {
		hours = 12
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 12 {
		limit = 6
	}
	// Look at the best-rated candidates only: computing slots costs a couple
	// of queries each.
	search, err := s.SearchProviders(ctx, &marketplacev1.SearchProvidersRequest{
		Kind: req.Kind, CampusId: req.CampusId, ViewerId: req.ViewerId, Company: req.Company, PageSize: 50, Mode: "online"})
	if err != nil {
		return nil, err
	}
	if req.Kind == "mentor" {
		// Resume-only mentors have no interview slots.
		search.Profiles = slices.DeleteFunc(search.Profiles, func(p *marketplacev1.ProviderProfile) bool { return len(p.Services) == 0 })
	}
	until := s.now().Add(time.Duration(hours) * time.Hour)
	out := &marketplacev1.AvailableSoonResponse{}
	for _, p := range search.Profiles {
		if p.UserId == req.ViewerId {
			continue
		}
		var tz string
		if err := s.db.QueryRow(ctx, `SELECT timezone FROM provider_profiles WHERE id = $1`, p.Id).Scan(&tz); err != nil {
			continue
		}
		next, err := s.nextSlot(ctx, p.UserId, tz, until)
		if err != nil {
			return nil, err
		}
		if !next.IsZero() {
			out.Slots = append(out.Slots, &marketplacev1.ProviderSlot{Profile: p, StartAt: next.Unix()})
		}
	}
	sort.SliceStable(out.Slots, func(i, j int) bool { return out.Slots[i].StartAt < out.Slots[j].StartAt })
	if len(out.Slots) > limit {
		out.Slots = out.Slots[:limit]
	}
	return out, nil
}

func (s *server) MentorCompanies(ctx context.Context, _ *marketplacev1.Empty) (*marketplacev1.CompanyCounts, error) {
	// Group spellings case-insensitively and show the most common one.
	rows, err := s.db.Query(ctx, `SELECT mode() WITHIN GROUP (ORDER BY c), count(DISTINCT p.id)
		FROM provider_profiles p, unnest(p.companies) c
		WHERE p.kind = 'mentor' AND p.status = 'active'
		GROUP BY lower(c) ORDER BY count(DISTINCT p.id) DESC, 1 LIMIT 24`)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &marketplacev1.CompanyCounts{}
	for rows.Next() {
		c := &marketplacev1.CompanyCount{}
		if err := rows.Scan(&c.Name, &c.Mentors); err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Companies = append(out.Companies, c)
	}
	return out, rows.Err()
}

// ── Progress ────────────────────────────────────────────────────────────────

// Which interview type practises a weak criterion.
var practiceFor = map[string]string{
	"communication":   "hr",
	"confidence":      "hr",
	"problem_solving": "coding",
	"technical_depth": "coding",
	"structure":       "system_design",
}

func (s *server) GetProgress(ctx context.Context, req *marketplacev1.UserRequest) (*marketplacev1.Progress, error) {
	out := &marketplacev1.Progress{Readiness: -1}
	rows, err := s.db.Query(ctx, `SELECT b.id, b.service, b.start_at, f.verdict, f.scores, b.provider_profile_id
		FROM bookings b JOIN interview_feedback f ON f.booking_id = b.id
		WHERE b.student_id = $1 AND b.kind = 'mentor' ORDER BY b.start_at`, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	latest := map[string]int32{}
	for rows.Next() {
		in := &marketplacev1.ProgressInterview{}
		var at time.Time
		var raw []byte
		if err := rows.Scan(&in.BookingId, &in.InterviewType, &at, &in.Verdict, &raw, &in.ProviderProfileId); err != nil {
			rows.Close()
			return nil, grpcx.Internal(err)
		}
		in.At = at.Unix()
		var m map[string]int32
		_ = json.Unmarshal(raw, &m)
		for _, c := range feedbackCriteria {
			if v, ok := m[c]; ok {
				in.Scores = append(in.Scores, &marketplacev1.Score{Criterion: c, Score: v})
				latest[c] = v
			}
		}
		out.Interviews = append(out.Interviews, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}

	rows, err = s.db.Query(ctx, `SELECT id, delivered_at, overall_score, target_role FROM resume_reviews
		WHERE student_id = $1 AND status = 'delivered' ORDER BY delivered_at`, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	for rows.Next() {
		r := &marketplacev1.ProgressResume{}
		var at time.Time
		if err := rows.Scan(&r.Id, &at, &r.Score, &r.TargetRole); err != nil {
			rows.Close()
			return nil, grpcx.Internal(err)
		}
		r.At = at.Unix()
		out.Resumes = append(out.Resumes, r)
	}
	rows.Close()

	if len(latest) == 0 {
		return out, nil
	}
	// Readiness: each skill's most recent score, averaged, as a percentage.
	// A resume score, when there is one, counts for 15%.
	var sum float64
	weakest, low := "", int32(6)
	for _, c := range feedbackCriteria {
		if v, ok := latest[c]; ok {
			sum += float64(v)
			if v < low {
				weakest, low = c, v
			}
		}
	}
	readiness := sum / float64(len(latest)) / 5
	if n := len(out.Resumes); n > 0 {
		readiness = 0.85*readiness + 0.15*float64(out.Resumes[n-1].Score)/10
	}
	out.Readiness = int32(readiness*100 + 0.5)
	out.WeakestCriterion = weakest
	out.SuggestedInterviewType = practiceFor[weakest]
	return out, nil
}
