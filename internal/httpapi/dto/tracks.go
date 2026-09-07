package dto

// TracksResponse is the body of GET /api/v1/tracks and GET /api/v1/tracks/{id}:
// a GeoJSON FeatureCollection of LineStrings, one per track.
type TracksResponse struct {
	Type     string         `json:"type"`
	Features []TrackFeature `json:"features"`
}

// TrackFeature is one element of [TracksResponse.Features].
type TrackFeature struct {
	Type       string          `json:"type"`
	Geometry   TrackGeometry   `json:"geometry"`
	Properties TrackProperties `json:"properties"`
}

// TrackGeometry is a GeoJSON LineString: an ordered list of
// [longitude, latitude] pairs.
type TrackGeometry struct {
	Type        string      `json:"type"`
	Coordinates [][]float64 `json:"coordinates"`
}

// TrackProperties is the `properties` object of a [TrackFeature].
//
// DominantMode and DominantModeEmoji are always null: this server infers no
// transport mode to report one for.
type TrackProperties struct {
	ID       int64   `json:"id"`
	Color    string  `json:"color"`
	StartAt  string  `json:"start_at"`
	EndAt    string  `json:"end_at"`
	Distance float64 `json:"distance"`  // Metres.
	AvgSpeed float64 `json:"avg_speed"` // Km/h.
	Duration int64   `json:"duration"`  // Seconds.

	DominantMode      *string `json:"dominant_mode"`
	DominantModeEmoji *string `json:"dominant_mode_emoji"`
}

// TrackPoint is one element of the body of
// GET /api/v1/tracks/{track_id}/points — a narrower shape than
// GET /api/v1/points answers with, matching upstream's own documented
// schema for this endpoint.
//
// Velocity is a JSON number here, unlike [Point.Velocity], which upstream
// sends as a string; the two endpoints are serialized differently upstream,
// and this reproduces that rather than the shared [Point] shape.
type TrackPoint struct {
	ID int64 `json:"id"`

	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Timestamp int64  `json:"timestamp"`

	Velocity    *float64 `json:"velocity"`
	CountryName *string  `json:"country_name"`
}
