-- Placement prep: live interview rooms, the mentors' question bank, and
-- pinned resume comments.

-- One room per booking, created the first time someone joins. It keeps what
-- the interview produced: the problem, the shared code (as plain text and as
-- the collaborative editor's state, so a browser that rejoins merges instead
-- of duplicating), the whiteboard, and the mentor's in-room scores.
CREATE TABLE interview_rooms (
    booking_id   UUID PRIMARY KEY REFERENCES bookings (id) ON DELETE CASCADE,
    problem      TEXT NOT NULL DEFAULT '',
    question_id  UUID,
    language     TEXT NOT NULL DEFAULT 'python',
    code         TEXT NOT NULL DEFAULT '',
    code_state   BYTEA,
    whiteboard   JSONB NOT NULL DEFAULT '[]',
    draft_scores JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Who was in the room and when, from the video server's webhooks. Settles
-- no-show disputes.
CREATE TABLE room_presence (
    id         BIGSERIAL PRIMARY KEY,
    booking_id UUID NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL,
    joined_at  TIMESTAMPTZ NOT NULL,
    left_at    TIMESTAMPTZ
);
CREATE INDEX room_presence_booking_idx ON room_presence (booking_id, joined_at);

-- The mentor's private notes, stamped with the interview clock.
CREATE TABLE mentor_notes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id UUID NOT NULL REFERENCES bookings (id) ON DELETE CASCADE,
    author_id  UUID NOT NULL,
    at_seconds INT NOT NULL CHECK (at_seconds >= 0),
    body       TEXT NOT NULL,
    tone       TEXT NOT NULL DEFAULT 'note' CHECK (tone IN ('plus', 'minus', 'note')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX mentor_notes_booking_idx ON mentor_notes (booking_id, at_seconds);

-- Problems mentors save and reuse. Mentors see each other's, students never
-- do: a question bank students could read would spoil the interviews.
CREATE TABLE questions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id      UUID NOT NULL,
    title          TEXT NOT NULL,
    body           TEXT NOT NULL,
    interview_type TEXT NOT NULL,
    difficulty     TEXT NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    companies      TEXT[] NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX questions_type_idx ON questions (interview_type, created_at DESC);
CREATE INDEX questions_companies_idx ON questions USING gin (companies);

-- Resume comments pinned to a spot on the PDF: [{page, x, y, section, comment}].
ALTER TABLE resume_reviews ADD COLUMN pins JSONB NOT NULL DEFAULT '[]';

-- Reply speed: when a provider first answered a request.
ALTER TABLE bookings ADD COLUMN answered_at TIMESTAMPTZ;
UPDATE bookings SET answered_at = updated_at WHERE status IN ('accepted', 'declined', 'completed');
CREATE INDEX bookings_answered_idx ON bookings (provider_profile_id, created_at) WHERE answered_at IS NOT NULL;
