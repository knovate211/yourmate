//go:build e2e

// Package e2e drives the running stack through GraphQL the way the web app
// does: real signups with OTPs read from Mailpit, ID verification, profiles,
// bookings, messages, reviews, moderation and the admin surface.
//
//	docker compose up -d --build   (from the repo root)
//	make e2e                       (from backend/)
package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

var (
	gatewayURL = env("E2E_GATEWAY", "http://localhost:8095")
	mailpitURL = env("E2E_MAILPIT", "http://localhost:8027")
	// Credentials come from the stack's .env (make e2e loads it); nothing
	// secret is written here.
	dbURL      = "postgres://yourmate:" + os.Getenv("POSTGRES_PASSWORD") + "@localhost:5437/yourmate?sslmode=disable"
	adminEmail = os.Getenv("ADMIN_EMAIL")
	adminPass  = os.Getenv("ADMIN_PASSWORD")
	runID      = fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000_000)
)

// TestMain refuses to run without credentials and deletes everything the
// tests created afterwards, so runs never leave test people in the database.
func TestMain(m *testing.M) {
	for _, k := range []string{"POSTGRES_PASSWORD", "ADMIN_EMAIL", "ADMIN_PASSWORD"} {
		if os.Getenv(k) == "" {
			fmt.Fprintf(os.Stderr, "e2e: %s is not set; run `make e2e` so the stack's .env is loaded\n", k)
			os.Exit(2)
		}
	}
	code := m.Run()
	if err := cleanup(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e cleanup:", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// cleanup removes the accounts, campuses and everything else this run made.
// Every test email and campus name contains runID.
func cleanup() error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	pattern := "%" + runID + "%"
	steps := []string{
		`CREATE TEMP TABLE gone ON COMMIT DROP AS SELECT id FROM identity.users WHERE email LIKE $1`,
		`CREATE TEMP TABLE gone_bookings ON COMMIT DROP AS SELECT id FROM marketplace.bookings
			WHERE student_id IN (SELECT id FROM gone) OR provider_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.reviews WHERE booking_id IN (SELECT id FROM gone_bookings) OR reviewer_id IN (SELECT id FROM gone)`,
		`CREATE TEMP TABLE gone_files ON COMMIT DROP AS SELECT unnest(ARRAY[file_key, feedback_file_key]) AS key FROM marketplace.resume_reviews
			WHERE student_id IN (SELECT id FROM gone) OR reviewer_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.resume_reviews WHERE student_id IN (SELECT id FROM gone) OR reviewer_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.questions WHERE author_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.interview_experiences WHERE author_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.messages WHERE booking_id IN (SELECT id FROM gone_bookings)`,
		`DELETE FROM marketplace.bookings WHERE id IN (SELECT id FROM gone_bookings)`,
		`DELETE FROM marketplace.provider_profiles WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.availability WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.notifications WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM marketplace.blocks WHERE user_id IN (SELECT id FROM gone) OR blocked_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.conversations WHERE user_a IN (SELECT id FROM gone) OR user_b IN (SELECT id FROM gone)`,
		`DELETE FROM community.communities WHERE owner_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_posts WHERE author_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_replies WHERE author_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_post_likes WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_members WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_join_requests WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.community_bans WHERE user_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.posts WHERE author_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.reports WHERE reporter_id IN (SELECT id FROM gone)`,
		`DELETE FROM community.inquiries WHERE message LIKE $1`,
		`UPDATE community.community_posts p SET
			like_count = (SELECT count(*) FROM community.community_post_likes l WHERE l.post_id = p.id),
			reply_count = (SELECT count(*) FROM community.community_replies r WHERE r.post_id = p.id AND NOT r.removed)`,
		`UPDATE community.communities c SET
			member_count = (SELECT count(*) FROM community.community_members m WHERE m.community_id = c.id),
			post_count = (SELECT count(*) FROM community.community_posts p WHERE p.community_id = c.id AND NOT p.removed)`,
		`DELETE FROM identity.login_failures WHERE email LIKE $1`,
		`DELETE FROM identity.users WHERE id IN (SELECT id FROM gone)`,
		`DELETE FROM identity.campuses c WHERE c.name LIKE $1 AND NOT EXISTS (SELECT 1 FROM identity.users u WHERE u.campus_id = c.id)`,
		`DELETE FROM community.communities WHERE campus_id IS NOT NULL AND campus_id NOT IN (SELECT id FROM identity.campuses)`,
	}
	var files []string
	for i, q := range steps {
		if i > 0 && strings.HasPrefix(steps[i-1], "CREATE TEMP TABLE gone_files") {
			rows, err := tx.Query(ctx, `SELECT key FROM gone_files WHERE key <> ''`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var k string
				if rows.Scan(&k) == nil {
					files = append(files, k)
				}
			}
			rows.Close()
		}
		var err error
		if strings.Contains(q, "$1") {
			_, err = tx.Exec(ctx, q, pattern)
		} else {
			_, err = tx.Exec(ctx, q)
		}
		if err != nil {
			return fmt.Errorf("%w\n%s", err, q)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	removeUploads(files)
	return nil
}

// removeUploads deletes the run's resume uploads from the gateway's volume.
// The rows pointing at them are gone, so nothing else would ever purge them.
// Best effort: a stack run some other way just keeps the (tiny) files.
func removeUploads(keys []string) {
	if len(keys) == 0 {
		return
	}
	args := []string{"compose", "exec", "-T", "gateway", "rm", "-f"}
	for _, k := range keys {
		args = append(args, "/data/uploads/"+k)
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = ".." // the repository root, where docker-compose.yml is
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: could not remove %d uploads: %v %s\n", len(keys), err, out)
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// ── GraphQL client ──────────────────────────────────────────────────────────

type client struct {
	t     *testing.T
	http  *http.Client
	token string
	ip    string // sent as X-Forwarded-For
}

func newClient(t *testing.T) *client {
	jar, _ := cookiejar.New(nil)
	// Each client gets its own random IP, as separate students would, so the
	// gateway's per-IP signup and login limits never pile up across tests.
	ip := fmt.Sprintf("10.%d.%d.%d", mrand.IntN(256), mrand.IntN(256), 1+mrand.IntN(254))
	return &client{t: t, http: &http.Client{Jar: jar, Timeout: 20 * time.Second}, ip: ip}
}

type gqlError struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

func (c *client) do(req *http.Request) gqlResponse {
	c.t.Helper()
	req.Header.Set("X-Forwarded-For", c.ip)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var out gqlResponse
	if err := json.Unmarshal(body, &out); err != nil {
		c.t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// try runs an operation and returns its errors without failing.
func (c *client) try(query string, vars map[string]any, into any) []gqlError {
	c.t.Helper()
	payload, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/graphql", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	out := c.do(req)
	if into != nil && len(out.Data) > 0 && string(out.Data) != "null" {
		if err := json.Unmarshal(out.Data, into); err != nil {
			c.t.Fatalf("decode data: %v", err)
		}
	}
	return out.Errors
}

// q runs an operation that must succeed.
func (c *client) q(query string, vars map[string]any, into any) {
	c.t.Helper()
	if errs := c.try(query, vars, into); len(errs) > 0 {
		c.t.Fatalf("graphql errors: %+v\nquery: %s", errs, query)
	}
}

// upload sends a GraphQL multipart request with one file variable.
func (c *client) upload(query, variable string, file []byte, filename string, into any) []gqlError {
	c.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	ops, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]any{variable: nil}})
	_ = w.WriteField("operations", string(ops))
	_ = w.WriteField("map", fmt.Sprintf(`{"0":["variables.%s"]}`, variable))
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="0"; filename="%s"`, filename))
	h.Set("Content-Type", "application/octet-stream")
	part, _ := w.CreatePart(h)
	_, _ = part.Write(file)
	_ = w.Close()
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/graphql", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Apollo-Require-Preflight", "true")
	out := c.do(req)
	if into != nil && len(out.Data) > 0 {
		_ = json.Unmarshal(out.Data, into)
	}
	return out.Errors
}

func code(errs []gqlError) string {
	if len(errs) == 0 {
		return ""
	}
	c, _ := errs[0].Extensions["code"].(string)
	if r, ok := errs[0].Extensions["reason"].(string); ok {
		return c + "/" + r
	}
	return c
}

// ── Mailpit ─────────────────────────────────────────────────────────────────

var otpRe = regexp.MustCompile(`\b(\d{6})\b`)

// latestCode waits for the newest email to addr and returns the 6-digit code in it.
func latestCode(t *testing.T, addr, subjectContains string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(mailpitURL + "/api/v1/search?query=" + url.QueryEscape(`to:"`+addr+`"`))
		if err == nil {
			var found struct {
				Messages []struct {
					ID      string `json:"ID"`
					Subject string `json:"Subject"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(res.Body).Decode(&found)
			res.Body.Close()
			for _, m := range found.Messages { // newest first
				if !strings.Contains(m.Subject, subjectContains) {
					continue
				}
				r, err := http.Get(mailpitURL + "/api/v1/message/" + m.ID)
				if err != nil {
					break
				}
				var msg struct{ Text string }
				_ = json.NewDecoder(r.Body).Decode(&msg)
				r.Body.Close()
				if c := otpRe.FindStringSubmatch(msg.Text); c != nil {
					return c[1]
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("no %q email with a code for %s", subjectContains, addr)
	return ""
}

// ── Helpers ─────────────────────────────────────────────────────────────────

type user struct {
	*client
	ID    string
	Email string
}

const authFields = `accessToken user { id email campusVerified idVerified isAdmin campus { id name } }`

type authPayload struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		ID             string `json:"id"`
		Email          string `json:"email"`
		CampusVerified bool   `json:"campusVerified"`
		IDVerified     bool   `json:"idVerified"`
		IsAdmin        bool   `json:"isAdmin"`
	} `json:"user"`
}

func campusID(t *testing.T, c *client, slug string) string {
	var out struct{ Campus struct{ ID string } }
	c.q(`query($s: String) { campus(slug: $s) { id } }`, map[string]any{"s": slug}, &out)
	if out.Campus.ID == "" {
		t.Fatalf("campus %s not seeded", slug)
	}
	return out.Campus.ID
}

func signup(t *testing.T, email, first, campus string) *user {
	t.Helper()
	c := newClient(t)
	c.q(`mutation($i: RegisterInput!) { register(input: $i) { email } }`, map[string]any{"i": map[string]any{
		"email": email, "password": "correct-horse-1", "firstName": first, "lastName": "Test", "campusId": campus, "phone": "+91 90000 00000",
	}}, nil)
	otp := latestCode(t, email, "verification code")
	var out struct{ VerifyEmail authPayload }
	c.q(`mutation($e: String!, $c: String!) { verifyEmail(email: $e, code: $c) { `+authFields+` } }`, map[string]any{"e": email, "c": otp}, &out)
	c.token = out.VerifyEmail.AccessToken
	return &user{client: c, ID: out.VerifyEmail.User.ID, Email: email}
}

func login(t *testing.T, email, password string) *user {
	t.Helper()
	c := newClient(t)
	var out struct{ Login authPayload }
	c.q(`mutation($e: String!, $p: String!) { login(email: $e, password: $p) { `+authFields+` } }`, map[string]any{"e": email, "p": password}, &out)
	c.token = out.Login.AccessToken
	return &user{client: c, ID: out.Login.User.ID, Email: email}
}

// A valid 1×1 PNG.
var tinyPNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01,
	0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78,
	0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82}

// ── The journey ─────────────────────────────────────────────────────────────

func TestMarketplaceJourney(t *testing.T) {
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	admin := login(t, adminEmail, adminPass)

	// 1. A tutor signs up with a campus email and is campus-verified at once.
	tutor := signup(t, "tutor"+runID+"@lpu.in", "Tutor", lpu)
	var me struct {
		Me struct{ CampusVerified, IDVerified bool }
	}
	tutor.q(`{ me { campusVerified idVerified } }`, nil, &me)
	if !me.Me.CampusVerified || me.Me.IDVerified {
		t.Fatalf("campus email should campus-verify but not ID-verify: %+v", me.Me)
	}

	// 2. Publishing needs a verified student ID.
	profileInput := map[string]any{
		"kind": "TUTOR", "headline": "Final-year CSE, DSA and Java mentor", "rateMinor": 30000,
		"bio":   "I scored top grades in DSA and Java and have helped juniors prepare for exams and placements for two years.",
		"modes": []string{"ONLINE", "IN_PERSON"}, "languages": []string{"English", "Hindi"}, "publish": true,
	}
	var subjects struct{ Subjects []struct{ ID, Name string } }
	anon.q(`{ subjects { id name } }`, nil, &subjects)
	var dsa string
	for _, s := range subjects.Subjects {
		if s.Name == "Data Structures & Algorithms" {
			dsa = s.ID
		}
	}
	profileInput["subjectIds"] = []string{dsa}
	errs := tutor.try(`mutation($i: ProviderProfileInput!) { saveProviderProfile(input: $i) { id } }`, map[string]any{"i": profileInput}, nil)
	if code(errs) != "FAILED_PRECONDITION/ID_VERIFICATION_REQUIRED" {
		t.Fatalf("publish without ID: got %q %+v", code(errs), errs)
	}

	// 3. ID upload → admin sees a signed document link → approves → file is deleted.
	if errs := tutor.upload(`mutation($d: Upload!) { submitVerification(document: $d) { status } }`, "d", []byte("not an image"), "id.txt", nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("non-image upload: got %q", code(errs))
	}
	if errs := tutor.upload(`mutation($d: Upload!) { submitVerification(document: $d) { status } }`, "d", tinyPNG, "id.png", nil); len(errs) > 0 {
		t.Fatalf("upload id: %+v", errs)
	}
	var queue struct {
		AdminVerifications struct {
			Items []struct {
				ID          string
				DocumentURL string
				User        struct{ ID string }
			}
		}
	}
	admin.q(`{ adminVerifications(status: PENDING, pageSize: 100) { items { id documentUrl user { id } } } }`, nil, &queue)
	var verID, docURL string
	for _, v := range queue.AdminVerifications.Items {
		if v.User.ID == tutor.ID {
			verID, docURL = v.ID, v.DocumentURL
		}
	}
	if verID == "" || docURL == "" {
		t.Fatal("verification not in admin queue or has no document link")
	}
	if res, _ := http.Get(gatewayURL + docURL); res == nil || res.StatusCode != 200 {
		t.Fatalf("signed document link should work")
	}
	unsigned := strings.Split(docURL, "?")[0]
	if res, _ := http.Get(gatewayURL + unsigned); res == nil || res.StatusCode != 403 {
		t.Fatalf("unsigned document link must be refused")
	}
	if errs := tutor.try(`{ adminVerifications { total } }`, nil, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("non-admin must not list verifications, got %q", code(errs))
	}
	admin.q(`mutation($id: ID!) { reviewVerification(id: $id, approve: true) { status } }`, map[string]any{"id": verID}, nil)
	if res, _ := http.Get(gatewayURL + docURL); res == nil || res.StatusCode != 404 {
		t.Fatalf("document should be deleted after review")
	}

	// 4. Now the profile publishes; availability every day 06:00–22:00 campus time.
	var saved struct{ SaveProviderProfile struct{ ID, Status string } }
	tutor.q(`mutation($i: ProviderProfileInput!) { saveProviderProfile(input: $i) { id status } }`, map[string]any{"i": profileInput}, &saved)
	if saved.SaveProviderProfile.Status != "ACTIVE" {
		t.Fatalf("profile should be active, got %s", saved.SaveProviderProfile.Status)
	}
	providerID := saved.SaveProviderProfile.ID
	var windows []map[string]any
	for d := 0; d < 7; d++ {
		windows = append(windows, map[string]any{"weekday": d, "startMinute": 360, "endMinute": 1320})
	}
	tutor.q(`mutation($w: [AvailabilityInput!]!) { setAvailability(windows: $w) { timezone } }`, map[string]any{"w": windows}, nil)

	// 5. A student with a personal email is not campus-verified and can't book.
	outsider := signup(t, "outsider"+runID+"@example.org", "Outsider", lpu)
	var slots struct {
		OpenSlots struct {
			Timezone string
			Slots    []struct{ StartAt string }
		}
	}
	anon.q(`query($p: ID!) { openSlots(providerId: $p, durationMinutes: 60) { timezone slots { startAt } } }`, map[string]any{"p": providerID}, &slots)
	if slots.OpenSlots.Timezone != "Asia/Kolkata" || len(slots.OpenSlots.Slots) < 20 {
		t.Fatalf("expected many open slots in Asia/Kolkata, got %d in %s", len(slots.OpenSlots.Slots), slots.OpenSlots.Timezone)
	}
	slot := slots.OpenSlots.Slots[3].StartAt
	bookingInput := map[string]any{"providerId": providerID, "startAt": slot, "durationMinutes": 60, "mode": "ONLINE", "subjectId": dsa, "notes": "Graphs and DP"}
	if errs := outsider.try(`mutation($i: BookingInput!) { requestBooking(input: $i) { id } }`, map[string]any{"i": bookingInput}, nil); code(errs) != "FAILED_PRECONDITION/VERIFICATION_REQUIRED" {
		t.Fatalf("unverified booking: got %q", code(errs))
	}

	// 6. A verified student finds the tutor and books.
	student := signup(t, "student"+runID+"@lpu.in", "Student", lpu)
	var search struct {
		Providers struct {
			Total int
			Items []struct {
				ID   string
				User struct{ Email *string }
				Rate struct{ AmountMinor int }
			}
		}
	}
	anon.q(`query($c: ID) { providers(filter: {kind: TUTOR, campusId: $c, query: "Algorithms"}, pageSize: 50) { total items { id rate { amountMinor } user { email } } } }`,
		map[string]any{"c": lpu}, &search)
	found := false
	for _, p := range search.Providers.Items {
		if p.ID == providerID {
			found = true
			if p.User.Email != nil {
				t.Fatal("anonymous search must not expose emails")
			}
		}
	}
	if !found {
		t.Fatal("tutor not found by subject search")
	}

	var booked struct {
		RequestBooking struct {
			ID     string
			Status string
			Price  struct{ AmountMinor int }
		}
	}
	student.q(`mutation($i: BookingInput!) { requestBooking(input: $i) { id status price { amountMinor } } }`, map[string]any{"i": bookingInput}, &booked)
	bookingID := booked.RequestBooking.ID
	if booked.RequestBooking.Status != "REQUESTED" || booked.RequestBooking.Price.AmountMinor != 30000 {
		t.Fatalf("unexpected booking: %+v", booked.RequestBooking)
	}
	// The same slot is no longer offered, and a second request for it fails.
	rival := signup(t, "rival"+runID+"@lpu.in", "Rival", lpu)
	if errs := rival.try(`mutation($i: BookingInput!) { requestBooking(input: $i) { id } }`, map[string]any{"i": bookingInput}, nil); code(errs) != "FAILED_PRECONDITION/SLOT_UNAVAILABLE" {
		t.Fatalf("double booking: got %q", code(errs))
	}
	if errs := tutor.try(`mutation($i: BookingInput!) { requestBooking(input: $i) { id } }`, map[string]any{"i": bookingInput}, nil); code(errs) == "" {
		t.Fatal("a tutor must not book themselves")
	}

	// 7. The tutor is notified, accepts with a meeting link; contact details appear.
	var notes struct{ Notifications struct{ UnreadCount int } }
	tutor.q(`{ notifications { unreadCount } }`, nil, &notes)
	if notes.Notifications.UnreadCount == 0 {
		t.Fatal("tutor should have a notification")
	}
	var before struct {
		Booking struct{ Contact *struct{ Email string } }
	}
	student.q(`query($id: ID!) { booking(id: $id) { contact { email } } }`, map[string]any{"id": bookingID}, &before)
	if before.Booking.Contact != nil {
		t.Fatal("contact must stay hidden until accepted")
	}
	if errs := student.try(`mutation($i: TransitionInput!) { transitionBooking(input: $i) { status } }`,
		map[string]any{"i": map[string]any{"bookingId": bookingID, "action": "ACCEPT"}}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("student must not accept, got %q", code(errs))
	}
	tutor.q(`mutation($i: TransitionInput!) { transitionBooking(input: $i) { status } }`,
		map[string]any{"i": map[string]any{"bookingId": bookingID, "action": "ACCEPT", "meetingLink": "https://meet.example.com/abc"}}, nil)
	var after struct {
		Booking struct {
			Status      string
			MeetingLink *string
			Contact     *struct{ Email string }
			MyRole      string
		}
	}
	student.q(`query($id: ID!) { booking(id: $id) { status meetingLink myRole contact { email } } }`, map[string]any{"id": bookingID}, &after)
	if after.Booking.Status != "ACCEPTED" || after.Booking.Contact == nil || after.Booking.Contact.Email != tutor.Email ||
		after.Booking.MeetingLink == nil || after.Booking.MyRole != "STUDENT" {
		t.Fatalf("accepted booking view wrong: %+v", after.Booking)
	}
	if errs := rival.try(`query($id: ID!) { booking(id: $id) { id } }`, map[string]any{"id": bookingID}, nil); len(errs) > 0 {
		t.Fatalf("outsider booking lookup should be null, not an error: %+v", errs)
	}

	// 8. Messages both ways.
	student.q(`mutation($b: ID!) { sendMessage(bookingId: $b, body: "Can we start with graphs?") { id } }`, map[string]any{"b": bookingID}, nil)
	tutor.q(`mutation($b: ID!) { sendMessage(bookingId: $b, body: "Sure, bring your questions.") { id } }`, map[string]any{"b": bookingID}, nil)
	var thread struct {
		Booking struct {
			Messages []struct {
				Body string
				Mine bool
			}
		}
	}
	student.q(`query($id: ID!) { booking(id: $id) { messages { body mine } } }`, map[string]any{"id": bookingID}, &thread)
	if len(thread.Booking.Messages) != 2 || !thread.Booking.Messages[0].Mine || thread.Booking.Messages[1].Mine {
		t.Fatalf("thread wrong: %+v", thread.Booking.Messages)
	}

	// 9. Completing before the start time is refused; move the session into
	// the past (as if time passed), then complete and review.
	if errs := tutor.try(`mutation($i: TransitionInput!) { transitionBooking(input: $i) { status } }`,
		map[string]any{"i": map[string]any{"bookingId": bookingID, "action": "COMPLETE"}}, nil); code(errs) != "FAILED_PRECONDITION" {
		t.Fatalf("early completion: got %q", code(errs))
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE marketplace.bookings SET start_at = now() - interval '2 hours', end_at = now() - interval '1 hour' WHERE id = $1`, bookingID); err != nil {
		t.Fatal(err)
	}
	tutor.q(`mutation($i: TransitionInput!) { transitionBooking(input: $i) { status } }`,
		map[string]any{"i": map[string]any{"bookingId": bookingID, "action": "COMPLETE"}}, nil)
	if errs := rival.try(`mutation($b: ID!) { leaveReview(bookingId: $b, rating: 1) { id } }`, map[string]any{"b": bookingID}, nil); code(errs) == "" {
		t.Fatal("only the student may review")
	}
	student.q(`mutation($b: ID!) { leaveReview(bookingId: $b, rating: 5, comment: "Clear and patient.") { id } }`, map[string]any{"b": bookingID}, nil)
	if errs := student.try(`mutation($b: ID!) { leaveReview(bookingId: $b, rating: 4) { id } }`, map[string]any{"b": bookingID}, nil); code(errs) != "CONFLICT" {
		t.Fatalf("second review: got %q", code(errs))
	}
	var prof struct {
		Provider struct {
			RatingAvg         float64
			RatingCount       int
			SessionsCompleted int
			Reviews           struct{ Total int }
		}
	}
	anon.q(`query($id: ID!) { provider(id: $id) { ratingAvg ratingCount sessionsCompleted reviews { total } } }`, map[string]any{"id": providerID}, &prof)
	if prof.Provider.RatingAvg != 5 || prof.Provider.RatingCount != 1 || prof.Provider.SessionsCompleted != 1 {
		t.Fatalf("profile stats wrong: %+v", prof.Provider)
	}

	// 10. Blocking hides the tutor from the student's search.
	student.q(`mutation($u: ID!) { blockUser(userId: $u) }`, map[string]any{"u": tutor.ID}, nil)
	var hidden struct {
		Providers struct{ Items []struct{ ID string } }
	}
	student.q(`query($c: ID) { providers(filter: {kind: TUTOR, campusId: $c}, pageSize: 50) { items { id } } }`, map[string]any{"c": lpu}, &hidden)
	for _, p := range hidden.Providers.Items {
		if p.ID == providerID {
			t.Fatal("blocked tutor still visible")
		}
	}
	student.q(`mutation($u: ID!) { unblockUser(userId: $u) }`, map[string]any{"u": tutor.ID}, nil)

	// 11. Feed: posts are moderated before they appear.
	var post struct{ CreatePost struct{ ID, Status string } }
	student.q(`mutation($i: PostInput!) { createPost(input: $i) { id status } }`, map[string]any{"i": map[string]any{
		"type": "TIP", "title": "Quiet study spots", "body": "The library's third floor is silent after 6pm and has plenty of outlets."}}, &post)
	if post.CreatePost.Status != "PENDING" {
		t.Fatalf("post should be pending, got %s", post.CreatePost.Status)
	}
	if errs := outsider.try(`mutation($i: PostInput!) { createPost(input: $i) { id } }`, map[string]any{"i": map[string]any{
		"type": "TIP", "title": "Hello there", "body": "Unverified users may not post to the feed."}}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("unverified post: got %q", code(errs))
	}
	admin.q(`mutation($id: ID!) { reviewPost(id: $id, approve: true) { status } }`, map[string]any{"id": post.CreatePost.ID}, nil)
	var feed struct {
		Posts struct{ Items []struct{ ID string } }
	}
	anon.q(`query($c: ID) { posts(campusId: $c, pageSize: 50) { items { id } } }`, map[string]any{"c": lpu}, &feed)
	if len(feed.Posts.Items) == 0 || feed.Posts.Items[0].ID != post.CreatePost.ID {
		t.Fatal("approved post missing from feed")
	}

	// 12. Reports reach the admin queue with a readable label.
	rival.q(`mutation($i: ReportInput!) { report(input: $i) }`, map[string]any{"i": map[string]any{
		"targetType": "PROVIDER", "targetId": providerID, "reason": "SPAM", "details": "e2e check"}}, nil)
	var reports struct {
		AdminReports struct {
			Items []struct{ ID, TargetID, TargetLabel string }
		}
	}
	admin.q(`{ adminReports(status: OPEN, pageSize: 100) { items { id targetId targetLabel } } }`, nil, &reports)
	var reportID string
	for _, r := range reports.AdminReports.Items {
		if r.TargetID == providerID {
			reportID = r.ID
			if !strings.Contains(r.TargetLabel, "DSA") {
				t.Fatalf("report label should describe the profile, got %q", r.TargetLabel)
			}
		}
	}
	if reportID == "" {
		t.Fatal("report missing from admin queue")
	}
	admin.q(`mutation($id: ID!) { resolveReport(id: $id, status: DISMISSED, note: "test") { status } }`, map[string]any{"id": reportID}, nil)

	var stats struct {
		AdminStats struct{ Users, BookingsCompleted, ActiveTutors int }
	}
	admin.q(`{ adminStats { users bookingsCompleted activeTutors } }`, nil, &stats)
	if stats.AdminStats.BookingsCompleted < 1 || stats.AdminStats.ActiveTutors < 1 || stats.AdminStats.Users < 5 {
		t.Fatalf("stats look wrong: %+v", stats.AdminStats)
	}
}

func TestSessionsAndCampuses(t *testing.T) {
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	u := signup(t, "session"+runID+"@lpu.in", "Session", lpu)

	// The refresh cookie rotates; reusing an old one ends every session.
	cookieURL, _ := url.Parse(gatewayURL + "/graphql")
	first := u.http.Jar.Cookies(cookieURL)
	if len(first) == 0 {
		t.Fatal("no refresh cookie after verification")
	}
	var r1 struct{ RefreshSession *authPayload }
	u.q(`mutation { refreshSession { accessToken } }`, nil, &r1)
	if r1.RefreshSession == nil || r1.RefreshSession.AccessToken == "" {
		t.Fatal("refresh failed")
	}
	thief := newClient(t)
	thief.http.Jar.SetCookies(cookieURL, first)
	var stolen struct{ RefreshSession *authPayload }
	thief.q(`mutation { refreshSession { accessToken } }`, nil, &stolen)
	if stolen.RefreshSession != nil {
		t.Fatal("reused refresh token must not work")
	}
	var r2 struct{ RefreshSession *authPayload }
	u.q(`mutation { refreshSession { accessToken } }`, nil, &r2)
	if r2.RefreshSession != nil {
		t.Fatal("token reuse should revoke the whole family")
	}

	// Wrong password, and a tampered access token.
	bad := newClient(t)
	if errs := bad.try(`mutation { login(email: "`+u.Email+`", password: "wrong-password") { accessToken } }`, nil, nil); code(errs) != "UNAUTHENTICATED" {
		t.Fatalf("bad password: got %q", code(errs))
	}
	bad.token = "not-a-jwt"
	if errs := bad.try(`{ me { id } }`, nil, nil); code(errs) != "TOKEN_EXPIRED" {
		t.Fatalf("bad token: got %q", code(errs))
	}

	// Password reset by emailed code.
	anon.q(`mutation($e: String!) { requestPasswordReset(email: $e) }`, map[string]any{"e": u.Email}, nil)
	resetCode := latestCode(t, u.Email, "Reset")
	anon.q(`mutation($e: String!, $c: String!) { resetPassword(email: $e, code: $c, newPassword: "a-brand-new-pass") }`,
		map[string]any{"e": u.Email, "c": resetCode}, nil)
	login(t, u.Email, "a-brand-new-pass")

	// Anyone can request a campus, but personal mail domains are refused.
	req := map[string]any{"name": "E2E University " + runID, "country": "Kenya", "city": "Nairobi", "timezone": "Africa/Nairobi",
		"currency": "KES", "emailDomains": []string{"gmail.com"}}
	if errs := anon.try(`mutation($i: RequestCampusInput!) { requestCampus(input: $i) { id } }`, map[string]any{"i": req}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("public domain campus: got %q", code(errs))
	}
	domain := "e2e" + runID + ".ac.ke"
	req["emailDomains"] = []string{domain}
	var campus struct{ RequestCampus struct{ ID, Status string } }
	anon.q(`mutation($i: RequestCampusInput!) { requestCampus(input: $i) { id status } }`, map[string]any{"i": req}, &campus)
	if campus.RequestCampus.Status != "PENDING" {
		t.Fatalf("new campus should be pending")
	}

	// A member who signs up before approval becomes campus-verified when the
	// admin approves the campus.
	early := signup(t, "early@"+domain, "Early", campus.RequestCampus.ID)
	var me struct{ Me struct{ CampusVerified bool } }
	early.q(`{ me { campusVerified } }`, nil, &me)
	if me.Me.CampusVerified {
		t.Fatal("pending campus domains must not verify anyone")
	}
	admin := login(t, adminEmail, adminPass)
	admin.q(`mutation($id: ID!) { reviewCampus(id: $id, approve: true) { status } }`, map[string]any{"id": campus.RequestCampus.ID}, nil)
	early.q(`{ me { campusVerified } }`, nil, &me)
	if !me.Me.CampusVerified {
		t.Fatal("approval should campus-verify existing members on the domain")
	}

	// The imported campus directory is searchable by name, initials and
	// apostrophe-free spellings, and paginates per country.
	for query, want := range map[string]string{
		"iit bombay":    "Indian Institute of Technology Bombay",
		"nit warangal":  "National Institute of Technology Warangal",
		"kings college": "King's College London",
		"tum":           "Technical University of Munich",
	} {
		var found struct{ Campuses []struct{ Name string } }
		anon.q(`query($q: String) { campuses(query: $q, limit: 5) { name } }`, map[string]any{"q": query}, &found)
		if len(found.Campuses) == 0 || found.Campuses[0].Name != want {
			t.Errorf("search %q: got %+v, want %q first", query, found.Campuses, want)
		}
	}
	var dir struct {
		CampusDirectory struct {
			Total int
			Items []struct{ Country string }
		}
	}
	anon.q(`{ campusDirectory(country: "Germany", page: 2, pageSize: 50) { total items { country } } }`, nil, &dir)
	if dir.CampusDirectory.Total < 131 || len(dir.CampusDirectory.Items) != 50 || dir.CampusDirectory.Items[0].Country != "Germany" {
		t.Fatalf("directory page wrong: total %d, %d items", dir.CampusDirectory.Total, len(dir.CampusDirectory.Items))
	}

	// A campus without student email domains can be requested (students
	// there verify with an ID card).
	noDomain := map[string]any{"name": "E2E Night College " + runID, "country": "Kenya", "timezone": "Africa/Nairobi", "currency": "KES", "emailDomains": []string{}}
	anon.q(`mutation($i: RequestCampusInput!) { requestCampus(input: $i) { id } }`, map[string]any{"i": noDomain}, nil)

	// A listed campus can't be requested again.
	dup := map[string]any{"name": "Imperial College London", "country": "United Kingdom", "timezone": "Europe/London", "currency": "GBP", "emailDomains": []string{}}
	if errs := anon.try(`mutation($i: RequestCampusInput!) { requestCampus(input: $i) { id } }`, map[string]any{"i": dup}, nil); code(errs) != "CONFLICT" {
		t.Fatalf("duplicate campus request: got %q", code(errs))
	}

	// Directory campuses start without email domains (students verify by ID);
	// once an admin adds the real domain, existing members are verified.
	var imperial struct {
		Campuses []struct {
			ID           string
			EmailDomains []string
		}
	}
	anon.q(`{ campuses(query: "imperial college london", limit: 1) { id emailDomains } }`, nil, &imperial)
	imperialID := imperial.Campuses[0].ID
	// Leave the real campus as the directory had it, even if a check fails.
	t.Cleanup(func() {
		admin.q(`mutation($id: ID!, $i: UpdateCampusInput!) { updateCampus(id: $id, input: $i) { id } }`, map[string]any{"id": imperialID, "i": map[string]any{
			"name": "Imperial College London", "country": "United Kingdom", "city": "London", "timezone": "Europe/London",
			"currency": "GBP", "website": "", "emailDomains": imperial.Campuses[0].EmailDomains, "detailsVerified": false}}, nil)
	})
	member := signup(t, "member"+runID+"@e2e-imperial-"+runID+".ac.uk", "Member", imperialID)
	member.q(`{ me { campusVerified } }`, nil, &me)
	if me.Me.CampusVerified {
		t.Fatal("unconfirmed directory campus must not verify by email")
	}
	update := map[string]any{"name": "Imperial College London", "country": "United Kingdom", "city": "London", "timezone": "Europe/London",
		"currency": "GBP", "website": "https://www.imperial.ac.uk", "emailDomains": []string{"e2e-imperial-" + runID + ".ac.uk"}, "detailsVerified": true}
	if errs := member.try(`mutation($id: ID!, $i: UpdateCampusInput!) { updateCampus(id: $id, input: $i) { id } }`, map[string]any{"id": imperialID, "i": update}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("non-admin campus update: got %q", code(errs))
	}
	admin.q(`mutation($id: ID!, $i: UpdateCampusInput!) { updateCampus(id: $id, input: $i) { id } }`, map[string]any{"id": imperialID, "i": update}, nil)
	member.q(`{ me { campusVerified } }`, nil, &me)
	if !me.Me.CampusVerified {
		t.Fatal("adding the campus domain should verify existing members")
	}

	// Contact form.
	anon.q(`mutation($m: String!) { contact(input: {name: "E2E", email: "e2e@example.com", message: $m}) }`,
		map[string]any{"m": "Hello from the end-to-end test " + runID + "."}, nil)
}

func TestCommunities(t *testing.T) {
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	stanford := campusID(t, anon, "stanford")
	owner := signup(t, "cowner"+runID+"@lpu.in", "Owner", lpu)
	member := signup(t, "cmember"+runID+"@lpu.in", "Joiner", lpu)
	unverified := signup(t, "cunver"+runID+"@example.org", "Unverified", lpu)
	visitor := signup(t, "cvisit"+runID+"@stanford.edu", "Visitor", stanford)

	const fields = `id slug name memberCount myRole requestPending canSeePosts canModerate isDefault`
	type community struct {
		ID, Slug, Name, MyRole      string
		MemberCount                 int
		RequestPending, CanSeePosts bool
		CanModerate, IsDefault      bool
	}
	join := func(u *user, id string) (string, []gqlError) {
		var out struct{ JoinCommunity struct{ Status string } }
		errs := u.try(`mutation($id: ID!) { joinCommunity(id: $id) { status } }`, map[string]any{"id": id}, &out)
		return out.JoinCommunity.Status, errs
	}

	// 1. Every campus has an "All students" community; verified students join in one tap.
	var list struct{ Communities struct{ Items []community } }
	owner.q(`{ communities(tab: CAMPUS) { items { `+fields+` } } }`, nil, &list)
	var all community
	for _, c := range list.Communities.Items {
		if c.IsDefault {
			all = c
		}
	}
	if all.ID == "" || all.Name != "All students" {
		t.Fatalf("campus default community missing: %+v", list.Communities.Items)
	}
	if st, errs := join(owner, all.ID); len(errs) > 0 || st != "JOINED" {
		t.Fatalf("open join: %q %+v", st, errs)
	}
	if _, errs := join(unverified, all.ID); code(errs) != "FAILED_PRECONDITION/VERIFICATION_REQUIRED" {
		t.Fatalf("unverified join: got %q", code(errs))
	}
	if _, errs := join(visitor, all.ID); code(errs) != "FORBIDDEN/ONLY_CAMPUS" {
		t.Fatalf("other-campus join: got %q", code(errs))
	}

	// 2. Creating: verified students only, at most three each.
	create := func(u *user, name string, allCampuses bool, policy string) (community, []gqlError) {
		var out struct{ CreateCommunity community }
		errs := u.try(`mutation($i: CommunityInput!) { createCommunity(input: $i) { `+fields+` } }`, map[string]any{"i": map[string]any{
			"name": name, "kind": "CLUB", "allCampuses": allCampuses, "joinPolicy": policy, "description": "e2e", "rules": "Be kind."}}, &out)
		return out.CreateCommunity, errs
	}
	if _, errs := create(unverified, "E2E Nope "+runID, false, "OPEN"); code(errs) != "FAILED_PRECONDITION/VERIFICATION_REQUIRED" {
		t.Fatalf("unverified create: got %q", code(errs))
	}
	club, errs := create(owner, "E2E Robotics "+runID, false, "APPROVAL")
	if len(errs) > 0 || club.MyRole != "OWNER" || club.MemberCount != 1 {
		t.Fatalf("create: %+v %+v", club, errs)
	}
	global, errs := create(owner, "E2E GATE Prep "+runID, true, "OPEN")
	if len(errs) > 0 {
		t.Fatalf("create global: %+v", errs)
	}
	if _, errs := create(owner, "E2E Robotics "+runID, false, "OPEN"); code(errs) != "CONFLICT" {
		t.Fatalf("duplicate name: got %q", code(errs))
	}
	if _, errs := create(owner, "E2E Third "+runID, false, "OPEN"); len(errs) > 0 {
		t.Fatalf("third community: %+v", errs)
	}
	if _, errs := create(owner, "E2E Fourth "+runID, false, "OPEN"); code(errs) != "RATE_LIMITED" {
		t.Fatalf("fourth community: got %q", code(errs))
	}

	// 3. Approval communities: request, wait, get approved. Posts stay private.
	if st, errs := join(member, club.ID); len(errs) > 0 || st != "REQUESTED" {
		t.Fatalf("request to join: %q %+v", st, errs)
	}
	if errs := member.try(`query($id: ID!) { communityPosts(communityId: $id) { total } }`, map[string]any{"id": club.ID}, nil); code(errs) != "FORBIDDEN/MEMBERS_ONLY" {
		t.Fatalf("non-member reading approval posts: got %q", code(errs))
	}
	var reqs struct {
		CommunityJoinRequests []struct {
			ID   string
			User struct{ ID string }
		}
	}
	if errs := member.try(`query($id: ID!) { communityJoinRequests(communityId: $id) { id } }`, map[string]any{"id": club.ID}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("non-moderator listing requests: got %q", code(errs))
	}
	owner.q(`query($id: ID!) { communityJoinRequests(communityId: $id) { id user { id } } }`, map[string]any{"id": club.ID}, &reqs)
	if len(reqs.CommunityJoinRequests) != 1 || reqs.CommunityJoinRequests[0].User.ID != member.ID {
		t.Fatalf("join requests: %+v", reqs)
	}
	owner.q(`mutation($id: ID!) { reviewJoinRequest(id: $id, approve: true) }`, map[string]any{"id": reqs.CommunityJoinRequests[0].ID}, nil)
	var got struct{ Community community }
	member.q(`query($s: String!) { community(slug: $s) { `+fields+` } }`, map[string]any{"s": club.Slug}, &got)
	if got.Community.MyRole != "MEMBER" || got.Community.MemberCount != 2 || !got.Community.CanSeePosts {
		t.Fatalf("after approval: %+v", got.Community)
	}
	var notes struct {
		Notifications struct{ Items []struct{ Title string } }
	}
	member.q(`{ notifications(limit: 5) { items { title } } }`, nil, &notes)
	if len(notes.Notifications.Items) == 0 || !strings.Contains(notes.Notifications.Items[0].Title, "You're in") {
		t.Fatalf("approval notification missing: %+v", notes.Notifications.Items)
	}

	// 4. All-campus communities are open to every campus.
	if st, errs := join(visitor, global.ID); len(errs) > 0 || st != "JOINED" {
		t.Fatalf("global join from another campus: %q %+v", st, errs)
	}
	if st, errs := join(member, global.ID); len(errs) > 0 || st != "JOINED" {
		t.Fatalf("global join: %q %+v", st, errs)
	}

	// 5. Posts appear instantly; replies notify the author; likes count.
	var post struct{ CreateCommunityPost struct{ ID string } }
	if errs := unverified.try(`mutation($c: ID!) { createCommunityPost(communityId: $c, body: "hi") { id } }`, map[string]any{"c": global.ID}, nil); code(errs) != "FAILED_PRECONDITION/NOT_MEMBER" {
		t.Fatalf("non-member post: got %q", code(errs))
	}
	member.q(`mutation($c: ID!) { createCommunityPost(communityId: $c, body: "Who's taking the mock test on Sunday?") { id } }`, map[string]any{"c": global.ID}, &post)
	postID := post.CreateCommunityPost.ID
	visitor.q(`mutation($p: ID!) { replyToCommunityPost(postId: $p, body: "Me! 9am?") { id } }`, map[string]any{"p": postID}, nil)
	visitor.q(`mutation($p: ID!) { likeCommunityPost(id: $p, liked: true) { likeCount } }`, map[string]any{"p": postID}, nil)
	var feed struct {
		CommunityPosts struct {
			Items []struct {
				ID                    string
				LikeCount, ReplyCount int
				LikedByMe             bool
			}
		}
	}
	visitor.q(`query($c: ID!) { communityPosts(communityId: $c) { items { id likeCount replyCount likedByMe } } }`, map[string]any{"c": global.ID}, &feed)
	if len(feed.CommunityPosts.Items) != 1 || feed.CommunityPosts.Items[0].ReplyCount != 1 || feed.CommunityPosts.Items[0].LikeCount != 1 || !feed.CommunityPosts.Items[0].LikedByMe {
		t.Fatalf("feed: %+v", feed.CommunityPosts.Items)
	}
	member.q(`{ notifications(limit: 5) { items { title } } }`, nil, &notes)
	if !strings.Contains(notes.Notifications.Items[0].Title, "replied to your post") {
		t.Fatalf("reply notification missing: %+v", notes.Notifications.Items)
	}

	// 6. Blocking hides the blocked person's posts.
	visitor.q(`mutation($u: ID!) { blockUser(userId: $u) }`, map[string]any{"u": member.ID}, nil)
	visitor.q(`query($c: ID!) { communityPosts(communityId: $c) { items { id likeCount replyCount likedByMe } } }`, map[string]any{"c": global.ID}, &feed)
	if len(feed.CommunityPosts.Items) != 0 {
		t.Fatal("blocked author's post still visible")
	}
	visitor.q(`mutation($u: ID!) { unblockUser(userId: $u) }`, map[string]any{"u": member.ID}, nil)

	// 7. Reports reach the community's moderators, who can remove the post.
	visitor.q(`mutation($i: ReportInput!) { report(input: $i) }`, map[string]any{"i": map[string]any{
		"targetType": "COMMUNITY_POST", "targetId": postID, "reason": "SPAM"}}, nil)
	var reports struct {
		CommunityReports []struct{ ID, TargetID string }
	}
	owner.q(`query($c: ID!) { communityReports(communityId: $c) { id targetId } }`, map[string]any{"c": global.ID}, &reports)
	if len(reports.CommunityReports) != 1 || reports.CommunityReports[0].TargetID != postID {
		t.Fatalf("community reports: %+v", reports)
	}
	owner.q(`mutation($id: ID!) { moderateCommunityReport(id: $id, removePost: true) }`, map[string]any{"id": reports.CommunityReports[0].ID}, nil)
	var gone struct{ CommunityPost *struct{ ID string } }
	member.q(`query($id: ID!) { communityPost(id: $id) { id } }`, map[string]any{"id": postID}, &gone)
	if gone.CommunityPost != nil {
		t.Fatal("removed post still readable")
	}

	// 8. Removed members are banned from rejoining for a while; owners can't leave.
	owner.q(`mutation($c: ID!, $u: ID!) { removeCommunityMember(communityId: $c, userId: $u) }`, map[string]any{"c": global.ID, "u": visitor.ID}, nil)
	if _, errs := join(visitor, global.ID); code(errs) != "FORBIDDEN" {
		t.Fatalf("rejoin after removal: got %q", code(errs))
	}
	if errs := owner.try(`mutation($c: ID!) { leaveCommunity(id: $c) }`, map[string]any{"c": global.ID}, nil); code(errs) != "FAILED_PRECONDITION" {
		t.Fatalf("owner leaving: got %q", code(errs))
	}
	member.q(`mutation($c: ID!) { leaveCommunity(id: $c) }`, map[string]any{"c": global.ID}, nil)

	// 9. The tutors tab works (no tutors here yet), and "Joined" lists memberships.
	var tutors struct{ CommunityTutors struct{ Total int } }
	member.q(`query($c: ID!) { communityTutors(communityId: $c) { total } }`, map[string]any{"c": club.ID}, &tutors)
	var joined struct{ Communities struct{ Items []community } }
	member.q(`{ communities(tab: JOINED) { items { `+fields+` } } }`, nil, &joined)
	if len(joined.Communities.Items) != 1 || joined.Communities.Items[0].ID != club.ID {
		t.Fatalf("joined tab: %+v", joined.Communities.Items)
	}
}

func TestDirectMessages(t *testing.T) {
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	stanford := campusID(t, anon, "stanford")
	alice := signup(t, "dma"+runID+"@lpu.in", "Sender", lpu)
	bob := signup(t, "dmb"+runID+"@lpu.in", "Recipient", lpu)
	stranger := signup(t, "dmc"+runID+"@stanford.edu", "Stranger", stanford)

	// Alice and Bob share their campus's All students community.
	var list struct {
		Communities struct {
			Items []struct {
				ID        string
				IsDefault bool
			}
		}
	}
	alice.q(`{ communities(tab: CAMPUS) { items { id isDefault } } }`, nil, &list)
	var all string
	for _, c := range list.Communities.Items {
		if c.IsDefault {
			all = c.ID
		}
	}
	for _, u := range []*user{alice, bob} {
		u.q(`mutation($id: ID!) { joinCommunity(id: $id) { status } }`, map[string]any{"id": all}, nil)
	}

	type permission struct{ CanMessage struct{ Allowed bool } }
	var perm permission
	alice.q(`query($u: ID!) { canMessage(userId: $u) { allowed } }`, map[string]any{"u": bob.ID}, &perm)
	if !perm.CanMessage.Allowed {
		t.Fatal("members of the same community should be able to message")
	}
	stranger.q(`query($u: ID!) { canMessage(userId: $u) { allowed } }`, map[string]any{"u": bob.ID}, &perm)
	if perm.CanMessage.Allowed {
		t.Fatal("people with no shared community must not be able to message")
	}
	if errs := stranger.try(`mutation($u: ID!) { startConversation(userId: $u) { id } }`, map[string]any{"u": bob.ID}, nil); code(errs) != "FORBIDDEN/NO_SHARED_COMMUNITY" {
		t.Fatalf("stranger starting a chat: got %q", code(errs))
	}

	// The first messages arrive as a request, capped until it is accepted.
	var start struct {
		StartConversation struct {
			ID, Status string
			CanSend    bool
		}
	}
	alice.q(`mutation($u: ID!) { startConversation(userId: $u) { id status canSend } }`, map[string]any{"u": bob.ID}, &start)
	conv := start.StartConversation.ID
	if start.StartConversation.Status != "REQUEST" || !start.StartConversation.CanSend {
		t.Fatalf("new chat: %+v", start.StartConversation)
	}
	send := func(u *user, body string) []gqlError {
		return u.try(`mutation($c: ID!, $b: String!) { sendDirectMessage(conversationId: $c, body: $b) { id } }`, map[string]any{"c": conv, "b": body}, nil)
	}

	// Live delivery: bob's open subscription receives alice's message.
	live := subscribe(t, bob, `subscription { messageEvents { conversationId message { body mine } } }`)
	if errs := send(alice, "Hi, live"); len(errs) > 0 {
		t.Fatal(errs)
	}
	var ev struct {
		MessageEvents struct {
			ConversationID string `json:"conversationId"`
			Message        struct {
				Body string
				Mine bool
			}
		} `json:"messageEvents"`
	}
	live(&ev)
	if ev.MessageEvents.ConversationID != conv || ev.MessageEvents.Message.Body != "Hi, live" || ev.MessageEvents.Message.Mine {
		t.Fatalf("live event: %+v", ev)
	}
	// The live message counts toward the request cap, so two more fill it.
	for i := 2; i <= 3; i++ {
		if errs := send(alice, fmt.Sprintf("Hi, message %d", i)); len(errs) > 0 {
			t.Fatalf("request message %d: %+v", i, errs)
		}
	}
	if errs := send(alice, "one too many"); code(errs) != "RATE_LIMITED" {
		t.Fatalf("message beyond the request cap: got %q", code(errs))
	}
	if errs := stranger.try(`query($c: ID!) { directMessages(conversationId: $c) { id } }`, map[string]any{"c": conv}, nil); code(errs) != "NOT_FOUND" {
		t.Fatalf("outsider reading the chat: got %q", code(errs))
	}

	var summary struct {
		InboxSummary struct{ Unread, Requests int }
	}
	bob.q(`{ inboxSummary { unread requests } }`, nil, &summary)
	if summary.InboxSummary.Requests != 1 {
		t.Fatalf("recipient should see one request: %+v", summary.InboxSummary)
	}
	var notes struct {
		Notifications struct{ Items []struct{ Title string } }
	}
	bob.q(`{ notifications(limit: 5) { items { title } } }`, nil, &notes)
	requestAlerts := 0
	for _, n := range notes.Notifications.Items {
		if strings.Contains(n.Title, "wants to message you") {
			requestAlerts++
		}
	}
	if requestAlerts != 1 {
		t.Fatalf("three request messages should make one notification, got %d", requestAlerts)
	}

	var requests struct {
		Conversations struct {
			Items []struct {
				ID             string
				IsRequestForMe bool
				UnreadCount    int
			}
		}
	}
	bob.q(`{ conversations(folder: REQUESTS) { items { id isRequestForMe unreadCount } } }`, nil, &requests)
	if len(requests.Conversations.Items) != 1 || !requests.Conversations.Items[0].IsRequestForMe || requests.Conversations.Items[0].UnreadCount != 3 {
		t.Fatalf("requests folder: %+v", requests.Conversations.Items)
	}

	// Replying accepts; reading clears unread.
	if errs := send(bob, "Hello! Sure."); len(errs) > 0 {
		t.Fatalf("reply: %+v", errs)
	}
	var thread struct {
		DirectMessages []struct {
			Body string
			Mine bool
		}
	}
	alice.q(`query($c: ID!) { directMessages(conversationId: $c) { body mine } }`, map[string]any{"c": conv}, &thread)
	if len(thread.DirectMessages) != 4 || thread.DirectMessages[3].Mine || !thread.DirectMessages[0].Mine {
		t.Fatalf("thread: %+v", thread.DirectMessages)
	}
	if errs := send(alice, "Great, thanks"); len(errs) > 0 {
		t.Fatalf("message after accept: %+v", errs)
	}
	var inbox struct {
		Conversations struct {
			Items []struct {
				Status      string
				UnreadCount int
			}
		}
	}
	bob.q(`{ conversations(folder: INBOX) { items { status unreadCount } } }`, nil, &inbox)
	if len(inbox.Conversations.Items) != 1 || inbox.Conversations.Items[0].Status != "ACTIVE" || inbox.Conversations.Items[0].UnreadCount != 1 {
		t.Fatalf("inbox: %+v", inbox.Conversations.Items)
	}

	// Blocking stops messages both ways.
	bob.q(`mutation($u: ID!) { blockUser(userId: $u) }`, map[string]any{"u": alice.ID}, nil)
	if errs := send(alice, "are you there?"); code(errs) != "FORBIDDEN" {
		t.Fatalf("message to someone who blocked you: got %q", code(errs))
	}
	bob.q(`mutation($u: ID!) { unblockUser(userId: $u) }`, map[string]any{"u": alice.ID}, nil)

	// Without a shared community the chat becomes read-only.
	bob.q(`mutation($id: ID!) { leaveCommunity(id: $id) }`, map[string]any{"id": all}, nil)
	var c struct {
		Conversation struct{ CanSend, SharesCommunity bool }
	}
	alice.q(`query($c: ID!) { conversation(id: $c) { canSend sharesCommunity } }`, map[string]any{"c": conv}, &c)
	if c.Conversation.CanSend || c.Conversation.SharesCommunity {
		t.Fatalf("chat after leaving: %+v", c.Conversation)
	}
	if errs := send(alice, "still here?"); code(errs) != "FAILED_PRECONDITION/NO_SHARED_COMMUNITY" {
		t.Fatalf("message without a shared community: got %q", code(errs))
	}
}

// subscribe opens a graphql-transport-ws subscription as u and returns a
// function that waits for the next event's data.
func subscribe(t *testing.T, u *user, query string) func(into any) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	wsURL := "ws" + strings.TrimPrefix(gatewayURL, "http") + "/graphql"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{"graphql-transport-ws"}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close(websocket.StatusNormalClosure, "")
		cancel()
	})
	type frame struct {
		ID      string          `json:"id,omitempty"`
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	read := func() frame {
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		defer rcancel()
		for {
			var f frame
			if err := wsjson.Read(rctx, conn, &f); err != nil {
				t.Fatalf("websocket read: %v", err)
			}
			if f.Type != "ping" && f.Type != "pong" && f.Type != "ka" {
				return f
			}
		}
	}
	auth, _ := json.Marshal(map[string]string{"Authorization": "Bearer " + u.token})
	_ = wsjson.Write(ctx, conn, frame{Type: "connection_init", Payload: auth})
	if f := read(); f.Type != "connection_ack" {
		t.Fatalf("expected connection_ack, got %s %s", f.Type, f.Payload)
	}
	q, _ := json.Marshal(map[string]string{"query": query})
	_ = wsjson.Write(ctx, conn, frame{ID: "1", Type: "subscribe", Payload: q})
	// Give the gateway a moment to open the stream to the community service.
	time.Sleep(300 * time.Millisecond)
	return func(into any) {
		t.Helper()
		f := read()
		if f.Type != "next" {
			t.Fatalf("expected next, got %s %s", f.Type, f.Payload)
		}
		var body struct {
			Data   json.RawMessage
			Errors []gqlError
		}
		_ = json.Unmarshal(f.Payload, &body)
		if len(body.Errors) > 0 {
			t.Fatal(body.Errors)
		}
		if err := json.Unmarshal(body.Data, into); err != nil {
			t.Fatal(err)
		}
	}
}

// ── Careers: mock interviews and resume reviews ─────────────────────────────

var tinyPDF = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")

// verifyID uploads a student ID for u and has the admin approve it.
func verifyID(t *testing.T, admin, u *user) {
	t.Helper()
	if errs := u.upload(`mutation($d: Upload!) { submitVerification(document: $d) { status } }`, "d", tinyPNG, "id.png", nil); len(errs) > 0 {
		t.Fatal(errs)
	}
	var queue struct {
		AdminVerifications struct {
			Items []struct {
				ID   string
				User struct{ ID string }
			}
		}
	}
	admin.q(`{ adminVerifications(status: PENDING, pageSize: 100) { items { id user { id } } } }`, nil, &queue)
	for _, v := range queue.AdminVerifications.Items {
		if v.User.ID == u.ID {
			admin.q(`mutation($id: ID!) { reviewVerification(id: $id, approve: true) { status } }`, map[string]any{"id": v.ID}, nil)
			return
		}
	}
	t.Fatal("verification not in the admin queue")
}

func TestCareers(t *testing.T) {
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	admin := login(t, adminEmail, adminPass)
	mentor := signup(t, "mentor"+runID+"@lpu.in", "Mentor", lpu)
	student := signup(t, "careerstudent"+runID+"@lpu.in", "Candidate", lpu)
	outsider := signup(t, "careeroutsider"+runID+"@lpu.in", "Outsider", lpu)
	verifyID(t, admin, mentor)

	// 1. A mentor must offer interviews, resume reviews, or both.
	profile := map[string]any{
		"kind": "MENTOR", "headline": "SDE at Amazon · mock interviews for placements", "rateMinor": 50000,
		"bio":   "Placed at Amazon from campus. I run realistic coding and HR rounds and give honest, specific feedback.",
		"modes": []string{"ONLINE"}, "languages": []string{"English"}, "publish": true,
	}
	save := `mutation($i: ProviderProfileInput!) { saveProviderProfile(input: $i) { id status interviewTypes companies resumeOffer { price { amountMinor } turnaroundDays } } }`
	if errs := mentor.try(save, map[string]any{"i": profile}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("mentor offering nothing: got %q", code(errs))
	}
	profile["interviewTypes"] = []string{"CODING", "HR"}
	profile["companies"] = []string{"Amazon", "Flipkart"}
	profile["resumeOffer"] = map[string]any{"priceMinor": 19900, "turnaroundDays": 2}
	var saved struct {
		SaveProviderProfile struct {
			ID, Status     string
			InterviewTypes []string
			Companies      []string
			ResumeOffer    *struct {
				Price          struct{ AmountMinor int }
				TurnaroundDays int
			}
		}
	}
	mentor.q(save, map[string]any{"i": profile}, &saved)
	p := saved.SaveProviderProfile
	if p.Status != "ACTIVE" || len(p.InterviewTypes) != 2 || len(p.Companies) != 2 || p.ResumeOffer == nil || p.ResumeOffer.Price.AmountMinor != 19900 {
		t.Fatalf("saved mentor: %+v", p)
	}
	mentorID := p.ID
	var windows []map[string]any
	for d := 0; d < 7; d++ {
		windows = append(windows, map[string]any{"weekday": d, "startMinute": 360, "endMinute": 1320})
	}
	mentor.q(`mutation($w: [AvailabilityInput!]!) { setAvailability(windows: $w) { timezone } }`, map[string]any{"w": windows}, nil)

	// 2. Students find mentors by company, interview type and resume reviews.
	for _, filter := range []string{`{kind: MENTOR, company: "amaz"}`, `{kind: MENTOR, interviewType: CODING}`, `{kind: MENTOR, resumeReviews: true}`, `{kind: MENTOR, query: "flipkart"}`} {
		var res struct {
			Providers struct{ Items []struct{ ID string } }
		}
		anon.q(`{ providers(filter: `+filter+`, pageSize: 50) { items { id } } }`, nil, &res)
		if !slices.ContainsFunc(res.Providers.Items, func(i struct{ ID string }) bool { return i.ID == mentorID }) {
			t.Fatalf("mentor not found with filter %s", filter)
		}
	}
	var none struct{ Providers struct{ Total int } }
	anon.q(`query($c: ID) { providers(filter: {kind: MENTOR, company: "no-such-company-`+runID+`", campusId: $c}) { total } }`, map[string]any{"c": lpu}, &none)
	if none.Providers.Total != 0 {
		t.Fatal("company filter should exclude non-matching mentors")
	}

	// 3. A mock interview is booked like any session, for an interview type the mentor runs.
	var slots struct {
		OpenSlots struct{ Slots []struct{ StartAt string } }
	}
	anon.q(`query($p: ID!) { openSlots(providerId: $p, durationMinutes: 60) { slots { startAt } } }`, map[string]any{"p": mentorID}, &slots)
	if len(slots.OpenSlots.Slots) < 5 {
		t.Fatal("mentor should have open slots")
	}
	book := `mutation($i: BookingInput!) { requestBooking(input: $i) { id kind interviewType } }`
	input := map[string]any{"providerId": mentorID, "startAt": slots.OpenSlots.Slots[2].StartAt, "durationMinutes": 60, "mode": "ONLINE", "interviewType": "SYSTEM_DESIGN"}
	if errs := student.try(book, map[string]any{"i": input}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("interview type the mentor doesn't run: got %q", code(errs))
	}
	input["interviewType"] = "CODING"
	var booked struct {
		RequestBooking struct{ ID, Kind, InterviewType string }
	}
	student.q(book, map[string]any{"i": input}, &booked)
	if booked.RequestBooking.Kind != "MENTOR" || booked.RequestBooking.InterviewType != "CODING" {
		t.Fatalf("booked: %+v", booked.RequestBooking)
	}
	bookingID := booked.RequestBooking.ID
	mentor.q(`mutation($b: ID!) { transitionBooking(input: {bookingId: $b, action: ACCEPT, meetingLink: "https://meet.example.com/x"}) { status } }`, map[string]any{"b": bookingID}, nil)

	// 4. The scorecard: only the mentor, only once the interview has started, every criterion.
	var catalog struct {
		Catalog struct {
			FeedbackCriteria []struct{ Value string }
			ResumeSections   []struct{ Value string }
		}
	}
	anon.q(`{ catalog { feedbackCriteria { value } resumeSections { value } } }`, nil, &catalog)
	var scores []map[string]any
	for i, c := range catalog.Catalog.FeedbackCriteria {
		scores = append(scores, map[string]any{"criterion": c.Value, "score": 1 + i%5})
	}
	feedback := map[string]any{"bookingId": bookingID, "scores": scores, "verdict": "ALMOST",
		"strengths": "Clear about trade-offs, wrote clean code.", "improvements": "Talk through the approach before coding; test edge cases."}
	saveFeedback := `mutation($i: InterviewFeedbackInput!) { saveInterviewFeedback(input: $i) { verdict scores { criterion score } } }`
	if errs := mentor.try(saveFeedback, map[string]any{"i": feedback}, nil); code(errs) != "FAILED_PRECONDITION" {
		t.Fatalf("feedback before the interview: got %q", code(errs))
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE marketplace.bookings SET start_at = now() - interval '2 hours', end_at = now() - interval '1 hour' WHERE id = $1`, bookingID); err != nil {
		t.Fatal(err)
	}
	if errs := student.try(saveFeedback, map[string]any{"i": feedback}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("student writing feedback: got %q", code(errs))
	}
	feedback["scores"] = scores[:len(scores)-1]
	if errs := mentor.try(saveFeedback, map[string]any{"i": feedback}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("missing a criterion: got %q", code(errs))
	}
	feedback["scores"] = scores
	mentor.q(saveFeedback, map[string]any{"i": feedback}, nil)
	var withFeedback struct {
		Booking struct {
			Feedback *struct {
				Verdict string
				Scores  []struct{ Criterion string }
			}
		}
	}
	student.q(`query($b: ID!) { booking(id: $b) { feedback { verdict scores { criterion } } } }`, map[string]any{"b": bookingID}, &withFeedback)
	if f := withFeedback.Booking.Feedback; f == nil || f.Verdict != "ALMOST" || len(f.Scores) != len(scores) {
		t.Fatalf("student should see the scorecard: %+v", withFeedback.Booking.Feedback)
	}

	// 5. Resume review: PDF only, private link, accept → deliver → rate.
	requestResume := func(u *user, file []byte, name string) (string, []gqlError) {
		var out struct{ RequestResumeReview struct{ ID string } }
		errs := u.upload(`mutation($f: Upload!) { requestResumeReview(input: {mentorId: "`+mentorID+`", targetRole: "SDE intern", notes: "Applying to product companies", resume: $f}) { id } }`,
			"f", file, name, &out)
		return out.RequestResumeReview.ID, errs
	}
	if _, errs := requestResume(student, tinyPNG, "resume.png"); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("non-PDF resume: got %q", code(errs))
	}
	if _, errs := requestResume(mentor, tinyPDF, "mine.pdf"); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("reviewing your own resume: got %q", code(errs))
	}
	reviewID, errs := requestResume(student, tinyPDF, "Candidate CV.pdf")
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	const fields = `id status fileName fileUrl dueAt overdue overallScore myRole rated sections { section rating } price { amountMinor }`
	type resume struct {
		ID, Status, FileName, MyRole string
		FileURL                      *string
		DueAt                        *string
		Overdue, Rated               bool
		OverallScore                 *int
		Sections                     []struct{ Section string }
		Price                        struct{ AmountMinor int }
	}
	var got struct{ ResumeReview *resume }
	student.q(`query($id: ID!) { resumeReview(id: $id) { `+fields+` } }`, map[string]any{"id": reviewID}, &got)
	r := got.ResumeReview
	if r == nil || r.Status != "REQUESTED" || r.FileName != "Candidate CV.pdf" || r.MyRole != "STUDENT" || r.FileURL == nil || r.Price.AmountMinor != 19900 {
		t.Fatalf("new resume review: %+v", r)
	}
	if res, _ := http.Get(gatewayURL + *r.FileURL); res == nil || res.StatusCode != 200 {
		t.Fatal("signed resume link should work")
	}
	if res, _ := http.Get(gatewayURL + strings.Split(*r.FileURL, "?")[0]); res == nil || res.StatusCode != 403 {
		t.Fatal("unsigned resume link must be refused")
	}
	outsider.q(`query($id: ID!) { resumeReview(id: $id) { id } }`, map[string]any{"id": reviewID}, &got)
	if got.ResumeReview != nil {
		t.Fatal("outsiders must not see a resume review")
	}
	var queue struct {
		ResumeReviews struct{ Items []struct{ ID string } }
	}
	mentor.q(`{ resumeReviews(as: REVIEWER, statuses: [REQUESTED]) { items { id } } }`, nil, &queue)
	if len(queue.ResumeReviews.Items) != 1 || queue.ResumeReviews.Items[0].ID != reviewID {
		t.Fatalf("mentor's queue: %+v", queue.ResumeReviews.Items)
	}

	var sections []map[string]any
	for _, sec := range catalog.Catalog.ResumeSections[:3] {
		sections = append(sections, map[string]any{"section": sec.Value, "rating": 4, "comment": "Quantify the impact of each point."})
	}
	deliver := `mutation($i: DeliverResumeReviewInput!) { deliverResumeReview(input: $i) { status overallScore } }`
	delivery := map[string]any{"id": reviewID, "overallScore": 7, "summary": "Strong projects; lead with them and cut the generic objective.", "sections": sections}
	if errs := mentor.try(deliver, map[string]any{"i": delivery}, nil); code(errs) != "FAILED_PRECONDITION" {
		t.Fatalf("deliver before accepting: got %q", code(errs))
	}
	transition := `mutation($id: ID!, $a: ResumeReviewAction!) { transitionResumeReview(id: $id, action: $a) { status dueAt } }`
	if errs := student.try(transition, map[string]any{"id": reviewID, "a": "ACCEPT"}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("student accepting: got %q", code(errs))
	}
	var accepted struct{ TransitionResumeReview resume }
	mentor.q(transition, map[string]any{"id": reviewID, "a": "ACCEPT"}, &accepted)
	if accepted.TransitionResumeReview.Status != "ACCEPTED" || accepted.TransitionResumeReview.DueAt == nil {
		t.Fatalf("accepted: %+v", accepted.TransitionResumeReview)
	}
	if errs := student.try(transition, map[string]any{"id": reviewID, "a": "CANCEL"}, nil); code(errs) != "FAILED_PRECONDITION" {
		t.Fatalf("student cancelling before the due date: got %q", code(errs))
	}
	delivery["sections"] = sections[:2]
	if errs := mentor.try(deliver, map[string]any{"i": delivery}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("too few sections: got %q", code(errs))
	}
	delivery["sections"] = sections
	mentor.q(deliver, map[string]any{"i": delivery}, nil)
	student.q(`query($id: ID!) { resumeReview(id: $id) { `+fields+` } }`, map[string]any{"id": reviewID}, &got)
	if r := got.ResumeReview; r.Status != "DELIVERED" || r.OverallScore == nil || *r.OverallScore != 7 || len(r.Sections) != 3 {
		t.Fatalf("delivered: %+v", r)
	}
	rate := `mutation($id: ID!) { rateResumeReview(id: $id, rating: 5, comment: "Specific and actionable.") { rating } }`
	student.q(rate, map[string]any{"id": reviewID}, nil)
	if errs := student.try(rate, map[string]any{"id": reviewID}, nil); code(errs) != "CONFLICT" {
		t.Fatalf("rating twice: got %q", code(errs))
	}
	var stats struct {
		Provider struct {
			RatingCount            int
			ResumeReviewsDelivered int
		}
	}
	anon.q(`query($id: ID!) { provider(id: $id) { ratingCount resumeReviewsDelivered } }`, map[string]any{"id": mentorID}, &stats)
	if stats.Provider.RatingCount != 1 || stats.Provider.ResumeReviewsDelivered != 1 {
		t.Fatalf("mentor stats: %+v", stats.Provider)
	}

	// 6. A student can have only a few reviews in progress at once.
	for i := 0; i < 3; i++ {
		if _, errs := requestResume(student, tinyPDF, "cv.pdf"); len(errs) > 0 {
			t.Fatalf("request %d: %+v", i, errs)
		}
	}
	if _, errs := requestResume(student, tinyPDF, "cv.pdf"); code(errs) != "RATE_LIMITED" {
		t.Fatalf("beyond the open-request cap: got %q", code(errs))
	}
}

// ── Placement prep: rooms, notes, question bank, progress, experiences ──────

func livekitWebhook(t *testing.T, body string) int {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": os.Getenv("LIVEKIT_API_KEY"), "exp": time.Now().Add(time.Minute).Unix(),
		"sha256": base64.StdEncoding.EncodeToString(sum[:]),
	})
	signed, err := tok.SignedString([]byte(os.Getenv("LIVEKIT_API_SECRET")))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/livekit/webhook", strings.NewReader(body))
	req.Header.Set("Authorization", signed)
	req.Header.Set("Content-Type", "application/webhook+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestPrep(t *testing.T) {
	if os.Getenv("LIVEKIT_API_SECRET") == "" {
		t.Skip("LIVEKIT_API_SECRET not set")
	}
	anon := newClient(t)
	lpu := campusID(t, anon, "lpu")
	admin := login(t, adminEmail, adminPass)
	mentor := signup(t, "prepmentor"+runID+"@lpu.in", "Mentor", lpu)
	student := signup(t, "prepstudent"+runID+"@lpu.in", "Candidate", lpu)
	outsider := signup(t, "prepoutsider"+runID+"@lpu.in", "Outsider", lpu)
	verifyID(t, admin, mentor)
	var saved struct{ SaveProviderProfile struct{ ID string } }
	mentor.q(`mutation($i: ProviderProfileInput!) { saveProviderProfile(input: $i) { id } }`, map[string]any{"i": map[string]any{
		"kind": "MENTOR", "headline": "SDE at Microsoft-`+runID+` · coding mocks", "rateMinor": 0,
		"bio": "Placed at Microsoft. I run coding rounds and write detailed scorecards for every candidate.", "publish": true,
		"modes": []string{"ONLINE"}, "languages": []string{"English"}, "interviewTypes": []string{"CODING"}, "companies": []string{"Microsoft-" + runID},
	}}, &saved)
	mentorID := saved.SaveProviderProfile.ID
	var windows []map[string]any
	for d := 0; d < 7; d++ {
		windows = append(windows, map[string]any{"weekday": d, "startMinute": 0, "endMinute": 1440})
	}
	mentor.q(`mutation($w: [AvailabilityInput!]!) { setAvailability(windows: $w) { timezone } }`, map[string]any{"w": windows}, nil)

	// 1. Hub: company list, next slot, "available soon".
	var hub struct {
		MentorCompanies []struct {
			Name  string
			Count int
		}
		AvailableSoon []struct {
			Provider struct{ ID string }
			StartAt  string
		}
		Provider struct{ NextSlotAt *string }
	}
	anon.q(`query($p: ID!, $c: String) { mentorCompanies { name count } availableSoon(kind: MENTOR, company: $c, withinHours: 24) { provider { id } startAt }
		provider(id: $p) { nextSlotAt } }`, map[string]any{"p": mentorID, "c": "microsoft-" + runID}, &hub)
	if !slices.ContainsFunc(hub.MentorCompanies, func(c struct {
		Name  string
		Count int
	}) bool { return c.Name == "Microsoft-"+runID }) {
		t.Fatalf("company missing from mentorCompanies: %+v", hub.MentorCompanies)
	}
	if len(hub.AvailableSoon) != 1 || hub.AvailableSoon[0].Provider.ID != mentorID || hub.Provider.NextSlotAt == nil {
		t.Fatalf("availability: soon=%+v next=%v", hub.AvailableSoon, hub.Provider.NextSlotAt)
	}

	// 2. Book and accept; the room stays shut until shortly before the start.
	var slots struct {
		OpenSlots struct{ Slots []struct{ StartAt string } }
	}
	anon.q(`query($p: ID!) { openSlots(providerId: $p, durationMinutes: 60) { slots { startAt } } }`, map[string]any{"p": mentorID}, &slots)
	var booked struct{ RequestBooking struct{ ID string } }
	student.q(`mutation($i: BookingInput!) { requestBooking(input: $i) { id } }`, map[string]any{"i": map[string]any{
		"providerId": mentorID, "startAt": slots.OpenSlots.Slots[3].StartAt, "durationMinutes": 60, "mode": "ONLINE", "interviewType": "CODING"}}, &booked)
	b := booked.RequestBooking.ID
	join := `mutation($b: ID!) { joinRoom(bookingId: $b) { url token role expiresAt } }`
	if errs := student.try(join, map[string]any{"b": b}, nil); code(errs) != "FAILED_PRECONDITION/ROOM_NOT_ACCEPTED" {
		t.Fatalf("room before accept: got %q", code(errs))
	}
	mentor.q(`mutation($b: ID!) { transitionBooking(input: {bookingId: $b, action: ACCEPT}) { status } }`, map[string]any{"b": b}, nil)
	if errs := student.try(join, map[string]any{"b": b}, nil); code(errs) != "FAILED_PRECONDITION/ROOM_NOT_OPEN" {
		t.Fatalf("room hours before the start: got %q", code(errs))
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `UPDATE marketplace.bookings SET start_at = now() - interval '5 minutes', end_at = now() + interval '55 minutes' WHERE id = $1`, b); err != nil {
		t.Fatal(err)
	}

	// 3. Only the two parties get a pass, for this room, with their role.
	var pass struct {
		JoinRoom struct{ URL, Token, Role, ExpiresAt string }
	}
	student.q(join, map[string]any{"b": b}, &pass)
	var claims struct {
		jwt.RegisteredClaims
		Video struct{ Room string } `json:"video"`
	}
	if _, err := jwt.ParseWithClaims(pass.JoinRoom.Token, &claims, func(*jwt.Token) (any, error) { return []byte(os.Getenv("LIVEKIT_API_SECRET")), nil }); err != nil {
		t.Fatalf("room pass doesn't verify: %v", err)
	}
	if pass.JoinRoom.Role != "STUDENT" || claims.Subject != student.ID || claims.Video.Room != "booking-"+b || pass.JoinRoom.URL == "" {
		t.Fatalf("pass: %+v claims: %+v", pass.JoinRoom, claims)
	}
	if errs := outsider.try(join, map[string]any{"b": b}, nil); code(errs) != "NOT_FOUND" {
		t.Fatalf("outsider joining: got %q", code(errs))
	}

	// 4. Room state: both edit code; only the mentor sets the problem and draft scores.
	save := `mutation($i: SaveRoomInput!) { saveRoom(input: $i) }`
	student.q(save, map[string]any{"i": map[string]any{"bookingId": b, "code": "print(1)", "codeState": base64.StdEncoding.EncodeToString([]byte{1, 2, 3}), "language": "PYTHON"}}, nil)
	if errs := student.try(save, map[string]any{"i": map[string]any{"bookingId": b, "problem": "spoiler"}}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("student setting the problem: got %q", code(errs))
	}
	if errs := student.try(save, map[string]any{"i": map[string]any{"bookingId": b, "language": "COBOL"}}, nil); code(errs) != "BAD_USER_INPUT" {
		t.Fatalf("unknown language: got %q", code(errs))
	}
	mentor.q(save, map[string]any{"i": map[string]any{"bookingId": b, "problem": "Number of islands", "draftScores": []map[string]any{{"criterion": "COMMUNICATION", "score": 4}}}}, nil)

	// 5. Notes are the mentor's alone.
	note := `mutation($b: ID!, $s: Int!, $t: NoteTone) { addMentorNote(bookingId: $b, atSeconds: $s, body: "Clarified constraints first", tone: $t) { id tone } }`
	mentor.q(note, map[string]any{"b": b, "s": 252, "t": "PLUS"}, nil)
	if errs := student.try(note, map[string]any{"b": b, "s": 10, "t": "PLUS"}, nil); code(errs) != "FORBIDDEN" {
		t.Fatalf("student adding a note: got %q", code(errs))
	}

	// 6. The video server's signed webhook builds the join log; unsigned ones are refused.
	event := func(kind, user string) string {
		return fmt.Sprintf(`{"event":"%s","room":{"name":"booking-%s"},"participant":{"identity":"%s","name":"Candidate"},"createdAt":"%d"}`, kind, b, user, time.Now().Unix())
	}
	if st := livekitWebhook(t, event("participant_joined", student.ID)); st != 200 {
		t.Fatalf("webhook: %d", st)
	}
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/livekit/webhook", strings.NewReader(event("participant_joined", outsider.ID)))
	if res, _ := http.DefaultClient.Do(req); res == nil || res.StatusCode != 401 {
		t.Fatal("unsigned webhook must be refused")
	}
	livekitWebhook(t, event("participant_left", student.ID))

	const roomFields = `room { problem language code codeState isOpen draftScores { criterion score } presence { user { id } joinedAt leftAt } } mentorNotes { atSeconds tone }`
	type roomView struct {
		Booking struct {
			Room struct {
				Problem, Language, Code string
				CodeState               *string
				IsOpen                  bool
				DraftScores             []struct{ Criterion string }
				Presence                []struct {
					User     struct{ ID string }
					JoinedAt string
					LeftAt   *string
				}
			}
			MentorNotes []struct {
				AtSeconds int
				Tone      string
			}
		}
	}
	var sv, mv roomView
	student.q(`query($b: ID!) { booking(id: $b) { `+roomFields+` } }`, map[string]any{"b": b}, &sv)
	mentor.q(`query($b: ID!) { booking(id: $b) { `+roomFields+` } }`, map[string]any{"b": b}, &mv)
	r := sv.Booking.Room
	if !r.IsOpen || r.Code != "print(1)" || r.Language != "PYTHON" || r.CodeState == nil || r.Problem != "Number of islands" {
		t.Fatalf("room for student: %+v", r)
	}
	if len(r.DraftScores) != 0 || len(sv.Booking.MentorNotes) != 0 {
		t.Fatal("student must not see the mentor's draft scores or notes")
	}
	if len(mv.Booking.Room.DraftScores) != 1 || len(mv.Booking.MentorNotes) != 1 || mv.Booking.MentorNotes[0].Tone != "PLUS" {
		t.Fatalf("mentor's room: %+v notes %+v", mv.Booking.Room.DraftScores, mv.Booking.MentorNotes)
	}
	if p := r.Presence; len(p) != 1 || p[0].User.ID != student.ID || p[0].LeftAt == nil {
		t.Fatalf("presence: %+v", p)
	}

	// 7. Question bank: mentors only, edits by the author only.
	qsave := `mutation($id: ID, $i: QuestionInput!) { saveQuestion(id: $id, input: $i) { id mine } }`
	qin := map[string]any{"title": "Number of islands", "body": "Count connected groups of 1s in a grid.", "interviewType": "CODING", "difficulty": "MEDIUM", "companies": []string{"Microsoft-" + runID}}
	if errs := student.try(qsave, map[string]any{"i": qin}, nil); code(errs) != "FORBIDDEN/MENTOR_REQUIRED" {
		t.Fatalf("student writing a question: got %q", code(errs))
	}
	var q struct {
		SaveQuestion struct {
			ID   string
			Mine bool
		}
	}
	mentor.q(qsave, map[string]any{"i": qin}, &q)
	if errs := student.try(`{ questions { total } }`, nil, nil); code(errs) != "FORBIDDEN/MENTOR_REQUIRED" {
		t.Fatalf("student reading the bank: got %q", code(errs))
	}
	var bank struct{ Questions struct{ Total int } }
	mentor.q(`query($c: String) { questions(company: $c, interviewType: CODING) { total } }`, map[string]any{"c": "microsoft-" + runID}, &bank)
	if bank.Questions.Total != 1 {
		t.Fatalf("bank search: %d", bank.Questions.Total)
	}
	conn.Exec(ctx, `DELETE FROM marketplace.questions WHERE id = $1`, q.SaveQuestion.ID) //nolint:errcheck

	// 8. Progress comes from scorecards.
	var scores []map[string]any
	for _, c := range []string{"COMMUNICATION", "PROBLEM_SOLVING", "TECHNICAL_DEPTH", "STRUCTURE", "CONFIDENCE"} {
		s := 4
		if c == "STRUCTURE" {
			s = 2
		}
		scores = append(scores, map[string]any{"criterion": c, "score": s})
	}
	mentor.q(`mutation($i: InterviewFeedbackInput!) { saveInterviewFeedback(input: $i) { verdict } }`, map[string]any{"i": map[string]any{
		"bookingId": b, "scores": scores, "verdict": "ALMOST", "strengths": "Clear and calm throughout.", "improvements": "Structure the answer before coding."}}, nil)
	var prog struct {
		MyProgress struct {
			Readiness              *int
			WeakestCriterion       *string
			SuggestedInterviewType *string
			Interviews             []struct{ Verdict string }
		}
	}
	student.q(`{ myProgress { readiness weakestCriterion suggestedInterviewType interviews { verdict } } }`, nil, &prog)
	p := prog.MyProgress
	if p.Readiness == nil || *p.Readiness != 72 || *p.WeakestCriterion != "STRUCTURE" || *p.SuggestedInterviewType != "SYSTEM_DESIGN" || len(p.Interviews) != 1 {
		t.Fatalf("progress: %+v", p)
	}

	// 9. Interview experiences: moderated, anonymous posts hide the author.
	share := `mutation($i: ExperienceInput!) { shareInterviewExperience(input: $i) { id status } }`
	var shared struct{ ShareInterviewExperience struct{ ID, Status string } }
	student.q(share, map[string]any{"i": map[string]any{"company": "Microsoft-" + runID, "role": "SDE intern", "year": 2026, "outcome": "SELECTED", "anonymous": true,
		"rounds": []map[string]any{{"name": "Online assessment", "details": "Two DSA questions in 90 minutes on graphs and DP."}}}}, &shared)
	if shared.ShareInterviewExperience.Status != "PENDING" {
		t.Fatalf("new experience should wait for review: %+v", shared)
	}
	exp := shared.ShareInterviewExperience.ID
	var list struct{ InterviewExperiences struct{ Total int } }
	outsider.q(`query($c: String) { interviewExperiences(company: $c) { total } }`, map[string]any{"c": "microsoft-" + runID}, &list)
	if list.InterviewExperiences.Total != 0 {
		t.Fatal("unreviewed experience must not be listed")
	}
	admin.q(`mutation($id: ID!) { reviewInterviewExperience(id: $id, approve: true) { status } }`, map[string]any{"id": exp}, nil)
	var one struct {
		InterviewExperience *struct {
			Author *struct{ ID string }
			Status string
		}
	}
	outsider.q(`query($id: ID!) { interviewExperience(id: $id) { author { id } status } }`, map[string]any{"id": exp}, &one)
	if one.InterviewExperience == nil || one.InterviewExperience.Status != "PUBLISHED" || one.InterviewExperience.Author != nil {
		t.Fatalf("published anonymous experience: %+v", one.InterviewExperience)
	}
	if errs := outsider.try(`mutation($id: ID!) { deleteInterviewExperience(id: $id) }`, map[string]any{"id": exp}, nil); code(errs) != "NOT_FOUND" {
		t.Fatalf("deleting someone else's experience: got %q", code(errs))
	}
	student.q(`mutation($id: ID!) { deleteInterviewExperience(id: $id) }`, map[string]any{"id": exp}, nil)
}
