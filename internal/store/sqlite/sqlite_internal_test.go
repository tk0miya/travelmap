package sqlite

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// The tests in this package are internal rather than in an external sqlite_test
// package because the SQL is what is being tested: the pragmas below are read
// off the connection the DSN configured, which nothing outside the package can
// reach.

// errRollback is what a transaction test fails with, so that the rollback it
// causes is unmistakably the one the test asked for.
var errRollback = errors.New("rollback, please")

// newTestDB opens a migrated database in a directory that goes away with the
// test. It is a real file rather than ":memory:" so that the tests exercise the
// journal mode, the locking and the file the server actually runs on.
func newTestDB(t *testing.T) *DB {
	t.Helper()

	db := openTestDB(t)

	if _, err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("Migrate returned %v", err)
	}

	return db
}

// openTestDB is [newTestDB] without the migrations, for the tests that are
// about migrating.
func openTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "travelmap.db"))
	if err != nil {
		t.Fatalf("Open returned %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close returned %v", err)
		}
	})

	return db
}

// testUser is a user with the fields the store requires filled in. The digest
// and the key are opaque strings to the store, which is why the test does not
// go through internal/auth to make them — it sits above the store, and a test
// importing it would put a cycle in the import graph the layering forbids.
func testUser(email string) model.User {
	return model.User{
		Email:        email,
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "apikey-" + email,
	}
}

// TestOpenAppliesThePragmas pins the settings that are invisible in any query: a
// database opened without them still works, and would only be found out to be
// wrong by a foreign key that never held or a write lost to a crash.
func TestOpenAppliesThePragmas(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	tests := map[string]struct {
		pragma string
		want   string
	}{
		"the journal is a write-ahead log": {pragma: "journal_mode", want: "wal"},
		"foreign keys are enforced":        {pragma: "foreign_keys", want: "1"},
		"commits do not wait for the disk": {pragma: "synchronous", want: "1"},
		"a busy database is waited for":    {pragma: "busy_timeout", want: "5000"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Deliberately not parallel: these read the state of the one
			// connection the pool holds.
			var got string
			if err := db.q.QueryRowContext(t.Context(), "PRAGMA "+tt.pragma).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s returned %v", tt.pragma, err)
			}

			if !strings.EqualFold(got, tt.want) {
				t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
}

func TestOpenRejectsAnEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := Open(t.Context(), ""); err == nil {
		t.Fatal("Open returned nil for an empty path")
	}
}

// TestDSN pins how a path becomes a URI, because a mistake there does not fail:
// SQLite creates whatever file name it is handed, so the server would come up
// happily on a database nobody meant.
func TestDSN(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"a relative path":        "travelmap.db",
		"an absolute path":       "/var/lib/travelmap/travelmap.db",
		"a path holding a space": "/tmp/my db.db",
		// Unescaped, everything from the ? on would be read as DSN parameters
		// and the file name would be truncated.
		"a path holding a question mark": "/tmp/what?.db",
	}

	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := dsn(path)

			// Split the way SQLite does — the file name is everything between
			// the scheme and the first unescaped ? — rather than with url.Parse,
			// which reports a relative path as an opaque URI and would let an
			// unescaped separator through unnoticed.
			name, params, found := strings.Cut(strings.TrimPrefix(got, "file:"), "?")
			if !strings.HasPrefix(got, "file:") || !found {
				t.Fatalf("dsn = %q, want file:<path>?<params>", got)
			}

			decoded, err := url.PathUnescape(name)
			if err != nil {
				t.Fatalf("the file name in %q does not unescape: %v", got, err)
			}

			if decoded != path {
				t.Errorf("the file name in %q is %q, want %q", got, decoded, path)
			}

			query, err := url.ParseQuery(params)
			if err != nil {
				t.Fatalf("the parameters in %q do not parse: %v", got, err)
			}

			// The driver validates the value of a key it knows and ignores a
			// key it does not, so a misspelled _txlock is not an error — it is
			// every transaction silently going back to BEGIN DEFERRED. No
			// PRAGMA reports the setting back, which is why it is pinned here
			// rather than in TestOpenAppliesThePragmas.
			if got := query.Get("_txlock"); got != "immediate" {
				t.Errorf("_txlock = %q, want immediate", got)
			}
		})
	}
}

// TestTxCommits is the plain case: what the callback wrote is there afterwards.
func TestTxCommits(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if err := db.Tx(t.Context(), func(ctx context.Context, tx store.Store) error {
		_, err := tx.Users().Create(ctx, testUser("committed@example.com"))

		return err
	}); err != nil {
		t.Fatalf("Tx returned %v", err)
	}

	if _, err := db.Users().ByEmail(t.Context(), "committed@example.com"); err != nil {
		t.Errorf("after the commit the lookup returned %v", err)
	}
}

// TestTxRollsBackOnError is the reason ingest may write points and daily_stats
// through one transaction: a failure part-way leaves neither behind.
func TestTxRollsBackOnError(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	err := db.Tx(t.Context(), func(ctx context.Context, tx store.Store) error {
		if _, err := tx.Users().Create(ctx, testUser("rolled-back@example.com")); err != nil {
			return err
		}

		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("Tx returned %v, want %v", err, errRollback)
	}

	if _, err := db.Users().ByEmail(t.Context(), "rolled-back@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after the rollback the lookup returned %v, want ErrNotFound", err)
	}
}

// TestTxNests covers a caller inside a transaction calling one that opens its
// own. With a single connection, opening a second transaction there would wait
// for a lock this goroutine is holding, so the nested call has to join instead —
// and the test would hang rather than fail if it ever stopped doing so.
func TestTxNests(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if err := db.Tx(t.Context(), func(ctx context.Context, tx store.Store) error {
		return tx.Tx(ctx, func(ctx context.Context, inner store.Store) error {
			_, err := inner.Users().Create(ctx, testUser("nested@example.com"))

			return err
		})
	}); err != nil {
		t.Fatalf("Tx returned %v", err)
	}

	if _, err := db.Users().ByEmail(t.Context(), "nested@example.com"); err != nil {
		t.Errorf("after the nested commit the lookup returned %v", err)
	}
}

// TestTxRollsBackFromANestedFailure pins that joining the outer transaction does
// not mean an inner failure is contained: the whole unit is undone, which is
// what a caller composing two operations expects.
func TestTxRollsBackFromANestedFailure(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	err := db.Tx(t.Context(), func(ctx context.Context, tx store.Store) error {
		if _, err := tx.Users().Create(ctx, testUser("outer@example.com")); err != nil {
			return err
		}

		return tx.Tx(ctx, func(context.Context, store.Store) error { return errRollback })
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("Tx returned %v, want %v", err, errRollback)
	}

	if _, err := db.Users().ByEmail(t.Context(), "outer@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after the rollback the lookup returned %v, want ErrNotFound", err)
	}
}
