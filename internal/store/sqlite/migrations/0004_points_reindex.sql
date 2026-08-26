-- +goose Up

-- points_user_id_timestamp_key shipped in 0002_points.sql with its rationale
-- comment before the statement, where SQLite drops it from sqlite_master —
-- comments only survive inside a statement's own parentheses (see
-- "Documents" in CLAUDE.md). Editing 0002 in place would not reach a
-- database that already ran it, so the index is dropped and recreated here
-- instead, with no structural change, so the comment reaches schema.sql too.
DROP INDEX points_user_id_timestamp_key;

CREATE UNIQUE INDEX points_user_id_timestamp_key ON points (
    -- Deduplicates inserts on conflict; also serves GET /points' time filter (Step 10).
    user_id, timestamp
);
