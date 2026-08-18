package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var errUnencodable = errors.New("cannot be encoded")

// unencodable is a value json.Encoder refuses, which is how the test reaches
// the failure branch of writeJSON.
type unencodable struct{}

func (unencodable) MarshalJSON() ([]byte, error) {
	return nil, errUnencodable
}

// TestWriteJSONAnswersA500WhenEncodingFails pins that a body that cannot be
// encoded produces a 500 rather than a 200 with nothing in it. The status is
// written after the encoding precisely so this stays possible.
func TestWriteJSONAnswersA500WhenEncodingFails(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	a := &api{logger: slog.New(slog.NewTextHandler(&logs, nil))}

	rec := httptest.NewRecorder()
	a.writeJSON(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil), http.StatusOK, unencodable{})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if got, want := rec.Body.String(), encodeFailedBody; !strings.Contains(got, want) {
		t.Errorf("body = %q, want it to contain %q", got, want)
	}

	if got := logs.String(); !strings.Contains(got, "encoding the response body failed") {
		t.Errorf("the encoding failure was not logged: %q", got)
	}
}
