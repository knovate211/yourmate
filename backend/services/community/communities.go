package main

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	communityv1 "github.com/yourmate/backend/gen/community/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "community"
	}
	return s
}

func actorOf(a *communityv1.Actor) *communityv1.Actor {
	if a == nil {
		return &communityv1.Actor{}
	}
	return a
}

// communityColumns reads a community plus the viewer's relationship to it.
// The viewer's id is always query argument $1 (possibly empty). It is built
// from the settings because the decline cooldown is part of the query.
var communityColumns string

func buildCommunityColumns() {
	communityColumns = fmt.Sprintf(communityColumnsTemplate, int(declineCooldown.Seconds()))
}

const communityColumnsTemplate = `c.id, c.slug, coalesce(c.campus_id::text, ''), c.name, c.description, c.kind, c.join_policy, c.rules,
	coalesce(c.owner_id::text, ''), c.is_default, c.member_count, c.post_count, c.status, c.created_at,
	coalesce((SELECT m.role FROM community_members m WHERE m.community_id = c.id AND m.user_id::text = $1), ''),
	coalesce((SELECT r.status FROM community_join_requests r WHERE r.community_id = c.id AND r.user_id::text = $1
		AND (r.status = 'pending' OR (r.status = 'declined' AND r.decided_at > now() - make_interval(secs => %d)))
		ORDER BY r.created_at DESC LIMIT 1), ''),
	coalesce((SELECT m.muted FROM community_members m WHERE m.community_id = c.id AND m.user_id::text = $1), false)`

func scanCommunity(row pgx.Row) (*communityv1.Community, error) {
	c := &communityv1.Community{}
	var created time.Time
	if err := row.Scan(&c.Id, &c.Slug, &c.CampusId, &c.Name, &c.Description, &c.Kind, &c.JoinPolicy, &c.Rules, &c.OwnerId,
		&c.IsDefault, &c.MemberCount, &c.PostCount, &c.Status, &created, &c.ViewerRole, &c.ViewerRequest, &c.ViewerMuted); err != nil {
		return nil, err
	}
	c.CreatedAt = created.Unix()
	return c, nil
}

// loadCommunity returns an active community (any status for admins).
func (s *server) loadCommunity(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor *communityv1.Actor, id string) (*communityv1.Community, error) {
	c, err := scanCommunity(q.QueryRow(ctx, `SELECT `+communityColumns+` FROM communities c WHERE c.id::text = $2`, actor.Id, id))
	if err != nil {
		return nil, notFound(err, "community")
	}
	if c.Status != "active" && !actor.IsAdmin {
		return nil, grpcx.NotFound("community not found")
	}
	return c, nil
}

func canModerate(a *communityv1.Actor, c *communityv1.Community) bool {
	return a.IsAdmin || c.ViewerRole == "moderator" || c.ViewerRole == "owner"
}

func canSeePosts(a *communityv1.Actor, c *communityv1.Community) bool {
	return c.JoinPolicy == "open" || c.ViewerRole != "" || a.IsAdmin
}

func requireUser(a *communityv1.Actor) error {
	if a.Id == "" {
		return grpcx.Unauthenticated("log in to continue")
	}
	return nil
}

// eligible checks who may be in a community: campus communities are only for
// verified students of that campus; all-campus ones for any member.
func eligible(a *communityv1.Actor, c *communityv1.Community) error {
	if err := requireUser(a); err != nil {
		return err
	}
	if c.CampusId == "" || a.IsAdmin {
		return nil
	}
	if a.CampusId != c.CampusId {
		return grpcx.Denied("ONLY_CAMPUS: %s is only for students of its campus", c.Name)
	}
	if !a.CampusVerified {
		return grpcx.Precondition("VERIFICATION_REQUIRED: verify you study at this campus to join %s", c.Name)
	}
	return nil
}

func validateCommunity(name, description, kind, policy, rules string) (string, string, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	description = strings.TrimSpace(description)
	rules = strings.TrimSpace(rules)
	switch {
	case len([]rune(name)) < 3 || len([]rune(name)) > 80:
		return "", "", "", grpcx.Invalid("name must be 3–80 characters")
	case len(description) > 500:
		return "", "", "", grpcx.Invalid("description must be at most 500 characters")
	case len(rules) > 2000:
		return "", "", "", grpcx.Invalid("rules must be at most 2000 characters")
	case !slices.Contains(communityKinds, kind):
		return "", "", "", grpcx.Invalid("choose a type")
	case !slices.Contains(joinPolicies, policy):
		return "", "", "", grpcx.Invalid("choose how people join")
	}
	return name, description, rules, nil
}

