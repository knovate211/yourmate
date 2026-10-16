package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	identityv1 "github.com/yourmate/backend/gen/identity/v1"
	"github.com/yourmate/backend/pkg/db"
	"github.com/yourmate/backend/pkg/grpcx"
)

const campusColumns = `c.id, c.slug, c.name, c.country, c.city, c.timezone, c.currency, c.email_domains, c.status,
	coalesce(c.requested_by::text, ''), c.created_at,
	(SELECT count(*) FROM users u WHERE u.campus_id = c.id),
	c.website, c.source, c.details_verified`

func scanCampus(row pgx.Row) (*identityv1.Campus, error) {
	c := &identityv1.Campus{}
	var created time.Time
	if err := row.Scan(&c.Id, &c.Slug, &c.Name, &c.Country, &c.City, &c.Timezone, &c.Currency, &c.EmailDomains,
		&c.Status, &c.RequestedBy, &created, &c.MemberCount, &c.Website, &c.Source, &c.DetailsVerified); err != nil {
		return nil, err
	}
	c.CreatedAt = created.Unix()
	return c, nil
}

func (s *server) queryCampuses(ctx context.Context, sql string, args ...any) ([]*identityv1.Campus, error) {
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	var out []*identityv1.Campus
	for rows.Next() {
		c, err := scanCampus(rows)
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, grpcx.Internal(err)
	}
	return out, nil
}

var (
	searchToken = regexp.MustCompile(`[[:alnum:]@.\-]+`)
	apostrophes = strings.NewReplacer("'", "", "’", "")
)

