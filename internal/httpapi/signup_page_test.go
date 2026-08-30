package httpapi_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// newTestServerEmpty starts the real router over a store holding no users,
// which is what a sign-up test needs: the account it creates must be the
// first row rather than a second one alongside [testUser].
func newTestServerEmpty(t *testing.T) *httptest.Server {
	t.Helper()

	return newTestServerWith(t, storetest.New(t))
}

// signupForm returns the values a sign-up submission sends, with password and
// its confirmation defaulting to the same value.
func signupForm(email, password string) url.Values {
	return url.Values{
		"email":                 {email},
		"password":              {password},
		"password_confirmation": {password},
	}
}

// apiKeyFromBody pulls the API key the sign-up done page shows out of resp's
// body — the same way a person configuring the phone app would read it.
func apiKeyFromBody(t *testing.T, body []byte) string {
	t.Helper()

	const openTag, closeTag = `<p class="api-key">`, `</p>`

	start := bytes.Index(body, []byte(openTag))
	if start < 0 {
		t.Fatalf("body = %q, want an api-key element", body)
	}

	start += len(openTag)

	end := bytes.Index(body[start:], []byte(closeTag))
	if end < 0 {
		t.Fatalf("body = %q, want a closing </p> after the api-key element", body)
	}

	return string(body[start : start+end])
}

// TestSignupPage covers the form itself: no credential is needed to reach it.
func TestSignupPage(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodGet, "/signup")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte(`action="/signup"`)) {
		t.Errorf("body = %q, want a form posting to /signup", resp.body)
	}
}

// TestSignupSubmitSuccess covers the golden path: signing up against an empty
// database creates one user, leaves a working session, and the API key the
// page shows authenticates GET /api/v1/users/me.
func TestSignupSubmitSuccess(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodPost, "/signup", withForm(signupForm(testEmail, testPassword)))

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.status, http.StatusOK, resp.body)
	}

	token := sessionCookie(t, resp)
	if token == "" {
		t.Fatal("no session cookie was set")
	}

	indexResp := do(t, srv, http.MethodGet, "/", withHeader("Cookie", "session="+token))
	if !bytes.Contains(indexResp.body, []byte(testEmail)) {
		t.Errorf("GET / body = %q, want it to name %s", indexResp.body, testEmail)
	}

	apiKey := apiKeyFromBody(t, resp.body)
	if len(apiKey) == 0 {
		t.Fatal("the done page showed no API key")
	}

	meResp := do(t, srv, http.MethodGet, "/api/v1/users/me", withHeader("Authorization", "Bearer "+apiKey))
	if meResp.status != http.StatusOK {
		t.Errorf("GET /api/v1/users/me with the issued key: status = %d, want %d", meResp.status, http.StatusOK)
	}
}

// TestSignupSubmitRejectsBadInput covers every way a submission is refused,
// each re-rendering the form against an otherwise empty database rather than
// writing anything.
func TestSignupSubmitRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		email, password, confirmation string
		wantBody                      string
	}{
		"not an address": {
			email: "not-an-address", password: testPassword, confirmation: testPassword,
			wantBody: "not a valid email address",
		},
		"password too short": {
			email: testEmail, password: strings.Repeat("a", auth.MinPasswordLength-1),
			confirmation: strings.Repeat("a", auth.MinPasswordLength-1),
			wantBody:     fmt.Sprintf("at least %d bytes", auth.MinPasswordLength),
		},
		"password too long": {
			email: testEmail, password: strings.Repeat("a", auth.MaxPasswordLength+1),
			confirmation: strings.Repeat("a", auth.MaxPasswordLength+1),
			wantBody:     fmt.Sprintf("at most %d bytes", auth.MaxPasswordLength),
		},
		"mismatched confirmation": {
			email: testEmail, password: testPassword, confirmation: testPassword + "!",
			wantBody: "does not match the password",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServerEmpty(t)
			resp := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
				"email":                 {tt.email},
				"password":              {tt.password},
				"password_confirmation": {tt.confirmation},
			}))

			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if !bytes.Contains(resp.body, []byte(tt.wantBody)) {
				t.Errorf("body = %q, want it to contain %q", resp.body, tt.wantBody)
			}

			if sessionCookie(t, resp) != "" {
				t.Error("a rejected sign-up set a session cookie")
			}
		})
	}
}

// TestSignupSubmitRejectsADuplicate pins that a second sign-up for an address
// already registered re-renders the form against the email field, and writes
// nothing.
func TestSignupSubmitRejectsADuplicate(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)

	first := do(t, srv, http.MethodPost, "/signup", withForm(signupForm(testEmail, testPassword)))
	if first.status != http.StatusOK {
		t.Fatalf("the first sign-up returned status %d (body %q)", first.status, first.body)
	}

	second := do(t, srv, http.MethodPost, "/signup", withForm(signupForm(testEmail, testPassword)))

	if second.status != http.StatusOK {
		t.Errorf("status = %d, want %d", second.status, http.StatusOK)
	}

	if !bytes.Contains(second.body, []byte("already registered")) {
		t.Errorf("body = %q, want it to say the address is already registered", second.body)
	}

	if sessionCookie(t, second) != "" {
		t.Error("the rejected duplicate sign-up set a session cookie")
	}
}

// TestSignupSubmitCrossSiteRejected pins that CrossOriginProtection is
// attached to the browser group here too, matching the login form.
func TestSignupSubmitCrossSiteRejected(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodPost, "/signup",
		withForm(signupForm(testEmail, testPassword)),
		withHeader("Sec-Fetch-Site", "cross-site"))

	if resp.status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.status, http.StatusForbidden)
	}
}
