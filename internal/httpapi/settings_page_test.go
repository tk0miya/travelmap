package httpapi_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/httpapi"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// otherUserAPIKey authenticates nothing outside this file, the same
// reasoning as [testAPIKey].
const otherUserAPIKey = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f1" //nolint:gosec // see testAPIKey

// settingsPageOptions is the Options every test in this file needs for the
// Swarm OAuth flow itself to work correctly, not just to exist: BaseURL and
// the client credentials are what /settings/foursquare/connect builds the
// Foursquare redirect from.
func settingsPageOptions(st store.Store) httpapi.Options {
	return httpapi.Options{
		Store:                  st,
		BaseURL:                "https://travelmap.example",
		FoursquareClientID:     "the-client-id",
		FoursquareClientSecret: "the-client-secret",
	}
}

// otherUser is a second account, distinct from [testUser], for the tests
// pinning that one user cannot see or disconnect another's Swarm link.
func otherUser(t *testing.T) model.User {
	t.Helper()

	hash, err := testPasswordDigest()
	if err != nil {
		t.Fatalf("hashing the test password: %v", err)
	}

	return model.User{
		ID:           2,
		Email:        "bob@example.com",
		PasswordHash: hash,
		APIKey:       otherUserAPIKey,
		CreatedAt:    testCreatedAt,
		UpdatedAt:    testUpdatedAt,
	}
}

// loginCookie signs in as email/password against srv and returns the session
// cookie value, the same login the browser's own form performs.
func loginCookie(t *testing.T, srv *httptest.Server, email string) string {
	t.Helper()

	resp := doNoRedirect(t, srv, http.MethodPost, "/login",
		withForm(url.Values{"email": {email}, "password": {testPassword}}))

	token := sessionCookie(t, resp)
	if token == "" {
		t.Fatalf("signing in as %s set no session cookie", email)
	}

	return token
}

func withSession(token string) requestOption {
	return withHeader("Cookie", "session="+token)
}

// TestSettingsPageRequiresASession pins that an anonymous visitor is sent to
// the login form rather than answered 401, the same as
// /settings/foursquare/connect.
func TestSettingsPageRequiresASession(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithOptions(t, settingsPageOptions(newTestStore(t)))

	resp := doNoRedirect(t, srv, http.MethodGet, "/settings")
	if resp.status != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.status, http.StatusFound)
	}

	if got := resp.header.Get("Location"); got != "/login" {
		t.Errorf("Location = %q, want /login", got)
	}
}

// TestFoursquareDisconnectRequiresASession covers the same guard on the POST
// route.
func TestFoursquareDisconnectRequiresASession(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithOptions(t, settingsPageOptions(newTestStore(t)))

	resp := doNoRedirect(t, srv, http.MethodPost, "/settings/foursquare/disconnect")
	if resp.status != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.status, http.StatusFound)
	}

	if got := resp.header.Get("Location"); got != "/login" {
		t.Errorf("Location = %q, want /login", got)
	}
}

// TestSettingsPageNotLinked covers a fresh account: no Swarm account linked
// yet, with a link to start the OAuth flow.
func TestSettingsPageNotLinked(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithOptions(t, settingsPageOptions(newTestStore(t)))
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/settings", withSession(token))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte("No Swarm account is connected")) {
		t.Errorf("body = %q, want it to say no account is connected", resp.body)
	}

	if !bytes.Contains(resp.body, []byte(`href="/settings/foursquare/connect"`)) {
		t.Errorf("body = %q, want a link starting the OAuth flow", resp.body)
	}
}

// TestSettingsPageLinked covers an account with a Swarm link already
// created — by the CLI or by a previous run of the OAuth flow, the page does
// not care which.
func TestSettingsPageLinked(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/settings", withSession(token))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if !bytes.Contains(resp.body, []byte(foursquareUserID)) {
		t.Errorf("body = %q, want it to name the linked Foursquare user %s", resp.body, foursquareUserID)
	}

	if !bytes.Contains(resp.body, []byte("No check-ins fetched yet")) {
		t.Errorf("body = %q, want it to say nothing has been fetched, SyncedThrough being nil", resp.body)
	}

	if !bytes.Contains(resp.body, []byte(`action="/settings/foursquare/disconnect"`)) {
		t.Errorf("body = %q, want a form to disconnect", resp.body)
	}
}

// TestSettingsPageReportsSyncedThrough covers the column docs/database.md's
// own "foursquare_accounts" entry reserves for reporting how current an
// account is — this page is that column's first reader.
func TestSettingsPageReportsSyncedThrough(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	through := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	if err := st.FoursquareAccounts().UpdateSyncedThrough(t.Context(), 1, through); err != nil {
		t.Fatalf("UpdateSyncedThrough: %v", err)
	}

	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/settings", withSession(token))

	if !bytes.Contains(resp.body, []byte(through.Format(time.RFC1123))) {
		t.Errorf("body = %q, want it to report SyncedThrough as %s", resp.body, through.Format(time.RFC1123))
	}
}

