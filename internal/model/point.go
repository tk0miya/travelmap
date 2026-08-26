package model

import "time"

// Point is one location recorded for a user.
//
// Coordinates and Timestamp are the fields every point has; everything else is
// a device property that may or may not have been reported, so it is a
// pointer rather than the zero value of its type — a speed of exactly 0 and a
// speed the device never sent are not the same point.
type Point struct {
	// ID is the primary key.
	ID int64

	// UserID is the owning [User]'s id.
	UserID int64

	// Timestamp is when the device recorded the point, at second resolution,
	// for the reason on users.created_at
	// (internal/store/sqlite/migrations/0001_users.sql).
	Timestamp time.Time

	Latitude  float64
	Longitude float64

	Altitude         *float64
	Velocity         *float64
	Accuracy         *float64
	VerticalAccuracy *float64
	Course           *float64
	CourseAccuracy   *float64
	BatteryStatus    *string
	Battery          *float64
	SSID             *string
	TrackerID        *string

	CreatedAt time.Time
	UpdatedAt time.Time
}
