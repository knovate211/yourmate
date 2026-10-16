# Your Mate — Implementation Plan

> **Status (2026-10-14):** implemented in [`yourmate/`](yourmate/README.md). The stack
> changed from this plan at the user's request: **React (Vite) + Go gRPC services behind a
> GraphQL gateway + Postgres**, and the product is **worldwide** (multi-campus from day 1)
> rather than LPU-only. Phases 0–5, 7 and 8 are built; Phase 6 (payments) is deferred as
> decided below.


## TL;DR
Your Mate is a **campus-only, verified, peer-to-peer marketplace** for LPU students:
- **Students** find help.
- **Tutors** (seniors and strong students) teach subjects for money.
- **Guides** help newcomers with the campus: tours, hostel, admission, local know-how.

Today `yourmate/YOUR-MATE` is only a **static HTML/CSS mockup**: 7 pages, forms that submit
nowhere, and broken validation. Nothing from it can be reused as working code. Only the
content, the copy and the look carry over.

This plan rebuilds it as a real product in phases. **The MVP is: verified signup, then profiles
and search, then booking requests, then reviews.** Payment happens off-platform (UPI or cash) at
first, so demand gets tested before we build payments.

Recommended stack (same tools as our other projects):
- **Frontend:** Next.js (App Router, TypeScript, Tailwind) on Netlify. Public pages need SEO,
  same reasoning as `knovate-web`.
- **Backend:** one Go service (REST on `chi` + `pgx`), a modular monolith. Not microservices;
  at this scale they're overkill.
- **Data:** Postgres. Redis only for OTP and rate limits.
- **Files:** S3-compatible storage (Cloudflare R2) for ID cards and profile photos.
- **Email:** Resend or SES for OTPs and booking notifications.
- **Payments (Phase 6):** Razorpay.

---

## The problem (recap)
| Side | Pain | What Your Mate gives |
|---|---|---|
| Student (demand) | Coaching is expensive. Office hours are limited. No easy way to find a senior who did the same course. Freshers get lost on a huge campus. | A verified senior who knows the exact syllabus or the campus, bookable in a few taps |
| Tutor / Guide (supply) | Wants part-time income on campus. Their skills go unused. | A listing, bookings and payouts without leaving campus |
| Both | No trust in strangers from WhatsApp groups | Every user is a **verified LPU student**, plus ratings |

**What sets it apart:** trust from verification, plus the same campus context. Verification is the
core of the product, not a small feature.

---

## What carries over from the current mockup
- **Brand:** name, logo (`l11.png`, `l13.png`) and tagline ("learn or earn").
- **Information architecture:** Home, About (team), Contact, Login, Signup.
- **The three roles and the signup fields:** name, reg no, gender, email, phone, field of interest.
- **Campus imagery** (Uni Mall, open auditorium, etc.) for the landing and guide pages.

Dropped: all HTML/CSS/JS (malformed, not responsive, broken validation), and three separate
login pages. The new app has **one login**, and roles are attributes of the user.

---

## Key product decisions (recommendations; confirm in Phase 0)
1. **One account, many roles.** A user signs up as a student and can then *apply* to become a
   tutor and/or guide. That beats choosing a role at signup, because most tutors are students too.
2. **Verification = LPU email OTP + ID card review.**
   - Typing a registration number proves nothing.
   - Step 1: OTP to an `@lpu.in` email address. **Confirm this domain exists for all students.**
   - Step 2: providers (tutor/guide) must also upload their ID card. An admin approves it before
     they're listed.
   - Students only need step 1, to keep signup friction low.
3. **The MVP handles no money.** Bookings are agreed in the app and paid by UPI or cash directly.
   Holding other people's money in escrow needs a registered business, KYC, refunds and dispute
   handling. Only build that once bookings show real demand.
4. **Commission model (Phase 6):** a 10–15% platform fee on paid sessions. Tutors get paid out
   weekly.
5. **Sessions can be online or offline.** The tutor gives a meet link, or names a campus place
   (library, block, etc.).

---

