package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
)

// contentTypeJSON is sent with every response, including the error ones. The
// charset is upstream's, and it is load-bearing: a Dart client decodes a body
// with no charset as latin1. See "Responses carry Content-Type" in TODO.md.
const contentTypeJSON = "application/json; charset=utf-8"

// encodeFailedBody is the last-resort body for a response whose payload could
// not be encoded. It is a literal because building it would be the very thing
// that just failed.
const encodeFailedBody = `{"error":"internal server error"}`

// writeJSON writes v as the JSON body of the response.
//
// The body is encoded into a buffer before anything is written, so that a
// failure to encode can still be answered with a 500 instead of a 200 with a
// truncated body. Once the status line is out, nothing can be taken back.
func (a *api) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		a.logger.Error("encoding the response body failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)

		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(http.StatusInternalServerError)

		if _, err := w.Write([]byte(encodeFailedBody + "\n")); err != nil {
			a.logger.Error("writing the response body failed", "error", err)
		}

		return
	}

	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)

	// A client that hung up mid-response is a fact about the client, so it is
	// logged and not turned into a server error.
	if _, err := buf.WriteTo(w); err != nil {
		a.logger.Error("writing the response body failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
	}
}

// writeError answers with the JSON error body every failing request shares.
//
// message is the whole of what the client is told: upstream sends a bare
// `{"error": "..."}`, so there is no field to put a code or a detail in, and
// inventing one would be a difference a client could trip over.
func (a *api) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	a.writeJSON(w, r, status, dto.Error{Error: message})
}
