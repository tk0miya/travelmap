package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// userBody builds the JSON body POST /travelmap/web/users expects, with
// password and its confirmation defaulting to the same value.
func userBody(email, password, confirmation string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q,"password_confirmation":%q}`, email, password, confirmation)
}

// apiKeyFromBody pulls "api_key" out of a successful response body — the same
// way a person configuring the phone app would read it.
func apiKeyFromBody(t *testing.T, body []byte) string {
	t.Helper()

	var decoded struct {
		APIKey string `json:"api_key"`
	}

	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the response body: %v (body %q)", err, body)
	}

	return decoded.APIKey
}

// TestCreateUser covers the golden path: signing up against an empty database
// creates one user, sets a session cookie, and the API key it returns
// authenticates GET /api/v1/users/me.
func TestCreateUser(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodPost, "/travelmap/web/users", withBody(userBody(testEmail, testPassword, testPassword)))

	if resp.status != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %q)", resp.status, http.StatusCreated, resp.body)
	}

	token := sessionCookie(t, resp)
	if token == "" {
		t.Fatal("no session cookie was set")
	}

	indexResp := do(t, srv, http.MethodGet, "/", withHeader("Cookie", "session="+token))
	if !strings.Contains(string(indexResp.body), testEmail) {
		t.Errorf("GET / body = %q, want it to name %s", indexResp.body, testEmail)
	}

	apiKey := apiKeyFromBody(t, resp.body)
	if apiKey == "" {
		t.Fatal("the response carried no api_key")
	}

	meResp := do(t, srv, http.MethodGet, "/api/v1/users/me", withHeader("Authorization", "Bearer "+apiKey))
	if meResp.status != http.StatusOK {
		t.Errorf("GET /api/v1/users/me with the issued key: status = %d, want %d", meResp.status, http.StatusOK)
	}
}

// TestCreateUserRejectsBadInput covers every way a submission is refused,
// each answering the field it belongs to and writing nothing.
func TestCreateUserRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		email, password, confirmation string
		wantGolden                    string
	}{
		"not an address": {
			email: "not-an-address", password: testPassword, confirmation: testPassword,
			wantGolden: "signup_invalid_email.json",
		},
		"password too short": {
			email: testEmail, password: strings.Repeat("a", auth.MinPasswordLength-1),
			confirmation: strings.Repeat("a", auth.MinPasswordLength-1),
			wantGolden:   "signup_password_too_short.json",
		},
		"password too long": {
			email: testEmail, password: strings.Repeat("a", auth.MaxPasswordLength+1),
			confirmation: strings.Repeat("a", auth.MaxPasswordLength+1),
			wantGolden:   "signup_password_too_long.json",
		},
		"mismatched confirmation": {
			email: testEmail, password: testPassword, confirmation: testPassword + "!",
			wantGolden: "signup_password_mismatch.json",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServerEmpty(t)
			resp := do(t, srv, http.MethodPost, "/travelmap/web/users",
				withBody(userBody(tt.email, tt.password, tt.confirmation)))

			if resp.status != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want %d", resp.status, http.StatusUnprocessableEntity)
			}

			assertGolden(t, tt.wantGolden, resp.body)

			if sessionCookie(t, resp) != "" {
				t.Error("a rejected sign-up set a session cookie")
			}
		})
	}
}

// TestCreateUserRejectsADuplicate pins that a second sign-up for an address
// already registered answers the email field, and writes nothing.
func TestCreateUserRejectsADuplicate(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)

	first := do(t, srv, http.MethodPost, "/travelmap/web/users", withBody(userBody(testEmail, testPassword, testPassword)))
	if first.status != http.StatusCreated {
		t.Fatalf("the first sign-up returned status %d (body %q)", first.status, first.body)
	}

	second := do(t, srv, http.MethodPost, "/travelmap/web/users", withBody(userBody(testEmail, testPassword, testPassword)))

	if second.status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", second.status, http.StatusUnprocessableEntity)
	}

	assertGolden(t, "signup_email_taken.json", second.body)

	if sessionCookie(t, second) != "" {
		t.Error("the rejected duplicate sign-up set a session cookie")
	}
}

// TestCreateUserRejectsAnUnreadableBody pins that a body that cannot be read
// is a bad request rather than a refused sign-up.
func TestCreateUserRejectsAnUnreadableBody(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodPost, "/travelmap/web/users", withBody(`{"email":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestCreateUserCrossSiteRejected pins that CrossOriginProtection is attached
// to the browser group here too, matching the session endpoint.
func TestCreateUserCrossSiteRejected(t *testing.T) {
	t.Parallel()

	srv := newTestServerEmpty(t)
	resp := do(t, srv, http.MethodPost, "/travelmap/web/users",
		withBody(userBody(testEmail, testPassword, testPassword)),
		withHeader("Sec-Fetch-Site", "cross-site"))

	if resp.status != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.status, http.StatusForbidden)
	}
}
