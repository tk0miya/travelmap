-- +goose Up

-- Locations recorded from a device. Step 7 predates internal/ingest, so it
-- writes here directly; from Step 9 on, internal/ingest is the only caller,
-- because every mutation also has to rebuild the affected days of
-- daily_stats.
--
-- Columns cover what POST /api/v1/points and POST /api/v1/overland/batches
-- actually populate, not the full width of upstream's own points table: that
-- one also carries columns for OwnTracks/Traccar ingest, imports, visits and
-- reverse geocoding, none of which this server implements yet. A step that
-- adds one of those adds the columns it needs in its own migration, rather
-- than every column of an unbuilt feature sitting unused.
CREATE TABLE points (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           INTEGER NOT NULL REFERENCES users (id),

    -- Unix seconds, UTC, for the reason on users.created_at.
    timestamp         INTEGER NOT NULL,

    latitude          REAL NOT NULL,
    longitude         REAL NOT NULL,

    -- Everything below is a device property that may or may not have been
    -- sent, so NULL is a real value here: it means "not reported", not zero.
    altitude          REAL,
    velocity          REAL,
    accuracy          REAL,
    vertical_accuracy REAL,
    course            REAL,
    course_accuracy   REAL,
    battery_status    TEXT,
    battery           REAL,
    ssid              TEXT,
    tracker_id        TEXT,

    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX points_user_id_timestamp_key ON points (user_id, timestamp);

-- No latitude/longitude index and no R*Tree: no in-scope endpoint takes a
-- bounding box (GET /points accepts only start_at/end_at/page/per_page/
-- order), and an index with no query to serve costs insert time and storage
-- for nothing. Add one if and when rectangular search is actually needed.
