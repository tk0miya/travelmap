package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// settingsData is what the settings page's template renders. Foursquare is
// its only section today; a later one adds fields here rather than a
// generic sections list, since there is only one shape to generalise from.
type settingsData struct {
	// FoursquareAvailable is whether the Swarm connection section shows at
	// all — [foursquareOAuth.configured], the same gate that keeps
	// /settings/foursquare/connect from existing. The other Foursquare
	// fields are meaningless when it is false.
	FoursquareAvailable bool

	// FoursquareLinked is whether the signed-in user has a Swarm account
	// linked at all. FoursquareUserID and FoursquareSyncedThrough are
	// meaningless when it is false.
	FoursquareLinked        bool
	FoursquareUserID        string
	FoursquareSyncedThrough string
}

// settingsPage answers GET /settings: the page that shows a signed-in user
// whether a Swarm account is linked, which one, and how current it is, with
// a button starting the OAuth flow at /settings/foursquare/connect. It
// exists regardless of which sections are configured — a settings page
// absent because its one section happens to be unconfigured would be a
// worse answer than a page saying there is nothing to configure yet.
func (a *api) settingsPage(w http.ResponseWriter, r *http.Request) {
	if !a.foursquareOAuth.configured() {
		a.renderPage(w, r, settingsTemplate, settingsData{})

		return
	}

	user, _ := userFrom(r.Context())

	account, err := a.store.FoursquareAccounts().ByUserID(r.Context(), user.ID)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.renderPage(w, r, settingsTemplate, settingsData{FoursquareAvailable: true})

		return
	case err != nil:
		a.logger.Error("looking up the Foursquare account failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	data := settingsData{
		FoursquareAvailable: true,
		FoursquareLinked:    true,
		FoursquareUserID:    account.FoursquareUserID,
	}
	if account.SyncedThrough != nil {
		data.FoursquareSyncedThrough = account.SyncedThrough.Format(time.RFC1123)
	}

	a.renderPage(w, r, settingsTemplate, data)
}

// foursquareDisconnect answers POST /settings/foursquare/disconnect: it
// removes the signed-in user's own linked account, scoped by session the
// same way every other browser-facing write is, so one user can reach only
// their own row. See [store.FoursquareAccountRepository.Delete] for what
// removing it does and does not affect.
func (a *api) foursquareDisconnect(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	if err := a.store.FoursquareAccounts().Delete(r.Context(), user.ID); err != nil {
		a.logger.Error("disconnecting the Foursquare account failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