// campusFilter builds the WHERE clause shared by listing and counting, and
// an ORDER BY that ranks the best matches first. The ORDER BY may reference
// one extra argument, returned separately so the count query can omit it.
// Every search word must appear in the name, city, country, slug or an email
// domain, or be the start of the campus's acronym ("iit bombay", "lpu").
func campusFilter(req *identityv1.ListCampusesRequest) (cond string, args []any, order string, orderArg []any) {
	status := req.Status
	if status == "" {
		status = "active"
	}
	where := []string{"($1 = 'all' OR c.status = $1)"}
	args = []any{status}
	// Apostrophes are dropped on both sides so "kings" finds "King's College".
	tokens := searchToken.FindAllString(apostrophes.Replace(strings.ToLower(req.Query)), 8)
	if len(tokens) > 0 {
		args = append(args, tokens)
		where = append(where, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM unnest($%d::text[]) AS t(tok) WHERE NOT (
			strpos(translate(lower(c.name || ' ' || c.city || ' ' || c.country || ' ' || c.slug || ' ' || array_to_string(c.email_domains, ' ')), '''’', ''), t.tok) > 0
			OR starts_with(c.acronym, t.tok)))`, len(args)))
	}
	if req.Country != "" {
		args = append(args, req.Country)
		where = append(where, fmt.Sprintf("c.country = $%d", len(args)))
	}
	if req.NeedsDetails {
		where = append(where, "NOT c.details_verified")
	}
	order = "(c.status = 'pending') DESC, c.name"
	if len(tokens) > 0 {
		orderArg = []any{strings.Join(tokens, "")}
		order = fmt.Sprintf(`(c.acronym = $%[1]d) DESC, starts_with(c.acronym, $%[1]d) DESC,
			starts_with(lower(c.name), $%[1]d) DESC, (c.status = 'pending') DESC, c.name`, len(args)+1)
	}
	return strings.Join(where, " AND "), args, order, orderArg
}

func (s *server) ListCampuses(ctx context.Context, req *identityv1.ListCampusesRequest) (*identityv1.ListCampusesResponse, error) {
	cond, args, order, orderArg := campusFilter(req)
	out := &identityv1.ListCampusesResponse{}
	limit, offset := req.Limit, int32(0)
	if req.Page > 0 {
		limit, offset = db.Page(req.Page, req.PageSize, 50, 200)
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM campuses c WHERE `+cond, args...).Scan(&out.Total); err != nil {
			return nil, grpcx.Internal(err)
		}
	} else if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(append(args, orderArg...), limit, offset)
	campuses, err := s.queryCampuses(ctx, fmt.Sprintf(`SELECT %s FROM campuses c WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		campusColumns, cond, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	out.Campuses = campuses
	if req.Page == 0 {
		out.Total = int64(len(campuses))
	}
	return out, nil
}

func (s *server) ListCampusCountries(ctx context.Context, req *identityv1.ListCampusCountriesRequest) (*identityv1.ListCampusCountriesResponse, error) {
	status := req.Status
	if status == "" {
		status = "active"
	}
	rows, err := s.db.Query(ctx, `SELECT country, count(*) FROM campuses WHERE ($1 = 'all' OR status = $1)
		GROUP BY country ORDER BY count(*) DESC, country`, status)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer rows.Close()
	out := &identityv1.ListCampusCountriesResponse{}
	for rows.Next() {
		cc := &identityv1.CountryCount{}
		if err := rows.Scan(&cc.Country, &cc.Campuses); err != nil {
			return nil, grpcx.Internal(err)
		}
		out.Countries = append(out.Countries, cc)
	}
	return out, rows.Err()
}

func (s *server) GetCampus(ctx context.Context, req *identityv1.GetCampusRequest) (*identityv1.Campus, error) {
	var c *identityv1.Campus
	var err error
	switch {
	case req.Id != "":
		c, err = scanCampus(s.db.QueryRow(ctx, `SELECT `+campusColumns+` FROM campuses c WHERE c.id = $1`, req.Id))
	case req.Slug != "":
		c, err = scanCampus(s.db.QueryRow(ctx, `SELECT `+campusColumns+` FROM campuses c WHERE c.slug = $1`, req.Slug))
	default:
		return nil, grpcx.Invalid("id or slug is required")
	}
	if err != nil {
		if db.IsNoRows(err) || strings.Contains(err.Error(), "invalid input syntax") {
			return nil, grpcx.NotFound("campus not found")
		}
		return nil, grpcx.Internal(err)
	}
	return c, nil
}

func (s *server) GetCampuses(ctx context.Context, req *identityv1.GetCampusesRequest) (*identityv1.ListCampusesResponse, error) {
	if len(req.Ids) == 0 {
		return &identityv1.ListCampusesResponse{}, nil
	}
	campuses, err := s.queryCampuses(ctx, `SELECT `+campusColumns+` FROM campuses c WHERE c.id = ANY($1::uuid[])`, req.Ids)
	if err != nil {
		return nil, err
	}
	return &identityv1.ListCampusesResponse{Campuses: campuses}, nil
}

// Personal mailbox providers can never identify a campus.
var publicMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "yahoo.co.in": true, "outlook.com": true,
	"hotmail.com": true, "live.com": true, "msn.com": true, "icloud.com": true, "me.com": true, "aol.com": true,
	"proton.me": true, "protonmail.com": true, "zoho.com": true, "yandex.com": true, "mail.com": true,
	"gmx.com": true, "gmx.de": true, "qq.com": true, "163.com": true, "rediffmail.com": true,
}

var (
	domainRe   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	slugStrip  = regexp.MustCompile(`[^a-z0-9]+`)
)

func normalizeDomains(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{} // non-nil: pgx writes nil as NULL, and the column is NOT NULL
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		d = strings.TrimPrefix(d, "@")
		if d == "" || seen[d] {
			continue
		}
		if !domainRe.MatchString(d) {
			return nil, grpcx.Invalid("%q is not a valid email domain", d)
		}
		if publicMailDomains[d] {
			return nil, grpcx.Invalid("%s is a personal email provider, not a campus domain", d)
		}
		seen[d] = true
		out = append(out, d)
	}
	if len(out) > 10 {
		return nil, grpcx.Invalid("at most 10 email domains")
	}
	return out, nil
}

func slugify(name string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "campus"
	}
	return s
}

type campusFields struct {
	name, country, city, timezone, currency, website string
	domains                                          []string
}

func validateCampus(name, country, city, timezone, currency, website string, domains []string) (*campusFields, error) {
	f := &campusFields{
		name:     strings.Join(strings.Fields(name), " "),
		country:  strings.TrimSpace(country),
		city:     strings.TrimSpace(city),
		timezone: strings.TrimSpace(timezone),
		currency: strings.ToUpper(strings.TrimSpace(currency)),
		website:  strings.TrimSpace(website),
	}
	switch {
	case len(f.name) < 3 || len(f.name) > 120:
		return nil, grpcx.Invalid("campus name must be 3–120 characters")
	case f.country == "" || len(f.country) > 80:
		return nil, grpcx.Invalid("country is required")
	case !currencyRe.MatchString(f.currency):
		return nil, grpcx.Invalid("currency must be a 3-letter ISO code such as INR or USD")
	case len(f.city) > 80:
		return nil, grpcx.Invalid("city is too long")
	}
	if _, err := time.LoadLocation(f.timezone); err != nil || f.timezone == "" || f.timezone == "Local" {
		return nil, grpcx.Invalid("timezone must be an IANA name such as Asia/Kolkata")
	}
	if f.website != "" {
		u, err := url.Parse(f.website)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.Contains(u.Host, ".") || len(f.website) > 200 {
			return nil, grpcx.Invalid("website must be a full URL such as https://www.university.edu")
		}
	}
	var err error
	if f.domains, err = normalizeDomains(domains); err != nil {
		return nil, err
	}
	return f, nil
}

// domainOwner returns the name of another live campus already using one of
// the domains, or "" when they are free.
func (s *server) domainOwner(ctx context.Context, domains []string, exceptID string) (string, error) {
	if len(domains) == 0 {
		return "", nil
	}
	var existing string
	err := s.db.QueryRow(ctx, `SELECT name FROM campuses WHERE status <> 'rejected' AND email_domains && $1
		AND ($2 = '' OR id::text <> $2) LIMIT 1`, domains, exceptID).Scan(&existing)
	if db.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", grpcx.Internal(err)
	}
	return existing, nil
}

func (s *server) RequestCampus(ctx context.Context, req *identityv1.RequestCampusRequest) (*identityv1.Campus, error) {
	f, err := validateCampus(req.Name, req.Country, req.City, req.Timezone, req.Currency, "", req.EmailDomains)
	if err != nil {
		return nil, err
	}
	name, country, currency, domains := f.name, f.country, f.currency, f.domains

	var listed string
	err = s.db.QueryRow(ctx, `SELECT name FROM campuses WHERE status <> 'rejected'
		AND (lower(name) = lower($1) OR lower(directory_name) = lower($1)) LIMIT 1`, name).Scan(&listed)
	if err == nil {
		return nil, grpcx.Exists("%s is already on Your Mate; pick it from the campus list", listed)
	}
	if !db.IsNoRows(err) {
		return nil, grpcx.Internal(err)
	}
	if owner, err := s.domainOwner(ctx, domains, ""); err != nil {
		return nil, err
	} else if owner != "" {
		return nil, grpcx.Exists("%s already uses one of these email domains; pick it from the list", owner)
	}
	if s.limitCampusRequests(ctx, req.ActorId) {
		return nil, grpcx.TooMany("you already have campus requests waiting for review")
	}

	var requestedBy any
	if req.ActorId != "" {
		requestedBy = req.ActorId
	}
	base := slugify(name)
	for i := 0; i < 20; i++ {
		slug := base
		if i > 0 {
			slug = fmt.Sprintf("%s-%d", base, i+1)
		}
		var id string
		err := s.db.QueryRow(ctx, `INSERT INTO campuses (slug, name, country, city, timezone, currency, email_domains, status, requested_by, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, 'request') RETURNING id`,
			slug, name, country, f.city, f.timezone, currency, domains, requestedBy).Scan(&id)
		if db.IsUniqueViolation(err, "campuses_slug_key") {
			continue
		}
		if err != nil {
			return nil, grpcx.Internal(err)
		}
		return s.GetCampus(ctx, &identityv1.GetCampusRequest{Id: id})
	}
	return nil, grpcx.Exists("a campus with this name already exists")
}

func (s *server) limitCampusRequests(ctx context.Context, actorID string) bool {
	if actorID == "" {
		return false
	}
	var n int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM campuses WHERE requested_by = $1 AND status = 'pending'`, actorID).Scan(&n)
	return n >= 3
}

func (s *server) ReviewCampus(ctx context.Context, req *identityv1.ReviewRequest) (*identityv1.Campus, error) {
	status := "rejected"
	if req.Approve {
		status = "active"
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Approving means an admin checked the details, so they count as verified.
	tag, err := tx.Exec(ctx, `UPDATE campuses SET status = $2, details_verified = details_verified OR $2 = 'active' WHERE id = $1`, req.Id, status)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, grpcx.NotFound("campus not found")
	}
	if req.Approve {
		// Members who already verified an email on the now-trusted domains
		// become campus-verified without doing anything.
		if _, err := tx.Exec(ctx, `UPDATE users u SET campus_email_verified = true, updated_at = now()
			FROM campuses c WHERE c.id = $1 AND u.campus_id = c.id AND u.email_verified
			AND domain_matches(u.email, c.email_domains)`, req.Id); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	c, err := s.GetCampus(ctx, &identityv1.GetCampusRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	if c.RequestedBy != "" {
		if u, err := s.loadUser(ctx, c.RequestedBy); err == nil {
			msg := fmt.Sprintf("Hi %s,\n\n%s is now live on Your Mate. Students there can sign up and verify with their campus email.\n", u.FirstName, c.Name)
			if !req.Approve {
				msg = fmt.Sprintf("Hi %s,\n\nWe couldn't add %s to Your Mate.%s\n\nReply to this email if you think this is a mistake.\n", u.FirstName, c.Name, reasonLine(req.Reason))
			}
			s.mail.SendAsync(u.Email, "Your campus request: "+c.Name, msg)
		}
	}
	return c, nil
}

func reasonLine(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return "\n\nReason: " + strings.TrimSpace(reason)
}

// UpdateCampus lets an admin correct a campus and confirm its email domains.
// Adding a domain to an active campus campus-verifies members who already
// confirmed an email on it; removing one never un-verifies anybody.
func (s *server) UpdateCampus(ctx context.Context, req *identityv1.UpdateCampusRequest) (*identityv1.Campus, error) {
	f, err := validateCampus(req.Name, req.Country, req.City, req.Timezone, req.Currency, req.Website, req.EmailDomains)
	if err != nil {
		return nil, err
	}
	current, err := s.GetCampus(ctx, &identityv1.GetCampusRequest{Id: req.Id})
	if err != nil {
		return nil, err
	}
	// Tutors on the campus priced their sessions in its currency.
	if f.currency != current.Currency && current.MemberCount > 0 {
		return nil, grpcx.Precondition("can't change the currency of a campus that already has members")
	}
	if owner, err := s.domainOwner(ctx, f.domains, req.Id); err != nil {
		return nil, err
	} else if owner != "" {
		return nil, grpcx.Exists("%s already uses one of these email domains", owner)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, grpcx.Internal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `UPDATE campuses SET name = $2, country = $3, city = $4, timezone = $5, currency = $6,
		website = $7, email_domains = $8, details_verified = $9 WHERE id = $1`,
		req.Id, f.name, f.country, f.city, f.timezone, f.currency, f.website, f.domains, req.DetailsVerified); err != nil {
		return nil, grpcx.Internal(err)
	}
	if current.Status == "active" && len(f.domains) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE users u SET campus_email_verified = true, updated_at = now()
			FROM campuses c WHERE c.id = $1 AND u.campus_id = c.id AND u.email_verified AND NOT u.campus_email_verified
			AND domain_matches(u.email, c.email_domains)`, req.Id); err != nil {
			return nil, grpcx.Internal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, grpcx.Internal(err)
	}
	return s.GetCampus(ctx, &identityv1.GetCampusRequest{Id: req.Id})
}
