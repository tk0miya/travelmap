// Package foursquare reads the wire shapes Foursquare sends: a Swarm User
// Push notification's "checkin" form parameter, today.
//
// It is a leaf package (see CLAUDE.md's layering rules): it returns the
// shapes Foursquare sends, and the package that owns the record converts them
// into internal/model types — a check-in in internal/checkin.
package foursquare