func (s *server) uniqueSlug(ctx context.Context, tx pgx.Tx, base string) (string, error) {
	slug := base
	for i := 2; i < 200; i++ {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM communities WHERE slug = $1)`, slug).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return "", grpcx.Exists("too many communities with this name")
}

// ── Communities ─────────────────────────────────────────────────────────────

func (s *server) EnsureDefaultCommunity(ctx context.Context, req *communityv1.EnsureDefaultCommunityRequest) (*communityv1.Community, error) {
	if req.CampusId == "" {
		return nil, grpcx.Invalid("campus is required")
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO communities (slug, campus_id, name, description, kind, join_policy, rules, is_default)
		VALUES ($1, $2, 'All students', 'Everyone at the campus: questions, announcements and finding people.', 'general', 'open',
		        'Be kind. No selling or spam. Keep personal details out of posts.', true)
		ON CONFLICT DO NOTHING`, "students-"+slugify(req.CampusSlug), req.CampusId); err != nil {
		return nil, grpcx.Internal(err)
	}
	c, err := scanCommunity(s.db.QueryRow(ctx, `SELECT `+communityColumns+` FROM communities c WHERE c.campus_id::text = $2 AND c.is_default`, "", req.CampusId))
	if err != nil {
		return nil, notFound(err, "community")
	}
	return c, nil
}

func (s *server) CreateCommunity(ctx context.Context, req *communityv1.CreateCommunityRequest) (*communityv1.Community, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	if !a.CampusVerified && !a.IsAdmin {
		return nil, grpcx.Precondition("VERIFICATION_REQUIRED: verify you're a student at your campus before creating a community")
	}
	name, description, rules, err := validateCommunity(req.Name, req.Description, req.Kind, req.JoinPolicy, req.Rules)
	if err != nil {
		return nil, err
	}
	var campus any
	if !req.AllCampuses {
		campus = a.CampusId
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('community-owner:' || $1))`, a.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	var owned int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM communities WHERE owner_id::text = $1 AND status = 'active'`, a.Id).Scan(&owned); err != nil {
		return nil, grpcx.Internal(err)
	}
	if owned >= maxOwnedCommunities && !a.IsAdmin {
		return nil, grpcx.TooMany("you can own up to %d communities; delete one or ask a moderator to take over", maxOwnedCommunities)
	}
	slug, err := s.uniqueSlug(ctx, tx, slugify(name))
	if err != nil {
		return nil, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO communities (slug, campus_id, name, description, kind, join_policy, rules, owner_id, member_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1) RETURNING id`,
		slug, campus, name, description, req.Kind, req.JoinPolicy, rules, a.Id).Scan(&id)
	if db.IsUniqueViolation(err, "communities_name_idx") {
		return nil, grpcx.Exists("a community called %q already exists here; join it instead", name)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO community_members (community_id, user_id, role) VALUES ($1, $2, 'owner')`, id, a.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.loadCommunity(ctx, s.db, a, id)
}

