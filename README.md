# Your Mate

A campus-verified, peer-to-peer marketplace for students anywhere in the world:
students book **tutors** (seniors who know their courses) and **campus guides**
(tours, housing, paperwork, the city), and those students earn by helping.
LPU was the original pilot; any university can be added.

It started from a static HTML mockup, [suhani0406/YOUR-MATE](https://github.com/suhani0406/YOUR-MATE).
The plan this implements is [`docs/PLAN.md`](docs/PLAN.md).

## Run it

```bash
ADMIN_EMAIL=you@example.com ADMIN_CAMPUS_SLUG=lpu scripts/dev-env.sh   # writes .env with fresh secrets
docker compose up -d --build
```

No credentials live in the repository. `scripts/dev-env.sh` generates `.env`, which is
git-ignored. It holds random database, JWT, internal-token and file-signing secrets, plus
the first admin account with a random password. Compose refuses to start while a value
is missing. The admin account is created on first start; log in with `ADMIN_EMAIL` and
the `ADMIN_PASSWORD` from `.env`.

| URL | What |
|---|---|
| http://localhost:5190 | Web app (nginx serving the React build, proxying the API) |
| http://localhost:8027 | Mailpit — every email the stack sends (signup codes, notifications) |
| http://localhost:8095/playground | GraphQL playground (dev mode only) |

There is no demo data: sign up in the app and copy the 6-digit code from Mailpit.

### Frontend with hot reload

```bash
cd web && npm install && npm run dev   # http://localhost:5191, proxies /graphql to :8095
```

## Architecture

```
React SPA (web/) ──/graphql──▶ gateway (GraphQL, gqlgen) ──gRPC──▶ identity     ─┐
                  ──/files───▶   uploads + signed URLs            ├─▶ marketplace ─┼─▶ Postgres
                                       ▲ webhook                  └─▶ community   ─┘  (one schema each)
                  ──video────▶ livekit (open-source video server, interview rooms)
```

| Service | Owns |
|---|---|
| `identity` (:50071) | users, sessions (JWT + rotating refresh tokens), email OTP, campuses, student-ID verification |
| `marketplace` (:50072) | tutor, guide and career-mentor profiles, subjects, weekly availability, open-slot calculation, bookings, interview scorecards, resume reviews, messages, reviews, blocks, notifications, reminders |
| `community` (:50073) | communities (join, posts, replies, moderation), moderated campus feed, abuse reports, contact inquiries |
| `gateway` (:8095) | the public GraphQL API, auth, privacy rules, uploads, rate limits, video-room passes |
| `livekit` (:7880, TCP 7881, UDP 52000–52100) | video and audio for interview rooms; reports joins and leaves to the gateway |

- **Contracts:** `backend/proto/*` (gRPC) and `backend/services/gateway/graph/schema.graphqls`
  (GraphQL). Regenerate with `make gen` in `backend/`.
- **Database:** each service migrates its own schema at startup from embedded SQL
  (`services/*/migrations`). Double-booking is impossible at the database level
  (exclusion constraints on accepted sessions).
- **Services trust the gateway** for authorization, and the gateway passes the acting
  user. Every gRPC call must also carry a shared `INTERNAL_TOKEN`, so an exposed service
  port isn't an open API.

### How "worldwide" works

- Every campus has its own **email domains, timezone and currency**.
- A student who verifies an email on their campus's domain is campus-verified straight
  away. Anyone else verifies by uploading a student ID, which an admin reviews.
  Providers always need the ID check.
- Anyone can **request a new campus**. Its domains count for verification only after an
  admin approves it. Personal mail domains (gmail.com, …) are refused. Approval
  retroactively verifies members who already confirmed an email on that domain.
- **Choices and limits are not hardcoded in the web app.** The lists students pick from
  (guide services, session types and lengths, community types, report reasons, post
  types) are defined once in the service that validates them, and served with their
  labels by the `catalog` GraphQL query. Business limits (booking notice, request caps,
  community and posting limits, OTP and login limits) are settings with defaults,
  overridable from `.env`; see `.env.example`.
- **The campus directory** comes from the 500-institution master list (India, Germany,
  London). It's checked in as `backend/services/identity/data/campus_directory.csv`
  and imported by the identity service at startup. The import is idempotent: it matches
  on current and original names, so admin edits, renames and rejections survive
  restarts.
  - The list has no websites or email domains, so imported campuses start with none, and
    their students verify with an ID card. An admin confirms the real domains under
    **Admin → Campuses → Edit**, using the "Details not verified" filter. Saving a domain
    automatically campus-verifies members who already confirmed an email on it.
  - To update the list:
    `python3 -I scripts/campus-directory/convert.py <list>.xlsx > services/identity/data/campus_directory.csv`
    (from `backend/`), then restart identity.
  - The API is GraphQL: `campuses(query, limit)` for search-as-you-type,
    `campusDirectory(query, country, page, pageSize)` for the paginated list, and
    `campusCountries` for per-country counts. Search matches every word against the name
    or the campus initials, so "iit bombay", "lpu" and "kings college" all work.
- Availability is stored as campus-local wall-clock times, and slots are computed per
  day in that timezone, so DST changes are handled. The app shows times in both the
  campus timezone and the viewer's own.
- Prices are integers in each currency's minor unit, and the web app formats them with
  `Intl`, including zero-decimal currencies like JPY.

### Communities

Groups students join directly: their batch, hostel, a club, an interest, or an exam.

- **Open** communities are joined in one tap. **Approval** communities take a request
  (with an optional note) that a moderator approves; their posts and members are visible
  to members only.
- **Campus** communities are only for verified students of that campus. **All-campus**
  ones (e.g. exam prep) are for anyone with a confirmed email.
- Every campus gets an **All students** community automatically, the first time anyone
  opens that campus's community list.
- Any campus-verified student can create up to 3 communities and becomes the owner. The
  owner picks moderators.
- Posts appear instantly. Members like and reply; the post's author is notified of replies
  unless they muted the community. Moderators pin and remove posts, handle reports raised
  inside the community, approve requests, and remove members (a 30-day ban). Blocking
  someone hides their posts and replies from you.
- Each community has a **Tutors** tab: the members who tutor, best rated first.

### Placement prep: mock interviews and resume reviews

A third kind of provider, the **career mentor**: a senior who has been placed or interned.
They list the companies they've worked at, and offer mock interviews, resume reviews,
or both. Like every provider, a mentor needs a verified student ID to publish. Students
find them under **Placement prep** (`/prep`), whose tabs are Mock interviews, Resume
reviews, Interview experiences and My progress.

- **Mock interviews** are ordinary bookings with an interview type (coding, system design,
  product, case, HR, and so on), so slots, accept/decline, chat, reminders and reviews
  all work as for tutoring. Once the interview has started, the mentor fills in a
  **scorecard**: 1–5 on each skill, a verdict ("ready", "almost", "needs more practice"),
  strengths, what to work on, and resources. Only the two of them see it, and the mentor
  can update it later.
- **Resume reviews** have no time slot. The student uploads a PDF and names the target
  role. The mentor accepts, which starts their promised turnaround (1–14 days), then
  delivers a score out of 10, a summary, notes on at least three sections, and
  optionally an annotated PDF. The student rates the review afterwards.
  - Unanswered requests expire after 3 days.
  - A student can have 3 reviews in progress at once.
  - A student can cancel an accepted review once it is overdue.
  - Resumes are private. Only the student and the mentor can open them, through
    15-minute signed links, and the gateway deletes the files 30 days after the review
    closes.
- **Live interview room** (`/interview/<booking>`), for every online session:
  - **When:** opens 10 minutes before the start and closes 30 minutes after the end
    (`ROOM_OPENS_BEFORE`, `ROOM_CLOSES_AFTER`). Only the two people on an accepted
    booking can enter.
  - **Lobby:** checks the camera, microphone and the other person's presence first, and
    offers an audio-only join for weak connections.
  - **In the room:**
    - video, mute, screen share, chat and an interview clock;
    - a shared code editor with five languages, where both people type at once and see
      each other's cursor;
    - a whiteboard for system design rounds;
    - the problem panel, which the mentor fills by typing or picking from the question
      bank.
  - **Mentor only:** a private panel for timestamped notes, quick tags and draft scores.
    "Finish & write scorecard" opens the scorecard pre-filled from them.
  - **Afterwards:** the code, whiteboard and problem are saved on the booking. A join log
    (who was in the room, and when) settles no-show disputes. When one person joins
    first, the other gets a "waiting in the room" notification.
  - **How it works:** the gateway checks the booking and signs a LiveKit pass that
    expires when the room closes. Media goes between the browser and the LiveKit
    server. The editor, whiteboard and chat sync over LiveKit's data channel (Yjs for
    the code), so no other server is needed.
