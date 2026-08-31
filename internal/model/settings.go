package model

import "time"

// Settings is a user's stored preferences: the union of what
// GET/PATCH /api/v1/settings/mobile and GET/PATCH /api/v1/settings answer
// with, one row per user.
//
// immich_url, immich_api_key, photoprism_url and photoprism_api_key are not
// fields here: that integration is a declared non-goal, so
// internal/httpapi answers those always null rather than storing them.
type Settings struct {
	UserID int64

	// The 12 fields of GET/PATCH /api/v1/settings/mobile.

	TrackingMode             string
	TrackingVisits           bool
	TrackVisitsIndependently bool
	AutoStart                bool
	DistanceFilter           int
	TimeFilter               int

	// TrackBreak is the mobile app's own track-splitting setting
	// (settings/mobile's track_break), in minutes — not
	// tracking.track_break_minutes (config.Config.TrackBreakMinutes).
	TrackBreak int

	Accuracy                        int
	ShowBackgroundLocationIndicator bool
	UploadAutomatically             bool
	UploadAllOnTrackingStop         bool
	BatchSize                       int

	// The map/route-drawing fields of GET/PATCH /api/v1/settings.

	// RouteOpacity is a fraction (0.0-1.0), matching GET /api/v1/users/me's
	// settings.route_opacity. /api/v1/settings itself speaks a 0-100
	// percentage — internal/httpapi converts at that boundary.
	RouteOpacity          float64
	MetersBetweenRoutes   int
	MinutesBetweenRoutes  int
	FogOfWarMeters        int
	TimeThresholdMinutes  int
	MergeThresholdMinutes int
	PreferredMapLayer     string
	SpeedColoredRoutes    bool
	PointsRenderingMode   string
	LiveMapEnabled        bool

	// SpeedColorScale is nil unless the user picked a scale; upstream has no
	// default for it.
	SpeedColorScale *string

	FogOfWarThreshold int
	DistanceUnit      string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultSettings is what internal/auth.Register seeds a new account's
// settings row with: upstream's own defaults, so that a client reading one
// finds the value it would find against a fresh upstream instance.
//
// The mobile 12 match the values upstream's own spec examples give
// identically for both GET and PATCH, which is what singles them out as the
// actual defaults rather than arbitrary sample data. The rest match
// Users::SafeSettings::DEFAULT_VALUES, the same source the constants this
// replaced in internal/httpapi were read from — not values read off
// /api/v1/settings's own spec examples, which disagree with that source
// (its fog_of_war_threshold example is 100, not FogOfWarThreshold's 50)
// and so are not trusted as defaults the way settings/mobile's are.
func DefaultSettings(userID int64) Settings {
	return Settings{
		UserID: userID,

		TrackingMode:                    "precise",
		TrackingVisits:                  true,
		TrackVisitsIndependently:        false,
		AutoStart:                       true,
		DistanceFilter:                  100,
		TimeFilter:                      10,
		TrackBreak:                      30,
		Accuracy:                        3,
		ShowBackgroundLocationIndicator: true,
		UploadAutomatically:             true,
		UploadAllOnTrackingStop:         false,
		BatchSize:                       100,

		RouteOpacity:          0.6,
		MetersBetweenRoutes:   500,
		MinutesBetweenRoutes:  30,
		FogOfWarMeters:        50,
		TimeThresholdMinutes:  30,
		MergeThresholdMinutes: 15,
		PreferredMapLayer:     "OpenStreetMap",
		SpeedColoredRoutes:    false,
		PointsRenderingMode:   "raw",
		LiveMapEnabled:        true,
		SpeedColorScale:       nil,
		FogOfWarThreshold:     50,
		DistanceUnit:          "km",
	}
}
