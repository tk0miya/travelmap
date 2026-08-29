package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	// The driver registers itself as "sqlite" and is only ever reached through
	// database/sql, which is why it is imported for its side effect alone.
	_ "modernc.org/sqlite"

	"github.com/tk0miya/travelmap/internal/store"
)

// busyTimeout is how long a statement waits for another writer to finish before
// giving up with SQLITE_BUSY, in milliseconds.
//
// One process would not need it: the pool below holds a single connection, so
// this server's own writes are already serialised. Two do — `travelmap user
// create` or `travelmap recalculate` run against the same file while the server
// is serving — and without a timeout the CLI would fail outright the moment it
// overlapped with an ingest.
const busyTimeout = 5000

// querier is the part of *sql.DB and *sql.Tx the repositories use, so that one
// repository implementation serves both a plain call and a call inside
// [DB.Tx].
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// DB is the SQLite implementation of [store.Store].
type DB struct {
	// pool is the connection pool. A transaction-scoped DB carries it too, so
	// that a nested [DB.Tx] can build the next scoped DB from it.
	pool *sql.DB

	// q is what statements run on: the pool, or the transaction when this DB
	// is the one handed to a [DB.Tx] callback.
	q querier
}

// The interface this package exists to satisfy. Asserted here so that a
// signature drifting from it is a compile error in this package rather than in
// cmd/travelmap, which is the only place that names the concrete type.
var _ store.Store = (*DB)(nil)

// Open opens the SQLite database at path, creating the file if it is not there,
// and verifies that it can be talked to. It does not apply the migrations;
// [DB.Migrate] does, so that starting the server and changing the schema stay
// separate operations.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("sqlite: no database path")
	}

	pool, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening %s: %w", path, err)
	}

	// A single connection, deliberately. SQLite allows one writer at a time, so
	// a pool of several would turn concurrent writes into SQLITE_BUSY errors
	// that every caller would have to be ready to retry; with one, they queue
	// in database/sql instead and each runs to completion. The cost is that a
	// read waits behind a write in progress, which for a personal server
	// holding one user's location history is not a load worth optimising for.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)

	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("sqlite: connecting to %s: %w", path, err)
	}

	return &DB{pool: pool, q: pool}, nil
}

// Close releases the connection.
func (db *DB) Close() error {
	if err := db.pool.Close(); err != nil {
		return fmt.Errorf("sqlite: closing: %w", err)
	}

	return nil
}

// Users implements [store.Store].
func (db *DB) Users() store.UserRepository {
	return userRepository{q: db.q}
}

// Points implements [store.Store].
func (db *DB) Points() store.PointRepository {
	return pointRepository{q: db.q}
}

// DailyStats implements [store.Store].
func (db *DB) DailyStats() store.DailyStatsRepository {
	return dailyStatsRepository{q: db.q}
}

// Checkins implements [store.Store].
func (db *DB) Checkins() store.CheckinRepository {
	return checkinRepository{q: db.q}
}

// FoursquareAccounts implements [store.Store].
func (db *DB) FoursquareAccounts() store.FoursquareAccountRepository {
	return foursquareAccountRepository{q: db.q}
}

// Sessions implements [store.Store].
func (db *DB) Sessions() store.SessionRepository {
	return sessionRepository{q: db.q}
}

// Tx implements [store.Store].
func (db *DB) Tx(ctx context.Context, fn func(ctx context.Context, tx store.Store) error) error {
	// A nested call joins the transaction already running rather than opening a
	// second one: with a single connection, waiting for the outer transaction
	// to commit would be waiting for ourselves. Joining means an error from the
	// inner function rolls the whole thing back, which is what a caller
	// composing two operations into one unit wants anyway.
	if _, ok := db.q.(*sql.Tx); ok {
		return fn(ctx, db)
	}

	tx, err := db.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: beginning a transaction: %w", err)
	}

	// Rollback after a commit returns sql.ErrTxDone and changes nothing, so the
	// deferred call is only doing work on the paths that need it — including a
	// panic, which would otherwise leave the transaction open and, with one
	// connection, the database unusable.
	defer func() { _ = tx.Rollback() }()

	if err := fn(ctx, &DB{pool: db.pool, q: tx}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: committing: %w", err)
	}

	return nil
}

// dsn builds the connection string for the database at path.
//
// The pragmas are set through the DSN rather than executed after opening
// because database/sql may drop a connection and dial a new one at any time:
// only the driver, which applies these to every connection it makes, can keep
// the per-connection ones in force.
func dsn(path string) string {
	params := url.Values{
		// WAL lets a reader run while a write is in progress and is a property
		// of the file, so it is set once and then simply confirmed.
		"_journal_mode": {"WAL"},

		// NORMAL is the mode WAL is designed around: a commit no longer waits
		// for the disk to confirm the write, and the guarantee lost with it is
		// only that the last commits survive losing power — the database
		// itself cannot be corrupted by NORMAL under WAL.
		"_synchronous": {"NORMAL"},

		// SQLite defaults foreign keys off for backwards compatibility, which
		// would make every REFERENCES clause in the schema decoration.
		"_foreign_keys": {"on"},

		"_busy_timeout": {fmt.Sprint(busyTimeout)},

		// Take the write lock when the transaction begins instead of when its
		// first write arrives. A deferred transaction that starts by reading
		// and then writes can find the lock taken and fail immediately, which
		// busy_timeout cannot help with because rolling back and retrying is
		// the only recovery; an immediate transaction waits out the timeout
		// instead.
		"_txlock": {"immediate"},
	}

	// The driver takes a URI, so a path is a URI path: anything that would
	// otherwise start the query string has to survive as part of the file name.
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + params.Encode()
}
