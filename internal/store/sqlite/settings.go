package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// settingsColumns is the select list [scanSettings] reads, in that order.
const settingsColumns = `user_id, tracking_mode, tracking_visits, track_visits_independently,
	auto_start, distance_filter, time_filter, track_break, accuracy,
	show_background_location_indicator, upload_automatically, upload_all_on_tracking_stop,
	batch_size, route_opacity, meters_between_routes, minutes_between_routes, fog_of_war_meters,
	time_threshold_minutes, merge_threshold_minutes, preferred_map_layer, speed_colored_routes,
	points_rendering_mode, live_map_enabled, speed_color_scale, fog_of_war_threshold,
	distance_unit, created_at, updated_at`

// settingsRepository implements [store.SettingsRepository].
type settingsRepository struct {
	q querier
}

// Get implements [store.SettingsRepository].
func (r settingsRepository) Get(ctx context.Context, userID int64) (model.Settings, error) {
	settings, err := scanSettings(r.q.QueryRowContext(ctx,
		`SELECT `+settingsColumns+` FROM settings WHERE user_id = ?`, userID))
	if err != nil {
		return model.Settings{}, fmt.Errorf("sqlite: looking up user %d's settings: %w", userID, err)
	}

	return settings, nil
}

// Create implements [store.SettingsRepository].
func (r settingsRepository) Create(ctx context.Context, settings model.Settings) (model.Settings, error) {
	now := time.Now().UTC().Truncate(time.Second)

	row := r.q.QueryRowContext(ctx,
		`INSERT INTO settings (
			user_id, tracking_mode, tracking_visits, track_visits_independently,
			auto_start, distance_filter, time_filter, track_break, accuracy,
			show_background_location_indicator, upload_automatically, upload_all_on_tracking_stop,
			batch_size, route_opacity, meters_between_routes, minutes_between_routes, fog_of_war_meters,
			time_threshold_minutes, merge_threshold_minutes, preferred_map_layer, speed_colored_routes,
			points_rendering_mode, live_map_enabled, speed_color_scale, fog_of_war_threshold,
			distance_unit, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 RETURNING `+settingsColumns,
		settings.UserID, settings.TrackingMode, sqliteBool(settings.TrackingVisits),
		sqliteBool(settings.TrackVisitsIndependently), sqliteBool(settings.AutoStart),
		settings.DistanceFilter, settings.TimeFilter, settings.TrackBreak, settings.Accuracy,
		sqliteBool(settings.ShowBackgroundLocationIndicator), sqliteBool(settings.UploadAutomatically),
		sqliteBool(settings.UploadAllOnTrackingStop), settings.BatchSize, settings.RouteOpacity,
		settings.MetersBetweenRoutes, settings.MinutesBetweenRoutes, settings.FogOfWarMeters,
		settings.TimeThresholdMinutes, settings.MergeThresholdMinutes, settings.PreferredMapLayer,
		sqliteBool(settings.SpeedColoredRoutes), settings.PointsRenderingMode,
		sqliteBool(settings.LiveMapEnabled), settings.SpeedColorScale, settings.FogOfWarThreshold,
		settings.DistanceUnit, unixTime(now), unixTime(now),
	)

	stored, err := scanSettings(row)
	if err != nil {
		return model.Settings{}, fmt.Errorf("sqlite: creating user %d's settings: %w", settings.UserID, translate(err))
	}

	return stored, nil
}

