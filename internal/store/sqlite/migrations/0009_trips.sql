-- +goose Up

-- A trip: a title and a time range the user typed in the browser, nothing
-- else. Nothing derives a trip and nothing rebuilds one, unlike daily_stats
-- and tracks above: every row here is what the user entered, so no config
-- change invalidates it and no recalculation pass touches this table.
--
-- Ranges may overlap, and nothing here rejects one that does — "Europe"
-- containing "Paris" is a real case a traveller has, and the timeline this
-- feeds is assembled from a time range rather than from rows that point at
-- a trip, so two trips covering the same hour is not an ambiguity anything
-- has to resolve.
--
-- No column holds what a trip contains, and none is planned: its contents
-- are found by reading points and check-ins against the range, not stored
-- as a join.
CREATE TABLE trips (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users (id),

    title        TEXT NOT NULL,

    started_at   INTEGER NOT NULL, -- Unix seconds UTC, per users.created_at.
    ended_at     INTEGER NOT NULL, -- Exclusive upper bound, like tracks.end_at.

    -- Free text about the trip as a whole; deliberately not called "note", which is a different, not-yet-built thing carrying its own timestamp.
    description  TEXT NOT NULL,

    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX trips_user_id_started_at_idx ON trips (
    -- Mirrors points(user_id, timestamp): narrows by user and time range first.
    user_id, started_at
);