func (s *server) UpdateCommunity(ctx context.Context, req *communityv1.UpdateCommunityRequest) (*communityv1.Community, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole != "owner" && !a.IsAdmin {
		return nil, grpcx.Denied("only the owner can change the community's settings")
	}
	name, description, rules, err := validateCommunity(req.Name, req.Description, req.Kind, req.JoinPolicy, req.Rules)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(ctx, `UPDATE communities SET name = $2, description = $3, kind = $4, join_policy = $5, rules = $6, updated_at = now()
		WHERE id = $1`, c.Id, name, description, req.Kind, req.JoinPolicy, rules)
	if db.IsUniqueViolation(err, "communities_name_idx") {
		return nil, grpcx.Exists("another community here is already called %q", name)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.loadCommunity(ctx, s.db, a, c.Id)
}

func (s *server) DeleteCommunity(ctx context.Context, req *communityv1.CommunityRef) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.IsDefault {
		return nil, grpcx.Precondition("a campus's All students community can't be deleted")
	}
	if c.ViewerRole != "owner" && !a.IsAdmin {
		return nil, grpcx.Denied("only the owner can delete the community")
	}
	if _, err := s.db.Exec(ctx, `UPDATE communities SET status = 'removed', updated_at = now() WHERE id = $1`, c.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &communityv1.Empty{}, nil
}

func (s *server) GetCommunity(ctx context.Context, req *communityv1.GetCommunityRequest) (*communityv1.Community, error) {
	a := actorOf(req.Actor)
	if req.Id != "" {
		return s.loadCommunity(ctx, s.db, a, req.Id)
	}
	c, err := scanCommunity(s.db.QueryRow(ctx, `SELECT `+communityColumns+` FROM communities c WHERE c.slug = $2`, a.Id, req.Slug))
	if err != nil {
		return nil, notFound(err, "community")
	}
	if c.Status != "active" && !a.IsAdmin {
		return nil, grpcx.NotFound("community not found")
	}
	return c, nil
}

func (s *server) ListCommunities(ctx context.Context, req *communityv1.ListCommunitiesRequest) (*communityv1.ListCommunitiesResponse, error) {
	a := actorOf(req.Actor)
	limit, offset := db.Page(req.Page, req.PageSize, 20, 50)
	// $1 is always the viewer; the count query must still give it a type even
	// when no filter mentions it.
	where := []string{"c.status = 'active'", "$1::text IS NOT NULL"}
	args := []any{a.Id}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	switch req.Tab {
	case "global":
		where = append(where, "c.campus_id IS NULL")
	case "joined":
		if a.Id == "" {
			return &communityv1.ListCommunitiesResponse{}, nil
		}
		where = append(where, "EXISTS (SELECT 1 FROM community_members m WHERE m.community_id = c.id AND m.user_id::text = $1)")
	default: // campus
		if req.CampusId == "" {
			return &communityv1.ListCommunitiesResponse{}, nil
		}
		add("c.campus_id::text = $%d", req.CampusId)
	}
	if req.Kind != "" {
		add("c.kind = $%d", req.Kind)
	}
	if q := strings.ToLower(strings.TrimSpace(req.Query)); q != "" {
		add("strpos(lower(c.name || ' ' || c.description), $%d) > 0", q)
	}
	cond := strings.Join(where, " AND ")
	out := &communityv1.ListCommunitiesResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM communities c WHERE `+cond, args...).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT %s FROM communities c WHERE %s
		ORDER BY c.is_default DESC, c.member_count DESC, c.created_at DESC LIMIT $%d OFFSET $%d`,
		communityColumns, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanCommunity(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Communities = append(out.Communities, c)
	}
	return out, rows.Err()
}

// ── Membership ──────────────────────────────────────────────────────────────

func (s *server) moderatorIDs(ctx context.Context, communityID string) []string {
	rows, err := s.db.Query(ctx, `SELECT user_id::text FROM community_members
		WHERE community_id = $1 AND role IN ('moderator', 'owner') AND NOT muted`, communityID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *server) JoinCommunity(ctx context.Context, req *communityv1.JoinCommunityRequest) (*communityv1.JoinCommunityResponse, error) {
	a := actorOf(req.Actor)
	note := strings.TrimSpace(req.Note)
	if len(note) > 300 {
		return nil, grpcx.Invalid("keep the note under 300 characters")
	}
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole != "" {
		return &communityv1.JoinCommunityResponse{Status: "joined", Community: c}, nil
	}
	if err := eligible(a, c); err != nil {
		return nil, err
	}
	var bannedUntil *time.Time
	if err := s.db.QueryRow(ctx, `SELECT until FROM community_bans WHERE community_id = $1 AND user_id = $2 AND until > now()`,
		c.Id, a.Id).Scan(&bannedUntil); err != nil && !db.IsNoRows(err) {
		return nil, grpcx.Internal(err)
	}
	if bannedUntil != nil {
		return nil, grpcx.Denied("a moderator removed you from %s; you can rejoin after %s", c.Name, bannedUntil.Format("2 Jan 2006"))
	}

	if c.JoinPolicy == "approval" && !a.IsAdmin {
		if c.ViewerRequest == "pending" {
			return &communityv1.JoinCommunityResponse{Status: "requested", Community: c}, nil
		}
		if c.ViewerRequest == "declined" {
			return nil, grpcx.TooMany("your last request was declined; you can ask again %s after it", humanDuration(declineCooldown))
		}
		_, err := s.db.Exec(ctx, `INSERT INTO community_join_requests (community_id, user_id, note) VALUES ($1, $2, $3)`, c.Id, a.Id, note)
		if err != nil && !db.IsUniqueViolation(err, "") {
			return nil, grpcx.Internal(err)
		}
		c, err = s.loadCommunity(ctx, s.db, a, c.Id)
		if err != nil {
			return nil, err
		}
		return &communityv1.JoinCommunityResponse{Status: "requested", Community: c, NotifyUserIds: s.moderatorIDs(ctx, c.Id)}, nil
	}

	if err := s.addMember(ctx, c.Id, a.Id); err != nil {
		return nil, err
	}
	c, err = s.loadCommunity(ctx, s.db, a, c.Id)
	if err != nil {
		return nil, err
	}
	return &communityv1.JoinCommunityResponse{Status: "joined", Community: c}, nil
}

func (s *server) addMember(ctx context.Context, communityID, userID string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `INSERT INTO community_members (community_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, communityID, userID)
	if err != nil {
		return grpcx.Internal(err)
	}
	if tag.RowsAffected() == 1 {
		if _, err := tx.Exec(ctx, `UPDATE communities SET member_count = member_count + 1 WHERE id = $1`, communityID); err != nil {
			return grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return grpcx.Internal(err)
	}
	return nil
}

func (s *server) removeMember(ctx context.Context, communityID, userID string, ban bool) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `DELETE FROM community_members WHERE community_id = $1 AND user_id = $2`, communityID, userID)
	if err != nil {
		return grpcx.Internal(err)
	}
	if tag.RowsAffected() == 1 {
		if _, err := tx.Exec(ctx, `UPDATE communities SET member_count = member_count - 1 WHERE id = $1`, communityID); err != nil {
			return grpcx.Internal(err)
		}
	}
	if ban {
		if _, err := tx.Exec(ctx, `INSERT INTO community_bans (community_id, user_id, until) VALUES ($1, $2, $3)
			ON CONFLICT (community_id, user_id) DO UPDATE SET until = EXCLUDED.until`, communityID, userID, time.Now().Add(removalBan)); err != nil {
			return grpcx.Internal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE community_join_requests SET status = 'declined', decided_at = now()
			WHERE community_id = $1 AND user_id = $2 AND status = 'pending'`, communityID, userID); err != nil {
			return grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return grpcx.Internal(err)
	}
	return nil
}

func (s *server) LeaveCommunity(ctx context.Context, req *communityv1.CommunityRef) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole == "owner" {
		return nil, grpcx.Precondition("owners can't leave; delete the community instead")
	}
	// Leaving also withdraws a pending request.
	if _, err := s.db.Exec(ctx, `DELETE FROM community_join_requests WHERE community_id = $1 AND user_id = $2 AND status = 'pending'`, c.Id, a.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := s.removeMember(ctx, c.Id, a.Id, false); err != nil {
		return nil, err
	}
	return &communityv1.Empty{}, nil
}

func (s *server) SetCommunityMuted(ctx context.Context, req *communityv1.SetCommunityMutedRequest) (*communityv1.Community, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE community_members SET muted = $3 WHERE community_id::text = $1 AND user_id = $2`, req.CommunityId, a.Id, req.Muted)
	if err != nil {
		return nil, notFound(err, "community")
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.Precondition("join the community first")
	}
	return s.loadCommunity(ctx, s.db, a, req.CommunityId)
}

func scanJoinRequest(row pgx.Row) (*communityv1.JoinRequest, error) {
	r := &communityv1.JoinRequest{}
	var created time.Time
	if err := row.Scan(&r.Id, &r.CommunityId, &r.UserId, &r.Note, &r.Status, &created); err != nil {
		return nil, err
	}
	r.CreatedAt = created.Unix()
	return r, nil
}

func (s *server) ListJoinRequests(ctx context.Context, req *communityv1.CommunityRef) (*communityv1.ListJoinRequestsResponse, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can see join requests")
	}
	rows, err := s.db.Query(ctx, `SELECT id, community_id, user_id, note, status, created_at FROM community_join_requests
		WHERE community_id = $1 AND status = 'pending' ORDER BY created_at LIMIT 200`, c.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListJoinRequestsResponse{}
	for rows.Next() {
		r, err := scanJoinRequest(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Requests = append(out.Requests, r)
	}
	return out, rows.Err()
}

func (s *server) ReviewJoinRequest(ctx context.Context, req *communityv1.ReviewJoinRequestRequest) (*communityv1.JoinRequest, error) {
	a := actorOf(req.Actor)
	r, err := scanJoinRequest(s.db.QueryRow(ctx, `SELECT id, community_id, user_id, note, status, created_at FROM community_join_requests
		WHERE id::text = $1`, req.RequestId))
	if err != nil {
		return nil, notFound(err, "request")
	}
	c, err := s.loadCommunity(ctx, s.db, a, r.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can review join requests")
	}
	status := "declined"
	if req.Approve {
		status = "approved"
	}
	tag, err := s.db.Exec(ctx, `UPDATE community_join_requests SET status = $2, decided_by = $3, decided_at = now()
		WHERE id = $1 AND status = 'pending'`, r.Id, status, a.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.Precondition("this request was already handled")
	}
	if req.Approve {
		if err := s.addMember(ctx, r.CommunityId, r.UserId); err != nil {
			return nil, err
		}
	}
	r.Status = status
	return r, nil
}

func (s *server) ListMembers(ctx context.Context, req *communityv1.ListMembersRequest) (*communityv1.ListMembersResponse, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canSeePosts(a, c) {
		return nil, grpcx.Denied("MEMBERS_ONLY: join %s to see who's in it", c.Name)
	}
	limit, offset := db.Page(req.Page, req.PageSize, 30, 100)
	rows, err := s.db.Query(ctx, `SELECT user_id, role, joined_at FROM community_members WHERE community_id = $1
		ORDER BY CASE role WHEN 'owner' THEN 0 WHEN 'moderator' THEN 1 ELSE 2 END, joined_at LIMIT $2 OFFSET $3`, c.Id, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListMembersResponse{Total: int64(c.MemberCount)}
	for rows.Next() {
		m := &communityv1.Member{}
		var joined time.Time
		if err := rows.Scan(&m.UserId, &m.Role, &joined); err != nil {
			return nil, grpcx.Internal(err)
		}
		m.JoinedAt = joined.Unix()
		out.Members = append(out.Members, m)
	}
	return out, rows.Err()
}

func (s *server) ListMemberIds(ctx context.Context, req *communityv1.CommunityRef) (*communityv1.ListMemberIdsResponse, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT user_id::text FROM community_members WHERE community_id = $1 LIMIT 2000`, c.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListMemberIdsResponse{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, grpcx.Internal(err)
		}
		out.UserIds = append(out.UserIds, id)
	}
	return out, rows.Err()
}

