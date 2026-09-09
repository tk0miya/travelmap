package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestFrontendServesUnclaimedPaths covers the catch-all Step 37 adds: a
// browser GET for a path no other route claims gets the frontend build's
// index.html rather than the Dawarich-style JSON 404, so a client-side route
// this server has never heard of still renders. TestErrorResponses covers
// the paths that still are errors.
func TestFrontendServesUnclaimedPaths(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/nope")

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got := resp.header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want a text/html prefix", got)
	}

	if !strings.Contains(string(resp.body), `<div id="root">`) {
		t.Errorf("body = %q, want it to contain the frontend build's root element", resp.body)
	}
}

// TestFrontendServesItsOwnAssets covers that a path the frontend build
// actually produced — its JS bundle, referenced from the same index.html
// TestFrontendServesUnclaimedPaths pins — is served as itself rather than
// falling back to index.html too.
func TestFrontendServesItsOwnAssets(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/nope")

	scriptSrc := extractScriptSrc(t, string(resp.body))

	asset := do(t, srv, http.MethodGet, scriptSrc)

	if asset.status != http.StatusOK {
		t.Errorf("status = %d, want %d", asset.status, http.StatusOK)
	}

	if got := asset.header.Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") &&
		!strings.HasPrefix(got, "application/javascript") {
		t.Errorf("Content-Type = %q, want a JavaScript prefix", got)
	}
}

// extractScriptSrc pulls the src out of index.html's <script type="module">
// tag, brittle by design: this test breaks the moment the build output it
// reads stops looking like what it expects, rather than silently checking
// nothing.
func extractScriptSrc(t *testing.T, html string) string {
	t.Helper()

	const marker = `<script type="module" crossorigin src="`

	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no <script type=%q> tag found in %q", "module", html)
	}

	rest := html[i+len(marker):]

	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatalf("unterminated script src in %q", html)
	}

	return rest[:j]
}
