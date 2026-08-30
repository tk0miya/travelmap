package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// maxFoursquarePushBody bounds the webhook's form body. The observed push was
// a few kilobytes; a megabyte is far more than a check-in payload needs and
// keeps this route from holding an arbitrary amount of memory for a body
// that never ends, the same concern [maxRequestBody] answers for the
// Dawarich-compatible routes.
const maxFoursquarePushBody = 1 << 20

// foursquareWebhook answers POST /webhooks/foursquare — a Swarm User Push
// notification. Every response here has an empty body: this is not a
// Dawarich-compatible endpoint, and Foursquare is not documented to read one.
//
// secret proves the request came from the application this server's
// TRAVELMAP_FOURSQUARE_PUSH_SECRET was configured for; the reason it is
// compared the way it is, is on [auth.CheckSecret].
func (a *api) foursquareWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFoursquarePushBody)

	if err := r.ParseForm(); err != nil {
		a.logger.Warn("a Foursquare push body could not be parsed as a form", "error", err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if !auth.CheckSecret(r.PostFormValue("secret"), a.foursquarePushSecret) {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	// A User Push notification never omits this parameter, so its absence
	// means the body is malformed or meant for something else (the Venue
	// Push API shares this same form shape under a different second
	// parameter). Refusing the request would buy nothing over answering as
	// handled and dropping it, since nothing is documented to happen either
	// way.
	raw := r.PostFormValue("checkin")
	if raw == "" {
		a.logger.Info("a Foursquare push carried no checkin parameter")
		w.WriteHeader(http.StatusOK)

		return
	}

	if _, err := checkin.WritePush(r.Context(), a.store, raw); err != nil {
		switch {
		case errors.Is(err, checkin.ErrUnknownAccount):
			// This server's own account, not this one's check-in to store:
			// the request was handled correctly and says so.
			a.logger.Info("a Foursquare push named an unlinked account", "error", err)
			w.WriteHeader(http.StatusOK)
		case errors.Is(err, checkin.ErrMalformedPush):
			a.logger.Warn("a Foursquare push checkin value could not be parsed", "error", err)
			w.WriteHeader(http.StatusOK)
		default:
			a.logger.Error("storing a Foursquare check-in failed", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
		}

		return
	}

	w.WriteHeader(http.StatusOK)
}

// foursquareOAuthCallbackURL is baseURL trimmed of a trailing slash plus
// foursquareOAuthCallbackPath — the redirect_uri both legs of the OAuth flow
// send Foursquare, which has to match the one registered on the Foursquare
// application exactly. Building this is the handlers' own job, not
// foursquareOAuth's: it is a detail of what start and callback each send
// Foursquare, not a fact about the flow's configuration itself.
func foursquareOAuthCallbackURL(baseURL string) string {
	return strings.TrimSuffix(baseURL, "/") + foursquareOAuthCallbackPath
}

// foursquareOAuthStart answers GET /settings/foursquare/connect: it sends
// the browser to Foursquare to link the signed-in session's account to a
// Swarm one. [requireSessionUser] guarantees a user is on the context.
func (a *api) foursquareOAuthStart(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	state, err := a.foursquareOAuth.states.New(user.ID)
	if err != nil {
		a.logger.Error("minting a Foursquare OAuth state failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	redirectURL := foursquareOAuthCallbackURL(a.foursquareOAuth.baseURL)

	http.Redirect(w, r,
		foursquare.AuthenticateURL(a.foursquareOAuth.clientID, redirectURL, state),
		http.StatusFound)
}

// foursquareOAuthCallback answers GET /foursquare/oauth/callback: Foursquare
// returns the browser here after the user accepts or refuses the link.
//
// It carries no credential of its own — the browser is coming back from
// Foursquare — so state is what names the travelmap user the flow was
// started for, and the session cookie (sent on this top-level GET
// navigation by SameSite=Lax) is checked against it: a callback naming a
// different signed-in user, or arriving with no session at all, is refused
// the same way an unrecognised state is. Foursquare's own reference
// documents neither state nor scope for this flow, so the echo is verified
// here rather than assumed to work.
func (a *api) foursquareOAuthCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	stateUserID, ok := a.foursquareOAuth.states.Consume(query.Get("state"))
	if !ok {
		w.WriteHeader(http.StatusForbidden)

		return
	}

	sessionUser, ok := userFrom(r.Context())
	if !ok || sessionUser.ID != stateUserID {
		w.WriteHeader(http.StatusForbidden)

		return
	}

	code := query.Get("code")
	if code == "" {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	token, err := foursquare.ExchangeCode(r.Context(), a.foursquareOAuth.httpClient, a.foursquareOAuth.tokenURL,
		a.foursquareOAuth.clientID, a.foursquareOAuth.clientSecret,
		foursquareOAuthCallbackURL(a.foursquareOAuth.baseURL), code)
	if err != nil {
		a.logger.Error("exchanging the Foursquare OAuth code failed", "user_id", sessionUser.ID, "error", err)
		w.WriteHeader(http.StatusBadGateway)

		return
	}

	// Whether a refresh token or an expiry also comes back is untested, so
	// the field names are logged rather than assumed. No renewal is
	// scheduled either way: a token that stops working shows up as a 401
	// from the fetch path, which disconnecting and reconnecting from the
	// settings page can already repair.
	a.logger.Info("Foursquare OAuth token exchange succeeded",
		"user_id", sessionUser.ID, "fields", token.Fields)

	foursquareUserID, err := foursquare.GetSelfUserID(r.Context(), a.foursquareOAuth.httpClient, a.foursquareOAuth.apiBaseURL, token.AccessToken)
	if err != nil {
		a.logger.Error("calling Foursquare users/self failed", "user_id", sessionUser.ID, "error", err)
		w.WriteHeader(http.StatusBadGateway)

		return
	}

	_, err = a.store.FoursquareAccounts().Create(r.Context(), model.FoursquareAccount{
		UserID:           sessionUser.ID,
		FoursquareUserID: foursquareUserID,
		AccessToken:      token.AccessToken,
	})

	switch {
	// The account or this Foursquare user id is already linked: an operator
	// repeating themselves, or two accounts racing to claim the same Swarm
	// id, not a server fault. Re-linking to a different Swarm account is the
	// settings page's own Disconnect followed by Connect again, not an
	// upsert here: Create staying Create is what keeps this handler from
	// having to decide, on every callback, whether a conflicting row is a
	// repeat or a deliberate switch.
	case errors.Is(err, store.ErrConflict):
		a.logger.Warn("linking a Foursquare account that is already linked",
			"user_id", sessionUser.ID, "foursquare_user_id", foursquareUserID)
		w.WriteHeader(http.StatusConflict)

		return
	case err != nil:
		a.logger.Error("linking the Foursquare account failed", "user_id", sessionUser.ID, "error", err)
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "linked to Foursquare user %s\n", foursquareUserID)
}
