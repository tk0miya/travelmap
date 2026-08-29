package httpapi_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// TestIndex covers the page a browser gets at the server's root, with no
// session yet to name an account.
func TestIndex(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	if !bytes.Contains(resp.body, []byte("travelmap")) {
		t.Errorf("body = %q, want it to mention travelmap", resp.body)
	}

	if !bytes.Contains(resp.body, []byte("Not signed in.")) {
		t.Errorf("body = %q, want it to say not signed in with no session", resp.body)
	}

	if !bytes.Contains(resp.body, []byte(`href="/login"`)) {
		t.Errorf("body = %q, want a link to /login with no session", resp.body)
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
