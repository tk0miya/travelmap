package store

import (
	"context"
	"errors"

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
	// per "Deduplication" under "Data Model" in TODO.md — and reports how many
	// rows were actually inserted.
	Create(ctx context.Context, points []model.Point) (int, error)
}