func (s *server) memberRole(ctx context.Context, communityID, userID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, `SELECT role FROM community_members WHERE community_id = $1 AND user_id::text = $2`, communityID, userID).Scan(&role)
	if db.IsNoRows(err) {
		return "", nil
	}
	return role, err
}

func (s *server) SetMemberRole(ctx context.Context, req *communityv1.SetMemberRoleRequest) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole != "owner" && !a.IsAdmin {
		return nil, grpcx.Denied("only the owner can choose moderators")
	}
	if req.Role != "member" && req.Role != "moderator" {
		return nil, grpcx.Invalid("role must be member or moderator")
	}
	role, err := s.memberRole(ctx, c.Id, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	switch role {
	case "":
		return nil, grpcx.Precondition("they aren't a member")
	case "owner":
		return nil, grpcx.Precondition("the owner's role can't be changed")
	}
	if _, err := s.db.Exec(ctx, `UPDATE community_members SET role = $3 WHERE community_id = $1 AND user_id::text = $2`, c.Id, req.UserId, req.Role); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &communityv1.Empty{}, nil
}

func (s *server) RemoveMember(ctx context.Context, req *communityv1.MemberRef) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can remove members")
	}
	if req.UserId == a.Id {
		return nil, grpcx.Invalid("use Leave to leave the community")
	}
	role, err := s.memberRole(ctx, c.Id, req.UserId)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	switch {
	case role == "":
		return nil, grpcx.Precondition("they aren't a member")
	case role == "owner":
		return nil, grpcx.Precondition("the owner can't be removed")
	case role == "moderator" && c.ViewerRole != "owner" && !a.IsAdmin:
		return nil, grpcx.Denied("only the owner can remove a moderator")
	}
	if err := s.removeMember(ctx, c.Id, req.UserId, true); err != nil {
		return nil, err
	}
	return &communityv1.Empty{}, nil
}

