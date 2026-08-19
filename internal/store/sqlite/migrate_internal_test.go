package sqlite

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pressly/goose/v3"
)

// TestMigrate covers a fresh database: the migrations are applied, and the
// database reports itself migrated afterwards.
func TestMigrate(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	migrated, err := db.Migrated(t.Context())
	if err != nil {
		t.Fatalf("Migrated returned %v", err)
	}

	if migrated {
		t.Error("a database no migration has run against reports itself migrated")
	}

	changed, err := db.Migrate(t.Context())
	if err != nil {
		t.Fatalf("Migrate returned %v", err)
	}

	if !changed {
		t.Error("Migrate on a fresh database reported nothing to apply")
	}

	if migrated, err = db.Migrated(t.Context()); err != nil || !migrated {
		t.Errorf("after Migrate, Migrated = (%v, %v), want (true, nil)", migrated, err)
	}
}

// TestMigratedRejectsAVersionTableWithoutTheSchema covers a migration that did
// not finish. goose creates its version table, with a row for version 0, in a
// transaction of its own before the first migration runs in another, so a run
// interrupted in between leaves exactly this state behind. Answering "migrated"
// to it would send `travelmap user create` past the check it exists for, to fail
// against a table that was never created.
func TestMigratedRejectsAVersionTableWithoutTheSchema(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	// The table as goose creates it, which is the point: what is being tested is
	// how this package reads goose's bookkeeping, not how goose writes it.
	if _, err := db.q.ExecContext(t.Context(), `CREATE TABLE `+goose.DefaultTablename+` (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("creating the version table returned %v", err)
	}

	if _, err := db.q.ExecContext(t.Context(),
		`INSERT INTO `+goose.DefaultTablename+` (version_id, is_applied) VALUES (0, true)`); err != nil {
		t.Fatalf("inserting the zero version returned %v", err)
	}

	migrated, err := db.Migrated(t.Context())
	if err != nil {
		t.Fatalf("Migrated returned %v", err)
	}

	if migrated {
		t.Error("a database with only the version table reports itself migrated")
	}
}

// TestMigrateIsANoOpWhenUpToDate is the completion condition of Step 4: the
// command can be run again — after an upgrade that added nothing, or by an
// operator who is not sure whether they ran it — without touching the data.
func TestMigrateIsANoOpWhenUpToDate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("kept@example.com"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	changed, err := db.Migrate(t.Context())
	if err != nil {
		t.Fatalf("the second Migrate returned %v", err)
	}

	if changed {
		t.Error("the second Migrate reported that it applied something")
	}

	got, err := db.Users().ByEmail(t.Context(), user.Email)
	if err != nil {
		t.Fatalf("after the second Migrate the lookup returned %v", err)
	}

	if diff := cmp.Diff(user, got); diff != "" {
		t.Errorf("the user differs after the second Migrate (-want +got):\n%s", diff)
	}
}
