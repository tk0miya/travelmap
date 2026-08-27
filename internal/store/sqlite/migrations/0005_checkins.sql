-- +goose Up

-- Swarm (Foursquare) check-ins — travelmap's own extension, not a Dawarich
-- concept, which is why it lives outside the tables above; see "travelmap's
-- own extensions" in TODO.md. The push webhook (Step 18) and the periodic
-- fetch (Step 19) both write through internal/checkin, which upserts on
-- foursquare_checkin_id: push and the fetch will carry the same check-in, by
-- design, so idempotency lives in that index rather than in a
-- look-before-you-write in the caller. See "checkins" in docs/database.md
-- for what a repeat write keeps and what it overwrites.
CREATE TABLE checkins (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id               INTEGER NOT NULL REFERENCES users (id),

    -- 24 hex characters, the payload's checkin.id.
    foursquare_checkin_id TEXT NOT NULL,

    -- The check-in's own time (checkin.createdAt), not this row's own
    -- bookkeeping below.
    checked_in_at         INTEGER NOT NULL,

    -- Minutes, checkin.timeZoneOffset. Nullable along with everything below
    -- through shout: the documented sample payload is narrower than what
    -- has been observed to arrive, so almost every field here may or may
    -- not be sent.
    timezone_offset       INTEGER,

    -- Nullable for a check-in made without one; the venueless shape has not
    -- been observed, so every other venue-derived column below is nullable
    -- for the same reason.
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

    -- 'push' or 'sync', naming the path that first observed the check-in. A
    -- repeat write keeps this and created_at and refreshes everything else.
    source                TEXT NOT NULL,

    -- The check-in JSON as received; the payload carries fields no column
    -- here does, and re-requesting it later spends the fetch path's rate
    -- limit for nothing.
    raw                   TEXT NOT NULL,

    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX checkins_foursquare_checkin_id_key ON checkins (foursquare_checkin_id);

CREATE INDEX checkins_user_id_checked_in_at_idx ON checkins (
    -- Mirrors points(user_id, timestamp): app queries narrow by user and
    -- time range first.
    user_id, checked_in_at
);

-- Links a travelmap account to a Swarm account, one row per user. Nothing is
-- collected for a user until this row exists, created by `travelmap
-- foursquare connect` (Step 17) or, once it lands, Step 20's browser flow.
CREATE TABLE foursquare_accounts (
    -- One Swarm account per travelmap user.
    user_id            INTEGER PRIMARY KEY REFERENCES users (id),

    -- TEXT: the payload sends it quoted ("1709193"). The unique index below
    -- is what resolves an incoming push to exactly one travelmap user.
    foursquare_user_id TEXT NOT NULL,

    -- Stored as issued: the database file is already the one place secrets
    -- live, per "Third-party credentials" in TODO.md's Technical Decisions.
    access_token       TEXT NOT NULL,

    -- The end of the last successful fetch window. NULL until the first one
    -- succeeds.
    synced_through     INTEGER,

    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX foursquare_accounts_foursquare_user_id_key ON foursquare_accounts (foursquare_user_id);