// ── Posts and replies ───────────────────────────────────────────────────────

const communityPostColumns = `p.id, p.community_id, p.author_id, p.body, p.pinned, p.like_count, p.reply_count,
	EXISTS (SELECT 1 FROM community_post_likes l WHERE l.post_id = p.id AND l.user_id::text = $1), p.created_at`

func scanCommunityPost(row pgx.Row) (*communityv1.CommunityPost, error) {
	p := &communityv1.CommunityPost{}
	var created time.Time
	if err := row.Scan(&p.Id, &p.CommunityId, &p.AuthorId, &p.Body, &p.Pinned, &p.LikeCount, &p.ReplyCount, &p.ViewerLiked, &created); err != nil {
		return nil, err
	}
	p.CreatedAt = created.Unix()
	return p, nil
}

// visiblePost loads a live post together with its community, checking the
// viewer may read it.
func (s *server) visiblePost(ctx context.Context, a *communityv1.Actor, postID string) (*communityv1.CommunityPost, *communityv1.Community, error) {
	p, err := scanCommunityPost(s.db.QueryRow(ctx, `SELECT `+communityPostColumns+` FROM community_posts p WHERE p.id::text = $2 AND NOT p.removed`, a.Id, postID))
	if err != nil {
		return nil, nil, notFound(err, "post")
	}
	c, err := s.loadCommunity(ctx, s.db, a, p.CommunityId)
	if err != nil {
		return nil, nil, err
	}
	if !canSeePosts(a, c) {
		return nil, nil, grpcx.Denied("MEMBERS_ONLY: join %s to see its posts", c.Name)
	}
	return p, c, nil
}

