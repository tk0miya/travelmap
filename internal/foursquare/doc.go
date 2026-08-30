// Package foursquare reads and fetches the wire shapes Foursquare sends, and
// makes the requests Foursquare expects in return: a Swarm User Push
// notification's "checkin" form parameter, the check-ins GET
// /v2/users/self/checkins answers with, and the v2 OAuth exchange and
// users/self call that link a travelmap account to one.
//
// It is a leaf package (see CLAUDE.md's layering rules): it returns the
// shapes Foursquare sends, and the package that owns the record converts them
// into internal/model types — a check-in in internal/checkin, an account in
// internal/httpapi.
package foursquare
