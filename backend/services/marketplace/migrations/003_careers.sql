-- Career mentors: mock interviews (booked like any session) and resume
-- reviews (asynchronous, with a fixed price and turnaround).

ALTER TABLE provider_profiles DROP CONSTRAINT provider_profiles_kind_check;
ALTER TABLE provider_profiles ADD CONSTRAINT provider_profiles_kind_check CHECK (kind IN ('tutor', 'guide', 'mentor'));

ALTER TABLE provider_profiles
    -- Where a mentor has worked, interned or been placed.
    ADD COLUMN companies TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN resume_reviews BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN resume_price_minor BIGINT NOT NULL DEFAULT 0 CHECK (resume_price_minor >= 0),
    ADD COLUMN resume_turnaround_days SMALLINT NOT NULL DEFAULT 3 CHECK (resume_turnaround_days BETWEEN 1 AND 30),
    ADD COLUMN resume_reviews_delivered INT NOT NULL DEFAULT 0;
CREATE INDEX provider_profiles_companies_idx ON provider_profiles USING gin (companies);

-- The mentor's scorecard after a mock interview. Scores are {criterion: 1-5}.
CREATE TABLE interview_feedback (
    booking_id   UUID PRIMARY KEY REFERENCES bookings (id) ON DELETE CASCADE,
    scores       JSONB NOT NULL,
    verdict      TEXT NOT NULL CHECK (verdict IN ('ready', 'almost', 'not_yet')),
    strengths    TEXT NOT NULL,
    improvements TEXT NOT NULL,
    resources    TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE resume_reviews (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_profile_id UUID NOT NULL REFERENCES provider_profiles (id),
    reviewer_id         UUID NOT NULL,
    student_id          UUID NOT NULL,
    campus_id           UUID NOT NULL,
    target_role         TEXT NOT NULL,
    notes               TEXT NOT NULL DEFAULT '',
    -- Private uploads in the gateway's store; cleared when the files are purged.
    file_key            TEXT NOT NULL DEFAULT '',
    file_name           TEXT NOT NULL,
    feedback_file_key   TEXT NOT NULL DEFAULT '',
    price_minor         BIGINT NOT NULL,
    currency            TEXT NOT NULL,
    turnaround_days     SMALLINT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'requested'
                        CHECK (status IN ('requested', 'accepted', 'delivered', 'declined', 'cancelled', 'expired')),
    status_reason       TEXT NOT NULL DEFAULT '',
    cancelled_by        UUID,
    due_at              TIMESTAMPTZ,
    accepted_at         TIMESTAMPTZ,
    delivered_at        TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    overall_score       SMALLINT CHECK (overall_score BETWEEN 1 AND 10),
    summary             TEXT NOT NULL DEFAULT '',
    sections            JSONB NOT NULL DEFAULT '[]',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (reviewer_id <> student_id)
);
CREATE INDEX resume_reviews_student_idx ON resume_reviews (student_id, created_at DESC);
CREATE INDEX resume_reviews_reviewer_idx ON resume_reviews (reviewer_id, created_at DESC);
CREATE INDEX resume_reviews_open_idx ON resume_reviews (status, created_at) WHERE status IN ('requested', 'accepted');
CREATE INDEX resume_reviews_purge_idx ON resume_reviews (closed_at) WHERE file_key <> '' OR feedback_file_key <> '';

-- A review rates either a session or a resume review.
ALTER TABLE reviews ALTER COLUMN booking_id DROP NOT NULL;
ALTER TABLE reviews ADD COLUMN resume_review_id UUID UNIQUE REFERENCES resume_reviews (id);
ALTER TABLE reviews ADD CONSTRAINT reviews_one_target CHECK (num_nonnulls(booking_id, resume_review_id) = 1);
