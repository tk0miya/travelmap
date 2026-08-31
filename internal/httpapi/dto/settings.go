package dto

import "encoding/json"

// unwrapSettings returns the bytes a settings PATCH body's fields should be
// decoded from.
//
// The published spec contradicts itself on the shape of a settings PATCH:
// its schema puts the fields at the top level, but its own example wraps
// them in a "settings" object. Accepting both means neither silently drops
// every field it sends.
func unwrapSettings(data []byte) []byte {
	var wrapper struct {
		Settings json.RawMessage `json:"settings"`
	}

	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Settings != nil {
		return wrapper.Settings
	}

	return data
}

// MobileSettingsEnvelope is the body of GET/PATCH /api/v1/settings/mobile.
type MobileSettingsEnvelope struct {
	Settings MobileSettings `json:"settings"`

	// Never absent — see internal/auth.Register for why every account
	// already has a settings row to stamp.
	UpdatedAt string `json:"updated_at"`

	Status string `json:"status"`

	// Empty on GET; PATCH is the only one that sends it.
	Message string `json:"message,omitempty"`
}

// MobileSettings is the settings block [MobileSettingsEnvelope] carries.
type MobileSettings struct {
	TrackingMode                    string `json:"tracking_mode"`
	TrackingVisits                  bool   `json:"tracking_visits"`
	TrackVisitsIndependently        bool   `json:"track_visits_independently"`
	AutoStart                       bool   `json:"auto_start"`
	DistanceFilter                  int    `json:"distance_filter"`
	TimeFilter                      int    `json:"time_filter"`
	TrackBreak                      int    `json:"track_break"`
	Accuracy                        int    `json:"accuracy"`
	ShowBackgroundLocationIndicator bool   `json:"show_background_location_indicator"`
	UploadAutomatically             bool   `json:"upload_automatically"`
	UploadAllOnTrackingStop         bool   `json:"upload_all_on_tracking_stop"`
	BatchSize                       int    `json:"batch_size"`
}

// MobileSettingsUpdate is the body PATCH /api/v1/settings/mobile decodes.
//
// Every field is a pointer so a request that omits one leaves the stored
// value unchanged rather than zeroing it.
type MobileSettingsUpdate struct {
	TrackingMode                    *string `json:"tracking_mode"`
	TrackingVisits                  *bool   `json:"tracking_visits"`
	TrackVisitsIndependently        *bool   `json:"track_visits_independently"`
	AutoStart                       *bool   `json:"auto_start"`
	DistanceFilter                  *int    `json:"distance_filter"`
	TimeFilter                      *int    `json:"time_filter"`
	TrackBreak                      *int    `json:"track_break"`
	Accuracy                        *int    `json:"accuracy"`
	ShowBackgroundLocationIndicator *bool   `json:"show_background_location_indicator"`
	UploadAutomatically             *bool   `json:"upload_automatically"`
	UploadAllOnTrackingStop         *bool   `json:"upload_all_on_tracking_stop"`
	BatchSize                       *int    `json:"batch_size"`
}

// UnmarshalJSON implements [json.Unmarshaler]; see [unwrapSettings].
func (u *MobileSettingsUpdate) UnmarshalJSON(data []byte) error {
	type mobileSettingsUpdate MobileSettingsUpdate

	return json.Unmarshal(unwrapSettings(data), (*mobileSettingsUpdate)(u))
}

// SettingsEnvelope is the body of GET /api/v1/settings.
type SettingsEnvelope struct {
	Settings Settings `json:"settings"`
}

// Settings is the settings block [SettingsEnvelope] carries.
//
// ImmichURL, ImmichAPIKey, PhotoprismURL and PhotoprismAPIKey are always
// null: that integration is a declared non-goal, so nothing here stores
// them, matching [UserSettings].
type Settings struct {
	RouteOpacity          float64  `json:"route_opacity"`
	MetersBetweenRoutes   int      `json:"meters_between_routes"`
	MinutesBetweenRoutes  int      `json:"minutes_between_routes"`
	FogOfWarMeters        int      `json:"fog_of_war_meters"`
	TimeThresholdMinutes  int      `json:"time_threshold_minutes"`
	MergeThresholdMinutes int      `json:"merge_threshold_minutes"`
	PreferredMapLayer     string   `json:"preferred_map_layer"`
	SpeedColoredRoutes    bool     `json:"speed_colored_routes"`
	PointsRenderingMode   string   `json:"points_rendering_mode"`
	LiveMapEnabled        bool     `json:"live_map_enabled"`
	ImmichURL             *string  `json:"immich_url"`
	ImmichAPIKey          *string  `json:"immich_api_key"`
	PhotoprismURL         *string  `json:"photoprism_url"`
	PhotoprismAPIKey      *string  `json:"photoprism_api_key"`
	SpeedColorScale       *string  `json:"speed_color_scale"`
	FogOfWarThreshold     int      `json:"fog_of_war_threshold"`
	Maps                  MapsPref `json:"maps"`
}

// SettingsUpdate is the body PATCH /api/v1/settings decodes.
//
// Every field is a pointer so a request that omits one leaves the stored
// value unchanged rather than zeroing it. immich_url, immich_api_key,
// photoprism_url and photoprism_api_key are deliberately not fields here:
// see [Settings].
type SettingsUpdate struct {
	RouteOpacity          *float64        `json:"route_opacity"`
	MetersBetweenRoutes   *int            `json:"meters_between_routes"`
	MinutesBetweenRoutes  *int            `json:"minutes_between_routes"`
	FogOfWarMeters        *int            `json:"fog_of_war_meters"`
	TimeThresholdMinutes  *int            `json:"time_threshold_minutes"`
	MergeThresholdMinutes *int            `json:"merge_threshold_minutes"`
	PreferredMapLayer     *string         `json:"preferred_map_layer"`
	SpeedColoredRoutes    *bool           `json:"speed_colored_routes"`
	PointsRenderingMode   *string         `json:"points_rendering_mode"`
	LiveMapEnabled        *bool           `json:"live_map_enabled"`
	SpeedColorScale       *string         `json:"speed_color_scale"`
	FogOfWarThreshold     *int            `json:"fog_of_war_threshold"`
	Maps                  *MapsPrefUpdate `json:"maps"`
}

// MapsPrefUpdate is the `maps` object [SettingsUpdate] decodes.
type MapsPrefUpdate struct {
	DistanceUnit *string `json:"distance_unit"`
}

// UnmarshalJSON implements [json.Unmarshaler]; see [unwrapSettings].
func (u *SettingsUpdate) UnmarshalJSON(data []byte) error {
	type settingsUpdate SettingsUpdate

	return json.Unmarshal(unwrapSettings(data), (*settingsUpdate)(u))
}