- **Prep hub and progress:**
  - **Finding a mentor:** filter by company ("placed at"), see who has an open slot in
    the next 12 hours, and see each mentor's next slot and median reply time.
  - **Readiness:** a score from the student's latest scorecard result for each skill,
    with the resume score counting 15%. It names the weakest skill and suggests the
    interview type that practises it.
  - **My progress:** charts each skill across interviews, plus verdicts and resume
    scores.
- **Question bank** (`/prep/questions`): problems mentors save by type, difficulty and
  company, and pick inside the room. Students can't read it, so the questions stay
  unseen until the interview.
- **Interview experiences:** round-by-round write-ups by verified students, optionally
  anonymous. Each waits for admin approval (**Admin → Experiences**), like feed posts.
- **Pinned resume comments:** the mentor clicks a line on the PDF to pin a comment
  there. The student sees the pins on their resume.
- The interview types, scorecard criteria, resume sections and editor languages come from
  the `catalog` query. The limits are in `.env.example`.

### Messages

Private chat between two people who share at least one community.

- Start one with **Message** on a member's profile or in a community's member list.
- The first messages arrive as a **request** (at most 3 before it's answered). The
  recipient accepts, ignores or blocks; replying also accepts. Ignored requests just stay
  unanswered for the sender.
- **Blocking** stops messages both ways. Once two people no longer share a community, the
  chat becomes read-only.
- Inbox and Requests with unread counts, a chat icon in the top bar, and one
  notification per unread run.
- **Live delivery over WebSocket.** The app keeps one socket open to `/graphql`
  (`graphql-transport-ws`, the `messageEvents` subscription), so new messages appear
  the moment they're sent. Sending a message calls `pg_notify` in the same transaction.
  Every community-service instance `LISTEN`s and pushes the event down a gRPC stream to
  the gateway, so this works with several instances. The socket authenticates with the
  access token in `connection_init`, and only origins listed in `CORS_ORIGINS` may open
  one. It closes when the token expires, and the app reconnects with a fresh one. A slow
  poll (30–60 s) still runs as a fallback.
- Limits: `DM_NEW_PER_HOUR` (new chats per hour, default 10) and `DM_PENDING_LIMIT`
  (messages before a request is accepted, default 3).

### Trust & safety

- Passwords use bcrypt. The access token (15 min) lives in memory only. The refresh
  token is an httpOnly `SameSite=Strict` cookie that rotates on every use; reusing an
  old one revokes all of that user's sessions.
- Login, OTP, signup and contact are rate-limited, per email in identity and per IP at
  the gateway.
- Contact details (email, phone) are hidden until a session is accepted, and only the
  two people in that session see them. Everyone else sees first name + last initial.
- Student ID files are private, viewable only by admins through 15-minute HMAC-signed
  links, and deleted as soon as they're reviewed. Uploads are content-sniffed, never
  trusted by extension.
- Users can report profiles, reviews, posts and bookings, and can block others. Admins
  can suspend users and profiles, hide reviews and moderate posts.

## Performance

- **The web app loads per page.** Each route is its own chunk, fetched on first visit.
  React, the router and the WebSocket client sit in a `vendor` chunk that survives
  deploys. A first visit downloads about 88 KB (gzipped), down from 451 KB.
- **nginx gzips** scripts, styles and API JSON. GraphQL responses shrink about 3×.
- **The gateway batches lookups.** Users, campuses, subjects and profiles go through
  per-request loaders, so a page of results costs one call per kind, not one per item.
- **Admin stats** come from one pass over each table, using `FILTER`, instead of a
  subquery per number.

## Tests

```bash
cd backend
make test   # unit tests (slot calculation incl. DST)
make e2e    # loads ../.env; full journeys against the running stack, then deletes
            # every account and record the run created: signup→OTP→ID review→publish→
            # search→book→double-book refused→accept→chat→complete→review→block→feed→
            # reports; refresh-token rotation and theft detection; password reset;
            # communities: open/approval/campus-only joins, posts, replies, likes,
            # moderation, bans and blocking; direct messages incl. live
            # WebSocket delivery;
            # campus requests and approval; career mentors: mock interview
            # scorecards and the resume review lifecycle; placement prep: room
            # access window and passes, signed join-log webhooks, notes and
            # draft-score privacy, question bank access, progress, experiences
cd ../web && npm run build   # typecheck + production build
```

The e2e tests create accounts with unique emails and remove them, with everything they
created, when the run ends.

## Deliberately not built yet

- **Payments.** As decided in the plan, the MVP shows the agreed price and students pay
  each other directly. Escrow and payouts (Razorpay/Stripe) need a registered business,
  KYC and a refund policy first; the plan's Phase 6 covers it.
- **Object storage.** Uploads go to a Docker volume. `gateway/storage` keeps the
  Save/Delete/URL surface small so it can be swapped for S3/R2.

## Deploying

[`deploy/README.md`](deploy/README.md) walks through putting it on one server (AWS EC2
in Mumbai) with HTTPS, video rooms and nightly backups:
`setup-server.sh` → `make-env.sh` → `deploy.sh`.

## Before deploying anywhere public

Set real values for `JWT_SECRET`, `INTERNAL_TOKEN`, `FILE_SIGNING_SECRET`,
`POSTGRES_PASSWORD` and `ADMIN_PASSWORD` (see `.env.example`). Also:

- Set `DEV_MODE=false`, which turns off the playground and introspection and makes
  cookies `Secure`.
- Point SMTP at a real provider.
- **For video rooms:**
  - Browsers allow the camera only over HTTPS (or on localhost), so rooms need the HTTPS
    domain.
  - Set `LIVEKIT_PUBLIC_URL=wss://<domain>/…` behind your proxy, `LIVEKIT_BIND=0.0.0.0`
    and `LIVEKIT_USE_EXTERNAL_IP=true`.
  - Open TCP 7881 and UDP 52000–52100. The default range avoids 50000–50059, which
    Microsoft Teams uses.
- Serve over HTTPS.
- Keep `TRUST_PROXY=true` only when the gateway is reachable solely through your proxy.
