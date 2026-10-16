package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	marketplacev1 "github.com/yourmate/backend/gen/marketplace/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

var (
	errTooManyWindows     = grpcx.Invalid("at most 70 availability windows")
	errBadWindow          = grpcx.Invalid("each window needs a weekday 0–6 and a start before its end")
	errUnalignedWindow    = grpcx.Invalid("availability must start and end on the hour or half hour")
	errOverlappingWindows = grpcx.Invalid("availability windows on the same day overlap")
)

const maxRateMinor = 100_000_000

// displayName shows first name and last initial, never the full surname.
func displayName(u *identityv1.User) string {
	name := strings.TrimSpace(u.FirstName)
	if last := strings.TrimSpace(u.LastName); last != "" {
		name += " " + strings.ToUpper(string([]rune(last)[0])) + "."
	}
	return name
}

func campusVerified(u *identityv1.User) bool { return u.CampusEmailVerified || u.IdVerified }

const profileColumns = `p.id, p.user_id, p.kind, p.campus_id, p.display_name, p.avatar_key, p.headline, p.bio, p.rate_minor,
	p.currency, p.modes, p.languages, p.services,
	coalesce((SELECT array_agg(ps.subject_id::text ORDER BY ps.subject_id) FROM provider_subjects ps WHERE ps.profile_id = p.id), '{}'),
	p.status, p.rating_avg, p.rating_count, p.sessions_completed, p.created_at, p.updated_at,
	p.companies, p.resume_reviews, p.resume_price_minor, p.resume_turnaround_days, p.resume_reviews_delivered`

func scanProfile(row pgx.Row) (*marketplacev1.ProviderProfile, error) {
	p := &marketplacev1.ProviderProfile{}
	var created, updated time.Time
	var resume bool
	var resumePrice int64
	var turnaround int16
	if err := row.Scan(&p.Id, &p.UserId, &p.Kind, &p.CampusId, &p.DisplayName, &p.AvatarKey, &p.Headline, &p.Bio,
		&p.RateMinor, &p.Currency, &p.Modes, &p.Languages, &p.Services, &p.SubjectIds, &p.Status, &p.RatingAvg,
		&p.RatingCount, &p.SessionsCompleted, &created, &updated,
		&p.Companies, &resume, &resumePrice, &turnaround, &p.ResumeReviewsDelivered); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = created.Unix(), updated.Unix()
	if resume {
		p.ResumeOffer = &marketplacev1.ResumeOffer{PriceMinor: resumePrice, TurnaroundDays: int32(turnaround)}
	}
	return p, nil
}

func (s *server) loadProfile(ctx context.Context, q pgx.Row) (*marketplacev1.ProviderProfile, error) {
	p, err := scanProfile(q)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("profile not found")
		}
		return nil, grpcx.Internal(err)
	}
	return p, nil
}