func (s *server) CreateCommunityPost(ctx context.Context, req *communityv1.CreateCommunityPostRequest) (*communityv1.CommunityPost, error) {
	a := actorOf(req.Actor)
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 4000 {
		return nil, grpcx.Invalid("posts must be 1–4000 characters")
	}
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole == "" {
		return nil, grpcx.Precondition("NOT_MEMBER: join %s to post", c.Name)
	}
	var recent int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM community_posts WHERE author_id = $1 AND created_at > now() - interval '1 hour'`, a.Id).Scan(&recent); err != nil {
		return nil, grpcx.Internal(err)
	}
	if recent >= postsPerHour {
		return nil, grpcx.TooMany("you've posted a lot this hour; try again later")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO community_posts (community_id, author_id, body) VALUES ($1, $2, $3) RETURNING id`, c.Id, a.Id, body).Scan(&id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE communities SET post_count = post_count + 1 WHERE id = $1`, c.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	p, _, err := s.visiblePost(ctx, a, id)
	return p, err
}

func (s *server) ListCommunityPosts(ctx context.Context, req *communityv1.ListCommunityPostsRequest) (*communityv1.ListCommunityPostsResponse, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canSeePosts(a, c) {
		return nil, grpcx.Denied("MEMBERS_ONLY: join %s to see its posts", c.Name)
	}
	limit, offset := db.Page(req.Page, req.PageSize, 20, 50)
	hide := req.HideAuthorIds
	if hide == nil {
		hide = []string{}
	}
	out := &communityv1.ListCommunityPostsResponse{}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM community_posts p WHERE p.community_id = $1 AND NOT p.removed
		AND NOT (p.author_id::text = ANY($2))`, c.Id, hide).Scan(&out.Total); err != nil {
		return nil, grpcx.Internal(err)
	}
	rows, err := s.db.Query(ctx, `SELECT `+communityPostColumns+` FROM community_posts p
		WHERE p.community_id = $2 AND NOT p.removed AND NOT (p.author_id::text = ANY($3))
		ORDER BY p.pinned DESC, p.created_at DESC LIMIT $4 OFFSET $5`, a.Id, c.Id, hide, limit, offset)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanCommunityPost(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Posts = append(out.Posts, p)
	}
	return out, rows.Err()
}

func (s *server) GetCommunityPost(ctx context.Context, req *communityv1.PostRef) (*communityv1.CommunityPost, error) {
	p, _, err := s.visiblePost(ctx, actorOf(req.Actor), req.PostId)
	return p, err
}

