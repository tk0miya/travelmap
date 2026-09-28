package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// otherUserAPIKey authenticates nothing outside this file, the same
// reasoning as [testAPIKey].
const otherUserAPIKey = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f1" //nolint:gosec // see testAPIKey

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
// cookie value, the same sign-in the browser's own SPA performs.
func loginCookie(t *testing.T, srv *httptest.Server, email string) string {
	t.Helper()

	resp := do(t, srv, http.MethodPost, "/travelmap/web/session", withBody(sessionBody(email, testPassword)))

	token := sessionCookie(t, resp)
	if token == "" {
		t.Fatalf("signing in as %s set no session cookie", email)
	}

	return token
}

func withSession(token string) requestOption {
	return withHeader("Cookie", "session="+token)
}

// TestGetFoursquareAccountRequiresASession covers that this JSON resource is
// refused the way /api/v1 refuses an unauthenticated request — requireUser's
// empty 401 — rather than the login-form redirect a browser navigation gets.
func TestGetFoursquareAccountRequiresASession(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty", resp.body)
	}
}

// TestDeleteFoursquareAccountRequiresASession covers the same guard on the
// DELETE route.
func TestDeleteFoursquareAccountRequiresASession(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodDelete, "/travelmap/web/foursquare_account")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}
}

// TestGetFoursquareAccountNotLinked covers a fresh account: no Swarm account
// linked yet.
func TestGetFoursquareAccountNotLinked(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(token))

	if resp.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
	}

	assertGolden(t, "not_found.json", resp.body)
}

// TestGetFoursquareAccountLinked covers an account with a Swarm link already
// created — by the CLI or by a previous run of the OAuth flow, this endpoint
// does not care which.
func TestGetFoursquareAccountLinked(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	srv := newTestServerWith(t, st)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(token))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "foursquare_account.json", resp.body)
}

// TestGetFoursquareAccountReportsSyncedThrough covers the column
// docs/database.md's own "foursquare_accounts" entry reserves for reporting
// how current an account is — this endpoint is that column's first reader.
func TestGetFoursquareAccountReportsSyncedThrough(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	through := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	if err := st.FoursquareAccounts().UpdateSyncedThrough(t.Context(), 1, through); err != nil {
		t.Fatalf("UpdateSyncedThrough: %v", err)
	}

	srv := newTestServerWith(t, st)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(token))

	want := `"synced_through":"2026-02-03T04:05:06.000Z"`
	if !strings.Contains(string(resp.body), want) {
		t.Errorf("body = %q, want it to contain %s", resp.body, want)
	}
}

// TestDeleteFoursquareAccountRemovesTheLink covers the golden path:
// disconnecting removes the row, the resource then reports not linked, and a
// check-in already collected is untouched.
func TestDeleteFoursquareAccountRemovesTheLink(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)
	before := probeCheckin(t, st)

	srv := newTestServerWith(t, st)
	token := loginCookie(t, srv, testEmail)

	deleteResp := do(t, srv, http.MethodDelete, "/travelmap/web/foursquare_account", withSession(token))
	if deleteResp.status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", deleteResp.status, http.StatusNoContent)
	}

	getResp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(token))
	if getResp.status != http.StatusNotFound {
		t.Errorf("status after disconnect = %d, want %d", getResp.status, http.StatusNotFound)
	}

	if _, err := st.FoursquareAccounts().ByUserID(t.Context(), 1); err == nil {
		t.Error("ByUserID after disconnect found a row, want it gone")
	}

	after := probeCheckin(t, st)
	if after.ID != before.ID || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Errorf("the check-in changed across disconnect: before %+v, after %+v", before, after)
	}
}

// TestDeleteFoursquareAccountWithNothingLinkedIsNotAnError pins that
// disconnecting an account with nothing linked still answers 204 rather than
// failing — [store.FoursquareAccountRepository.Delete]'s own idempotency.
func TestDeleteFoursquareAccountWithNothingLinkedIsNotAnError(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodDelete, "/travelmap/web/foursquare_account", withSession(token))
	if resp.status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.status, http.StatusNoContent)
	}
}

// TestFoursquareAccountScopedToSession pins that one signed-in user sees and
// can disconnect only their own link, never another user's.
func TestFoursquareAccountScopedToSession(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser(t), otherUser(t))
	linkFoursquareAccount(t, st)

	srv := newTestServerWith(t, st)
	otherToken := loginCookie(t, srv, "bob@example.com")

	getResp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(otherToken))
	if getResp.status != http.StatusNotFound {
		t.Errorf("the other user's status = %d, want %d", getResp.status, http.StatusNotFound)
	}

	do(t, srv, http.MethodDelete, "/travelmap/web/foursquare_account", withSession(otherToken))

	if _, err := st.FoursquareAccounts().ByUserID(t.Context(), 1); err != nil {
		t.Errorf("ByUserID for user 1 after the other user's disconnect returned %v, want the link untouched", err)
	}
}

// TestGetFoursquareAccountStoreFailure and
// TestDeleteFoursquareAccountStoreFailure cover the 500 path: a store that
// cannot be read or written answers as one rather than as an unlinked
// account.
func TestGetFoursquareAccountStoreFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableFoursquareAccounts(t, testUser(t))
	srv := newTestServerWith(t, st)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodGet, "/travelmap/web/foursquare_account", withSession(token))
	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

func TestDeleteFoursquareAccountStoreFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableFoursquareAccounts(t, testUser(t))
	srv := newTestServerWith(t, st)
	token := loginCookie(t, srv, testEmail)

	resp := do(t, srv, http.MethodDelete, "/travelmap/web/foursquare_account", withSession(token))
	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}
