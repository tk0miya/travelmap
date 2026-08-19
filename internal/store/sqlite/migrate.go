package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// migrationsDir is where the .sql files live inside the embedded filesystem.
const migrationsDir = "migrations"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate brings the schema up to date, reporting whether it had anything to
// apply. An up-to-date database is not an error: it changes nothing and reports
// false, so the command that calls it can be run as often as anyone likes.
//
// Alongside an error the flag says whether anything was applied before the
// failure. Nothing acts on it — a failed run is followed by another one, which
// resumes from the last migration that committed.
func (db *DB) Migrate(ctx context.Context) (bool, error) {
	provider, err := db.migrationProvider()
	if err != nil {
		return false, err
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return len(results) > 0, fmt.Errorf("sqlite: migrating: %w", err)
	}

	return len(results) > 0, nil
}

// Migrated reports whether the schema has been applied to the database.
func (db *DB) Migrated(ctx context.Context) (bool, error) {
	// Not goose's own GetDBVersion, which creates its version table when the
	// table is missing: that would answer the question by changing it, leaving a
	// database behind on the very path the caller is about to reject.
	var tables int
	if err := db.q.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		goose.DefaultTablename,
	).Scan(&tables); err != nil {
		return false, fmt.Errorf("sqlite: checking whether the schema has been applied: %w", err)
	}

	if tables == 0 {
		return false, nil
	}

	// The table existing is not the schema existing. goose creates it, with a
	// row for version 0, in a transaction of its own before the first migration
	// runs in another — so a run interrupted in between leaves the table there
	// and no tables of ours, and answering "migrated" to that would send the
	// caller on to fail against a table that was never created.
	var applied int
	if err := db.q.QueryRowContext(ctx,
		`SELECT count(*) FROM `+goose.DefaultTablename+` WHERE version_id > 0`,
	).Scan(&applied); err != nil {
		return false, fmt.Errorf("sqlite: checking whether the schema has been applied: %w", err)
	}

	return applied > 0, nil
}

// migrationProvider builds the goose provider over the embedded migrations.
// goose reads them from the root of the filesystem it is given, so the
// subdirectory has to be peeled off first.
func (db *DB) migrationProvider() (*goose.Provider, error) {
	dir, err := fs.Sub(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("sqlite: reading the embedded migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, db.pool, dir)
	if err != nil {
		return nil, fmt.Errorf("sqlite: preparing the migrations: %w", err)
	}

	return provider, nil
}
