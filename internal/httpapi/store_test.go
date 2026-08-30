package httpapi_test

import (
	"sync"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// The account every test in this package authenticates as. The API key is a
// plausible one — 64 hex characters, as internal/auth issues — because it is
// also what the golden files show a client being handed back.
const (
	testEmail    = "alice@example.com"
	testPassword = "correct horse battery"
	// A test fixture, not a credential: it authenticates nothing outside this
	// file, and shortening it would stop the golden file from showing what a
	// client is actually handed back.
	testAPIKey = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above
)

// The timestamps of the test user, fixed so that the golden file can pin how a
// time is written.
var (
	testCreatedAt = time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	testUpdatedAt = time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)
)

// testPasswordDigest is the bcrypt digest of [testPassword], made once for the
// package.
//
// It is a real digest rather than a fixed string, so that the password check in
// the login handler is the one that runs: a hand-written digest would only ever
// exercise the failure path. Hashing costs tens of milliseconds by design,
// though, and a test server is built per subtest — so it is made once here
// instead of once per server.
var testPasswordDigest = sync.OnceValues(func() (string, error) {
	return auth.HashPassword(testPassword)
})

// testUser builds the account the store holds.
func testUser(t *testing.T) model.User {
	t.Helper()

	hash, err := testPasswordDigest()
	if err != nil {
		t.Fatalf("hashing the test password: %v", err)
	}

	return model.User{
		ID:           1,
		Email:        testEmail,
		PasswordHash: hash,
		APIKey:       testAPIKey,
		CreatedAt:    testCreatedAt,
		UpdatedAt:    testUpdatedAt,
	}
}

// newTestStore returns the store the handlers are tested against: a real,
// migrated SQLite database holding [testUser] and nothing else.
func newTestStore(t *testing.T) store.Store {
	t.Helper()

	return storetest.New(t, testUser(t))
}

// newUnavailableStore returns a store that cannot be read at all, which is how
// the tests reach the paths that answer 500.
func newUnavailableStore(t *testing.T) store.Store {
	t.Helper()

	return storetest.Unavailable(t)
}

// newStoreWithUnavailablePoints returns a store that authenticates as
// [testUser] normally but fails every write to the point repository, for the
// tests newUnavailableStore would fail too early for.
func newStoreWithUnavailablePoints(t *testing.T) store.Store {
	t.Helper()

	return storetest.UnavailablePoints(t, testUser(t))
}

// newStoreWithUnavailableDailyStats returns a store that authenticates as
// [testUser] normally but fails every read of daily_stats, for the tests
// newUnavailableStore would fail too early for.
func newStoreWithUnavailableDailyStats(t *testing.T) store.Store {
	t.Helper()

	return storetest.UnavailableDailyStats(t, testUser(t))
}

// newStoreWithUnavailableTracks returns a store that authenticates as
// [testUser] normally but fails every read of tracks, for the tests
// newUnavailableStore would fail too early for.
func newStoreWithUnavailableTracks(t *testing.T) store.Store {
	t.Helper()

	return storetest.UnavailableTracks(t, testUser(t))
}
