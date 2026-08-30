package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestIndexRedirectsAnonymous covers that a first-time visitor with no
// session lands on the login form rather than a status page pointing at it.
func TestIndexRedirectsAnonymous(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := doNoRedirect(t, srv, http.MethodGet, "/")

	if resp.status != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.status, http.StatusFound)
	}

	if got, want := resp.header.Get("Location"), "/login"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
}

// TestStaticStylesheet covers that the stylesheet is served out of the same
// embed.FS as the templates, at the URL the base layout links.
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
