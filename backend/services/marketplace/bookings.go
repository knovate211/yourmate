package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

const bookingColumns = `b.id, b.provider_profile_id, b.provider_id, b.student_id, b.kind, b.campus_id, coalesce(b.subject_id::text, ''),
	b.service, b.start_at, b.end_at, b.mode, b.location, b.meeting_link, b.price_minor, b.currency, b.notes, b.status,
	b.status_reason, coalesce(b.cancelled_by::text, ''), EXISTS (SELECT 1 FROM reviews r WHERE r.booking_id = b.id),
	b.created_at, b.updated_at, EXISTS (SELECT 1 FROM interview_feedback f WHERE f.booking_id = b.id)`

func scanBooking(row pgx.Row) (*marketplacev1.Booking, error) {
	b := &marketplacev1.Booking{}
	var start, end, created, updated time.Time
	if err := row.Scan(&b.Id, &b.ProviderProfileId, &b.ProviderId, &b.StudentId, &b.Kind, &b.CampusId, &b.SubjectId, &b.Service,
		&start, &end, &b.Mode, &b.Location, &b.MeetingLink, &b.PriceMinor, &b.Currency, &b.Notes, &b.Status, &b.StatusReason,
		&b.CancelledBy, &b.Reviewed, &created, &updated, &b.HasFeedback); err != nil {
		return nil, err
	}
	b.StartAt, b.EndAt, b.CreatedAt, b.UpdatedAt = start.Unix(), end.Unix(), created.Unix(), updated.Unix()
	return b, nil
}

func (s *server) bookingByID(ctx context.Context, q pgx.Row) (*marketplacev1.Booking, error) {
	b, err := scanBooking(q)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("booking not found")
		}
		return nil, grpcx.Internal(err)
	}
	return b, nil
}

func (s *server) isBlocked(ctx context.Context, a, b string) (bool, error) {
	var blocked bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM blocks WHERE (user_id = $1 AND blocked_id = $2) OR (user_id = $2 AND blocked_id = $1))`, a, b).Scan(&blocked)
	return blocked, err
}

func (s *server) CreateBooking(ctx context.Context, req *marketplacev1.CreateBookingRequest) (*marketplacev1.Booking, error) {
	if !allowedDurations[req.DurationMinutes] {
		return nil, grpcx.Invalid("duration must be %s", durationsText())
	}
	notes := strings.TrimSpace(req.Notes)
	location := strings.TrimSpace(req.Location)
	if len(notes) > 1000 || len(location) > 200 {
		return nil, grpcx.Invalid("notes or location are too long")
	}

	student, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: req.ActorId})
	if err != nil {
		return nil, err
	}
	switch {
	case student.Status != "active":
		return nil, grpcx.Denied("your account is suspended")
	case !campusVerified(student):
		return nil, grpcx.Precondition("VERIFICATION_REQUIRED: verify your campus email or student ID before booking")
	}

	var p struct {
		userID, kind, campusID, timezone, currency, status string
		rate                                               int64
		modes, services                                    []string
	}
	err = s.db.QueryRow(ctx, `SELECT user_id, kind, campus_id, timezone, currency, status, rate_minor, modes, services
		FROM provider_profiles WHERE id = $1`, req.ProviderProfileId).Scan(
		&p.userID, &p.kind, &p.campusID, &p.timezone, &p.currency, &p.status, &p.rate, &p.modes, &p.services)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("profile not found")
		}
		return nil, grpcx.Internal(err)
	}
	switch {
	case p.status != "active":
		return nil, grpcx.Precondition("this profile isn't taking bookings right now")
	case p.userID == req.ActorId:
		return nil, grpcx.Invalid("you can't book yourself")
	case !slices.Contains(p.modes, req.Mode):
		return nil, grpcx.Invalid("this %s doesn't offer %s sessions", p.kind, strings.ReplaceAll(req.Mode, "_", "-"))
	}
	if blocked, err := s.isBlocked(ctx, req.ActorId, p.userID); err != nil {
		return nil, grpcx.Internal(err)
	} else if blocked {
		return nil, grpcx.Denied("you can't book this person")
	}

	var subjectID any
	service := ""
	if p.kind == "tutor" {
		var ok bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM provider_subjects WHERE profile_id = $1 AND subject_id = $2::uuid)`,
			req.ProviderProfileId, req.SubjectId).Scan(&ok); err != nil || !ok {
			return nil, grpcx.Invalid("choose one of the subjects this tutor teaches")
		}
		subjectID = req.SubjectId
	} else {
		if p.kind == "mentor" && len(p.services) == 0 {
			return nil, grpcx.Precondition("this mentor only reviews resumes")
		}
		if !slices.Contains(p.services, req.Service) {
			if p.kind == "mentor" {
				return nil, grpcx.Invalid("choose one of the interview types this mentor runs")
			}
			return nil, grpcx.Invalid("choose one of the services this guide offers")
		}
		service = req.Service
	}

	loc, err := time.LoadLocation(p.timezone)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	dur := time.Duration(req.DurationMinutes) * time.Minute
	start := time.Unix(req.StartAt, 0)
	if start.Before(s.now().Add(leadTime)) {
		return nil, grpcx.Invalid("sessions must be booked at least %s ahead", humanDuration(leadTime))
	}
	if start.After(s.now().Add(time.Duration(maxWin) * 24 * time.Hour)) {
		return nil, grpcx.Invalid("sessions can be booked up to %d days ahead", maxWin)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Serialise bookings per provider so two students can't take the same slot.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, p.userID); err != nil {
		return nil, grpcx.Internal(err)
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM bookings WHERE student_id = $1 AND status = 'requested'`, req.ActorId).Scan(&open); err != nil {
		return nil, grpcx.Internal(err)
	}
	if open >= maxOpenRequests {
		return nil, grpcx.TooMany("you have %d requests waiting for a reply; cancel one or wait for answers", open)
	}
	ws, err := s.loadWindows(ctx, p.userID)
	if err != nil {
		return nil, err
	}
	busy, err := s.busyIntervals(ctx, tx, []string{p.userID, req.ActorId}, start, start.Add(dur))
	if err != nil {
		return nil, err
	}
	slots := computeSlots(ws, loc, start, start.Add(dur), dur, busy)
	if len(slots) == 0 || !slots[0].Start.Equal(start) {
		return nil, grpcx.Precondition("SLOT_UNAVAILABLE: that time is no longer available; pick another slot")
	}

	price := p.rate * int64(req.DurationMinutes) / 60
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO bookings (provider_profile_id, provider_id, student_id, kind, campus_id, subject_id, service,
			start_at, end_at, mode, location, price_minor, currency, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		req.ProviderProfileId, p.userID, req.ActorId, p.kind, p.campusID, subjectID, service, start, start.Add(dur),
		req.Mode, location, price, p.currency, notes).Scan(&id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}

	b, err := s.bookingByID(ctx, s.db.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings b WHERE b.id = $1`, id))
	if err != nil {
		return nil, err
	}
	s.notify(ctx, p.userID, "booking_requested", fmt.Sprintf("New session request from %s", displayName(student)),
		fmt.Sprintf("%s requested a %d-minute %s session on %s. Accept or decline it soon; requests expire when the start time passes.",
			displayName(student), req.DurationMinutes, strings.ReplaceAll(req.Mode, "_", "-"), formatWhen(start, loc)),
		"/bookings/"+id, true)
	return b, nil
}

