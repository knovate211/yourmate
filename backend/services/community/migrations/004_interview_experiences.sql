-- Interview experiences: seniors' round-by-round write-ups of a company's
-- process. Moderated like the campus feed: published once an admin approves.
CREATE TABLE interview_experiences (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id     UUID NOT NULL,
    campus_id     UUID NOT NULL,
    company       TEXT NOT NULL,
    role          TEXT NOT NULL,
    year          SMALLINT NOT NULL CHECK (year BETWEEN 2000 AND 2100),
    outcome       TEXT NOT NULL CHECK (outcome IN ('selected', 'rejected', 'waiting')),
    rounds        JSONB NOT NULL,          -- [{name, details}]
    tips          TEXT NOT NULL DEFAULT '',
    anonymous     BOOLEAN NOT NULL DEFAULT false,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'rejected')),
    review_reason TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX interview_experiences_company_idx ON interview_experiences (lower(company), created_at DESC) WHERE status = 'published';
CREATE INDEX interview_experiences_status_idx ON interview_experiences (status, created_at);
CREATE INDEX interview_experiences_author_idx ON interview_experiences (author_id, created_at DESC);
