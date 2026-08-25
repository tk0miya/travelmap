package store

import (
	"context"
	"errors"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
)

// The errors every repository reports, so that a caller can tell the three
// outcomes apart without knowing which backend produced them. An
// implementation wraps these rather than returning a driver error, and callers
// compare with [errors.Is].
var (
	// ErrNotFound is returned by a lookup that matched nothing. It is not the
	// zero value plus a nil error, because "no such user" and "a user with no
	// fields set" are answers a handler has to distinguish.
	ErrNotFound = errors.New("not found")

	// ErrConflict is returned when a write would duplicate a value that has to
	// be unique, such as a second user with an email already taken.
	ErrConflict = errors.New("already exists")
)

// Store is the entry point to the repositories.
//
// Repositories are reached through it rather than being handed out
// individually so that [Store.Tx] can offer all of them inside one
// transaction: from Step 9 on, a point insert and the daily_stats rebuild it
// triggers have to commit together, and that is only expressible if both
// repositories can be scoped to the same transaction.
type Store interface {
	// Users returns the user repository.
	Users() UserRepository

	// Points returns the point repository.
	Points() PointRepository

	// DailyStats returns the daily_stats repository.
	DailyStats() DailyStatsRepository

	// Tx runs fn inside a transaction, committing when it returns nil and
	// rolling back when it returns an error, which Tx then returns.
	//
	// The Store passed to fn is scoped to the transaction: every repository
	// reached through it writes and reads inside it. The outer Store must not
	// be used from fn — a backend is free to hold a single connection, in
	// which case a query issued outside the transaction cannot run until it
	// has finished.
	Tx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
}

// UserRepository stores and looks up users.
//
// The lookups are the two the API authenticates with: by API key for every
// request from a device, and by email for POST /api/v1/auth/login.
type UserRepository interface {
	// Create stores user and returns it as stored, with ID, CreatedAt and
	// UpdatedAt filled in. It returns [ErrConflict] if the email or the API key
	// is already taken.
	Create(ctx context.Context, user model.User) (model.User, error)

	// ByEmail finds a user by email, case-insensitively, and returns
	// [ErrNotFound] if there is none.
	ByEmail(ctx context.Context, email string) (model.User, error)

	// ByAPIKey finds a user by API key and returns [ErrNotFound] if there is
	// none. The comparison is exact: an API key is a credential, not something
	// a human types.
	ByAPIKey(ctx context.Context, apiKey string) (model.User, error)
}

// PointRepository stores the points ingested from a device.
//
// Step 7 predates internal/ingest, which from Step 9 on is the only caller
// allowed to reach this: every mutation of a point also has to rebuild the
// affected days of daily_stats, and a second caller writing directly would be
// guaranteed to eventually forget.
type PointRepository interface {
	// Create inserts points, silently dropping any whose (user_id, timestamp)
	// pair is already stored — the same pair the caller cannot query without,
	// per "Deduplication" under "points" in docs/database.md — and reports how
	// many rows were actually inserted.
	Create(ctx context.Context, points []model.Point) (int, error)

	// UserIDs returns the distinct user_id of every user with at least one
	// point, for a full recalculation to iterate over.
	UserIDs(ctx context.Context) ([]int64, error)

	// Timestamps returns every timestamp recorded for user, in ascending
	// order. A full recalculation groups these into calendar days itself,
	// because which day a timestamp falls on depends on TRAVELMAP_TIMEZONE,
	// which this package does not know.
	Timestamps(ctx context.Context, userID int64) ([]time.Time, error)
}

// DailyStatsRepository stores the daily_stats table: one precomputed per-day
// aggregate per user, rebuilt from points rather than adjusted arithmetically.
// See "daily_stats" in docs/database.md.
type DailyStatsRepository interface {
	// Rebuild recomputes user's row for day from scratch and writes it, or —
	// if the day now has no points — deletes it. day must be local midnight
	// in the timezone day boundaries are cut on, i.e. what
	// PointRepository.Timestamps' caller grouped a timestamp into.
	//
	// The rebuild input is that day's points plus the point immediately
	// preceding it, which may belong to an earlier day: the day's first
	// point measures its segment against that point. A segment whose gap
	// exceeds trackBreak is excluded entirely from km, matching
	// TRAVELMAP_TRACK_BREAK_MINUTES.
	Rebuild(ctx context.Context, userID int64, day time.Time, trackBreak time.Duration) error

	// DeleteAll removes every row. It is the first step of a full
	// recalculation: changing TRAVELMAP_TIMEZONE reshuffles which days
	// exist, and rebuilding only the days the new grouping produces would
	// leave rows from the old grouping behind forever.
	DeleteAll(ctx context.Context) error

	// Get returns user's row for day, and [ErrNotFound] if that day has no
	// points at all — the state Rebuild represents by deleting the row
	// rather than storing one at zero.
	Get(ctx context.Context, userID int64, day time.Time) (model.DailyStat, error)
}
