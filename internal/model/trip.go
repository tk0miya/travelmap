package model

import "time"

// Trip is a time range a user declared as one trip: a title and a range,
// nothing more. Nothing derives a Trip and nothing rebuilds one — every row
// comes from what the user typed, so no config change invalidates it and no
// recalculation pass touches it.
//
// Ranges may overlap ("Europe" containing "Paris" is a real case), and
// nothing rejects one that does. No field holds what a trip contains: its
// contents are found by reading points and check-ins against StartedAt and
// EndedAt, not stored as a join.
type Trip struct {
	// ID is the primary key.
	ID int64

	// UserID is the owning [User]'s id.
	UserID int64

	Title string

	// StartedAt and EndedAt bound the trip. EndedAt is an exclusive upper
	// bound.
	StartedAt time.Time
	EndedAt   time.Time

	// Description is free text about the trip as a whole — deliberately not
	// called "note", which is a different, not-yet-built thing carrying its
	// own timestamp.
	Description string

	CreatedAt time.Time
	UpdatedAt time.Time
}
