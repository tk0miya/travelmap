-- +goose Up

-- A precomputed per-day aggregate. /stats and /points/tracked_months (Step 11)
-- read only from here and must never aggregate points directly, per
-- "daily_stats" under "Data Model" in TODO.md.
--
-- Rebuilt from scratch, never adjusted arithmetically: a row is deleted
-- entirely once its day has zero points, rather than kept at zero, per the
-- same section.
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