func (s *server) profileByID(ctx context.Context, id string) (*marketplacev1.ProviderProfile, error) {
	return s.loadProfile(ctx, s.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM provider_profiles p WHERE p.id = $1`, id))
}

// ── Subjects ────────────────────────────────────────────────────────────────

func (s *server) querySubjects(ctx context.Context, sql string, args ...any) ([]*marketplacev1.Subject, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	var out []*marketplacev1.Subject
	for rows.Next() {
		sub := &marketplacev1.Subject{}
		if err := rows.Scan(&sub.Id, &sub.CampusId, &sub.Code, &sub.Name, &sub.Category); err != nil {
			return nil, grpcx.Internal(err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

const subjectColumns = `id, coalesce(campus_id::text, ''), code, name, category`

func (s *server) ListSubjects(ctx context.Context, req *marketplacev1.ListSubjectsRequest) (*marketplacev1.ListSubjectsResponse, error) {
	limit := req.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var campus any
	if req.CampusId != "" {
		campus = req.CampusId
	}
	q := "%" + strings.ToLower(strings.TrimSpace(req.Query)) + "%"
	subs, err := s.querySubjects(ctx, `SELECT `+subjectColumns+` FROM subjects
		WHERE (campus_id IS NULL OR campus_id = $1::uuid) AND (lower(name) LIKE $2 OR lower(code) LIKE $2)
		ORDER BY campus_id NULLS LAST, category, name LIMIT $3`, campus, q, limit)
	if err != nil {
		return nil, err
	}
	return &marketplacev1.ListSubjectsResponse{Subjects: subs}, nil
}

func (s *server) GetSubjects(ctx context.Context, req *marketplacev1.GetSubjectsRequest) (*marketplacev1.ListSubjectsResponse, error) {
	if len(req.Ids) == 0 {
		return &marketplacev1.ListSubjectsResponse{}, nil
	}
	subs, err := s.querySubjects(ctx, `SELECT `+subjectColumns+` FROM subjects WHERE id = ANY($1::uuid[])`, req.Ids)
	if err != nil {
		return nil, err
	}
	return &marketplacev1.ListSubjectsResponse{Subjects: subs}, nil
}

func (s *server) CreateSubject(ctx context.Context, req *marketplacev1.CreateSubjectRequest) (*marketplacev1.Subject, error) {
	name := strings.TrimSpace(req.Name)
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "Other"
	}
	if len(name) < 2 || len(name) > 100 || len(code) > 20 || len(category) > 40 {
		return nil, grpcx.Invalid("subject name must be 2–100 characters; code at most 20")
	}
	var campus any
	if req.CampusId != "" {
		campus = req.CampusId
	}
	sub := &marketplacev1.Subject{}
	err := s.db.QueryRow(ctx, `INSERT INTO subjects (campus_id, code, name, category) VALUES ($1, $2, $3, $4) RETURNING `+subjectColumns,
		campus, code, name, category).Scan(&sub.Id, &sub.CampusId, &sub.Code, &sub.Name, &sub.Category)
	if db.IsUniqueViolation(err, "") {
		subs, qerr := s.querySubjects(ctx, `SELECT `+subjectColumns+` FROM subjects
			WHERE coalesce(campus_id::text, '') = $1 AND lower(name) = lower($2)`, req.CampusId, name)
		if qerr == nil && len(subs) == 1 {
			return subs[0], nil // already exists: hand back the existing one
		}
		return nil, grpcx.Exists("that subject already exists")
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return sub, nil
}

// ── Provider profiles ───────────────────────────────────────────────────────

func cleanList(in []string, maxItems, maxLen int, what string) ([]string, error) {
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || slices.Contains(out, v) {
			continue
		}
		if len(v) > maxLen {
			return nil, grpcx.Invalid("each %s must be at most %d characters", what, maxLen)
		}
		out = append(out, v)
	}
	if len(out) > maxItems {
		return nil, grpcx.Invalid("at most %d %ss", maxItems, what)
	}
	return out, nil
}

func (s *server) SaveProviderProfile(ctx context.Context, req *marketplacev1.SaveProviderProfileRequest) (*marketplacev1.ProviderProfile, error) {
	if req.Kind != "tutor" && req.Kind != "guide" && req.Kind != "mentor" {
		return nil, grpcx.Invalid("kind must be tutor, guide or mentor")
	}
	headline := strings.TrimSpace(req.Headline)
	bio := strings.TrimSpace(req.Bio)
	switch {
	case len(headline) < 10 || len(headline) > 120:
		return nil, grpcx.Invalid("headline must be 10–120 characters")
	case len(bio) < 30 || len(bio) > 2000:
		return nil, grpcx.Invalid("bio must be 30–2000 characters")
	case req.RateMinor < 0 || req.RateMinor > maxRateMinor:
		return nil, grpcx.Invalid("hourly rate is out of range")
	}
	modes, err := cleanList(req.Modes, 2, 20, "mode")
	if err != nil {
		return nil, err
	}
	if len(modes) == 0 {
		return nil, grpcx.Invalid("choose online, in person, or both")
	}
	for _, m := range modes {
		if !slices.Contains(validModes, m) {
			return nil, grpcx.Invalid("unknown mode %q", m)
		}
	}
	languages, err := cleanList(req.Languages, 8, 30, "language")
	if err != nil {
		return nil, err
	}
	if len(languages) == 0 {
		return nil, grpcx.Invalid("list at least one language you can teach in")
	}

	user, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: req.ActorId})
	if err != nil {
		return nil, err
	}
	campus, err := s.identity.GetCampus(ctx, &identityv1.GetCampusRequest{Id: user.CampusId})
	if err != nil {
		return nil, err
	}

	// Non-nil so pgx writes '{}' rather than NULL for the unused list.
	services, subjectIDs, companies := []string{}, []string{}, []string{}
	var resumeOn bool
	var resumePrice int64
	turnaround := int32(3)
	switch req.Kind {
	case "mentor":
		if services, err = cleanList(req.Services, len(interviewTypes), 30, "interview type"); err != nil {
			return nil, err
		}
		for _, sv := range services {
			if !slices.Contains(interviewTypes, sv) {
				return nil, grpcx.Invalid("unknown interview type %q", sv)
			}
		}
		if companies, err = cleanList(req.Companies, 6, 60, "company"); err != nil {
			return nil, err
		}
		if companies == nil {
			companies = []string{}
		}
		if o := req.ResumeOffer; o != nil {
			if o.PriceMinor < 0 || o.PriceMinor > maxRateMinor {
				return nil, grpcx.Invalid("resume review price is out of range")
			}
			if o.TurnaroundDays < 1 || int(o.TurnaroundDays) > resumeMaxTurnaround {
				return nil, grpcx.Invalid("turnaround must be 1–%d days", resumeMaxTurnaround)
			}
			resumeOn, resumePrice, turnaround = true, o.PriceMinor, o.TurnaroundDays
		}
		if len(services) == 0 && !resumeOn {
			return nil, grpcx.Invalid("offer mock interviews, resume reviews, or both")
		}
		if services == nil {
			services = []string{}
		}
	case "guide":
		if services, err = cleanList(req.Services, len(guideServices), 30, "service"); err != nil {
			return nil, err
		}
		if len(services) == 0 {
			return nil, grpcx.Invalid("choose at least one service you offer")
		}
		for _, sv := range services {
			if !slices.Contains(guideServices, sv) {
				return nil, grpcx.Invalid("unknown guide service %q", sv)
			}
		}
	default:
		if subjectIDs, err = cleanList(req.SubjectIds, 15, 40, "subject"); err != nil {
			return nil, err
		}
		if len(subjectIDs) == 0 {
			return nil, grpcx.Invalid("choose at least one subject you teach")
		}
		var valid int
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE id = ANY($1::uuid[]) AND (campus_id IS NULL OR campus_id = $2)`,
			subjectIDs, user.CampusId).Scan(&valid); err != nil {
			return nil, grpcx.Invalid("unknown subject")
		}
		if valid != len(subjectIDs) {
			return nil, grpcx.Invalid("one of the subjects isn't available at your campus")
		}
	}

	if req.Publish {
		if err := canPublish(user, campus); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id, status string
	err = tx.QueryRow(ctx, `INSERT INTO provider_profiles (user_id, kind, campus_id, timezone, display_name, avatar_key, headline, bio,
			rate_minor, currency, modes, languages, services, companies, resume_reviews, resume_price_minor, resume_turnaround_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (user_id, kind) DO UPDATE SET campus_id = EXCLUDED.campus_id, timezone = EXCLUDED.timezone,
			display_name = EXCLUDED.display_name, avatar_key = EXCLUDED.avatar_key, headline = EXCLUDED.headline,
			bio = EXCLUDED.bio, rate_minor = EXCLUDED.rate_minor, currency = EXCLUDED.currency, modes = EXCLUDED.modes,
			languages = EXCLUDED.languages, services = EXCLUDED.services, companies = EXCLUDED.companies,
			resume_reviews = EXCLUDED.resume_reviews, resume_price_minor = EXCLUDED.resume_price_minor,
			resume_turnaround_days = EXCLUDED.resume_turnaround_days, updated_at = now()
		RETURNING id, status`,
		req.ActorId, req.Kind, user.CampusId, campus.Timezone, displayName(user), user.AvatarKey, headline, bio,
		req.RateMinor, campus.Currency, modes, languages, services, companies, resumeOn, resumePrice, turnaround).Scan(&id, &status)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if status == "suspended" {
		return nil, grpcx.Denied("this profile is suspended; contact support")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM provider_subjects WHERE profile_id = $1`, id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if len(subjectIDs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO provider_subjects (profile_id, subject_id) SELECT $1, unnest($2::uuid[])`, id, subjectIDs); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if req.Publish {
		if _, err := tx.Exec(ctx, `UPDATE provider_profiles SET status = 'active' WHERE id = $1`, id); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.profileByID(ctx, id)
}

func canPublish(u *identityv1.User, c *identityv1.Campus) error {
	switch {
	case u.Status != "active":
		return grpcx.Denied("your account is suspended")
	case c.Status != "active":
		return grpcx.Precondition("CAMPUS_PENDING: your campus is still being reviewed; you can publish once it's approved")
	case !u.IdVerified:
		return grpcx.Precondition("ID_VERIFICATION_REQUIRED: verify your student ID before publishing a profile")
	}
	return nil
}

func (s *server) SetProviderStatus(ctx context.Context, req *marketplacev1.SetProviderStatusRequest) (*marketplacev1.ProviderProfile, error) {
	p, err := s.profileByID(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	if req.ActorIsAdmin {
		if !slices.Contains([]string{"active", "paused", "suspended", "draft"}, req.Status) {
			return nil, grpcx.Invalid("unknown status")
		}
	} else {
		if p.UserId != req.ActorId {
			return nil, grpcx.Denied("not your profile")
		}
		if p.Status == "suspended" {
			return nil, grpcx.Denied("this profile is suspended; contact support")
		}
		switch req.Status {
		case "paused", "draft":
		case "active":
			user, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: p.UserId})
			if err != nil {
				return nil, err
			}
			campus, err := s.identity.GetCampus(ctx, &identityv1.GetCampusRequest{Id: user.CampusId})
			if err != nil {
				return nil, err
			}
			if err := canPublish(user, campus); err != nil {
				return nil, err
			}
		default:
			return nil, grpcx.Invalid("you can set your profile to active, paused or draft")
		}
	}
	if _, err := s.db.Exec(ctx, `UPDATE provider_profiles SET status = $2, updated_at = now() WHERE id = $1`, req.Id, req.Status); err != nil {
		return nil, grpcx.Internal(err)
	}
	if req.ActorIsAdmin && req.Status == "suspended" && p.Status != "suspended" {
		s.notify(ctx, p.UserId, "profile_suspended", "Your "+p.Kind+" profile was suspended",
			"An admin suspended your profile after a review. Reply to our email or use the contact page to appeal.", "/dashboard/profile", true)
	}
	return s.profileByID(ctx, req.Id)
}