func (s *server) GetBooking(ctx context.Context, req *marketplacev1.GetBookingRequest) (*marketplacev1.Booking, error) {
	b, err := s.bookingByID(ctx, s.db.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings b WHERE b.id = $1`, req.Id))
	if err != nil {
		return nil, err
	}
	if !req.ActorIsAdmin && b.StudentId != req.ActorId && b.ProviderId != req.ActorId {
		return nil, grpcx.NotFound("booking not found")
	}
	return b, nil
}

func (s *server) ListBookings(ctx context.Context, req *marketplacev1.ListBookingsRequest) (*marketplacev1.ListBookingsResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 20, 100)
	var where []string
	var args []any
	switch {
	case req.AdminAll:
		where = append(where, "true")
	case req.As == "student":
		args = append(args, req.ActorId)
		where = append(where, "b.student_id = $1")
	case req.As == "provider":
		args = append(args, req.ActorId)
		where = append(where, "b.provider_id = $1")
	default:
		args = append(args, req.ActorId)
		where = append(where, "(b.student_id = $1 OR b.provider_id = $1)")
	}
	if len(req.Statuses) > 0 {
		args = append(args, req.Statuses)
		where = append(where, fmt.Sprintf("b.status = ANY($%d)", len(args)))
	}
	order := "b.start_at DESC"
	if req.UpcomingOnly {
		args = append(args, s.now())
		where = append(where, fmt.Sprintf("b.end_at > $%d", len(args)))
		order = "b.start_at ASC"
	}
	cond := strings.Join(where, " AND ")
	out := &marketplacev1.ListBookingsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM bookings b WHERE `+cond, args...).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM bookings b WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		bookingColumns, cond, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Bookings = append(out.Bookings, b)
	}
	return out, rows.Err()
}

func validMeetingLink(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && len(link) <= 500
}

