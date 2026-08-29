package httpapi

import (
	"errors"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/checkin"
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
