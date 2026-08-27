package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// userContextKey is the key the authenticated user is carried under. It is an
// unexported type so that nothing outside this package can collide with it, or
// reach the user without going through [userFrom].
type userContextKey struct{}

// withUser returns ctx carrying user.
func withUser(ctx context.Context, user model.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// userFrom returns the user the request authenticated as, and whether it
// authenticated at all. Handlers behind [requireUser] can rely on the second
// result being true.
func userFrom(ctx context.Context) (model.User, bool) {
	user, ok := ctx.Value(userContextKey{}).(model.User)

	return user, ok
}

// authenticate resolves the credentials a request carries, if any, and puts
// the user it names on the request context.
//
// It refuses no request for failing to authenticate: /api/v1/health answers
// either way and says which in its X-Dawarich-Response header, POST
// /api/v1/auth/login is how a client gets a key in the first place, and every
// other route is behind [requireUser]. A key that names no user is therefore
// not an error here — it simply leaves the request unauthenticated. The one
// request this does answer itself is the one whose key could not be looked up
// at all, which is the server's fault rather than the client's and is answered
// as one on every route.
func (a *api) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := apiKeyFrom(r)
		if key == "" {
			next.ServeHTTP(w, r)

			return
		}

		user, err := a.store.Users().ByAPIKey(r.Context(), key)

		switch {
		case errors.Is(err, store.ErrNotFound):
			next.ServeHTTP(w, r)

			return
		case err != nil:
			// A database that cannot be read is not a request that failed to
			// authenticate: answering 401 would send an operator looking for a
			// wrong API key while the actual fault is here.
			a.logger.Error("looking up the API key failed",
				"method", r.Method,
				"path", r.URL.Path,
				"error", err,
			)

			// The headers are set by the middleware this one wraps, which
			// never runs on this path.
			setDawarichHeaders(w, false)
			a.writeError(w, r, http.StatusInternalServerError, "internal server error")

			return
		}

		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// requireUser answers a request that did not authenticate, so that the
// handlers behind it are only reached with a user on the context.
//
// The 401 has an empty body, which is upstream's `head :unauthorized` and not
// an oversight: a client parsing the body of a 401 gets nothing, so sending
// the usual error body would be a difference to trip over rather than an
// improvement.
func requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := userFrom(r.Context()); !ok {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// apiKeyFrom returns the API key a request carries, or "" if it carries none.
//
// Both places are read on every route. The upstream spec documents one or the
// other per endpoint — the query parameter for points and stats, the header for
// users/me — but clients do not follow that split: the community Android client
// sends the header everywhere.
func apiKeyFrom(r *http.Request) string {
	if key := r.URL.Query().Get("api_key"); key != "" {
		return key
	}

	return bearerToken(r.Header.Get("Authorization"))
}

// bearerToken returns the token of an `Authorization: Bearer <token>` header
// value, or "" if the value is anything else.
//
// The scheme is compared case-insensitively, as RFC 9110 requires and upstream
// does. A value with anything after the token is refused rather than trimmed:
// what follows is not part of the credential, and guessing at it would accept
// a header no other server accepts.
func bearerToken(header string) string {
	space := strings.IndexFunc(header, unicode.IsSpace)
	if space < 0 || !strings.EqualFold(header[:space], "bearer") {
		return ""
	}

	token := strings.TrimSpace(header[space:])
	if strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return ""
	}

	return token
}
