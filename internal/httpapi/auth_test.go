package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/httpapi"
)

// bearer is the Authorization header value carrying key.
func bearer(key string) requestOption {
	return withHeader("Authorization", "Bearer "+key)
}

// TestLogin covers the endpoint a client with only a password uses to get the
// API key every other endpoint wants.
func TestLogin(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/auth/login",
		withBody(`{"email":"Alice@Example.com","password":"`+testPassword+`"}`))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	// The address is normalised before the lookup, so the capitals a client
	// sends do not decide whether the account is found.
	assertGolden(t, "login.json", resp.body)
}

// TestLoginRefused covers every way a login can fail. They all get the same
// answer: which half was wrong is not something an endpoint that anyone can
// call should say.
func TestLoginRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"the wrong password":       `{"email":"` + testEmail + `","password":"not the password"}`,
		"an unregistered email":    `{"email":"bob@example.com","password":"` + testPassword + `"}`,
		"an email that is not one": `{"email":"alice","password":"` + testPassword + `"}`,
		"no password":              `{"email":"` + testEmail + `"}`,
		"no fields at all":         `{}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/api/v1/auth/login", withBody(body))

			if resp.status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
			}

			// Unlike the 401 of an unauthenticated request, this one has a
			// body: it is the shape upstream's auth controllers render, and
			// the spec documents it as this endpoint's 401.
			assertGolden(t, "login_failed.json", resp.body)
		})
	}
}

// TestLoginRejectsAnUnreadableBody pins that a body that cannot be read is a
// bad request rather than a failed login: a client sending one has a bug, and
// "invalid email or password" would send whoever debugs it after the password.
func TestLoginRejectsAnUnreadableBody(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"a body that is not JSON": `{"email":`,
		// Over the limit the request body is read up to, which is what stops a
		// client from making this server hold a body that never ends. Without
		// a case for it, removing that limit would break no test.
		"a body over the size limit": `{"email":"` + strings.Repeat("a", 2<<20) + `"}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPost, "/api/v1/auth/login", withBody(body))

			if resp.status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
			}

			assertGolden(t, "invalid_request_body.json", resp.body)
		})
	}
}

// TestUsersMe covers the endpoint a client calls to confirm that the key it
// was configured with belongs to somebody, through both ways of sending it.
func TestUsersMe(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		opts []requestOption
	}{
		// The spec documents this endpoint as header-only, but clients send
		// the query parameter to endpoints documented the other way round, so
		// both are accepted everywhere.
		"the api_key query parameter": {path: "/api/v1/users/me?api_key=" + testAPIKey},
		"an Authorization header":     {path: "/api/v1/users/me", opts: []requestOption{bearer(testAPIKey)}},
		// RFC 9110 makes the scheme case-insensitive, and a client that spells
		// it in lower case is sending a valid header.
		"a lowercase scheme": {
			path: "/api/v1/users/me",
			opts: []requestOption{withHeader("Authorization", "bearer "+testAPIKey)},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodGet, tt.path, tt.opts...)

			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if got, want := resp.header.Get("X-Dawarich-Response"), authenticatedResponse; got != want {
				t.Errorf("X-Dawarich-Response = %q, want %q", got, want)
			}

			assertGolden(t, "users_me.json", resp.body)
		})
	}
}

// TestUsersMeReportsTheConfiguredTimezone pins that settings.timezone comes
// from Options.Timezone (TRAVELMAP_TIMEZONE) rather than a constant, which
// [TestUsersMe]'s golden file — taken under the "UTC" default — cannot tell
// apart from a hardcoded "UTC" on its own.
func TestUsersMeReportsTheConfiguredTimezone(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithOptions(t, httpapi.Options{Store: newTestStore(t), Timezone: "Asia/Tokyo"})
	resp := do(t, srv, http.MethodGet, "/api/v1/users/me?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	var body struct {
		User struct {
			Settings struct {
				Timezone string `json:"timezone"`
			} `json:"settings"`
		} `json:"user"`
	}

	if err := json.Unmarshal(resp.body, &body); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	if got := body.User.Settings.Timezone; got != "Asia/Tokyo" {
		t.Errorf("settings.timezone = %q, want Asia/Tokyo", got)
	}
}

