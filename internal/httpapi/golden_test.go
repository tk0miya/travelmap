package httpapi_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// update rewrites the golden files instead of comparing against them:
// `go test ./internal/httpapi/... -update`.
var update = flag.Bool("update", false, "update the golden files")

// assertGolden compares a JSON response body against testdata/golden/<name>.
//
// Key names, their casing and their types are the compatibility contract with
// Dawarich clients, which is why they are pinned as a file rather than
// asserted field by field: a change to any of them shows up as a diff in the
// pull request that makes it, and gets reviewed as the compatibility change it
// is. The file holds the indented form of the body so that the diff is one
// line per field; indenting preserves the order the server wrote, so a moved
// or renamed key still shows.
func assertGolden(t *testing.T, name string, body []byte) {
	t.Helper()

	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("response body is not JSON: %v\nbody: %s", err, body)
	}

	// json.Indent keeps whatever whitespace the body ended with, and the
	// encoder ends every body with a newline. Normalising to exactly one
	// keeps the file from ending in a blank line that an editor would strip.
	want := append(bytes.TrimRight(indented.Bytes(), "\n"), '\n')

	path := filepath.Join("testdata", "golden", name)

	// Comparing against what was just written would check nothing, and two
	// parallel subtests may share a golden file: reading one back while the
	// other rewrites it fails on a half-written file.
	if *update {
		if err := os.WriteFile(path, want, 0o600); err != nil {
			t.Fatalf("writing the golden file: %v", err)
		}

		return
	}

	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the golden file (run `go test ./internal/httpapi/... -update` to create it): %v", err)
	}

	if diff := cmp.Diff(string(golden), string(want)); diff != "" {
		t.Errorf("response body differs from %s (-want +got):\n%s", path, diff)
	}
}
