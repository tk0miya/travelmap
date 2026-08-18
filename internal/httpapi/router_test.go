package httpapi_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/httpapi"
)

// dawarichVersion is repeated here rather than exported from the package
// under test: the header value is a promise made to clients, so a change to it
// should fail a test instead of being carried along by both sides.
const dawarichVersion = "1.12.2"

// newTestServer starts the real router on a test server, so that the route
// registration and the middleware chain are part of what each test exercises.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	handler := httpapi.New(httpapi.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv
}

// response is what a test looks at: the parts of an http.Response that
// outlive the connection. Returning it instead of the *http.Response keeps the
// body's lifetime inside do, where it is closed.
type response struct {
	status int
	header http.Header
	body   []byte
}

// do issues a request against the test server and reads the whole response.
func do(t *testing.T, srv *httptest.Server, method, path string) response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	resp, err := srv.Client().Do(req)
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

func TestHealth(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/health")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	// Both headers are required of a Dawarich server, and a client that does
	// not find them treats the server as unusable.
	if got, want := resp.header.Get("X-Dawarich-Response"), "Hey, I'm alive!"; got != want {
		t.Errorf("X-Dawarich-Response = %q, want %q", got, want)
	}

	if got := resp.header.Get("X-Dawarich-Version"); got != dawarichVersion {
		t.Errorf("X-Dawarich-Version = %q, want %q", got, dawarichVersion)
	}

	assertGolden(t, "health.json", resp.body)
}

// TestHeadIsAnsweredLikeGet pins that a GET route also answers HEAD with the
// same status and headers, for the reason the get helper carries.
func TestHeadIsAnsweredLikeGet(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodHead, "/api/v1/health")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got := resp.header.Get("X-Dawarich-Version"); got != dawarichVersion {
		t.Errorf("X-Dawarich-Version = %q, want %q", got, dawarichVersion)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a HEAD response", resp.body)
	}
}

// TestErrorResponses covers the two ways a request can miss: an endpoint this
// server does not implement, and a method a known route does not serve. What
// rides on the 404 in particular is in the notFound handler.
func TestErrorResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		method     string
		path       string
		wantStatus int
		wantGolden string
		wantAllow  string
	}{
		"unimplemented endpoint": {
			method:     http.MethodGet,
			path:       "/api/v1/points",
			wantStatus: http.StatusNotFound,
			wantGolden: "not_found.json",
		},
		"unknown path outside the API": {
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
			wantGolden: "not_found.json",
		},
		"method the route does not serve": {
			method:     http.MethodPost,
			path:       "/api/v1/health",
			wantStatus: http.StatusMethodNotAllowed,
			wantGolden: "method_not_allowed.json",
			wantAllow:  "GET, HEAD",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, tt.method, tt.path)

			if resp.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.status, tt.wantStatus)
			}

			if got, want := resp.header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}

			assertGolden(t, tt.wantGolden, resp.body)

			// RFC 9110 requires a 405 to say which methods would work, and
			// chi's own answer is lost as soon as its handler is replaced.
			if got := resp.header.Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
		})
	}
}

// TestDawarichHeadersOnEveryAPIResponse pins that the headers are not special
// to /health, which is what the dawarichHeaders middleware exists for.
func TestDawarichHeadersOnEveryAPIResponse(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/points")

	want := map[string]string{
		"X-Dawarich-Response": "Hey, I'm alive!",
		"X-Dawarich-Version":  dawarichVersion,
	}

	got := map[string]string{
		"X-Dawarich-Response": resp.header.Get("X-Dawarich-Response"),
		"X-Dawarich-Version":  resp.header.Get("X-Dawarich-Version"),
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("headers differ (-want +got):\n%s", diff)
	}
}