## Data model (Postgres)
```
users              id, email (unique), password_hash, first_name, last_name, reg_no (unique),
                   gender, phone, avatar_url, is_email_verified, status (active/suspended),
                   created_at
user_roles         user_id, role (student|tutor|guide|admin)            PK(user_id, role)
verifications      id, user_id, id_card_url, status (pending|approved|rejected),
                   reviewed_by, reviewed_at, reject_reason

subjects           id, code (e.g. CSE310), name, department
tutor_profiles     user_id PK, headline, bio, hourly_rate, modes (online|offline|both),
                   status (pending|approved|paused), rating_avg, rating_count
tutor_subjects     tutor_id, subject_id, grade_obtained (optional)      PK(tutor_id, subject_id)
guide_profiles     user_id PK, bio, services[] (campus_tour, hostel_help, admission_help,
                   local_info), rate, languages[], status, rating_avg, rating_count
availability       id, provider_id, weekday, start_time, end_time

bookings           id, student_id, provider_id, kind (tutor|guide), subject_id NULL,
                   start_at, end_at, mode, location_or_link, price, notes,
                   status (requested|accepted|declined|cancelled|completed|no_show|disputed),
                   created_at, updated_at
messages           id, booking_id, sender_id, body, created_at        -- per-booking thread
reviews            id, booking_id (unique), reviewer_id, provider_id, rating 1-5, comment

payments  (Ph 6)   id, booking_id, razorpay_order_id, razorpay_payment_id, amount,
                   platform_fee, status (created|paid|refunded|failed)
payouts   (Ph 6)   id, provider_id, period_start, period_end, amount, upi_id, status, paid_at

campus_posts (Ph 7) id, author_id, type (event|notice|place|tip), title, body, image_url,
                   starts_at, status (pending|published|rejected)
reports            id, reporter_id, target_type (user|review|post), target_id, reason, status
contact_inquiries  id, name, email, message, created_at
```

## API surface (Go, `/api/v1`)
```
Auth        POST /auth/signup · POST /auth/verify-otp · POST /auth/login · POST /auth/refresh
            POST /auth/forgot · POST /auth/reset · GET /me · PATCH /me
Verify      POST /me/verification (ID card upload, presigned URL) · GET /me/verification
Roles       POST /me/tutor-profile · PATCH /me/tutor-profile · POST /me/guide-profile · ...
            PUT  /me/availability
Discover    GET  /tutors?subject&mode&min_rating&max_rate&sort · GET /tutors/{id}
            GET  /guides?service · GET /guides/{id} · GET /subjects?q
Bookings    POST /bookings · GET /bookings?role=student|provider&status
            POST /bookings/{id}/accept|decline|cancel|complete
            GET/POST /bookings/{id}/messages
Reviews     POST /bookings/{id}/review · GET /providers/{id}/reviews
Feed (Ph7)  GET /posts · POST /posts
Reports     POST /reports
Contact     POST /contact
Admin       GET /admin/verifications · POST /admin/verifications/{id}/approve|reject
            GET /admin/users · PATCH /admin/users/{id} (suspend) · GET /admin/reports
            GET /admin/bookings · GET /admin/metrics · CRUD /admin/subjects
Payments    POST /bookings/{id}/pay (create Razorpay order) · POST /webhooks/razorpay
 (Ph 6)     GET /admin/payouts · POST /admin/payouts/{id}/mark-paid
```
Auth: short-lived JWT access token plus an httpOnly refresh cookie, and bcrypt for passwords.
Role checks happen in middleware.

## Frontend (Next.js) routes
```
/                     landing (hero, how it works, the 2 services, testimonials, CTA)
/about  /contact      ported from the mockup, with the contact form wired to POST /contact
/login  /signup       one auth flow; signup → OTP screen → onboarding
/onboarding           pick "I want to learn" / "teach" / "guide" → profile setup
/tutors               search + filters (subject, mode, rating, price)
/tutors/[id]          profile, subjects, availability, reviews, "Request session"
/guides  /guides/[id] same pattern for guides
/dashboard            role-aware: my bookings (as student / as provider), requests to act on
/bookings/[id]        details, status actions, message thread, review after completion
/become-a-tutor  /become-a-guide   application + ID upload
/settings             profile, password, payout UPI (Ph 6)
/feed                 campus updates (Ph 7)
/admin/*              verification queue, users, reports, subjects, metrics (Ph 8)
```
Public pages (`/`, `/tutors`, profiles) are server-rendered for SEO. The dashboard and app
pages are client-side.

---

## Phased delivery

### Phase 0 — Decisions & setup (~2 days)
- Confirm the open items below, especially the LPU email domain and the payments approach.
- Repos: `yourmate-web` (Next.js) and `yourmate-api` (Go). Docker-compose for Postgres + Redis +
  API locally.
- CI: lint, typecheck, Go tests. Migrations with `goose` or `golang-migrate`.

### Phase 1 — Marketing site rebuild (~3–4 days)
- Port Home, About and Contact into Next.js + Tailwind, responsive at all widths. Reuse the copy,
  logo and campus images, compressed to WebP.
- Contact form → `POST /contact` (stored, plus an email to the team).
- Remove the mockup bugs (absolute Windows paths, malformed HTML, non-functional buttons).
- **Exit:** the public site is live on Netlify and Lighthouse scores ≥ 90.

### Phase 2 — Accounts & verification (~1 week)
- Signup → OTP to the college email → login. Forgot/reset password. Rate-limit OTP (Redis).
- Onboarding: choose an intent. Everyone gets `student`, and the tutor/guide application includes
  the ID card upload.
