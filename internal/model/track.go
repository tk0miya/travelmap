package model

import "time"

// Coordinate is one point of a [Track]'s geometry, in GeoJSON's own
// longitude-then-latitude order.
type Coordinate struct {
	Longitude float64
	Latitude  float64
}

// Track is one contiguous run of a user's points, split from its neighbours
// by a gap exceeding TRAVELMAP_TRACK_BREAK_MINUTES of inactivity — the same
// value daily_stats' own segment attribution uses. internal/track is the
// only writer: every track is rebuilt from scratch whenever a point changes
// anywhere in the user's history, since one new point can shift where every
// later boundary falls.
type Track struct {
	// ID is the primary key.
	ID int64

	// UserID is the owning [User]'s id.
	UserID int64

	// StartAt and EndAt are the timestamps of the track's first and last
	// point.
	StartAt time.Time
	EndAt   time.Time

	// DistanceMeters is the sum of the great-circle distance between each
	// consecutive pair of points in the track.
	DistanceMeters float64

	// Geometry is every point's coordinate, in timestamp order — the GeoJSON
	// LineString GET /api/v1/tracks answers with. Precomputed and stored
	// rather than rebuilt from points on every read, the same reason
	// daily_stats is a precomputed aggregate rather than one computed from
	// points at request time.
	Geometry []Coordinate

	CreatedAt time.Time
	UpdatedAt time.Time
}
