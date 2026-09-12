package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// postSession submits a sign-in against srv, carrying cookie as the session
// cookie if it is not empty, and returns the response.
func postSession(t *testing.T, srv *httptest.Server, cookie string) *http.Response {
	t.Helper()

	body := `{"email":"` + testLoginEmail + `","password":"` + testLoginPassword + `"}`

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/session", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: cookie}) //nolint:gosec
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/session: %v", err)
	}

	return resp
}

// deleteSessionRequest is [postSession] for DELETE /api/session, which takes
// no body.
func deleteSessionRequest(t *testing.T, srv *httptest.Server, cookie string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, srv.URL+"/api/session", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: cookie}) //nolint:gosec

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE /api/session: %v", err)
	}

	return resp
}

// responseSessionCookie returns the value of resp's "session" Set-Cookie, or
// "" if it set none.
func responseSessionCookie(resp *http.Response) string {
	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			return c.Value
		}
	}

	return ""
}

const (
	testLoginEmail    = "login@example.com"
	testLoginPassword = "correct horse battery staple"
)

// newLoginTestStore returns a store holding one account testLoginEmail /
// testLoginPassword can sign into.
func newLoginTestStore(t *testing.T) store.Store {
	t.Helper()

	hash, err := auth.HashPassword(testLoginPassword)
	if err != nil {
		t.Fatalf("hashing the test password: %v", err)
	}

	return storetest.New(t, model.User{
		ID:           1,
		Email:        testLoginEmail,
		PasswordHash: hash,
		APIKey:       "login-page-test-key",
	})
}

// TestCreateSessionRenewsSessionToken pins that a successful sign-in calls
// RenewToken before the user id goes into the session: an anonymous session
// planted beforehand must not still be the one the response cookie carries
// afterwards.
func TestCreateSessionRenewsSessionToken(t *testing.T) {
	t.Parallel()

	st := newLoginTestStore(t)
	srv := newSessionTestServer(t, st)

	// An anonymous session, the shape a browser that has only visited /login
	// would already carry: Commit without Put still mints a token.
	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), "")
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	before, _, err := sm.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit returned %v", err)
	}

	resp := postSession(t, srv, before)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	after := responseSessionCookie(resp)
	if after == "" {
		t.Fatal("no session cookie was set")
	}

	if after == before {
		t.Errorf("the session token did not change across sign-in: %q", after)
	}
}

// TestDeleteSessionDeletesSessionRow pins that signing out removes the row
// from the store, not just the cookie: read back through the same
// SessionManager shape production commits through, since HashTokenInStore
// means the row is keyed by a digest of the token, not the token itself.
func TestDeleteSessionDeletesSessionRow(t *testing.T) {
	t.Parallel()

	st := newLoginTestStore(t)
	srv := newSessionTestServer(t, st)

	loginResp := postSession(t, srv, "")
	defer loginResp.Body.Close()

	token := responseSessionCookie(loginResp)
	if token == "" {
		t.Fatal("no session cookie was set by sign-in")
	}

	logoutResp := deleteSessionRequest(t, srv, token)
	defer logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", logoutResp.StatusCode, http.StatusNoContent)
	}

	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got := sm.GetInt64(ctx, sessionUserIDKey); got != 0 {
		t.Errorf("after sign-out, the session still resolves a user id (%d) — the row was not deleted", got)
	}
}
