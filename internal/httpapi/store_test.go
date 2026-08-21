package httpapi_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
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

// errStoreUnavailable stands in for a database that cannot be read, which the
// handlers have to tell apart from a lookup that simply matched nothing.
var errStoreUnavailable = errors.New("the database is unavailable")

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

// testUser builds the account the fake store holds.
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

// fakeStore is the store the handlers are tested against.
//
// The HTTP tests are about what the handlers do with what the store returns —
// including its errors, which a real database cannot be asked for on demand —
// so they substitute it rather than open one. The SQL itself is tested in
// internal/store/sqlite, against a real database.
type fakeStore struct {
	users fakeUsers
}

// newFakeStore returns a store holding [testUser] and nothing else.
func newFakeStore(t *testing.T) *fakeStore {
	t.Helper()

	return &fakeStore{users: fakeUsers{user: testUser(t)}}
}

// newFailingStore returns a store whose lookups all fail, which is how the
// tests reach the paths that answer 500.
func newFailingStore() *fakeStore {
	return &fakeStore{users: fakeUsers{err: errStoreUnavailable}}
}

// Users implements [store.Store].
func (s *fakeStore) Users() store.UserRepository { return s.users }

// Tx implements [store.Store].
func (s *fakeStore) Tx(ctx context.Context, fn func(ctx context.Context, tx store.Store) error) error {
	return fn(ctx, s)
}

// fakeUsers implements [store.UserRepository] over a single user.
type fakeUsers struct {
	user model.User
	err  error
}

// Create implements [store.UserRepository]. Users are issued from the command
// line, so no handler creates one and this is only here to satisfy the
// interface.
func (u fakeUsers) Create(_ context.Context, _ model.User) (model.User, error) {
	return model.User{}, errors.New("fakeUsers: Create is not implemented")
}

// ByEmail implements [store.UserRepository]. The comparison is exact because
// every caller has normalised the address first, which is what the real
// column's NOCASE collation is there to survive.
func (u fakeUsers) ByEmail(_ context.Context, email string) (model.User, error) {
	return u.match(email == u.user.Email)
}

// ByAPIKey implements [store.UserRepository].
func (u fakeUsers) ByAPIKey(_ context.Context, apiKey string) (model.User, error) {
	return u.match(apiKey == u.user.APIKey)
}

// match answers a lookup: the configured failure first, then the user or
// [store.ErrNotFound].
func (u fakeUsers) match(found bool) (model.User, error) {
	switch {
	case u.err != nil:
		return model.User{}, u.err
	case found:
		return u.user, nil
	default:
		return model.User{}, store.ErrNotFound
	}
}
