package storetest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	// The seeding below opens a connection of its own by driver name, so this
	// package names the driver rather than leaning on the import that
	// internal/store/sqlite makes for its own use.
	_ "modernc.org/sqlite"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/sqlite"
)

// New returns a store over a real, migrated SQLite database holding users.
//
// A user is stored exactly as given, its ID and timestamps included, so that a
// caller can pin what a golden file shows a client being handed back.
func New(t *testing.T, users ...model.User) store.Store {
	t.Helper()

	return open(t, prepare(t, users))
}

// Unavailable returns a store whose database has been closed, so that every
// call through it fails. It is how a test reaches the paths that answer 500.
func Unavailable(t *testing.T) store.Store {
	t.Helper()

	db := open(t, prepare(t, nil))

	if err := db.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}

	return db
}

// UnavailablePoints returns a store holding users whose points table has been
// dropped, so that a lookup succeeds and a point write fails. Authenticating
// and writing are two different calls, and a test about the write failing needs
// the lookup before it to still work.
func UnavailablePoints(t *testing.T, users ...model.User) store.Store {
	t.Helper()

	path := prepare(t, users)

	// Dropped before the store is opened, so that the store never sees the
	// schema it is about to lose.
	exec(t, path, `DROP TABLE points`)

	return open(t, path)
}

// UnavailableDailyStats returns a store holding users whose daily_stats
// table has been dropped, so that authenticating and reading points still
// work but a daily_stats read fails.
func UnavailableDailyStats(t *testing.T, users ...model.User) store.Store {
	t.Helper()

	path := prepare(t, users)

	// Dropped before the store is opened, for the same reason as
	// UnavailablePoints.
	exec(t, path, `DROP TABLE daily_stats`)

	return open(t, path)
}

// NewWithPoints is [New], plus points seeded directly with their own ID,
// CreatedAt and UpdatedAt — not [store.PointRepository.Create]'s, which
// stamps the timestamps with the current time — so a caller can pin what a
// golden file shows a client being handed back.
func NewWithPoints(t *testing.T, users []model.User, points []model.Point) store.Store {
	t.Helper()

	path := prepare(t, users)
	seedPoints(t, path, points)

	return open(t, path)
}

// prepare copies the migrated template into a directory of the test's own,
// seeds it with users and returns the path to it.
func prepare(t *testing.T, users []model.User) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "travelmap.db")

	schema, err := template()
	if err != nil {
		t.Fatalf("building the template database: %v", err)
	}

	if err := os.WriteFile(path, schema, 0o600); err != nil {
		t.Fatalf("writing the test database: %v", err)
	}

	seed(t, path, users)

	return path
}

// open returns a store over the database at path.
func open(t *testing.T, path string) *sqlite.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}

	// Unavailable has closed it already, so a second close here is ignored
	// rather than reported.
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// template is the migrated, empty database every test starts from, built once
// per test binary.
//
// The migration is what costs, and copying its result is what a test pays
// instead: the driver is pure Go, so under -race — which `make test` always
// passes — the race detector instruments the translated SQLite along with
// everything else. Migrating costs about 65 ms per database under -race;
// copying the migrated file instead brings a test's own database to about
// 12 ms.
var template = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "storetest")
	if err != nil {
		return nil, err
	}

	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "template.db")

	db, err := sqlite.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Migrate(context.Background()); err != nil {
		_ = db.Close()

		return nil, err
	}

	// Closing checkpoints the write-ahead log into the file itself, which is
	// what makes the bytes below a complete database rather than one missing
	// everything the migrations just wrote.
	if err := db.Close(); err != nil {
		return nil, err
	}

	return os.ReadFile(path)
})

// seed writes users directly, rather than through the repository, because
// UserRepository.Create sets the timestamps to the current time and a caller
// needs the ones it asked for.
func seed(t *testing.T, path string, users []model.User) {
	t.Helper()

	if len(users) == 0 {
		return
	}

	for _, user := range users {
		exec(t, path,
			`INSERT INTO users (id, email, password_hash, api_key, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			user.ID, user.Email, user.PasswordHash, user.APIKey,
			user.CreatedAt.Unix(), user.UpdatedAt.Unix(),
		)
	}
}

// seedPoints writes points directly, rather than through the repository, for
// the same reason [seed] does for users: PointRepository.Create sets
// created_at/updated_at to the current time, and a caller pinning a golden
// file needs the ones it asked for.
func seedPoints(t *testing.T, path string, points []model.Point) {
	t.Helper()

	if len(points) == 0 {
		return
	}

	for _, p := range points {
		exec(t, path,
			`INSERT INTO points (
				id, user_id, timestamp, latitude, longitude, altitude, velocity,
				accuracy, vertical_accuracy, course, course_accuracy,
				battery_status, battery, ssid, tracker_id, created_at, updated_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.UserID, p.Timestamp.Unix(), p.Latitude, p.Longitude, p.Altitude, p.Velocity,
			p.Accuracy, p.VerticalAccuracy, p.Course, p.CourseAccuracy,
			p.BatteryStatus, p.Battery, p.SSID, p.TrackerID,
			p.CreatedAt.Unix(), p.UpdatedAt.Unix(),
		)
	}
}

// exec runs one statement against the database at path on a connection of its
// own, which is how this package writes what the store's own interfaces do not
// expose.
func exec(t *testing.T, path string, query string, args ...any) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("closing the seeding connection: %v", err)
		}
	}()

	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("running %q: %v", query, err)
	}
}
