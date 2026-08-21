package dto

// UserEnvelope is the body of GET /api/v1/users/me.
//
// The user is wrapped in an object rather than being the body itself, which is
// upstream's shape: its serializer returns `{user: {...}}`, and on Cloud adds a
// `subscription` key beside it. A self-hosted instance sends the `user` key
// alone, so that is what this carries.
type UserEnvelope struct {
	User User `json:"user"`
}

// User is the account as GET /api/v1/users/me reports it.
//
// It carries no id: upstream's serializer does not send one, and a client that
// needs it has the `user_id` of POST /api/v1/auth/login.
type User struct {
	Email string `json:"email"`

	// Theme is the web UI's colour scheme. Nothing here stores one, so it is
	// answered with upstream's default until the Milestone H web UI has a
	// setting to report.
	Theme string `json:"theme"`

	// Written in the form of timestampFormat, which carries the reason for it.
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	Settings UserSettings `json:"settings"`
}

// UserSettings is the settings block inside [User].
//
// These are the fields upstream's user serializer sends, in its order, and not
// the larger set GET /api/v1/settings answers with. Nothing here is stored yet,
// so the handler fills them in with upstream's own defaults; the fields that
// belong to a non-goal — the Immich and Photoprism URLs — stay null for good.
type UserSettings struct {
	Timezone string   `json:"timezone"`
	Maps     MapsPref `json:"maps"`

	FogOfWarMeters        int     `json:"fog_of_war_meters"`
	MetersBetweenRoutes   int     `json:"meters_between_routes"`
	PreferredMapLayer     string  `json:"preferred_map_layer"`
	SpeedColoredRoutes    bool    `json:"speed_colored_routes"`
	PointsRenderingMode   string  `json:"points_rendering_mode"`
	MinutesBetweenRoutes  int     `json:"minutes_between_routes"`
	TimeThresholdMinutes  int     `json:"time_threshold_minutes"`
	MergeThresholdMinutes int     `json:"merge_threshold_minutes"`
	LiveMapEnabled        bool    `json:"live_map_enabled"`
	RouteOpacity          float64 `json:"route_opacity"`

	// A pointer so that an unset value is sent as null rather than as an empty
	// string, which is what upstream sends and what a client tells "not
	// configured" from.
	ImmichURL     *string `json:"immich_url"`
	PhotoprismURL *string `json:"photoprism_url"`

	VisitsSuggestionsEnabled bool `json:"visits_suggestions_enabled"`

	// Null unless the user picked a scale; upstream has no default for it.
	SpeedColorScale *string `json:"speed_color_scale"`

	FogOfWarThreshold int  `json:"fog_of_war_threshold"`
	GlobeProjection   bool `json:"globe_projection"`
}

// MapsPref is the `maps` object inside [UserSettings].
type MapsPref struct {
	DistanceUnit string `json:"distance_unit"`
}
