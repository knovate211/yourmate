-- Private chat between two people who share a community. The first message
-- arrives as a request the other person accepts (or ignores), so members of
-- a large community can't be spammed by strangers.

CREATE TABLE conversations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The two people, stored in a fixed order so each pair has one conversation.
    user_a          UUID NOT NULL,
    user_b          UUID NOT NULL,
    requested_by    UUID NOT NULL,
    status          TEXT NOT NULL DEFAULT 'request' CHECK (status IN ('request', 'active', 'declined')),
    a_last_read_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    b_last_read_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (user_a < user_b),
    CHECK (requested_by IN (user_a, user_b)),
    UNIQUE (user_a, user_b)
);
CREATE INDEX conversations_a_idx ON conversations (user_a, last_message_at DESC);
CREATE INDEX conversations_b_idx ON conversations (user_b, last_message_at DESC);
CREATE INDEX conversations_requested_idx ON conversations (requested_by, created_at DESC);

CREATE TABLE direct_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       UUID NOT NULL,
    body            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX direct_messages_conversation_idx ON direct_messages (conversation_id, created_at);