func (s *server) TransitionBooking(ctx context.Context, req *marketplacev1.TransitionBookingRequest) (*marketplacev1.Booking, error) {
	reason := strings.TrimSpace(req.Reason)
	link := strings.TrimSpace(req.MeetingLink)
	location := strings.TrimSpace(req.Location)
	if len(reason) > 500 || len(location) > 200 {
		return nil, grpcx.Invalid("reason or location is too long")
	}
	if link != "" && !validMeetingLink(link) {
		return nil, grpcx.Invalid("meeting link must be a full http(s) URL")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	b, err := s.bookingByID(ctx, tx.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings b WHERE b.id = $1 FOR UPDATE`, req.Id))
	if err != nil {
		return nil, err
	}
	isProvider, isStudent := b.ProviderId == req.ActorId, b.StudentId == req.ActorId
	if !isProvider && !isStudent {
		return nil, grpcx.NotFound("booking not found")
	}
	now := s.now()
	start := time.Unix(b.StartAt, 0)

	var newStatus string
	switch req.Action {
	case "accept":
		switch {
		case !isProvider:
			return nil, grpcx.Denied("only the %s can accept", b.Kind)
		case b.Status != "requested":
			return nil, grpcx.Precondition("only requested sessions can be accepted")
		case !start.After(now):
			return nil, grpcx.Precondition("this session's start time has passed")
		}
		newStatus = "accepted"
	case "decline":
		if !isProvider {
			return nil, grpcx.Denied("only the %s can decline", b.Kind)
		}
		if b.Status != "requested" {
			return nil, grpcx.Precondition("only requested sessions can be declined")
		}
		newStatus = "declined"
	case "cancel":
		if b.Status != "requested" && b.Status != "accepted" {
			return nil, grpcx.Precondition("this session can no longer be cancelled")
		}
		if !start.After(now) {
			return nil, grpcx.Precondition("the session has already started; mark it complete or report a problem")
		}
		if b.Status == "accepted" && reason == "" {
			return nil, grpcx.Invalid("tell the other person why you're cancelling")
		}
		newStatus = "cancelled"
	case "complete":
		switch {
		case !isProvider:
			return nil, grpcx.Denied("only the %s can mark a session complete", b.Kind)
		case b.Status != "accepted":
			return nil, grpcx.Precondition("only accepted sessions can be completed")
		case now.Before(start):
			return nil, grpcx.Precondition("you can mark the session complete once it has started")
		}
		newStatus = "completed"
	default:
		return nil, grpcx.Invalid("unknown action %q", req.Action)
	}

	var cancelledBy any
	if newStatus == "cancelled" {
		cancelledBy = req.ActorId
	}
	if link == "" {
		link = b.MeetingLink
	}
	if location == "" {
		location = b.Location
	}
	_, err = tx.Exec(ctx, `UPDATE bookings SET status = $2, status_reason = $3, cancelled_by = $4, meeting_link = $5, location = $6, updated_at = now()
		WHERE id = $1`, b.Id, newStatus, reason, cancelledBy, link, location)
	if db.IsExclusionViolation(err) {
		return nil, grpcx.Precondition("SLOT_CONFLICT: one of you already has an accepted session at this time")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}

	var declinedIDs []string
	if newStatus == "accepted" {
		// The slot is taken now; politely release the other requests for it.
		rows, err := tx.Query(ctx, `UPDATE bookings SET status = 'declined', status_reason = 'The provider booked this time with someone else.', updated_at = now()
			WHERE provider_id = $1 AND id <> $2 AND status = 'requested' AND tstzrange(start_at, end_at) && tstzrange($3, $4)
			RETURNING student_id`, b.ProviderId, b.Id, start, time.Unix(b.EndAt, 0))
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		for rows.Next() {
			var sid string
			if err := rows.Scan(&sid); err != nil {
				rows.Close()
				return nil, grpcx.Internal(err)
			}
			declinedIDs = append(declinedIDs, sid)
		}
		rows.Close()
	}
	if newStatus == "completed" {
		if _, err := tx.Exec(ctx, `UPDATE provider_profiles SET sessions_completed = sessions_completed + 1 WHERE id = $1`, b.ProviderProfileId); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}

	s.notifyTransition(ctx, b, newStatus, req.ActorId, reason)
	for _, sid := range declinedIDs {
		s.notify(ctx, sid, "booking_declined", "Your session request was declined",
			"The time you asked for was booked by someone else. Pick another slot on their profile.", "/bookings", true)
	}
	return s.bookingByID(ctx, s.db.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings b WHERE b.id = $1`, b.Id))
}

