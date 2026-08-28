-- +goose Up

-- Swarm (Foursquare) check-ins — travelmap's own extension, not a Dawarich
-- concept, which is why it lives outside the tables above; see "travelmap's
-- own extensions" in TODO.md. The push webhook and the periodic fetch both
-- write through internal/checkin, which upserts on foursquare_checkin_id:
-- push and the fetch will carry the same check-in, by design, so
-- idempotency lives in that index rather than in a look-before-you-write in
-- the caller. See "checkins" in docs/database.md for what a repeat write
-- keeps and what it overwrites.
CREATE TABLE checkins (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id               INTEGER NOT NULL REFERENCES users (id),

    -- 24 hex characters, the payload's checkin.id.
    foursquare_checkin_id TEXT NOT NULL,

    -- The check-in's own time (checkin.createdAt), not created_at below.
    checked_in_at         INTEGER NOT NULL,

    -- Minutes, checkin.timeZoneOffset; nullable like every column through shout, since the documented payload is narrower than what has been observed to arrive.
    timezone_offset       INTEGER,

    -- Nullable, like every venue-derived column below, for a check-in without a venue — a shape that has not itself been observed.
    venue_id              TEXT,
    venue_name            TEXT,
    latitude              REAL,
    longitude             REAL,

    -- The payload's cc: stable, unlike the localised text below.
    country_code          TEXT,

    city                  TEXT,
    state                 TEXT,
    country               TEXT,
    category_id           TEXT,
    category_name         TEXT,

    -- Absent as a key, not empty, in the observed push.
    shout                 TEXT,

    -- 'push' or 'sync'; a repeat write keeps this and created_at, refreshes everything else.
    source                TEXT NOT NULL,

    -- The check-in JSON as received; not this server's to re-request at will.
    raw                   TEXT NOT NULL,

    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX checkins_foursquare_checkin_id_key ON checkins (foursquare_checkin_id);

CREATE INDEX checkins_user_id_checked_in_at_idx ON checkins (
    -- Mirrors points(user_id, timestamp): narrows by user and time range first.
    user_id, checked_in_at
);

-- Links a travelmap account to a Swarm account, one row per user. Nothing is
-- collected for a user until this row exists, created by `travelmap
-- foursquare connect` or, once it lands, a browser-driven OAuth flow.
CREATE TABLE foursquare_accounts (
    -- One Swarm account per travelmap user.
    user_id            INTEGER PRIMARY KEY REFERENCES users (id),

    -- TEXT: the payload sends it quoted ("1709193"). Its unique index resolves a push to one user.
    foursquare_user_id TEXT NOT NULL,

    -- Stored as issued, not encrypted: the database file is already where secrets live.
    access_token       TEXT NOT NULL,

    -- The end of the last successful fetch window; NULL until the first one succeeds.
    synced_through     INTEGER,

    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX foursquare_accounts_foursquare_user_id_key ON foursquare_accounts (foursquare_user_id);