func (s *server) GetProviderProfile(ctx context.Context, req *marketplacev1.GetProviderProfileRequest) (*marketplacev1.ProviderProfile, error) {
	if req.Id != "" {
		return s.profileByID(ctx, req.Id)
	}
	return s.loadProfile(ctx, s.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM provider_profiles p WHERE p.user_id = $1 AND p.kind = $2`,
		req.UserId, req.Kind))
}

func (s *server) ListUserProviderProfiles(ctx context.Context, req *marketplacev1.UserRequest) (*marketplacev1.ProviderProfileList, error) {
	rows, err := s.db.Query(ctx, `SELECT `+profileColumns+` FROM provider_profiles p WHERE p.user_id = $1 ORDER BY p.kind DESC`, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &marketplacev1.ProviderProfileList{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Profiles = append(out.Profiles, p)
	}
	return out, rows.Err()
}

func (s *server) SearchProviders(ctx context.Context, req *marketplacev1.SearchProvidersRequest) (*marketplacev1.SearchProvidersResponse, error) {
	limit, offset := db.Page(req.Page, req.PageSize, 12, 50)
	where := []string{"p.status = 'active'"}
	if req.AnyStatus {
		where = []string{"true"}
	}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if req.Kind != "" {
		add("p.kind = $%d", req.Kind)
	}
	if req.CampusId != "" {
		add("p.campus_id = $%d::uuid", req.CampusId)
	}
	if req.SubjectId != "" {
		add("EXISTS (SELECT 1 FROM provider_subjects ps WHERE ps.profile_id = p.id AND ps.subject_id = $%d::uuid)", req.SubjectId)
	}
	if req.Mode != "" {
		add("$%d = ANY(p.modes)", req.Mode)
	}
	if req.Service != "" {
		add("$%d = ANY(p.services)", req.Service)
	}
	if req.ResumeReviews {
		where = append(where, "p.resume_reviews")
	}
	if c := strings.TrimSpace(req.Company); c != "" {
		add("EXISTS (SELECT 1 FROM unnest(p.companies) c WHERE lower(c) LIKE $%d)", "%"+strings.ToLower(c)+"%")
	}
	if req.MinRating > 0 {
		add("p.rating_avg >= $%d", req.MinRating)
	}
	if req.MaxRateMinor > 0 {
		add("p.rate_minor <= $%d", req.MaxRateMinor)
	}
	if q := strings.TrimSpace(req.Query); q != "" {
		args = append(args, q, "%"+strings.ToLower(q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(`(p.search @@ plainto_tsquery('simple', $%d) OR lower(p.headline) LIKE $%d
			OR lower(p.display_name) LIKE $%d OR EXISTS (SELECT 1 FROM provider_subjects ps JOIN subjects sj ON sj.id = ps.subject_id
			WHERE ps.profile_id = p.id AND (lower(sj.name) LIKE $%d OR lower(sj.code) LIKE $%d))
			OR EXISTS (SELECT 1 FROM unnest(p.companies) c WHERE lower(c) LIKE $%d))`, n-1, n, n, n, n, n))
	}
	if len(req.UserIds) > 0 {
		add("p.user_id = ANY($%d::uuid[])", req.UserIds)
	}
	if req.ViewerId != "" {
		add("NOT EXISTS (SELECT 1 FROM blocks b WHERE (b.user_id = $%[1]d AND b.blocked_id = p.user_id) OR (b.user_id = p.user_id AND b.blocked_id = $%[1]d))", req.ViewerId)
	}
	order := "p.rating_avg DESC, p.rating_count DESC, p.sessions_completed DESC, p.created_at DESC"
	switch req.Sort {
	case "price_asc":
		order = "p.rate_minor ASC, p.rating_avg DESC"
	case "price_desc":
		order = "p.rate_minor DESC, p.rating_avg DESC"
	case "newest":
		order = "p.created_at DESC"
	}
	cond := strings.Join(where, " AND ")

	out := &marketplacev1.SearchProvidersResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM provider_profiles p WHERE `+cond, args...).Scan(&out.Total); err != nil {
		if strings.Contains(err.Error(), "invalid input syntax") {
			return out, nil
		}
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM provider_profiles p WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		profileColumns, cond, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Profiles = append(out.Profiles, p)
	}
	return out, rows.Err()
}

