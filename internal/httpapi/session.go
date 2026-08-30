package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// sessionUserIDKey is the scs session key the signed-in user's id is stored
// under. loginSubmit is what writes it.
const sessionUserIDKey = "user_id"

// newSessionManager builds the scs.SessionManager the browser group loads and
// saves through, over st's own session repository rather than scs's bundled
// sqlite3store — unusable here since it needs the CGO
// github.com/mattn/go-sqlite3, per "Browser sessions" under "Technical
// decisions" in docs/architecture.md.
func newSessionManager(st store.Store, lifetime time.Duration, cookieSecure bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = sessionStore{sessions: st.Sessions()}
	sm.Lifetime = lifetime

	// The store then holds a digest, so a copy of the database file — a
	// backup, a support tarball — hands out no live session.
	sm.HashTokenInStore = true

	sm.Cookie.HttpOnly = true
	sm.Cookie.Path = "/"
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Secure = cookieSecure

	return sm
}

// loadSessionUser resolves the user id a session carries, if any, and puts
// the user it names on the request context, the way authenticate does for
// /api/v1. A handler reads it back with userFrom regardless of which chain it
// sits behind.
func (a *api) loadSessionUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := a.sessions.GetInt64(r.Context(), sessionUserIDKey)
		if id == 0 {
			next.ServeHTTP(w, r)

			return
		}

		user, err := a.store.Users().ByID(r.Context(), id)

		switch {
		case errors.Is(err, store.ErrNotFound):
			next.ServeHTTP(w, r)

			return
		case err != nil:
			// A database that cannot be read is not a session that failed to
			// resolve: answering as signed out would send someone looking for
			// a session that expired while the actual fault is here.
			a.logger.Error("looking up the session's user failed",
				"method", r.Method,
				"path", r.URL.Path,
				"error", err,
			)
			a.writeError(w, r, http.StatusInternalServerError, "internal server error")

			return
		}

		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// requireSessionUser answers a browser request with no signed-in session by
// redirecting to the login form, rather than [requireUser]'s empty 401: a
// browser hitting this route directly has somewhere useful to go, unlike a
// device calling /api/v1 with no key.
func requireSessionUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := userFrom(r.Context()); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// sessionStore adapts [store.SessionRepository] to scs.CtxStore.
//
// Its three plain methods exist only because scs.SessionManager.Store is
// declared as the non-Ctx scs.Store, so a value assigned to it has to satisfy
// that interface structurally; scs itself always prefers the Ctx variant when
// the concrete store implements it (see doStoreCommit and its siblings), so
// these never run for a real request — each delegates with a background
// context rather than reimplementing anything.
type sessionStore struct {
	sessions store.SessionRepository
}

// FindCtx implements scs.CtxStore.
func (s sessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	session, err := s.sessions.ByToken(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	return session.Data, true, nil
}

// CommitCtx implements scs.CtxStore.
func (s sessionStore) CommitCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	return s.sessions.Upsert(ctx, model.Session{Token: token, Data: data, Expiry: expiry})
}

// DeleteCtx implements scs.CtxStore.
func (s sessionStore) DeleteCtx(ctx context.Context, token string) error {
	return s.sessions.Delete(ctx, token)
}

// Find implements scs.Store.
func (s sessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

// Commit implements scs.Store.
func (s sessionStore) Commit(token string, data []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, data, expiry)
}

// Delete implements scs.Store.
func (s sessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}

// The interface this type exists to satisfy.
var _ scs.CtxStore = sessionStore{}
