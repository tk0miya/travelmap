package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestIndexServesTheFrontendShell covers that GET / falls through to the
// frontend build's catch-all like any other unclaimed path, now that
// requireSessionUser no longer guards it: the redirect an anonymous visitor
// used to get from this route itself is React's own to answer now — see
// RequireAuth.test.tsx and App.test.tsx.
func TestIndexServesTheFrontendShell(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !strings.Contains(string(resp.body), `<div id="root">`) {
		t.Errorf("body = %q, want it to contain the frontend build's root element", resp.body)
	}
}

// TestStaticStylesheet covers that the stylesheet is still served at the URL
// base.html links, now out of the frontend build's own embed.FS — see
// frontend_test.go for the rest of what that embed serves.
func TestStaticStylesheet(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/static/style.css")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got := resp.header.Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want a text/css prefix", got)
	}

	if len(resp.body) == 0 {
		t.Error("the stylesheet body is empty")
	}
}
