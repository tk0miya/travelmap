package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// SQLite's extended result codes for a write that violated a uniqueness
// constraint. They are spelled out rather than taken from modernc.org/sqlite's
// generated `lib` package, which is machine-translated C and not part of the
// driver's supported surface.
const (
	sqliteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
	sqliteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
)

// userColumns is the select list every lookup shares, in the order
// [scanUser] reads them.
const userColumns = `id, email, password_hash, api_key, created_at, updated_at`

// userRepository implements [store.UserRepository].
type userRepository struct {
	q querier
}

// Create implements [store.UserRepository].
func (r userRepository) Create(ctx context.Context, user model.User) (model.User, error) {
	// Truncated to the second because that is the resolution the column has, so
	// the returned user is what a later lookup will find rather than something
	// that compares unequal to it.
	now := time.Now().UTC().Truncate(time.Second)

	result, err := r.q.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, api_key, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		user.Email, user.PasswordHash, user.APIKey, unixTime(now), unixTime(now),
	)
	if err != nil {
		return model.User{}, fmt.Errorf("sqlite: creating the user %q: %w", user.Email, translate(err))
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.User{}, fmt.Errorf("sqlite: creating the user %q: reading the id: %w", user.Email, err)
	}

	user.ID = id
	user.CreatedAt = now
	user.UpdatedAt = now

	return user, nil
}

// ByEmail implements [store.UserRepository].
func (r userRepository) ByEmail(ctx context.Context, email string) (model.User, error) {
	// The comparison is case-insensitive because the column is declared
	// COLLATE NOCASE, not because of anything written here.
	return r.one(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email)
}

// ByAPIKey implements [store.UserRepository].
func (r userRepository) ByAPIKey(ctx context.Context, apiKey string) (model.User, error) {
	return r.one(ctx, `SELECT `+userColumns+` FROM users WHERE api_key = ?`, apiKey)
}

// ByID implements [store.UserRepository].
func (r userRepository) ByID(ctx context.Context, id int64) (model.User, error) {
	return r.one(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
}

// one runs a lookup expected to match at most one user.
func (r userRepository) one(ctx context.Context, query string, args ...any) (model.User, error) {
	user, err := scanUser(r.q.QueryRowContext(ctx, query, args...))
	if err != nil {
		return model.User{}, fmt.Errorf("sqlite: looking up a user: %w", err)
	}

	return user, nil
}

// scanUser reads one row of [userColumns], reporting a missing row as
// [store.ErrNotFound].
func scanUser(row *sql.Row) (model.User, error) {
	var (
		user                 model.User
		createdAt, updatedAt unixTime
	)

	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.APIKey, &createdAt, &updatedAt); err != nil {
		return model.User{}, translate(err)
	}

	user.CreatedAt = time.Time(createdAt)
	user.UpdatedAt = time.Time(updatedAt)

	return user, nil
}

// translate turns the errors a caller is expected to handle into the ones
// [store] declares, so that nothing outside this package has to know a SQLite
// result code or import database/sql to recognise a missing row.
func translate(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}

	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			// The message names the index, and therefore the column, which is
			// the part a caller cannot work out for itself.
			return fmt.Errorf("%w: %s", store.ErrConflict, sqliteErr)
		}
	}

	return err
}
