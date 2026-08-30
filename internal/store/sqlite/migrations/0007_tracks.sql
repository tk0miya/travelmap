-- +goose Up

-- One contiguous run of a user's points, split from its neighbours by a gap
-- exceeding TRAVELMAP_TRACK_BREAK_MINUTES of inactivity — the same value
-- daily_stats' own segment attribution uses (see "Segment attribution" in
-- docs/database.md). internal/track is the only writer: every track is
-- rebuilt from scratch whenever a point changes anywhere in the user's
-- history, since a single new point can shift where every later boundary
-- falls, the same "rebuild, never adjust arithmetically" rule daily_stats
-- follows.
CREATE TABLE tracks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users (id),

    start_at   INTEGER NOT NULL,
    end_at     INTEGER NOT NULL,

    distance   REAL NOT NULL, -- Metres; avg_speed and duration are derived from this and start_at/end_at, not stored.

    -- JSON array of [longitude, latitude] pairs, one per point, in timestamp
    -- order — precomputed so GET /api/v1/tracks needs no join against points.
    geometry   TEXT NOT NULL,

    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE INDEX tracks_user_id_start_at_idx ON tracks (
    -- Mirrors points(user_id, timestamp): narrows by user and time range first.
    user_id, start_at
);

-- One pending rebuild request per user, drained by internal/track's
-- background worker — the "Background workers" row's rare per-item
-- exception in docs/architecture.md. A user already queued is left alone
-- rather than duplicated: whatever runs next rereads every point that
-- exists by then, so a second request would do no more than the first.
CREATE TABLE track_split_jobs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT, -- NextPending's FIFO order; requested_at alone ties when two requests coalesce within the same second.
    user_id      INTEGER NOT NULL UNIQUE REFERENCES users (id),
    requested_at INTEGER NOT NULL
) STRICT;
