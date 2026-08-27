package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tempDatabase points the configuration at a database in a directory that goes
// away with the test, and returns the environment to run with and the path it
// names.
func tempDatabase(t *testing.T) (func(string) string, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "travelmap.db")

	return func(name string) string {
		if name == "TRAVELMAP_DATABASE" {
			return path
		}

		return ""
	}, path
}

// TestMigrateCommand verifies travelmap migrate from outside: the first run
// creates the schema, and a second one changes nothing and says so instead of
// failing.
func TestMigrateCommand(t *testing.T) {
	t.Parallel()

	env, path := tempDatabase(t)

	var first bytes.Buffer
	if err := run([]string{"migrate"}, env, noStdin(), &first, &first); err != nil {
		t.Fatalf("the first migrate returned %v", err)
	}

	if got := first.String(); !strings.Contains(got, "schema brought up to date") {
		t.Errorf("the first migrate printed %q, want it to report that it migrated", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the database was not created: %v", err)
	}

	var second bytes.Buffer
	if err := run([]string{"migrate"}, env, noStdin(), &second, &second); err != nil {
		t.Fatalf("the second migrate returned %v", err)
	}

	if got := second.String(); !strings.Contains(got, "schema already up to date") {
		t.Errorf("the second migrate printed %q, want it to report an up-to-date schema", got)
	}
}
