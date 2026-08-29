package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// postLogin submits the login form against srv, carrying cookie as the
// session cookie if it is not empty, and returns the response with
// redirects left unfollowed — the default client would otherwise read back
// whatever GET / answers instead of the login response itself.
func postLogin(t *testing.T, srv *httptest.Server, cookie string) *http.Response {
	t.Helper()

	body := url.Values{"email": {testLoginEmail}, "password": {testLoginPassword}}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/login", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: cookie}) //nolint:gosec
	}

	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}

	return resp
}

// postLogout is [postLogin] for POST /logout, which takes no body.
func postLogout(t *testing.T, srv *httptest.Server, cookie string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/logout", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: cookie}) //nolint:gosec

	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /logout: %v", err)
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
// testLoginPassword can log into.
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

// TestLoginRenewsSessionToken pins that a successful login calls RenewToken
// before the user id goes into the session: an anonymous session planted
// before the login must not still be the one the response cookie carries
// afterwards.
func TestLoginRenewsSessionToken(t *testing.T) {
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

	resp := postLogin(t, srv, before)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}

	after := responseSessionCookie(resp)
	if after == "" {
		t.Fatal("no session cookie was set")
	}

	if after == before {
		t.Errorf("the session token did not change across login: %q", after)
	}
}

// TestLogoutDeletesSessionRow pins that logout removes the row from the
// store, not just the cookie: read back through the same SessionManager
// shape production commits through, since HashTokenInStore means the row is
// keyed by a digest of the token, not the token itself.
func TestLogoutDeletesSessionRow(t *testing.T) {
	t.Parallel()

	st := newLoginTestStore(t)
	srv := newSessionTestServer(t, st)

	loginResp := postLogin(t, srv, "")
	defer loginResp.Body.Close()

	token := responseSessionCookie(loginResp)
	if token == "" {
		t.Fatal("no session cookie was set by login")
	}

	logoutResp := postLogout(t, srv, token)
	defer logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", logoutResp.StatusCode, http.StatusSeeOther)
	}

	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got := sm.GetInt64(ctx, sessionUserIDKey); got != 0 {
		t.Errorf("after logout, the session still resolves a user id (%d) — the row was not deleted", got)
	}
}
