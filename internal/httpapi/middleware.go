package httpapi

import (
	"errors"
	"net/http"
	"runtime/debug"
)

// dawarichVersion is the upstream release reported in X-Dawarich-Version. It
// is a compatibility claim rather than this server's own version, which
// `travelmap --version` reports. See the version bullet in "Risks and Open
// Questions" in TODO.md for when it is raised.
const dawarichVersion = "1.12.2"

// responseAlive is the X-Dawarich-Response header of an unauthenticated
// request. Step 5 makes the header authentication-aware, at which point an
// authenticated one reads "Hey, I'm alive and authenticated!".
const responseAlive = "Hey, I'm alive!"

// dawarichHeaders sets the two headers upstream sets on every API response.
// They are deliberately not specific to /health; see the /api/v1/health bullet
// in "Per-endpoint" in TODO.md.
func dawarichHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Dawarich-Response", responseAlive)
		w.Header().Set("X-Dawarich-Version", dawarichVersion)

		next.ServeHTTP(w, r)
	})
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
