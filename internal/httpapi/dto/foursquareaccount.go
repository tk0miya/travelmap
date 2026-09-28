package dto

// FoursquareAccountResponse is the 200 body of
// GET /travelmap/web/foursquare_account.
type FoursquareAccountResponse struct {
	FoursquareUserID string `json:"foursquare_user_id"`

	// SyncedThrough is the end of the last successful check-in fetch, null
	// until the first one succeeds.
	SyncedThrough *string `json:"synced_through"`
}
