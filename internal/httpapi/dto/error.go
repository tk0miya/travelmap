package dto

// Error is the body every failing request answers with.
//
// Upstream Dawarich renders `{"error": "..."}` from its API controllers, with
// no code or details alongside it, so a client has nothing else to match on.
type Error struct {
	Error string `json:"error"`
}
