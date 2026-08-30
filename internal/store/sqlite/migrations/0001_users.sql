-- +goose Up

-- The accounts the API authenticates. Issued from the command line
-- (`travelmap user create`) or the browser sign-up screen, both through
-- auth.Register — neither needs a column of its own. Likewise no
-- status/plan/subscription_source/active_until: those are upstream Cloud's
-- billing fields, billing is a non-goal, and auth/login answers them with
-- constants instead.
--
-- STRICT, here and in every table that follows: without it SQLite's type
-- affinity accepts a string where an integer is declared and stores it as a
-- string, so a timestamp written the wrong way would be found by a query that
-- silently stops matching rather than by an error at the write.
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,

    -- NOCASE so that the address is one identity however it was typed. The
    -- unique index below inherits the collation, which is what stops
    -- "Alice@example.com" from becoming a second account.
    email         TEXT NOT NULL COLLATE NOCASE,

    -- The bcrypt digest, as produced by internal/auth.
    password_hash TEXT NOT NULL,

    -- Stored as issued rather than hashed; the reason is on model.User.APIKey.
    api_key       TEXT NOT NULL,

    -- Unix seconds, UTC. Times are integers throughout the schema because
    -- points.timestamp arrives from the API as a Unix timestamp and is compared
    -- by range on every request; storing some times as text would mean a
    -- conversion in those comparisons, and two ways to be wrong.
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE UNIQUE INDEX users_api_key_key ON users (api_key);