// Update implements [store.SettingsRepository].
//
// RETURNING doubles as the existence check, the same reason
// [pointRepository.Update] uses it: a WHERE that matches nothing returns no
// row, which [scanSettings] turns into [store.ErrNotFound] via [translate].
func (r settingsRepository) Update(ctx context.Context, settings model.Settings) (model.Settings, error) {
	now := time.Now().UTC().Truncate(time.Second)

	row := r.q.QueryRowContext(ctx,
		`UPDATE settings SET
			tracking_mode = ?, tracking_visits = ?, track_visits_independently = ?,
			auto_start = ?, distance_filter = ?, time_filter = ?, track_break = ?, accuracy = ?,
			show_background_location_indicator = ?, upload_automatically = ?, upload_all_on_tracking_stop = ?,
			batch_size = ?, route_opacity = ?, meters_between_routes = ?, minutes_between_routes = ?,
			fog_of_war_meters = ?, time_threshold_minutes = ?, merge_threshold_minutes = ?,
			preferred_map_layer = ?, speed_colored_routes = ?, points_rendering_mode = ?,
			live_map_enabled = ?, speed_color_scale = ?, fog_of_war_threshold = ?, distance_unit = ?,
			updated_at = ?
		 WHERE user_id = ?
		 RETURNING `+settingsColumns,
		settings.TrackingMode, sqliteBool(settings.TrackingVisits),
		sqliteBool(settings.TrackVisitsIndependently), sqliteBool(settings.AutoStart),
		settings.DistanceFilter, settings.TimeFilter, settings.TrackBreak, settings.Accuracy,
		sqliteBool(settings.ShowBackgroundLocationIndicator), sqliteBool(settings.UploadAutomatically),
		sqliteBool(settings.UploadAllOnTrackingStop), settings.BatchSize, settings.RouteOpacity,
		settings.MetersBetweenRoutes, settings.MinutesBetweenRoutes, settings.FogOfWarMeters,
		settings.TimeThresholdMinutes, settings.MergeThresholdMinutes, settings.PreferredMapLayer,
		sqliteBool(settings.SpeedColoredRoutes), settings.PointsRenderingMode,
		sqliteBool(settings.LiveMapEnabled), settings.SpeedColorScale, settings.FogOfWarThreshold,
		settings.DistanceUnit, unixTime(now), settings.UserID,
	)

	stored, err := scanSettings(row)
	if err != nil {
		return model.Settings{}, fmt.Errorf("sqlite: updating user %d's settings: %w", settings.UserID, translate(err))
	}

	return stored, nil
}

// scanSettings reads one row of [settingsColumns].
func scanSettings(row rowScanner) (model.Settings, error) {
	var (
		s                               model.Settings
		trackingVisits                  sqliteBool
		trackVisitsIndependently        sqliteBool
		autoStart                       sqliteBool
		showBackgroundLocationIndicator sqliteBool
		uploadAutomatically             sqliteBool
		uploadAllOnTrackingStop         sqliteBool
		speedColoredRoutes              sqliteBool
		liveMapEnabled                  sqliteBool
		speedColorScale                 sql.NullString
		createdAt, updatedAt            unixTime
	)

	err := row.Scan(
		&s.UserID, &s.TrackingMode, &trackingVisits, &trackVisitsIndependently,
		&autoStart, &s.DistanceFilter, &s.TimeFilter, &s.TrackBreak, &s.Accuracy,
		&showBackgroundLocationIndicator, &uploadAutomatically, &uploadAllOnTrackingStop,
		&s.BatchSize, &s.RouteOpacity, &s.MetersBetweenRoutes, &s.MinutesBetweenRoutes, &s.FogOfWarMeters,
		&s.TimeThresholdMinutes, &s.MergeThresholdMinutes, &s.PreferredMapLayer, &speedColoredRoutes,
		&s.PointsRenderingMode, &liveMapEnabled, &speedColorScale, &s.FogOfWarThreshold,
		&s.DistanceUnit, &createdAt, &updatedAt,
	)
	if err != nil {
		return model.Settings{}, translate(err)
	}

	s.TrackingVisits = bool(trackingVisits)
	s.TrackVisitsIndependently = bool(trackVisitsIndependently)
	s.AutoStart = bool(autoStart)
	s.ShowBackgroundLocationIndicator = bool(showBackgroundLocationIndicator)
	s.UploadAutomatically = bool(uploadAutomatically)
	s.UploadAllOnTrackingStop = bool(uploadAllOnTrackingStop)
	s.SpeedColoredRoutes = bool(speedColoredRoutes)
	s.LiveMapEnabled = bool(liveMapEnabled)
	s.SpeedColorScale = nullString(speedColorScale)
	s.CreatedAt = time.Time(createdAt)
	s.UpdatedAt = time.Time(updatedAt)

	return s, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.SettingsRepository = settingsRepository{}