// TestFoursquareDisconnectRemovesTheLink covers the golden path: disconnecting
// removes the row, the page then reports not connected, and a check-in
// already collected is untouched.
func TestFoursquareDisconnectRemovesTheLink(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)
	before := probeCheckin(t, st)

	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	token := loginCookie(t, srv, testEmail)

	disconnectResp := doNoRedirect(t, srv, http.MethodPost, "/settings/foursquare/disconnect", withSession(token))
	if disconnectResp.status != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", disconnectResp.status, http.StatusSeeOther)
	}

	if got := disconnectResp.header.Get("Location"); got != "/settings" {
		t.Errorf("Location = %q, want /settings", got)
	}

	pageResp := do(t, srv, http.MethodGet, "/settings", withSession(token))
	if !bytes.Contains(pageResp.body, []byte("No Swarm account is connected")) {
		t.Errorf("body after disconnect = %q, want it to report not connected", pageResp.body)
	}

	if _, err := st.FoursquareAccounts().ByUserID(t.Context(), 1); err == nil {
		t.Error("ByUserID after disconnect found a row, want it gone")
	}

	after := probeCheckin(t, st)
	if after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("the check-in changed across disconnect: before %+v, after %+v", before, after)
	}
}

// TestFoursquareDisconnectWithNothingLinkedIsNotAnError pins that
// disconnecting an account with nothing linked still redirects rather than
// failing — [store.FoursquareAccountRepository.Delete]'s own idempotency.
func TestFoursquareDisconnectWithNothingLinkedIsNotAnError(t *testing.T) {
	t.Parallel()

	srv := newTestServerWithOptions(t, settingsPageOptions(newTestStore(t)))
	token := loginCookie(t, srv, testEmail)

	resp := doNoRedirect(t, srv, http.MethodPost, "/settings/foursquare/disconnect", withSession(token))
	if resp.status != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.status, http.StatusSeeOther)
	}
}

// TestSettingsPageScopedToSession pins that one signed-in user sees and can
// disconnect only their own link, never another user's.
func TestSettingsPageScopedToSession(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser(t), otherUser(t))
	linkFoursquareAccount(t, st)

	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	otherToken := loginCookie(t, srv, "bob@example.com")

	pageResp := do(t, srv, http.MethodGet, "/settings", withSession(otherToken))
	if bytes.Contains(pageResp.body, []byte(foursquareUserID)) {
		t.Errorf("the other user's page = %q, want it not to name %s's account", pageResp.body, testEmail)
	}

	if !bytes.Contains(pageResp.body, []byte("No Swarm account is connected")) {
		t.Errorf("the other user's page = %q, want it to report not connected", pageResp.body)
	}

	doNoRedirect(t, srv, http.MethodPost, "/settings/foursquare/disconnect", withSession(otherToken))

	if _, err := st.FoursquareAccounts().ByUserID(t.Context(), 1); err != nil {
		t.Errorf("ByUserID for user 1 after the other user's disconnect returned %v, want the link untouched", err)
	}
}

// TestSettingsPageStoreFailure and TestFoursquareDisconnectStoreFailure
// cover the 500 path: a store that cannot be read or written answers as one
// rather than as a fresh, unlinked account.
func TestSettingsPageStoreFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableFoursquareAccounts(t, testUser(t))
	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/settings", withSession(token))
	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}
}

func TestFoursquareDisconnectStoreFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableFoursquareAccounts(t, testUser(t))
	srv := newTestServerWithOptions(t, settingsPageOptions(st))
	token := loginCookie(t, srv, testEmail)

	resp := doNoRedirect(t, srv, http.MethodPost, "/settings/foursquare/disconnect", withSession(token))
	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}
}

// TestHeaderLinksToSettingsWhenSignedIn covers that a signed-in visitor is
// shown the way to the settings page from anywhere — settingsPage itself
// decides what a visitor sees there, so the header link no longer depends on
// whether Foursquare is configured. A signed-out visitor is not shown it at
// all, since /settings would just send them on to /login.
func TestHeaderLinksToSettingsWhenSignedIn(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	token := loginCookie(t, srv, testEmail)

	signedInResp := do(t, srv, http.MethodGet, "/", withSession(token))
	if !bytes.Contains(signedInResp.body, []byte(`href="/settings"`)) {
		t.Errorf("body = %q, want a header link to /settings for a signed-in visitor", signedInResp.body)
	}

	signedOutResp := do(t, srv, http.MethodGet, "/login")
	if bytes.Contains(signedOutResp.body, []byte(`href="/settings"`)) {
		t.Errorf("body = %q, want no header link to /settings for a signed-out visitor", signedOutResp.body)
	}
}
