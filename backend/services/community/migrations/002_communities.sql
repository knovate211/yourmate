-- Communities students join: a batch, a hostel, a club, an interest or an
-- exam they're preparing for. Open ones are joined in one tap; approval ones
-- by request. Posts appear instantly and are moderated after the fact.

ALTER TABLE reports DROP CONSTRAINT reports_target_type_check;
ALTER TABLE reports ADD CONSTRAINT reports_target_type_check
    CHECK (target_type IN ('user', 'provider', 'review', 'post', 'booking', 'community', 'community_post'));

CREATE TABLE communities (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         TEXT NOT NULL UNIQUE,
    -- NULL: open to students of every campus.
    campus_id    UUID,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    kind         TEXT NOT NULL CHECK (kind IN ('general', 'course', 'hostel', 'club', 'interest', 'exam')),
    join_policy  TEXT NOT NULL CHECK (join_policy IN ('open', 'approval')),
    rules        TEXT NOT NULL DEFAULT '',
    -- NULL for a campus's default "All students" community, which admins run.
    owner_id     UUID,
    is_default   BOOLEAN NOT NULL DEFAULT false,
    member_count INT NOT NULL DEFAULT 0 CHECK (member_count >= 0),
    post_count   INT NOT NULL DEFAULT 0 CHECK (post_count >= 0),
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX communities_name_idx
    ON communities (coalesce(campus_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name)) WHERE status = 'active';
CREATE UNIQUE INDEX communities_default_idx ON communities (campus_id) WHERE is_default;
CREATE INDEX communities_list_idx ON communities (status, campus_id, member_count DESC);

CREATE TABLE community_members (
    community_id UUID NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'moderator', 'owner')),
    muted        BOOLEAN NOT NULL DEFAULT false,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, user_id)
);
CREATE INDEX community_members_user_idx ON community_members (user_id);

CREATE TABLE community_join_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id UUID NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL,
    note         TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'declined')),
    decided_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at   TIMESTAMPTZ
);
CREATE UNIQUE INDEX community_join_requests_pending_idx ON community_join_requests (community_id, user_id) WHERE status = 'pending';
CREATE INDEX community_join_requests_user_idx ON community_join_requests (user_id, community_id, created_at DESC);

-- People a moderator removed can't rejoin until the ban ends.
CREATE TABLE community_bans (
    community_id UUID NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL,
    until        TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (community_id, user_id)
);

CREATE TABLE community_posts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    community_id UUID NOT NULL REFERENCES communities (id) ON DELETE CASCADE,
    author_id    UUID NOT NULL,
    body         TEXT NOT NULL,
    pinned       BOOLEAN NOT NULL DEFAULT false,
    like_count   INT NOT NULL DEFAULT 0 CHECK (like_count >= 0),
    reply_count  INT NOT NULL DEFAULT 0 CHECK (reply_count >= 0),
    removed      BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX community_posts_feed_idx ON community_posts (community_id, pinned DESC, created_at DESC) WHERE NOT removed;
CREATE INDEX community_posts_author_idx ON community_posts (author_id, created_at DESC);

CREATE TABLE community_post_likes (
    post_id UUID NOT NULL REFERENCES community_posts (id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    PRIMARY KEY (post_id, user_id)
);

CREATE TABLE community_replies (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    UUID NOT NULL REFERENCES community_posts (id) ON DELETE CASCADE,
    author_id  UUID NOT NULL,
    body       TEXT NOT NULL,
    removed    BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX community_replies_post_idx ON community_replies (post_id, created_at);
CREATE INDEX community_replies_author_idx ON community_replies (author_id, created_at DESC);
