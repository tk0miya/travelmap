package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecovererAnswersWithTheErrorBody pins that a panicking handler still
// produces the JSON error body every other failure produces: a client that
// gets a truncated or empty body cannot tell a bug from a network fault.
func TestRecovererAnswersWithTheErrorBody(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	a := &api{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	handler := a.recoverer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if got, want := rec.Body.String(), `{"error":"internal server error"}`; !strings.Contains(got, want) {
		t.Errorf("body = %q, want it to contain %q", got, want)
	}

	// The panic is the only record of the bug, so it has to reach the log.
	if got := logs.String(); !strings.Contains(got, "boom") {
		t.Errorf("the panic was not logged: %q", got)
	}
}

// TestRecovererLetsErrAbortHandlerThrough pins that net/http's own way of
// aborting a response is not swallowed by the recovery.
func TestRecovererLetsErrAbortHandlerThrough(t *testing.T) {
	t.Parallel()

	a := &api{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}

	handler := a.recoverer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("http.ErrAbortHandler was swallowed, want it to propagate")
		}
	}()

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
}
