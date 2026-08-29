-- +goose Up

-- It holds the browser sessions scs hands out, which are a different
-- credential from users.api_key: a session expires, POST /logout destroys
-- it, and one account may hold several at once while the API key is one per
-- user and never expires.
--
-- What lands in token is a digest, not the token the browser holds — scs is
-- configured with HashTokenInStore, so a copy of the database file hands out
-- no live session. That is the session middleware's setting rather than this
-- table's, which is why the column's own comment does not claim it: a
-- comment inside a statement cannot be edited once the migration is merged,
-- and this one would go false the day that setting changed. The leading
-- comment can be edited, so it is where the claim belongs, alongside the
-- paragraph below.
--
-- There is no user_id column, and the absence is the design. scs writes this
-- row and knows nothing about one, so the user id lives inside data where
-- only scs can read it. The cost is that "log out every session of this
-- user" is not a query — it would have to iterate through scs.IterableStore
-- and decode each row — and nothing needs it yet, there being no password
-- change to invalidate sessions after. Adding the column would mean writing
-- the row twice, once by scs and once by this server, with no writer owning
-- it.
CREATE TABLE sessions (
    -- What scs keys a session by.
    token  TEXT PRIMARY KEY,

    -- scs's gob-encoded session data.
    data   BLOB NOT NULL,

    -- Unix seconds, UTC, per users.created_at. Its index serves the sweep, not reads.
    expiry INTEGER NOT NULL
) STRICT;

CREATE INDEX sessions_expiry_idx ON sessions (expiry);