// takeDownPost hides a post and closes every open report about it.
func (s *server) takeDownPost(ctx context.Context, postID, note string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var communityID string
	err = tx.QueryRow(ctx, `UPDATE community_posts SET removed = true WHERE id = $1 AND NOT removed RETURNING community_id`, postID).Scan(&communityID)
	if err != nil && !db.IsNoRows(err) {
		return grpcx.Internal(err)
	}
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE communities SET post_count = post_count - 1 WHERE id = $1`, communityID); err != nil {
			return grpcx.Internal(err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE reports SET status = 'resolved', resolution_note = $2, resolved_at = now()
		WHERE target_type = 'community_post' AND target_id = $1 AND status = 'open'`, postID, note); err != nil {
		return grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return grpcx.Internal(err)
	}
	return nil
}

func (s *server) RemoveCommunityPost(ctx context.Context, req *communityv1.PostRef) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	p, c, err := s.visiblePost(ctx, a, req.PostId)
	if err != nil {
		return nil, err
	}
	if p.AuthorId != a.Id && !canModerate(a, c) {
		return nil, grpcx.Denied("you can only remove your own posts")
	}
	note := "Removed by its author"
	if p.AuthorId != a.Id {
		note = "Removed by a moderator"
	}
	if err := s.takeDownPost(ctx, p.Id, note); err != nil {
		return nil, err
	}
	return &communityv1.Empty{}, nil
}

func (s *server) PinCommunityPost(ctx context.Context, req *communityv1.PinCommunityPostRequest) (*communityv1.CommunityPost, error) {
	a := actorOf(req.Actor)
	p, c, err := s.visiblePost(ctx, a, req.PostId)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can pin posts")
	}
	if _, err := s.db.Exec(ctx, `UPDATE community_posts SET pinned = $2 WHERE id = $1`, p.Id, req.Pinned); err != nil {
		return nil, grpcx.Internal(err)
	}
	p, _, err = s.visiblePost(ctx, a, p.Id)
	return p, err
}