func (s *server) bookingTimezone(ctx context.Context, profileID string) *time.Location {
	var tz string
	if err := s.db.QueryRow(ctx, `SELECT timezone FROM provider_profiles WHERE id = $1`, profileID).Scan(&tz); err == nil {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

func (s *server) notifyTransition(ctx context.Context, b *marketplacev1.Booking, status, actorID, reason string) {
	when := formatWhen(time.Unix(b.StartAt, 0), s.bookingTimezone(ctx, b.ProviderProfileId))
	link := "/bookings/" + b.Id
	reasonText := ""
	if reason != "" {
		reasonText = " Reason: " + reason
	}
	switch status {
	case "accepted":
		s.notify(ctx, b.StudentId, "booking_accepted", "Your session is confirmed",
			fmt.Sprintf("Your session on %s was accepted. Contact details and the meeting info are on the booking page. Pay the agreed price (%s) directly as arranged.",
				when, formatMoney(b.PriceMinor, b.Currency)), link, true)
	case "declined":
		s.notify(ctx, b.StudentId, "booking_declined", "Your session request was declined",
			fmt.Sprintf("Your request for %s was declined.%s Try another time or another %s.", when, reasonText, b.Kind), link, true)
	case "cancelled":
		other := b.StudentId
		if actorID == b.StudentId {
			other = b.ProviderId
		}
		s.notify(ctx, other, "booking_cancelled", "A session was cancelled",
			fmt.Sprintf("The session on %s was cancelled.%s", when, reasonText), link, true)
	case "completed":
		s.notify(ctx, b.StudentId, "booking_completed", "How was your session?",
			fmt.Sprintf("Your session on %s is complete. Leave a review to help other students.", when), link, true)
	}
}

// ── Scheduler ───────────────────────────────────────────────────────────────

// runScheduler expires stale requests, sends reminders and auto-completes
// sessions whose provider never marked them done.
func (s *server) runScheduler(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *server) tick(ctx context.Context) {
	now := s.now()
	// Expire requests nobody answered before the start time.
	if rows, err := s.db.Query(ctx, `UPDATE bookings b SET status = 'expired', updated_at = now()
		WHERE status = 'requested' AND start_at <= $1 RETURNING `+bookingColumns, now); err == nil {
		expired := collectBookings(rows)
		for _, b := range expired {
			s.notify(ctx, b.StudentId, "booking_expired", "Your session request expired",
				"The provider didn't answer before the start time. Try another slot or another provider.", "/bookings/"+b.Id, false)
		}
	} else {
		slog.Error("expire bookings", "err", err)
	}

	// Remind both sides an hour before.
	if rows, err := s.db.Query(ctx, `UPDATE bookings b SET reminder_sent = true
		WHERE status = 'accepted' AND NOT reminder_sent AND start_at > $1 AND start_at <= $2 RETURNING `+bookingColumns,
		now, now.Add(time.Hour)); err == nil {
		for _, b := range collectBookings(rows) {
			when := formatWhen(time.Unix(b.StartAt, 0), s.bookingTimezone(ctx, b.ProviderProfileId))
			for _, uid := range []string{b.StudentId, b.ProviderId} {
				s.notify(ctx, uid, "booking_reminder", "Your session starts soon", "Reminder: your session starts "+when+".", "/bookings/"+b.Id, true)
			}
		}
	} else {
		slog.Error("remind bookings", "err", err)
	}

	// Auto-complete sessions long past their end.
	if rows, err := s.db.Query(ctx, `UPDATE bookings b SET status = 'completed', status_reason = 'Completed automatically', updated_at = now()
		WHERE status = 'accepted' AND end_at <= $1 RETURNING `+bookingColumns, now.Add(-autoCompleteWait)); err == nil {
		for _, b := range collectBookings(rows) {
			if _, err := s.db.Exec(ctx, `UPDATE provider_profiles SET sessions_completed = sessions_completed + 1 WHERE id = $1`, b.ProviderProfileId); err != nil {
				slog.Error("count completed session", "err", err)
			}
			s.notify(ctx, b.StudentId, "booking_completed", "How was your session?", "Leave a review to help other students.", "/bookings/"+b.Id, false)
		}
	} else {
		slog.Error("auto-complete bookings", "err", err)
	}

	s.expireResumeRequests(ctx, now)
}

func collectBookings(rows pgx.Rows) []*marketplacev1.Booking {
	defer rows.Close()
	var out []*marketplacev1.Booking
	for rows.Next() {
		if b, err := scanBooking(rows); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// ── Formatting ──────────────────────────────────────────────────────────────

func formatWhen(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 2 Jan 2006, 15:04 MST")
}

// Currencies whose minor unit is the major unit.
var zeroDecimal = map[string]bool{"JPY": true, "KRW": true, "VND": true, "CLP": true, "ISK": true, "UGX": true, "XAF": true, "XOF": true}

func formatMoney(minor int64, currency string) string {
	if minor == 0 {
		return "free"
	}
	if zeroDecimal[currency] {
		return fmt.Sprintf("%s %d", currency, minor)
	}
	return fmt.Sprintf("%s %d.%02d", currency, minor/100, minor%100)
}