- Admin verification queue (minimal page behind the admin role).
- **Exit:** a new user can sign up, verify and apply as a tutor, and an admin can approve them.

### Phase 3 — Profiles & discovery (~1 week)
- Tutor/guide profile editor, subjects (seeded from the LPU course codes list), availability.
- `/tutors` search with filters and sorting (rating, price, newest). Postgres full-text search on
  headline/bio is enough. No search engine needed.
- **Exit:** a student can find an approved tutor for a subject and view their profile.

### Phase 4 — Bookings (~1.5 weeks)
- Request a session in an available slot → provider accepts or declines → either side can cancel
  under a policy → provider marks it complete.
- Per-booking message thread (polling at first; websockets later if needed).
- Email notifications on every status change. Reminder 1 hour before a session (cron in the Go
  service).
- Show the agreed price; payment happens off-platform for now.
- **Exit:** the full request → session → complete loop works end-to-end.

### Phase 5 — Reviews & trust (~4 days) — **MVP complete here**
- After `completed`, the student can leave one review per booking. Ratings are recalculated on
  the provider profile.
- Report a user or review. Admin can suspend users and hide reviews.
- Basic abuse controls: per-user booking request limits, and blocking a user.
- **Exit:** profiles show real ratings, and admins can act on reports.

### Phase 6 — Payments & payouts (~1.5 weeks; only after the pilot shows demand)
- **Prerequisite:** a registered entity, a Razorpay account with KYC, and a written refund/dispute
  policy.
- When a booking is accepted, the student pays via Razorpay Checkout. The webhook confirms payment
  and the booking becomes `paid`.
- Funds are held until the booking is `completed` (plus a 24-hour dispute window). Then the
  provider earns `price − platform_fee`.
- Weekly payout batch for admins (manual UPI transfer + mark paid). Razorpay Route can automate
  this later.
- Refunds for provider cancellations or no-shows.
- **Exit:** money flows correctly in Razorpay test mode, including refunds and a full payout cycle.

### Phase 7 — Campus feed (the "guide / surroundings" promise) (~4 days)
- Verified users post events, notices, places and tips. Admins moderate before publishing.
- The feed page appears in navigation, and guides can link posts from their profiles.

### Phase 8 — Admin panel & metrics (~1 week, partly built alongside earlier phases)
- Users, verifications, bookings, reports, subjects, payouts.
- Metrics dashboard (see below).

### Phase 9 — Pilot launch
- Soft-launch with one or two departments or hostels. Hand-recruit 20–30 tutors first, because
  supply has to exist before demand arrives.
- Weekly review of the metrics. Fix the funnel before adding features.

---

## Metrics to track from day 1
- **Funnel:** signups → email verified → profile complete → first booking.
- **Supply:** approved tutors per subject, and median response time to requests.
- **Demand:** searches with zero results (by subject; this shows which tutors to recruit),
  booking requests per week.
- **Quality:** accept rate, completion rate, average rating, repeat-booking rate.
- **(Phase 6)** GMV, take rate, refund rate.

## Security & compliance checklist
- Passwords hashed with bcrypt. No plaintext anywhere (the old login pages used `type="text"`).
- ID card images go in a private bucket. Admins see them only through short-lived signed URLs.
  Delete them once verification is done.
- Rate limits on auth and OTP endpoints. CSRF protection on the cookie refresh endpoint.
- Hide phone numbers until a booking is accepted. Keep messaging in the app.
- Privacy policy + terms (data collected, refund policy, code of conduct), required before
  payments.
- Don't publish registration numbers or personal emails. The current About page exposes the team
  members' reg numbers and emails; remove them.

---

## Effort estimate (one full-stack developer)
| Phase | Estimate |
|---|---|
| 0 Setup | 2 days |
| 1 Marketing site | 3–4 days |
| 2 Accounts & verification | 1 week |
| 3 Profiles & discovery | 1 week |
| 4 Bookings | 1.5 weeks |
| 5 Reviews & trust | 4 days |
| **MVP total (0–5)** | **≈ 5–6 weeks** |
| 6 Payments & payouts | 1.5 weeks |
| 7 Campus feed | 4 days |
| 8 Admin & metrics | 1 week |

---

## Open questions (answer in Phase 0)
1. Does every LPU student have an `@lpu.in` (or similar) email that can receive OTP? If not,
   verification has to rely on the ID card for everyone.
2. Is this LPU-only for good, or should the schema be multi-campus from day 1? Recommendation: add
   a `campus_id` column now. It's cheap and avoids a later migration.
3. Is there a registered entity for collecting payments? This decides when Phase 6 can start.
4. Does the university need to approve (or partner on) paid peer tutoring on campus?
5. Is a mobile app needed? Recommendation: a responsive web app / PWA first, and native only if
   the pilot shows the need.
6. Who works as admin/moderator during the pilot, and what's the approval turnaround target
   (e.g. 24 hours)?
