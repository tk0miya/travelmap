package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"
)

// sessionBody builds the JSON body POST /api/session expects.
func sessionBody(email, password string) string {
	return `{"email":"` + email + `","password":"` + password + `"}`
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

// TestCreateSession covers the golden path: a session cookie is set, and
// then names the account on GET /.
func TestCreateSession(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/session", withBody(sessionBody(testEmail, testPassword)))

	if resp.status != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.status, http.StatusCreated)
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

// TestCreateSessionRefused covers every way it is refused: a wrong password,
// an address with no account, and an address that is not one. All three
// answer the same message POST /api/v1/auth/login gives, and set no cookie.
func TestCreateSessionRefused(t *testing.T) {
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
			resp := do(t, srv, http.MethodPost, "/api/session", withBody(sessionBody(tt.email, tt.password)))

			if resp.status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
			}

			assertGolden(t, "auth_failed.json", resp.body)

			if token := sessionCookie(t, resp); token != "" {
				t.Errorf("a refused session set a cookie: %q", token)
			}
		})
	}
}

// TestCreateSessionRejectsAnUnreadableBody pins that a body that cannot be
// read is a bad request rather than a failed sign-in — the same distinction
// POST /api/v1/auth/login makes.
func TestCreateSessionRejectsAnUnreadableBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/session", withBody(`{"email":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestCreateSessionCrossSiteRejected pins that CrossOriginProtection is
// attached to the browser group: a cross-site POST never reaches the
// handler at all.
func TestCreateSessionCrossSiteRejected(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/session",
		withBody(sessionBody(testEmail, testPassword)),
		withHeader("Sec-Fetch-Site", "cross-site"))

	if resp.status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.status, http.StatusForbidden)
	}
}

// TestDeleteSession covers that signing out ends the session: the old
// cookie no longer reaches GET / as a signed-in account.
func TestDeleteSession(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	loginResp := do(t, srv, http.MethodPost, "/api/session", withBody(sessionBody(testEmail, testPassword)))
	token := sessionCookie(t, loginResp)

	logoutResp := do(t, srv, http.MethodDelete, "/api/session", withHeader("Cookie", "session="+token))

	if logoutResp.status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", logoutResp.status, http.StatusNoContent)
	}

	indexResp := doNoRedirect(t, srv, http.MethodGet, "/", withHeader("Cookie", "session="+token))
	if indexResp.status != http.StatusFound {
		t.Errorf("GET / with the old cookie: status = %d, want %d — treated as signed out", indexResp.status,
			http.StatusFound)
	}

	if got, want := indexResp.header.Get("Location"), "/login"; got != want {
		t.Errorf("GET / with the old cookie: Location = %q, want %q", got, want)
	}
}
