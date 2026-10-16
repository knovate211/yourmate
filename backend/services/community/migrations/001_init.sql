-- Campus feed: verified students share events, notices, places and tips.
-- Posts are moderated before they appear.
CREATE TABLE posts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campus_id     UUID NOT NULL,
    author_id     UUID NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('event', 'notice', 'place', 'tip')),
    title         TEXT NOT NULL,
    body          TEXT NOT NULL,
    image_key     TEXT NOT NULL DEFAULT '',
    event_at      TIMESTAMPTZ,
    location      TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'rejected')),
    review_reason TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX posts_feed_idx ON posts (campus_id, status, created_at DESC);
CREATE INDEX posts_author_idx ON posts (author_id, created_at DESC);

CREATE TABLE reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id     UUID NOT NULL,
    target_type     TEXT NOT NULL CHECK (target_type IN ('user', 'provider', 'review', 'post', 'booking')),
    target_id       UUID NOT NULL,
    reason          TEXT NOT NULL,
    details         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    resolution_note TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ
);
CREATE INDEX reports_status_idx ON reports (status, created_at);
-- One open report per reporter per target.
CREATE UNIQUE INDEX reports_open_unique_idx ON reports (reporter_id, target_type, target_id) WHERE status = 'open';

CREATE TABLE inquiries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    email      TEXT NOT NULL,
    message    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX inquiries_created_idx ON inquiries (created_at DESC);
