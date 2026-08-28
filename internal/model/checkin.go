package model

import "time"

// Checkin is a Swarm (Foursquare) check-in collected for a user — travelmap's
// own extension, not a Dawarich concept. See "travelmap's own extensions" in
// TODO.md.
//
// Both collection paths (the push webhook and the periodic fetch) write a
// Checkin through internal/checkin, which upserts on FoursquareCheckinID; see
// "checkins" in docs/database.md for what a repeat write keeps and what it
// overwrites.
type Checkin struct {
	// ID is the primary key.
	ID int64

	// UserID is the owning [User]'s id.
	UserID int64

	// FoursquareCheckinID is the payload's checkin.id (24 hex characters),
	// and the column a repeat write is matched against.
	FoursquareCheckinID string

	// CheckedInAt is the check-in's own time — the payload's
	// checkin.createdAt — not CreatedAt below, which is this row's own
	// bookkeeping.
	CheckedInAt time.Time

	// TimezoneOffset is the payload's checkin.timeZoneOffset, in minutes.
	TimezoneOffset *int

	// VenueID and VenueName are nil for a check-in made without one.
	VenueID   *string
	VenueName *string

	Latitude  *float64
	Longitude *float64

	// CountryCode is the payload's cc. It is kept separately from Country
	// because the display text is localised and cc is not.
	CountryCode *string

	// City, State and Country are display text only; nothing keys off them.
	City    *string
	State   *string
	Country *string

	CategoryID   *string
	CategoryName *string

	Shout *string

	// Source is "push" or "sync", naming the path that first observed the
	// check-in. A repeat write keeps this and CreatedAt and overwrites every
	// other field.
	Source string

	// Raw is the check-in JSON as received.
	Raw string

	CreatedAt time.Time
	UpdatedAt time.Time
}