// TestUnauthenticatedRequestsAreRefused covers what an authenticated route
// does with credentials it cannot accept.
func TestUnauthenticatedRequestsAreRefused(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		opts []requestOption
	}{
		"no credentials at all": {path: "/api/v1/users/me"},
		"an api_key nobody has": {path: "/api/v1/users/me?api_key=" + testAPIKey + "0"},
		"a bearer token nobody has": {
			path: "/api/v1/users/me",
			opts: []requestOption{bearer(testAPIKey + "0")},
		},
		// Not a Bearer credential, so there is nothing to look up. Reading the
		// value after any scheme would accept headers no other server accepts.
		"another authorization scheme": {
			path: "/api/v1/users/me",
			opts: []requestOption{withHeader("Authorization", "Basic "+testAPIKey)},
		},
		"a scheme with no token": {
			path: "/api/v1/users/me",
			opts: []requestOption{withHeader("Authorization", "Bearer")},
		},
		"a token with something after it": {
			path: "/api/v1/users/me",
			opts: []requestOption{withHeader("Authorization", "Bearer "+testAPIKey+" extra")},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodGet, tt.path, tt.opts...)

			if resp.status != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
			}

			// Upstream answers `head :unauthorized`, so there is nothing to
			// parse. A client that reads the body of a 401 reads nothing here
			// too, rather than something only this server sends.
			if len(resp.body) != 0 {
				t.Errorf("body = %q, want it empty on a 401", resp.body)
			}

			// The compatibility headers are on every API response, including
			// the ones that refuse the request.
			if got := resp.header.Get("X-Dawarich-Version"); got != dawarichVersion {
				t.Errorf("X-Dawarich-Version = %q, want %q", got, dawarichVersion)
			}

			if got := resp.header.Get("X-Dawarich-Response"); got != aliveResponse {
				t.Errorf("X-Dawarich-Response = %q, want %q", got, aliveResponse)
			}
		})
	}
}

// TestHeadIsAnsweredOnAnAuthenticatedRoute pins that the HEAD registered
// alongside every GET goes through the same authentication, rather than
// answering 200 to a request carrying nothing.
func TestHeadIsAnsweredOnAnAuthenticatedRoute(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	if got := do(t, srv, http.MethodHead, "/api/v1/users/me", bearer(testAPIKey)).status; got != http.StatusOK {
		t.Errorf("HEAD with the API key: status = %d, want %d", got, http.StatusOK)
	}

	if got := do(t, srv, http.MethodHead, "/api/v1/users/me").status; got != http.StatusUnauthorized {
		t.Errorf("HEAD without credentials: status = %d, want %d", got, http.StatusUnauthorized)
	}
}

// TestHealthIsAuthenticationAware pins the header a client reads to find out
// whether the credentials it was configured with are accepted. /health is the
// only endpoint that answers 200 without any, so it is the only place the
// difference can show.
func TestHealthIsAuthenticationAware(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		opts []requestOption
		want string
	}{
		"no credentials": {path: "/api/v1/health", want: aliveResponse},
		"a key that names a user": {
			path: "/api/v1/health?api_key=" + testAPIKey,
			want: authenticatedResponse,
		},
		"a bearer token that names a user": {
			path: "/api/v1/health",
			opts: []requestOption{bearer(testAPIKey)},
			want: authenticatedResponse,
		},
		// A key nobody has is not an error here: /health takes no
		// authentication, so it answers 200 and says it was not authenticated.
		"a key nobody has": {path: "/api/v1/health?api_key=nope", want: aliveResponse},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodGet, tt.path, tt.opts...)

			if resp.status != http.StatusOK {
				t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if got := resp.header.Get("X-Dawarich-Response"); got != tt.want {
				t.Errorf("X-Dawarich-Response = %q, want %q", got, tt.want)
			}

			assertGolden(t, "health.json", resp.body)
		})
	}
}

// TestHealthDoesNotNeedTheStore pins that a request carrying no credentials is
// answered without a lookup at all, which is what lets /health report a server
// as reachable while its database cannot be read. Upstream instead looks for a
// user whose api_key is NULL; the difference is deliberate, and this is what
// holds it.
func TestHealthDoesNotNeedTheStore(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newUnavailableStore(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/health")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got := resp.header.Get("X-Dawarich-Response"); got != aliveResponse {
		t.Errorf("X-Dawarich-Response = %q, want %q", got, aliveResponse)
	}

	assertGolden(t, "health.json", resp.body)
}

// TestAStoreFailureIsNotA401 pins that a database that cannot be read is
// reported as this server's fault. Answering 401 would send an operator
// looking for a wrong API key while the actual fault is here.
func TestAStoreFailureIsNotA401(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		method string
		path   string
		opts   []requestOption
	}{
		"authenticating a request": {
			method: http.MethodGet,
			path:   "/api/v1/users/me",
			opts:   []requestOption{bearer(testAPIKey)},
		},
		"looking a user up to log in": {
			method: http.MethodPost,
			path:   "/api/v1/auth/login",
			opts:   []requestOption{withBody(`{"email":"` + testEmail + `","password":"` + testPassword + `"}`)},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServerWith(t, newUnavailableStore(t))
			resp := do(t, srv, tt.method, tt.path, tt.opts...)

			if resp.status != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
			}

			assertGolden(t, "internal_server_error.json", resp.body)

			// The failure the authentication answers itself is the one
			// response the header middleware never reaches, so both headers
			// have to be on it all the same — and this is the only place
			// either of them is written by hand.
			want := map[string]string{
				"X-Dawarich-Response": aliveResponse,
				"X-Dawarich-Version":  dawarichVersion,
			}

			got := map[string]string{
				"X-Dawarich-Response": resp.header.Get("X-Dawarich-Response"),
				"X-Dawarich-Version":  resp.header.Get("X-Dawarich-Version"),
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("headers differ (-want +got):\n%s", diff)
			}
		})
	}
}
