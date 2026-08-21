package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIKeyFrom pins where a request's credentials are read from, one case at
// a time.
//
// The tests through the router cover several of these again, deliberately: what
// they pin is that this is wired into the middleware at all, which is the
// difference between reading a header and acting on it. What they cannot show
// is which of two credentials wins, or that a malformed header is refused
// rather than merely unmatched, and that is what belongs here.
func TestAPIKeyFrom(t *testing.T) {
	t.Parallel()

	const key = "0123456789abcdef"

	tests := map[string]struct {
		target        string
		authorization string
		want          string
	}{
		"the query parameter":  {target: "/api/v1/points?api_key=" + key, want: key},
		"a Bearer header":      {target: "/api/v1/points", authorization: "Bearer " + key, want: key},
		"a lowercase scheme":   {target: "/api/v1/points", authorization: "bearer " + key, want: key},
		"an uppercase scheme":  {target: "/api/v1/points", authorization: "BEARER " + key, want: key},
		"extra spacing":        {target: "/api/v1/points", authorization: "Bearer   " + key + " ", want: key},
		"nothing at all":       {target: "/api/v1/points"},
		"an empty api_key":     {target: "/api/v1/points?api_key="},
		"another scheme":       {target: "/api/v1/points", authorization: "Basic " + key},
		"a scheme on its own":  {target: "/api/v1/points", authorization: "Bearer"},
		"a token with a space": {target: "/api/v1/points", authorization: "Bearer " + key + " more"},
		// The query parameter wins, because a client sending both means them
		// to be the same key and a server that picked either would answer
		// differently from one that picked the other.
		"both, with the header ignored": {
			target:        "/api/v1/points?api_key=" + key,
			authorization: "Bearer other",
			want:          key,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.authorization != "" {
				r.Header.Set("Authorization", tt.authorization)
			}

			if got := apiKeyFrom(r); got != tt.want {
				t.Errorf("apiKeyFrom = %q, want %q", got, tt.want)
			}
		})
	}
}
