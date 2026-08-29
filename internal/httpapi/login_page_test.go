package httpapi_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// doNoRedirect is [do] for a request whose own redirect response is what the
// test wants to inspect — the login and logout handlers answer 303, and
// following it would read back GET /'s response instead, on a client with no
// cookie jar to carry the Set-Cookie across that hop.
func doNoRedirect(t *testing.T, srv *httptest.Server, method, path string, opts ...requestOption) response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	for _, opt := range opts {
		opt(req)
	}

	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}

	return response{status: resp.StatusCode, header: resp.Header, body: body}
}

// withForm sends values as an application/x-www-form-urlencoded request
// body, the way the login form itself submits.
func withForm(values url.Values) requestOption {
	body := values.Encode()

	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
}

// sessionCookie returns the value of the "session" cookie a response set, or
// "" if it set none.
func sessionCookie(t *testing.T, resp response) string {
	t.Helper()

	header := http.Header{"Set-Cookie": resp.header.Values("Set-Cookie")}
	req := http.Response{Header: header}

	for _, c := range req.Cookies() {
		if c.Name == "session" {
			return c.Value
		}
	}

	return ""
}

// TestLoginPage covers the form itself: no credential is needed to reach it.
func TestLoginPage(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/login")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte(`action="/login"`)) {
		t.Errorf("body = %q, want a form posting to /login", resp.body)
	}
}

// TestLoginSubmitSuccess covers the golden path: a login sets a cookie that
// then names the account on GET /.
func TestLoginSubmitSuccess(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := doNoRedirect(t, srv, http.MethodPost, "/login",
		withForm(url.Values{"email": {testEmail}, "password": {testPassword}}))

	if resp.status != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.status, http.StatusSeeOther)
	}

	if got, want := resp.header.Get("Location"), "/"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	token := sessionCookie(t, resp)
	if token == "" {
		t.Fatal("no session cookie was set")
	}

	indexResp := do(t, srv, http.MethodGet, "/", withHeader("Cookie", "session="+token))
	if !bytes.Contains(indexResp.body, []byte(testEmail)) {
		t.Errorf("GET / body = %q, want it to name %s", indexResp.body, testEmail)
	}
}

// TestLoginSubmitRefused covers every way a login is refused: a wrong
// password, an address with no account, and an address that is not one. All
// three re-render the form with the same message POST /api/v1/auth/login
// gives, and set no cookie.
func TestLoginSubmitRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		email    string
		password string
	}{
		"wrong password":           {email: testEmail, password: "wrong password entirely"},
		"unknown email":            {email: "nobody@example.com", password: testPassword},
		"an email that is not one": {email: "not-an-email", password: testPassword},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/login",
				withForm(url.Values{"email": {tt.email}, "password": {tt.password}}))

			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if !bytes.Contains(resp.body, []byte("Invalid email or password")) {
				t.Errorf("body = %q, want the refusal message", resp.body)
			}

			if token := sessionCookie(t, resp); token != "" {
				t.Errorf("a refused login set a session cookie: %q", token)
			}
		})
	}
}

// TestLoginSubmitCrossSiteRejected pins that CrossOriginProtection is
// attached to the browser group: a cross-site POST never reaches the
// handler at all.
func TestLoginSubmitCrossSiteRejected(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/login",
		withForm(url.Values{"email": {testEmail}, "password": {testPassword}}),
		withHeader("Sec-Fetch-Site", "cross-site"))

	if resp.status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.status, http.StatusForbidden)
	}
}

// TestLogout covers that logging out returns the browser to the form, and
// that the old cookie no longer reaches GET / as a signed-in account.
func TestLogout(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	loginResp := doNoRedirect(t, srv, http.MethodPost, "/login",
		withForm(url.Values{"email": {testEmail}, "password": {testPassword}}))
	token := sessionCookie(t, loginResp)

	logoutResp := doNoRedirect(t, srv, http.MethodPost, "/logout", withHeader("Cookie", "session="+token))

	if logoutResp.status != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", logoutResp.status, http.StatusSeeOther)
	}

	if got, want := logoutResp.header.Get("Location"), "/login"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	indexResp := do(t, srv, http.MethodGet, "/", withHeader("Cookie", "session="+token))
	if !bytes.Contains(indexResp.body, []byte("Not signed in.")) {
		t.Errorf("GET / with the old cookie = %q, want it treated as signed out", indexResp.body)
	}
}
