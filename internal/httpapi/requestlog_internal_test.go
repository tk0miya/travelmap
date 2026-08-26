package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRedactQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  string
	}{
		"no query at all": {
			query: "",
			want:  "",
		},
		"the documented credential parameter": {
			query: "api_key=0f1e2d3c",
			want:  "api_key=[REDACTED]",
		},
		// Which parameters a client sends is the finding; only the value is
		// secret, so the name survives whatever it is.
		"a credential among ordinary parameters, sorted by name": {
			query: "start_at=2026-01-01&api_key=0f1e2d3c&per_page=100",
			want:  "api_key=[REDACTED]&per_page=100&start_at=2026-01-01",
		},
		"a credential parameter under another name": {
			query: "access_token=0f1e2d3c&SessionSecret=x&auth=y",
			want:  "SessionSecret=[REDACTED]&access_token=[REDACTED]&auth=[REDACTED]",
		},
		"a parameter sent more than once": {
			query: "order=asc&order=desc",
			want:  "order=asc&order=desc",
		},
		"values are logged decoded": {
			query: "start_at=2026-01-01T00%3A00%3A00%2B09%3A00",
			want:  "start_at=2026-01-01T00:00:00+09:00",
		},
		"a parameter with no value": {
			query: "slim",
			want:  "slim=",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			query, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("parsing %q: %v", tt.query, err)
			}

			if got := redactQuery(query); got != tt.want {
				t.Errorf("redactQuery(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestHeaderValue(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		header string
		values []string
		want   string
	}{
		"an ordinary header": {
			header: "User-Agent",
			values: []string{"Dawarich/1.0"},
			want:   "Dawarich/1.0",
		},
		"the credential this API reads": {
			header: "Authorization",
			values: []string{"Bearer 0f1e2d3c"},
			want:   "[REDACTED]",
		},
		// A closed-source client may authenticate in a way this server does
		// not read, and the log must not be where that credential lands. The
		// first of these is caught by its name, the second by the short list
		// of names that say nothing about what they carry.
		"a credential under a header this API does not read": {
			header: "X-Auth-Token",
			values: []string{"0f1e2d3c"},
			want:   "[REDACTED]",
		},
		"a credential under a name that does not say so": {
			header: "Cookie",
			values: []string{"session=0f1e2d3c"},
			want:   "[REDACTED]",
		},
		// The same value Cookie is redacted for, under a name that does say
		// so: the word list is what has to catch this one.
		"a session identifier of its own": {
			header: "X-Session-Id",
			values: []string{"0f1e2d3c"},
			want:   "[REDACTED]",
		},
		"a header sent more than once": {
			header: "Accept",
			values: []string{"application/json", "text/plain"},
			want:   "application/json, text/plain",
		},
		// A device may hand its previous URL back verbatim in Referer, and
		// that URL is a plausible carrier for the api_key it authenticated
		// with — the same credential redactQuery exists to hide from the
		// query string this server received directly.
		"a Referer carrying the query-string credential": {
			header: "Referer",
			values: []string{"https://app.example/api/v1/points?api_key=0f1e2d3c&per_page=100"},
			want:   "https://app.example/api/v1/points?api_key=[REDACTED]&per_page=100",
		},
		"a Referer with no query string": {
			header: "Referer",
			values: []string{"https://app.example/settings"},
			want:   "https://app.example/settings",
		},
		// Not a URL at all: passed through rather than dropped, the same as
		// any other header this facility cannot make sense of.
		"a Referer that does not parse as a URL": {
			header: "Referer",
			values: []string{"bad\x7fvalue"},
			want:   "bad\x7fvalue",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := headerValue(tt.header, tt.values); got != tt.want {
				t.Errorf("headerValue(%q, %q) = %q, want %q", tt.header, tt.values, got, tt.want)
			}
		})
	}
}

// TestStatusOfAHandlerThatWroteNothing pins that the status a line reports is
// the one that went on the wire: net/http sends 200 for a handler that never
// called WriteHeader, where the recorder still holds zero.
func TestStatusOfAHandlerThatWroteNothing(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	a := &api{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	handler := a.logRequests(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if got, want := logs.String(), "status=200"; !strings.Contains(got, want) {
		t.Errorf("log = %q, want it to contain %q", got, want)
	}
}

// TestLogRequestsRecordsTheStatusTheRecoveryWrote pins what the order New
// registers these two in buys: with the recovery wrapped by the log, a panic
// is logged as the 500 the client was answered with. The other way round the
// log's defer runs while the panic is still unwinding, before the recovery
// writes anything, and the line would say 200 about a request that got a 500.
//
// The chain is built here rather than taken from New, which no test can reach
// this path through: nothing New routes to panics. So this pins the property,
// and the comment on the r.Use call in New is what ties it to the order.
func TestLogRequestsRecordsTheStatusTheRecoveryWrote(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	a := &api{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	handler := a.logRequests(a.recoverer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if got, want := logs.String(), "status=500"; !strings.Contains(got, want) {
		t.Errorf("log = %q, want it to contain %q", got, want)
	}
}

// TestLogRequestsLogsThePanicItCannotSee pins that a request unwinding past
// the middleware is still logged: without the deferred write, the one request
// an operator most wants a line for would be the one missing from the log.
func TestLogRequestsLogsThePanicItCannotSee(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	a := &api{logger: slog.New(slog.NewTextHandler(&logs, nil))}
	handler := a.logRequests(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	func() {
		defer func() { _ = recover() }()

		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/points", nil))
	}()

	if got, want := logs.String(), "path=/api/v1/points"; !strings.Contains(got, want) {
		t.Errorf("log = %q, want it to contain %q", got, want)
	}
}
