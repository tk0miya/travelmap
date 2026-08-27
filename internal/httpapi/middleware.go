package httpapi

import (
	"errors"
	"net/http"
	"runtime/debug"
)

// dawarichVersion is the upstream release reported in X-Dawarich-Version. It
// is a compatibility claim rather than this server's own version, which
// `travelmap --version` reports.
const dawarichVersion = "1.12.2"

// The two values of X-Dawarich-Response. Which one a request gets is how a
// client learns that the credentials it sent were accepted: GET
// /api/v1/health is the only endpoint that answers 200 without any, so the
// header is where the difference has to show.
const (
	responseAlive              = "Hey, I'm alive!"
	responseAliveAuthenticated = "Hey, I'm alive and authenticated!"
)

// dawarichHeaders sets the two headers upstream sets on every API response.
// They are deliberately not specific to /health.
//
// It runs inside authenticate rather than around it, because the value of
// X-Dawarich-Response is the outcome of that lookup.
func dawarichHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, authenticated := userFrom(r.Context())

		setDawarichHeaders(w, authenticated)

		next.ServeHTTP(w, r)
	})
}

// setDawarichHeaders writes the two headers onto a response. It is separate
// from the middleware for the one response the middleware never reaches: the
// failure authenticate itself answers.
func setDawarichHeaders(w http.ResponseWriter, authenticated bool) {
	response := responseAlive
	if authenticated {
		response = responseAliveAuthenticated
	}

	w.Header().Set("X-Dawarich-Response", response)
	w.Header().Set("X-Dawarich-Version", dawarichVersion)
}

// recoverer turns a panic in a handler into a logged 500 with the same JSON
// error body as any other failure, so that a bug in one handler neither takes
// the process down nor hands the client a body it cannot parse.
func (a *api) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// net/http uses this panic to abort a response on purpose;
			// swallowing it would defeat that.
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}

			a.logger.Error("handler panicked",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", rec,
				"stack", string(debug.Stack()),
			)
			a.writeError(w, r, http.StatusInternalServerError, "internal server error")
		}()

		next.ServeHTTP(w, r)
	})
}
