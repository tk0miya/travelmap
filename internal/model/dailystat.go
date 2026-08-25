package model

import "time"

// DailyStat is one user's precomputed distance and point count for one
// calendar day. See "daily_stats" in docs/database.md for why nothing else
// may compute one by aggregating Points directly.
type DailyStat struct {
	UserID int64

	// Day is local midnight of the day this row covers, in whatever timezone
	// TRAVELMAP_TIMEZONE named when the row was built. It is part of the
	// primary key together with UserID.
	Day time.Time

	Points                int
	ReverseGeocodedPoints int
	KM                    float64

	// Countries and Cities are the country and city names visited this day.
	// Both stay empty until reverse geocoding (Milestone G) is enabled.
	Countries []string
	Cities    []string
}
