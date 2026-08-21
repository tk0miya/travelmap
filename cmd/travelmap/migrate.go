package main

import (
	"context"
	"fmt"
	"io"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/store/sqlite"
)

// migrate brings the configured database's schema up to date.
//
// It is a command of its own rather than something `serve` does on the way up:
// a schema change is the one operation that cannot be undone by restarting, so
// it happens when an operator asks for it, and its output says what it did.
func migrate(getenv func(string) string, stdout io.Writer) error {
	ctx := context.Background()

	db, path, err := openDatabase(ctx, getenv)
	if err != nil {
		return err
	}

	defer closeDatabase(db)

	changed, err := db.Migrate(ctx)
	if err != nil {
		return err
	}

	// The two cases are told apart, but neither names a version or a file: what
	// the operator asked is whether the database is at the current schema, and
	// the distinction only says whether this upgrade carried a schema change.
	if changed {
		fmt.Fprintf(stdout, "%s: schema brought up to date\n", path)
	} else {
		fmt.Fprintf(stdout, "%s: schema already up to date\n", path)
	}

	return nil
}

// openDatabase opens the database the environment points at, returning it and
// the path it came from — every message about it names the file, since which
// database was touched is the thing an operator most needs to be sure of.
func openDatabase(ctx context.Context, getenv func(string) string) (*sqlite.DB, string, error) {
	cfg, err := config.Load(getenv)
	if err != nil {
		return nil, "", err
	}

	db, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, "", err
	}

	return db, cfg.DatabasePath, nil
}

// requireMigrated reports an unmigrated database as the error that names the
// command to fix it.
//
// Opening a SQLite database creates the file, so every command that opens one
// can find itself pointed at an empty file it made itself. Migrating implicitly
// would turn that into a working server holding none of the operator's history,
// which is why it is refused here instead. See migrate for the whole argument.
func requireMigrated(ctx context.Context, db *sqlite.DB, path string) error {
	migrated, err := db.Migrated(ctx)
	if err != nil {
		return err
	}

	if !migrated {
		return fmt.Errorf("%s: no schema yet, run \"travelmap migrate\" first", path)
	}

	return nil
}

// closeDatabase releases the database at the end of a command.
//
// The error is dropped deliberately: by the time a command returns, everything
// it did has been committed, so a failure to close is not something the
// operator can act on and reporting it would only cast doubt on work that
// succeeded.
func closeDatabase(db *sqlite.DB) {
	_ = db.Close()
}
