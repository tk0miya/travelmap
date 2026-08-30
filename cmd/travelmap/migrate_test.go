package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requiredTOML is every setting [config.Load] treats as mandatory, folded
// into every config file this package's tests write so a test only has to
// name the settings it actually cares about.
const requiredTOML = `
[server]
base_url = "https://travelmap.example"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`

// writeConfigWithDB writes a TOML config file in a fresh temporary directory
// pointing the database at dbPath, plus whatever extra TOML the test needs,
// and returns the config file's path. extra must not itself declare
// [server] or [foursquare] — see rewriteConfigWithDB for a test that needs
// to add to those.
func writeConfigWithDB(t *testing.T, dbPath, extra string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "travelmap.toml")
	if err := rewriteConfigWithDB(configPath, dbPath, extra); err != nil {
		t.Fatalf("writing %s: %v", configPath, err)
	}

	return configPath
}

// tempDatabase points the configuration at a database in a directory that
// goes away with the test, and returns the config file's path to run with
// and the database path it names.
func tempDatabase(t *testing.T) (string, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "travelmap.db")

	return writeConfigWithDB(t, dbPath, ""), dbPath
}

// rewriteConfigWithDB writes the whole config file at path from scratch:
// dbPath, the required settings, and extra. Used both by writeConfigWithDB
// for a fresh file and by a test that needs to change a [foursquare] setting
// after commands that do not care about it (migrate, seedFoursquareAccount)
// have already run against the file — TOML refuses a table declared twice,
// so that rewrites the file rather than appending to it.
func rewriteConfigWithDB(configPath, dbPath, extra string) error {
	content := fmt.Sprintf("[database]\npath = %q\n%s\n%s", dbPath, requiredTOML, extra)

	return os.WriteFile(configPath, []byte(content), 0o600)
}

// migrated returns the config file's path and the database path for a
// database the schema has already been applied to, which is the state
// every other command's own tests start from.
func migrated(t *testing.T) (string, string) {
	t.Helper()

	configPath, dbPath := tempDatabase(t)

	var out bytes.Buffer
	if err := run(withConfig(configPath, "migrate"), noStdin(), &out, &out); err != nil {
		t.Fatalf("migrate returned %v", err)
	}

	return configPath, dbPath
}

// TestMigrateCommand verifies travelmap migrate from outside: the first run
// creates the schema, and a second one changes nothing and says so instead of
// failing.
func TestMigrateCommand(t *testing.T) {
	t.Parallel()

	configPath, path := tempDatabase(t)

	var first bytes.Buffer
	if err := run(withConfig(configPath, "migrate"), noStdin(), &first, &first); err != nil {
		t.Fatalf("the first migrate returned %v", err)
	}

	if got := first.String(); !strings.Contains(got, "schema brought up to date") {
		t.Errorf("the first migrate printed %q, want it to report that it migrated", got)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("the database was not created: %v", err)
	}

	var second bytes.Buffer
	if err := run(withConfig(configPath, "migrate"), noStdin(), &second, &second); err != nil {
		t.Fatalf("the second migrate returned %v", err)
	}

	if got := second.String(); !strings.Contains(got, "schema already up to date") {
		t.Errorf("the second migrate printed %q, want it to report an up-to-date schema", got)
	}
}
