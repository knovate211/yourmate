-- Campuses can now come from an imported directory (data/campus_directory.csv)
-- whose website and email domains still need confirming by an admin.
ALTER TABLE campuses
    ADD COLUMN website          TEXT NOT NULL DEFAULT '',
    ADD COLUMN source           TEXT NOT NULL DEFAULT 'seed' CHECK (source IN ('seed', 'directory', 'request')),
    ADD COLUMN details_verified BOOLEAN NOT NULL DEFAULT false,
    -- The name a campus had in the directory file, so a later rename by an
    -- admin doesn't make the next import add it again.
    ADD COLUMN directory_name   TEXT;

-- The hand-picked starter campuses were checked when they were written.
UPDATE campuses SET details_verified = true WHERE requested_by IS NULL AND status = 'active';
-- Everything else so far came from "Add your campus" requests.
UPDATE campuses SET source = 'request' WHERE requested_by IS NOT NULL OR status <> 'active';

CREATE INDEX campuses_lower_name_idx ON campuses (lower(name));
CREATE UNIQUE INDEX campuses_directory_name_idx ON campuses (lower(directory_name)) WHERE directory_name IS NOT NULL;
CREATE INDEX campuses_country_idx ON campuses (status, country, name);

-- Initials of the significant words, so "iit", "iitb", "lpu" or "tum" find
-- "Indian Institute of Technology Bombay", "Lovely Professional University"
-- and "Technical University of Munich".
CREATE FUNCTION campus_acronym(n TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE AS $$
    SELECT coalesce(string_agg(left(w, 1), '' ORDER BY ord), '')
    FROM regexp_split_to_table(lower(regexp_replace(n, '[^[:alnum:] ]', ' ', 'g')), '\s+') WITH ORDINALITY AS t(w, ord)
    WHERE w <> '' AND w NOT IN ('of', 'and', 'the', 'for', 'at', 'in')
$$;
ALTER TABLE campuses ADD COLUMN acronym TEXT GENERATED ALWAYS AS (campus_acronym(name)) STORED;
CREATE INDEX campuses_acronym_idx ON campuses (acronym text_pattern_ops);