func (s *server) LikeCommunityPost(ctx context.Context, req *communityv1.LikeCommunityPostRequest) (*communityv1.CommunityPost, error) {
	a := actorOf(req.Actor)
	if err := requireUser(a); err != nil {
		return nil, err
	}
	p, c, err := s.visiblePost(ctx, a, req.PostId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole == "" && !a.IsAdmin {
		return nil, grpcx.Precondition("NOT_MEMBER: join %s to like posts", c.Name)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var tag interface{ RowsAffected() int64 }
	delta := 1
	if req.Liked {
		tag, err = tx.Exec(ctx, `INSERT INTO community_post_likes (post_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, p.Id, a.Id)
	} else {
		delta = -1
		tag, err = tx.Exec(ctx, `DELETE FROM community_post_likes WHERE post_id = $1 AND user_id = $2`, p.Id, a.Id)
	}
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 1 {
		if _, err := tx.Exec(ctx, `UPDATE community_posts SET like_count = like_count + $2 WHERE id = $1`, p.Id, delta); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	p, _, err = s.visiblePost(ctx, a, p.Id)
	return p, err
}

func (s *server) CreateReply(ctx context.Context, req *communityv1.CreateReplyRequest) (*communityv1.CreateReplyResponse, error) {
	a := actorOf(req.Actor)
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		return nil, grpcx.Invalid("replies must be 1–2000 characters")
	}
	p, c, err := s.visiblePost(ctx, a, req.PostId)
	if err != nil {
		return nil, err
	}
	if c.ViewerRole == "" {
		return nil, grpcx.Precondition("NOT_MEMBER: join %s to reply", c.Name)
	}
	var recent int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM community_replies WHERE author_id = $1 AND created_at > now() - interval '1 hour'`, a.Id).Scan(&recent); err != nil {
		return nil, grpcx.Internal(err)
	}
	if recent >= repliesPerHour {
		return nil, grpcx.TooMany("you've replied a lot this hour; try again later")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	r := &communityv1.Reply{}
	var created time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO community_replies (post_id, author_id, body) VALUES ($1, $2, $3)
		RETURNING id, post_id, author_id, body, created_at`, p.Id, a.Id, body).Scan(&r.Id, &r.PostId, &r.AuthorId, &r.Body, &created); err != nil {
		return nil, grpcx.Internal(err)
	}
	r.CreatedAt = created.Unix()
	if _, err := tx.Exec(ctx, `UPDATE community_posts SET reply_count = reply_count + 1 WHERE id = $1`, p.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}

	out := &communityv1.CreateReplyResponse{Reply: r, CommunitySlug: c.Slug, CommunityName: c.Name}
	if p.AuthorId != a.Id {
		var muted bool
		_ = s.db.QueryRow(ctx, `SELECT muted FROM community_members WHERE community_id = $1 AND user_id = $2`, c.Id, p.AuthorId).Scan(&muted)
		if !muted {
			out.NotifyUserId = p.AuthorId
		}
	}
	return out, nil
}

func (s *server) ListReplies(ctx context.Context, req *communityv1.PostRef) (*communityv1.ListRepliesResponse, error) {
	a := actorOf(req.Actor)
	p, _, err := s.visiblePost(ctx, a, req.PostId)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id, post_id, author_id, body, created_at FROM community_replies
		WHERE post_id = $1 AND NOT removed ORDER BY created_at LIMIT 300`, p.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListRepliesResponse{}
	for rows.Next() {
		r := &communityv1.Reply{}
		var created time.Time
		if err := rows.Scan(&r.Id, &r.PostId, &r.AuthorId, &r.Body, &created); err != nil {
			return nil, grpcx.Internal(err)
		}
		r.CreatedAt = created.Unix()
		out.Replies = append(out.Replies, r)
	}
	return out, rows.Err()
}

func (s *server) RemoveReply(ctx context.Context, req *communityv1.ReplyRef) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	var postID, authorID string
	if err := s.db.QueryRow(ctx, `SELECT post_id, author_id FROM community_replies WHERE id::text = $1 AND NOT removed`, req.ReplyId).Scan(&postID, &authorID); err != nil {
		return nil, notFound(err, "reply")
	}
	_, c, err := s.visiblePost(ctx, a, postID)
	if err != nil {
		return nil, err
	}
	if authorID != a.Id && !canModerate(a, c) {
		return nil, grpcx.Denied("you can only remove your own replies")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE community_replies SET removed = true WHERE id::text = $1`, req.ReplyId); err != nil {
		return nil, grpcx.Internal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE community_posts SET reply_count = reply_count - 1 WHERE id = $1 AND reply_count > 0`, postID); err != nil {
		return nil, grpcx.Internal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &communityv1.Empty{}, nil
}

// ── Moderation of reports ───────────────────────────────────────────────────

func (s *server) ListCommunityReports(ctx context.Context, req *communityv1.CommunityRef) (*communityv1.ListReportsResponse, error) {
	a := actorOf(req.Actor)
	c, err := s.loadCommunity(ctx, s.db, a, req.CommunityId)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can see reports")
	}
	rows, err := s.db.Query(ctx, `SELECT `+reportColumns+` FROM reports WHERE status = 'open' AND target_type = 'community_post'
		AND target_id IN (SELECT id FROM community_posts WHERE community_id = $1 AND NOT removed) ORDER BY created_at LIMIT 200`, c.Id)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &communityv1.ListReportsResponse{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Reports = append(out.Reports, r)
	}
	out.Total = int64(len(out.Reports))
	return out, rows.Err()
}

func (s *server) ModerateCommunityReport(ctx context.Context, req *communityv1.ModerateCommunityReportRequest) (*communityv1.Empty, error) {
	a := actorOf(req.Actor)
	r, err := scanReport(s.db.QueryRow(ctx, `SELECT `+reportColumns+` FROM reports WHERE id::text = $1`, req.ReportId))
	if err != nil {
		return nil, notFound(err, "report")
	}
	if r.TargetType != "community_post" || r.Status != "open" {
		return nil, grpcx.Precondition("this report isn't open for community moderation")
	}
	var communityID string
	if err := s.db.QueryRow(ctx, `SELECT community_id FROM community_posts WHERE id = $1`, r.TargetId).Scan(&communityID); err != nil {
		return nil, notFound(err, "post")
	}
	c, err := s.loadCommunity(ctx, s.db, a, communityID)
	if err != nil {
		return nil, err
	}
	if !canModerate(a, c) {
		return nil, grpcx.Denied("only moderators can handle reports")
	}
	if req.RemovePost {
		if err := s.takeDownPost(ctx, r.TargetId, "Post removed by a moderator"); err != nil {
			return nil, err
		}
		return &communityv1.Empty{}, nil
	}
	if _, err := s.db.Exec(ctx, `UPDATE reports SET status = 'dismissed', resolution_note = 'Dismissed by a community moderator', resolved_at = now()
		WHERE id = $1 AND status = 'open'`, r.Id); err != nil {
		return nil, grpcx.Internal(err)
	}
	return &communityv1.Empty{}, nil
}
