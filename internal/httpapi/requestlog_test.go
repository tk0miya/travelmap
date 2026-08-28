package httpapi_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tk0miya/travelmap/internal/httpapi"
)

// logBuffer collects what a server logs. It is locked because the server
// writes from the goroutine serving the request while the test reads from its
// own, which is a race however quiet it stays in practice.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// newLoggedTestServer starts the real router with the request log switched on
// or off, and hands back what it logged. The router is the thing under test
// here: whether an unmatched route reaches the middleware is decided by the
// registration and by chi, not by the middleware itself.
func newLoggedTestServer(t *testing.T, debugLogRequests bool) (*httptest.Server, *logBuffer) {
	t.Helper()

	logs := &logBuffer{}

	handler := httpapi.New(httpapi.Options{
		Logger:           slog.New(slog.NewTextHandler(logs, nil)),
		Store:            newTestStore(t),
		DebugLogRequests: debugLogRequests,
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv, logs
}

// TestRequestLogRecordsUnmatchedRoutes pins the point of the whole facility:
// an endpoint this server does not serve is answered 404 and shows up in the
// log all the same, because that 404 is the record of something the app wants.
func TestRequestLogRecordsUnmatchedRoutes(t *testing.T) {
	t.Parallel()

	srv, logs := newLoggedTestServer(t, true)

	resp := do(t, srv, http.MethodGet, "/api/v1/countries/visited_cities?year=2026")
	if resp.status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotFound)
	}

	for _, want := range []string{
		"method=GET",
		"path=/api/v1/countries/visited_cities",
		"year=2026",
		"status=404",
	} {
		if got := logs.String(); !strings.Contains(got, want) {
			t.Errorf("log = %q, want it to contain %q", got, want)
		}
	}
}

// TestRequestLogCarriesNoCredentials pins the other half: the traffic worth
// capturing is a real device's, and a real device sends live credentials both
// ways this server accepts them.
func TestRequestLogCarriesNoCredentials(t *testing.T) {
	t.Parallel()

	srv, logs := newLoggedTestServer(t, true)

	resp := do(t, srv, http.MethodGet, "/api/v1/users/me?api_key="+testAPIKey,
		withHeader("Authorization", "Bearer "+testAPIKey),
		withHeader("Cookie", "session="+testAPIKey),
		// A client that reached this endpoint from a page carrying its own
		// api_key in the URL, the way this API's query-parameter
		// authentication makes routine.
		withHeader("Referer", "https://app.example/map?api_key="+testAPIKey),
	)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	got := logs.String()

	if strings.Contains(got, testAPIKey) {
		t.Errorf("the API key reached the log: %q", got)
	}

	// The credential is gone; that the client sent one, and where, is not.
	for _, want := range []string{
		"api_key=[REDACTED]",
		"header.Authorization=[REDACTED]",
		`header.Referer="https://app.example/map?api_key=[REDACTED]"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log = %q, want it to contain %q", got, want)
		}
	}
}

// TestRequestLogNeverCarriesTheBody pins the other credential channel: a
// request body. POST /api/v1/auth/login carries a password in its body, which
// is exactly the shape a logger written as "log the body unless told
// otherwise" would leak on the first login it sees.
func TestRequestLogNeverCarriesTheBody(t *testing.T) {
	t.Parallel()

	srv, logs := newLoggedTestServer(t, true)

	resp := do(t, srv, http.MethodPost, "/api/v1/auth/login",
		withBody(`{"email":"`+testEmail+`","password":"`+testPassword+`"}`))
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got := logs.String(); strings.Contains(got, testPassword) {
		t.Errorf("the password reached the log: %q", got)
	}
}

// TestRequestLogIsOffByDefault pins that a server nobody configured logs no
// request lines: this is a capture facility, not an access log, and it prints
// the headers of every request to prove it.
func TestRequestLogIsOffByDefault(t *testing.T) {
	t.Parallel()

	srv, logs := newLoggedTestServer(t, false)

	do(t, srv, http.MethodGet, "/api/v1/health")

	if got := logs.String(); got != "" {
		t.Errorf("log = %q, want it empty", got)
	}
}
