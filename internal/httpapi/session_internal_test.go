package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// plantSession writes a session naming userID directly through a
// SessionManager wired the way New wires one, bypassing the login form so a
// test can isolate session/expiry behaviour from the login flow itself. It
// returns the token a client presents as the session cookie's value.
func plantSession(t *testing.T, st store.Store, userID int64) string {
	t.Helper()

	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), "")
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	sm.Put(ctx, sessionUserIDKey, userID)

	token, _, err := sm.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit returned %v", err)
	}

	return token
}

// newSessionTestServer starts the real router over st, the way router_test.go's
// newTestServerWith does — duplicated here rather than shared, since that
// helper lives in the external httpapi_test package and cannot see the
// unexported plantSession needs.
func newSessionTestServer(t *testing.T, st store.Store) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:  st,
	}))
	t.Cleanup(srv.Close)

	return srv
}

// doGetWithCookie issues a GET to path carrying a "session" cookie of token,
// without following a redirect — a caller checking for one wants its own
// status and Location, not the page it points to.
func doGetWithCookie(t *testing.T, srv *httptest.Server, path, token string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	// A request cookie, not a response one: Secure/HttpOnly/SameSite have no
	// meaning on the Cookie header a client sends.
	req.AddCookie(&http.Cookie{Name: "session", Value: token}) //nolint:gosec

	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	return resp
}

// getWithCookie is [doGetWithCookie] for a caller that only wants the
// response body.
func getWithCookie(t *testing.T, srv *httptest.Server, path, token string) []byte {
	t.Helper()

	resp := doGetWithCookie(t, srv, path, token)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}

	return body
}

// TestSessionManagerCookieAttributes covers the cookie newSessionManager
// configures, and that a session it commits actually lands in the store:
// read back through a fresh Load rather than by peeking at the row directly,
// since HashTokenInStore means the row is keyed by a digest of the token, not
// the token itself.
func TestSessionManagerCookieAttributes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		secure bool
	}{
		"insecure": {secure: false},
		"secure":   {secure: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := storetest.New(t)
			sm := newSessionManager(st, time.Hour, tt.secure)

			handler := sm.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				sm.Put(r.Context(), sessionUserIDKey, int64(1))
			}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d Set-Cookie headers, want 1", len(cookies))
			}

			c := cookies[0]

			if !c.HttpOnly {
				t.Error("HttpOnly is not set")
			}

			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
			}

			if c.Path != "/" {
				t.Errorf("Path = %q, want /", c.Path)
			}

			if c.Secure != tt.secure {
				t.Errorf("Secure = %v, want %v", c.Secure, tt.secure)
			}

			ctx, err := sm.Load(t.Context(), c.Value)
			if err != nil {
				t.Fatalf("Load returned %v", err)
			}

			if got := sm.GetInt64(ctx, sessionUserIDKey); got != 1 {
				t.Errorf("after a fresh Load, GetInt64 = %d, want 1 — the row did not land in sessions", got)
			}
		})
	}
}

// TestIndexNamesSessionUser covers that GET / reads the user a session
// carries and has the page name them.
func TestIndexNamesSessionUser(t *testing.T) {
	t.Parallel()

	user := model.User{
		// Not 0: loadSessionUser reads a zero user id as no session at all.
		ID:           1,
		Email:        "session@example.com",
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "session-test-key",
	}

	st := storetest.New(t, user)

	created, err := st.Users().ByEmail(t.Context(), user.Email)
	if err != nil {
		t.Fatalf("looking up the seeded user: %v", err)
	}

	token := plantSession(t, st, created.ID)

	body := getWithCookie(t, newSessionTestServer(t, st), "/", token)

	if !bytes.Contains(body, []byte("Signed in as "+created.Email)) {
		t.Errorf("body = %q, want it to name %s", body, created.Email)
	}

	if !bytes.Contains(body, []byte(`action="/logout"`)) {
		t.Errorf("body = %q, want a way to log out with a session", body)
	}
}

// TestIndexIgnoresExpiredSession pins that an expired row is no session:
// filtered out by SessionRepository.ByToken itself rather than left for the
// periodic sweep to catch up with.
func TestIndexIgnoresExpiredSession(t *testing.T) {
	t.Parallel()

	user := model.User{
		ID:           1,
		Email:        "expired@example.com",
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "expired-test-key",
	}

	st := storetest.New(t, user)

	created, err := st.Users().ByEmail(t.Context(), user.Email)
	if err != nil {
		t.Fatalf("looking up the seeded user: %v", err)
	}

	token := plantSession(t, st, created.ID)

	// Rewritten through the same SessionManager shape production commits
	// through, rather than by touching the store directly: HashTokenInStore
	// means the row is keyed by a digest of token, not token itself.
	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	sm.SetDeadline(ctx, time.Now().Add(-time.Minute))

	if _, _, err := sm.Commit(ctx); err != nil {
		t.Fatalf("expiring the session: %v", err)
	}

	resp := doGetWithCookie(t, newSessionTestServer(t, st), "/", token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d — an expired session treated as none", resp.StatusCode, http.StatusFound)
	}

	if got, want := resp.Header.Get("Location"), "/login"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}
