CREATE EXTENSION IF NOT EXISTS btree_gist WITH SCHEMA public;

-- Subjects are global (campus_id NULL) or specific to one campus, e.g. a
-- course code that only exists there.
CREATE TABLE subjects (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campus_id  UUID,
    code       TEXT NOT NULL DEFAULT '',
    name       TEXT NOT NULL,
    category   TEXT NOT NULL DEFAULT 'Other',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX subjects_unique_name_idx
    ON subjects (coalesce(campus_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
CREATE INDEX subjects_campus_idx ON subjects (campus_id);

-- One profile per user per kind. Display name, avatar, campus, currency and
-- timezone are copied from identity so search never needs a cross-service call.
CREATE TABLE provider_profiles (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL,
    kind               TEXT NOT NULL CHECK (kind IN ('tutor', 'guide')),
    campus_id          UUID NOT NULL,
    timezone           TEXT NOT NULL,
    display_name       TEXT NOT NULL,
    avatar_key         TEXT NOT NULL DEFAULT '',
    headline           TEXT NOT NULL,
    bio                TEXT NOT NULL,
    rate_minor         BIGINT NOT NULL CHECK (rate_minor >= 0),
    currency           TEXT NOT NULL,
    modes              TEXT[] NOT NULL,
    languages          TEXT[] NOT NULL DEFAULT '{}',
    services           TEXT[] NOT NULL DEFAULT '{}',
    status             TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'suspended')),
    rating_avg         DOUBLE PRECISION NOT NULL DEFAULT 0,
    rating_count       INT NOT NULL DEFAULT 0,
    sessions_completed INT NOT NULL DEFAULT 0,
    search             TSVECTOR GENERATED ALWAYS AS (
                           to_tsvector('simple', display_name || ' ' || headline || ' ' || bio)) STORED,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind)
);
CREATE INDEX provider_profiles_search_idx ON provider_profiles USING gin (search);
CREATE INDEX provider_profiles_listing_idx ON provider_profiles (kind, status, campus_id);

CREATE TABLE provider_subjects (
    profile_id UUID NOT NULL REFERENCES provider_profiles (id) ON DELETE CASCADE,
    subject_id UUID NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    PRIMARY KEY (profile_id, subject_id)
);
CREATE INDEX provider_subjects_subject_idx ON provider_subjects (subject_id);

-- Weekly recurring availability, in the provider's campus timezone. Shared by
-- a user's tutor and guide profiles because it is the same person's time.
CREATE TABLE availability (
    user_id      UUID NOT NULL,
    weekday      SMALLINT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    start_minute SMALLINT NOT NULL CHECK (start_minute BETWEEN 0 AND 1439),
    end_minute   SMALLINT NOT NULL CHECK (end_minute BETWEEN 1 AND 1440),
    CHECK (end_minute > start_minute),
    PRIMARY KEY (user_id, weekday, start_minute)
);

CREATE TABLE bookings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_profile_id UUID NOT NULL REFERENCES provider_profiles (id),
    provider_id         UUID NOT NULL,
    student_id          UUID NOT NULL,
    kind                TEXT NOT NULL,
    campus_id           UUID NOT NULL,
    subject_id          UUID REFERENCES subjects (id),
    service             TEXT NOT NULL DEFAULT '',
    start_at            TIMESTAMPTZ NOT NULL,
    end_at              TIMESTAMPTZ NOT NULL,
    mode                TEXT NOT NULL CHECK (mode IN ('online', 'in_person')),
    location            TEXT NOT NULL DEFAULT '',
    meeting_link        TEXT NOT NULL DEFAULT '',
    price_minor         BIGINT NOT NULL,
    currency            TEXT NOT NULL,
    notes               TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'requested'
                        CHECK (status IN ('requested', 'accepted', 'declined', 'cancelled', 'completed', 'expired')),
    status_reason       TEXT NOT NULL DEFAULT '',
    cancelled_by        UUID,
    reminder_sent       BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_at > start_at),
    CHECK (provider_id <> student_id),
    -- Nobody can be in two accepted sessions at once, as provider or student.
    CONSTRAINT bookings_provider_no_overlap EXCLUDE USING gist (
        provider_id WITH =, tstzrange(start_at, end_at) WITH &&) WHERE (status = 'accepted'),
    CONSTRAINT bookings_student_no_overlap EXCLUDE USING gist (
        student_id WITH =, tstzrange(start_at, end_at) WITH &&) WHERE (status = 'accepted')
);
CREATE INDEX bookings_student_idx ON bookings (student_id, start_at DESC);
CREATE INDEX bookings_provider_idx ON bookings (provider_id, start_at DESC);
CREATE INDEX bookings_open_idx ON bookings (status, start_at) WHERE status IN ('requested', 'accepted');

CREATE TABLE messages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id UUID NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    sender_id  UUID NOT NULL,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX messages_booking_idx ON messages (booking_id, created_at);

CREATE TABLE reviews (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id          UUID NOT NULL UNIQUE REFERENCES bookings (id),
    reviewer_id         UUID NOT NULL,
    provider_profile_id UUID NOT NULL REFERENCES provider_profiles (id),
    rating              SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment             TEXT NOT NULL DEFAULT '',
    hidden              BOOLEAN NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX reviews_profile_idx ON reviews (provider_profile_id, created_at DESC);

CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL,
    type       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

CREATE TABLE blocks (
    user_id    UUID NOT NULL,
    blocked_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, blocked_id),
    CHECK (user_id <> blocked_id)
);
CREATE INDEX blocks_blocked_idx ON blocks (blocked_id);