func (s *server) SyncUserCard(ctx context.Context, req *marketplacev1.SyncUserCardRequest) (*marketplacev1.Empty, error) {
	if _, err := s.db.Exec(ctx, `UPDATE provider_profiles SET display_name = $2, avatar_key = $3, updated_at = now() WHERE user_id = $1`,
		req.UserId, req.DisplayName, req.AvatarKey); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

func (s *server) SyncCampusTimezone(ctx context.Context, req *marketplacev1.SyncCampusTimezoneRequest) (*marketplacev1.Empty, error) {
	if _, err := time.LoadLocation(req.Timezone); err != nil || req.Timezone == "" {
		return nil, grpcx.Invalid("invalid timezone")
	}
	if _, err := s.db.Exec(ctx, `UPDATE provider_profiles SET timezone = $2, updated_at = now() WHERE campus_id = $1 AND timezone <> $2`,
		req.CampusId, req.Timezone); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &marketplacev1.Empty{}, nil
}

// ── Availability ────────────────────────────────────────────────────────────

func (s *server) loadWindows(ctx context.Context, userID string) ([]window, error) {
	rows, err := s.db.Query(ctx, `SELECT weekday, start_minute, end_minute FROM availability WHERE user_id = $1 ORDER BY weekday, start_minute`, userID)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	var out []window
	for rows.Next() {
		var w window
		if err := rows.Scan(&w.Weekday, &w.StartMinute, &w.EndMinute); err != nil {
			return nil, grpcx.Internal(err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func toProtoWindows(ws []window) []*marketplacev1.AvailabilityWindow {
	out := make([]*marketplacev1.AvailabilityWindow, len(ws))
	for i, w := range ws {
		out[i] = &marketplacev1.AvailabilityWindow{Weekday: int32(w.Weekday), StartMinute: int32(w.StartMinute), EndMinute: int32(w.EndMinute)}
	}
	return out
}

func (s *server) userTimezone(ctx context.Context, userID string) string {
	var tz string
	if err := s.db.QueryRow(ctx, `SELECT timezone FROM provider_profiles WHERE user_id = $1 ORDER BY updated_at DESC LIMIT 1`, userID).Scan(&tz); err == nil {
		return tz
	}
	if u, err := s.identity.GetUser(ctx, &identityv1.UserIdRequest{UserId: userID}); err == nil {
		if c, err := s.identity.GetCampus(ctx, &identityv1.GetCampusRequest{Id: u.CampusId}); err == nil {
			return c.Timezone
		}
	}
	return "UTC"
}

func (s *server) SetAvailability(ctx context.Context, req *marketplacev1.SetAvailabilityRequest) (*marketplacev1.AvailabilityResponse, error) {
	ws := make([]window, len(req.Windows))
	for i, w := range req.Windows {
		ws[i] = window{Weekday: int(w.Weekday), StartMinute: int(w.StartMinute), EndMinute: int(w.EndMinute)}
	}
	if err := validateWindows(ws); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM availability WHERE user_id = $1`, req.ActorId); err != nil {
		return nil, grpcx.Internal(err)
	}
	for _, w := range ws {
		if _, err := tx.Exec(ctx, `INSERT INTO availability (user_id, weekday, start_minute, end_minute) VALUES ($1, $2, $3, $4)`,
			req.ActorId, w.Weekday, w.StartMinute, w.EndMinute); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.GetAvailability(ctx, &marketplacev1.UserRequest{UserId: req.ActorId})
}

func (s *server) GetAvailability(ctx context.Context, req *marketplacev1.UserRequest) (*marketplacev1.AvailabilityResponse, error) {
	ws, err := s.loadWindows(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	return &marketplacev1.AvailabilityResponse{Windows: toProtoWindows(ws), Timezone: s.userTimezone(ctx, req.UserId)}, nil
}

// busyIntervals returns the provider's and (optionally) the student's
// requested or accepted sessions overlapping [from, to).
func (s *server) busyIntervals(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, userIDs []string, from, to time.Time) ([]interval, error) {
	rows, err := q.Query(ctx, `SELECT start_at, end_at FROM bookings
		WHERE (provider_id = ANY($1::uuid[]) OR student_id = ANY($1::uuid[]))
		  AND status IN ('requested', 'accepted') AND start_at < $3 AND end_at > $2`, userIDs, from, to)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	var out []interval
	for rows.Next() {
		var iv interval
		if err := rows.Scan(&iv.Start, &iv.End); err != nil {
			return nil, grpcx.Internal(err)
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

type profileSched struct {
	userID   string
	timezone string
	status   string
}

func (s *server) GetOpenSlots(ctx context.Context, req *marketplacev1.GetOpenSlotsRequest) (*marketplacev1.GetOpenSlotsResponse, error) {
	if !allowedDurations[req.DurationMinutes] {
		return nil, grpcx.Invalid("duration must be %s", durationsText())
	}
	var p profileSched
	err := s.db.QueryRow(ctx, `SELECT user_id, timezone, status FROM provider_profiles WHERE id = $1`, req.ProviderProfileId).Scan(&p.userID, &p.timezone, &p.status)
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("profile not found")
		}
		return nil, grpcx.Internal(err)
	}
	out := &marketplacev1.GetOpenSlotsResponse{Timezone: p.timezone}
	if p.status != "active" {
		return out, nil
	}
	loc, err := time.LoadLocation(p.timezone)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	days := int(req.Days)
	if days <= 0 {
		days = defaultWin
	}
	if days > maxWin {
		days = maxWin
	}
	from := s.now().Add(leadTime)
	if req.From > 0 && time.Unix(req.From, 0).After(from) {
		from = time.Unix(req.From, 0)
	}
	to := from.Add(time.Duration(days) * 24 * time.Hour)

	ws, err := s.loadWindows(ctx, p.userID)
	if err != nil {
		return nil, err
	}
	busy, err := s.busyIntervals(ctx, s.db, []string{p.userID}, from, to)
	if err != nil {
		return nil, err
	}
	for _, sl := range computeSlots(ws, loc, from, to, time.Duration(req.DurationMinutes)*time.Minute, busy) {
		out.Slots = append(out.Slots, &marketplacev1.TimeSlot{StartAt: sl.Start.Unix(), EndAt: sl.End.Unix()})
	}
	return out, nil
}
