package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// maxRequestBody bounds what a request body may be read up to, so that a
// client cannot make this server hold an arbitrary amount of memory by
// sending one that never ends.
//
// A megabyte is far more than the bodies of this milestone need — a login is
// under a hundred bytes. Step 7's point batches are the first bodies where the
// limit is a real question, and this is the one place to raise it.
const maxRequestBody = 1 << 20

// decodeJSON reads the request body as JSON into v.
//
// Unknown fields are ignored rather than refused: a client is free to send
// more than this server reads, and upstream, being Rails, ignores them too.
//
// A body over [maxRequestBody] comes back as an ordinary read failure, so a
// caller answers it as it answers a body that is not JSON. RFC 9110 has 413
// for it, but upstream is fronted by a proxy that would answer first, and
// inventing a status this API is not known to send is the kind of difference a
// client trips over. Revisit with Step 7, where the limit first matters.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(v); err != nil {
		return fmt.Errorf("decoding the request body: %w", err)
	}

	return nil
}
