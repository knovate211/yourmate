CREATE TABLE campuses (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    country       TEXT NOT NULL,
    city          TEXT NOT NULL DEFAULT '',
    timezone      TEXT NOT NULL,
    currency      TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    -- Students who verify an email on one of these domains (or a subdomain)
    -- count as campus-verified. Only trusted once an admin activates the campus.
    email_domains TEXT[] NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('active', 'pending', 'rejected')),
    requested_by  UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX campuses_status_name_idx ON campuses (status, name);
CREATE INDEX campuses_domains_idx ON campuses USING gin (email_domains);

CREATE TABLE users (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                 TEXT NOT NULL UNIQUE CHECK (email = lower(email)),
    password_hash         TEXT NOT NULL,
    first_name            TEXT NOT NULL,
    last_name             TEXT NOT NULL DEFAULT '',
    campus_id             UUID NOT NULL REFERENCES campuses (id),
    student_id_no         TEXT NOT NULL DEFAULT '',
    gender                TEXT NOT NULL DEFAULT '',
    phone                 TEXT NOT NULL DEFAULT '',
    avatar_key            TEXT NOT NULL DEFAULT '',
    bio                   TEXT NOT NULL DEFAULT '',
    email_verified        BOOLEAN NOT NULL DEFAULT false,
    campus_email_verified BOOLEAN NOT NULL DEFAULT false,
    id_verified           BOOLEAN NOT NULL DEFAULT false,
    is_admin              BOOLEAN NOT NULL DEFAULT false,
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX users_campus_idx ON users (campus_id);
CREATE INDEX users_created_idx ON users (created_at DESC);

-- One-time codes for email verification and password reset. Only a hash of
-- the code is stored.
CREATE TABLE email_otps (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL CHECK (purpose IN ('verify', 'reset')),
    code_hash   TEXT NOT NULL,
    attempts    INT NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX email_otps_user_idx ON email_otps (user_id, purpose, created_at DESC);

-- Refresh tokens rotate on every use; a reused (already rotated) token
-- revokes the whole family because it means the token leaked.
CREATE TABLE refresh_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);

CREATE TABLE login_failures (
    email  TEXT NOT NULL,
    at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX login_failures_email_idx ON login_failures (email, at DESC);

-- Student ID card reviews. The document itself lives in file storage and is
-- deleted once reviewed; only the key is kept here.
CREATE TABLE verifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_key TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewer_id  UUID,
    reason       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX verifications_one_pending_idx ON verifications (user_id) WHERE status = 'pending';
CREATE INDEX verifications_status_idx ON verifications (status, created_at);

-- True when the email's domain is one of domains or a subdomain of one
-- (e.g. s123@student.uni.edu matches uni.edu).
CREATE FUNCTION domain_matches(email TEXT, domains TEXT[]) RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM unnest(domains) AS d
        WHERE split_part(email, '@', 2) = d OR split_part(email, '@', 2) LIKE '%.' || d
    )
$$;
