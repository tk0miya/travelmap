package httpapi

import (
	"errors"
	"net/http"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/store"
)

// getFoursquareAccount answers GET /travelmap/web/foursquare_account: whether
// the signed-in user has a Swarm account linked, and which one. No Dawarich
// endpoint reports this, which is why it lives here rather than under
// /api/v1. 404 is "not linked" — there is nothing else this resource could
// answer with once a session is required to reach it at all.
func (a *api) getFoursquareAccount(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	account, err := a.store.FoursquareAccounts().ByUserID(r.Context(), user.ID)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	case err != nil:
		a.logger.Error("looking up the Foursquare account failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	resp := dto.FoursquareAccountResponse{FoursquareUserID: account.FoursquareUserID}
	if account.SyncedThrough != nil {
		synced := formatTimestamp(*account.SyncedThrough)
		resp.SyncedThrough = &synced
	}

	a.writeJSON(w, r, http.StatusOK, resp)
}

// deleteFoursquareAccount answers DELETE /travelmap/web/foursquare_account:
// it removes the signed-in user's own linked account, scoped by session the
// same way every other browser-facing write is, so one user can reach only
// their own row. See [store.FoursquareAccountRepository.Delete] for what
// removing it does and does not affect.
func (a *api) deleteFoursquareAccount(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	if err := a.store.FoursquareAccounts().Delete(r.Context(), user.ID); err != nil {
		a.logger.Error("disconnecting the Foursquare account failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
