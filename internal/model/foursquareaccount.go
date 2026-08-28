package model

import "time"

// FoursquareAccount links a travelmap [User] to a Swarm account, one row per
// user. Created by `travelmap foursquare connect`; nothing is collected for a
// user until this row exists.
type FoursquareAccount struct {
	// UserID is the linked [User]'s id, and the primary key: one Swarm
	// account per travelmap user.
	UserID int64

	// FoursquareUserID is the Swarm account's own id. It is a string because
	// every payload observed sends it quoted (e.g. "1709193").
	FoursquareUserID string

	// AccessToken is stored as issued: the database file is already the one
	// place secrets live.
	AccessToken string

	// SyncedThrough is the end of the last successful fetch window, and nil
	// until the first one succeeds.
	SyncedThrough *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}
