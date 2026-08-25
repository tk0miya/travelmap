-- +goose Up

-- A precomputed per-day aggregate; see "daily_stats" in docs/database.md for
-- why nothing else may compute one by aggregating points directly, and for
-- the rebuild-from-scratch and delete-at-zero rules it is rebuilt under.
CREATE TABLE daily_stats (
    user_id                 INTEGER NOT NULL REFERENCES users (id),

    -- Local midnight of the day this row covers, formatted "YYYY-MM-DD" in
    -- whatever timezone TRAVELMAP_TIMEZONE named when the row was built.
    day                     TEXT NOT NULL,

    points                  INTEGER NOT NULL,
    reverse_geocoded_points INTEGER NOT NULL,
    km                      REAL NOT NULL,

    -- JSON arrays of the country and city names visited that day. Empty
    -- ("[]") until reverse geocoding (Milestone G) is enabled.
    countries               TEXT NOT NULL,
    cities                  TEXT NOT NULL,

    PRIMARY KEY (user_id, day)
) STRICT;
