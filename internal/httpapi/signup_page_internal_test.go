package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// postSignup submits the sign-up form against srv, carrying cookie as the
// session cookie if it is not empty, and returns the response. Unlike
// postLogin, no redirect ever needs leaving unfollowed: signupSubmit renders
// its response directly rather than redirecting.
func postSignup(t *testing.T, srv *httptest.Server, cookie string) *http.Response {
	t.Helper()

	body := url.Values{
		"email":                 {"signup-internal@example.com"},
		"password":              {"correct horse battery staple"},
		"password_confirmation": {"correct horse battery staple"},
	}.Encode()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/signup", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: cookie}) //nolint:gosec
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /signup: %v", err)
	}

	return resp
}

// TestSignupRenewsSessionToken pins that a successful sign-up calls
// RenewToken before the user id goes into the session, matching
// TestLoginRenewsSessionToken in login_page_internal_test.go: an anonymous
// session planted before the sign-up must not still be the one the response
// cookie carries afterwards.
func TestSignupRenewsSessionToken(t *testing.T) {
	t.Parallel()

	st := storetest.New(t)
	srv := newSessionTestServer(t, st)

	// An anonymous session, the shape a browser that has only visited
	// /signup would already carry: Commit without Put still mints a token.
	sm := newSessionManager(st, time.Hour, false)

	ctx, err := sm.Load(t.Context(), "")
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	before, _, err := sm.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit returned %v", err)
	}

	resp := postSignup(t, srv, before)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	after := responseSessionCookie(resp)
	if after == "" {
		t.Fatal("no session cookie was set")
	}

	if after == before {
		t.Errorf("the session token did not change across sign-up: %q", after)
	}
}
