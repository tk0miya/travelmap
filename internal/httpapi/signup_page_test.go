package httpapi_test

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// apiKeyFromSignupPage extracts the API key the sign-up confirmation shows,
// the way a person reading the page would copy it out of the input field.
func apiKeyFromSignupPage(t *testing.T, body []byte) string {
	t.Helper()

	match := regexp.MustCompile(`id="api-key" type="text" value="([^"]+)"`).FindSubmatch(body)
	if match == nil {
		t.Fatalf("body = %q, want it to show an API key input", body)
	}

	return string(match[1])
}

// TestSignupPage covers the form itself: no credential is needed to reach it.
func TestSignupPage(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/signup")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte(`action="/signup"`)) {
		t.Errorf("body = %q, want a form posting to /signup", resp.body)
	}
}

// TestSignupSubmitSuccess covers the golden path against an empty database: a
// sign-up creates the account, signs it in, and shows an API key that works.
func TestSignupSubmitSuccess(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.New(t))
	resp := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"new@example.com"},
		"password":              {testPassword},
		"password_confirmation": {testPassword},
	}))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if token := sessionCookie(t, resp); token == "" {
		t.Error("no session cookie was set")
	}

	apiKey := apiKeyFromSignupPage(t, resp.body)

	meResp := do(t, srv, http.MethodGet, "/api/v1/users/me?api_key="+apiKey)
	if meResp.status != http.StatusOK {
		t.Errorf("GET /api/v1/users/me with the shown key: status = %d, want %d", meResp.status, http.StatusOK)
	}

	if !bytes.Contains(meResp.body, []byte("new@example.com")) {
		t.Errorf("GET /api/v1/users/me body = %q, want it to name the new account", meResp.body)
	}
}

// TestSignupSubmitDuplicateEmail covers that a second sign-up for an address
// already in use re-renders the form against that field rather than writing
// a second account.
func TestSignupSubmitDuplicateEmail(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.New(t))

	values := url.Values{
		"email":                 {"taken@example.com"},
		"password":              {testPassword},
		"password_confirmation": {testPassword},
	}

	first := do(t, srv, http.MethodPost, "/signup", withForm(values))
	if first.status != http.StatusOK {
		t.Fatalf("first sign-up: status = %d, want %d", first.status, http.StatusOK)
	}

	second := do(t, srv, http.MethodPost, "/signup", withForm(values))

	if second.status != http.StatusOK {
		t.Errorf("second sign-up: status = %d, want %d", second.status, http.StatusOK)
	}

	if !bytes.Contains(second.body, []byte("already exists")) {
		t.Errorf("second sign-up body = %q, want it to say the address is taken", second.body)
	}

	if token := sessionCookie(t, second); token != "" {
		t.Errorf("the refused sign-up set a session cookie: %q", token)
	}
}

// TestSignupSubmitMismatchedConfirmation covers that a confirmation not
// matching the password writes nothing: a later sign-up for the same address
// still succeeds, which it could not if the first attempt had created the
// account.
func TestSignupSubmitMismatchedConfirmation(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.New(t))

	mismatched := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"mismatch@example.com"},
		"password":              {testPassword},
		"password_confirmation": {"something else entirely"},
	}))

	if mismatched.status != http.StatusOK {
		t.Errorf("status = %d, want %d", mismatched.status, http.StatusOK)
	}

	if !bytes.Contains(mismatched.body, []byte("does not match")) {
		t.Errorf("body = %q, want the confirmation error", mismatched.body)
	}

	if token := sessionCookie(t, mismatched); token != "" {
		t.Errorf("the refused sign-up set a session cookie: %q", token)
	}

	retry := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"mismatch@example.com"},
		"password":              {testPassword},
		"password_confirmation": {testPassword},
	}))

	if retry.status != http.StatusOK || sessionCookie(t, retry) == "" {
		t.Errorf("a correct retry for the same address was refused: status = %d", retry.status)
	}
}

// TestSignupSubmitPasswordTooShort covers the lower password bound, stated in
// bytes since that is what bcrypt limits.
func TestSignupSubmitPasswordTooShort(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.New(t))
	resp := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"short@example.com"},
		"password":              {"short"},
		"password_confirmation": {"short"},
	}))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte("bytes")) {
		t.Errorf("body = %q, want the password bound stated in bytes", resp.body)
	}

	if token := sessionCookie(t, resp); token != "" {
		t.Errorf("the refused sign-up set a session cookie: %q", token)
	}
}

// TestSignupSubmitPasswordTooLong covers the upper password bound — bcrypt's
// own 72-byte limit — the other side of the switch
// TestSignupSubmitPasswordTooShort covers.
func TestSignupSubmitPasswordTooLong(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("a", 73)

	srv := newTestServerWith(t, storetest.New(t))
	resp := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"long@example.com"},
		"password":              {tooLong},
		"password_confirmation": {tooLong},
	}))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte("bytes")) {
		t.Errorf("body = %q, want the password bound stated in bytes", resp.body)
	}

	if token := sessionCookie(t, resp); token != "" {
		t.Errorf("the refused sign-up set a session cookie: %q", token)
	}
}

// TestSignupSubmitInvalidEmail covers an address that is not one, the way
// TestLoginSubmitRefused covers it for /login.
func TestSignupSubmitInvalidEmail(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.New(t))
	resp := do(t, srv, http.MethodPost, "/signup", withForm(url.Values{
		"email":                 {"not-an-email"},
		"password":              {testPassword},
		"password_confirmation": {testPassword},
	}))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte("valid email address")) {
		t.Errorf("body = %q, want the email error", resp.body)
	}

	if token := sessionCookie(t, resp); token != "" {
		t.Errorf("the refused sign-up set a session cookie: %q", token)
	}
}

// TestSignupSubmitCrossSiteRejected pins that CrossOriginProtection is
// attached to /signup too, the way TestLoginSubmitCrossSiteRejected pins it
// for /login: both sit in the same browser route group.
func TestSignupSubmitCrossSiteRejected(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/signup",
		withForm(url.Values{
			"email":                 {"cross-site@example.com"},
			"password":              {testPassword},
			"password_confirmation": {testPassword},
		}),
		withHeader("Sec-Fetch-Site", "cross-site"))

	if resp.status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.status, http.StatusForbidden)
	}
}
