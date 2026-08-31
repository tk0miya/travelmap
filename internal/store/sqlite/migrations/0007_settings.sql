-- +goose Up

-- One row per user, created alongside it in the same transaction
-- (internal/auth.Register), seeded from model.DefaultSettings — never
-- written lazily on a later PATCH, so SettingsRepository.Get never has to
-- fall back to a computed default for a real account. Range and enum
-- validation (tracking_mode, distance_filter, ...) lives in Go, matching
-- checkins.source below.
--
-- immich_url, immich_api_key, photoprism_url and photoprism_api_key are not
-- columns here: that photo integration is a declared non-goal, so
-- internal/httpapi accepts those fields on a write and always answers them
-- null, storing none of them.
CREATE TABLE settings (
    user_id                             INTEGER PRIMARY KEY REFERENCES users (id),

    -- The 12 fields of GET/PATCH /api/v1/settings/mobile.
    tracking_mode                       TEXT NOT NULL,
    tracking_visits                     INTEGER NOT NULL,
    track_visits_independently          INTEGER NOT NULL,
    auto_start                          INTEGER NOT NULL,
    distance_filter                     INTEGER NOT NULL,
    time_filter                         INTEGER NOT NULL,

    -- The device's own track-splitting setting, distinct from tracking.track_break_minutes.
    track_break                         INTEGER NOT NULL,

    accuracy                            INTEGER NOT NULL,
    show_background_location_indicator  INTEGER NOT NULL,
    upload_automatically                INTEGER NOT NULL,
    upload_all_on_tracking_stop         INTEGER NOT NULL,
    batch_size                          INTEGER NOT NULL,

    -- The map/route-drawing fields of GET/PATCH /api/v1/settings below.

    -- A fraction (0.0-1.0); /api/v1/settings itself speaks a 0-100 percentage instead.
    route_opacity                       REAL NOT NULL,

    meters_between_routes               INTEGER NOT NULL,
    minutes_between_routes              INTEGER NOT NULL,
    fog_of_war_meters                   INTEGER NOT NULL,
    time_threshold_minutes              INTEGER NOT NULL,
    merge_threshold_minutes             INTEGER NOT NULL,
    preferred_map_layer                 TEXT NOT NULL,
    speed_colored_routes                INTEGER NOT NULL,
    points_rendering_mode               TEXT NOT NULL,
    live_map_enabled                    INTEGER NOT NULL,

    -- NULL unless the user picked a scale; upstream has no default for it.
    speed_color_scale                   TEXT,

    fog_of_war_threshold                INTEGER NOT NULL,
    distance_unit                       TEXT NOT NULL,

    created_at                          INTEGER NOT NULL,
    updated_at                          INTEGER NOT NULL
) STRICT;
