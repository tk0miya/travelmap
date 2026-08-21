package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestServeRefusesAnUnmigratedDatabase covers the check that happens before the
// listener opens. Serving from a database with no schema would answer every
// request with an error about a missing table, and a typo in
// TRAVELMAP_DATABASE looks exactly like that — the history everyone expects is
// in the file nobody named.
//
// It is the whole of what can be tested through run: a serve that gets past
// this blocks until the process is signalled.
func TestServeRefusesAnUnmigratedDatabase(t *testing.T) {
	t.Parallel()

	env, path := tempDatabase(t)

	var stdout, stderr bytes.Buffer

	err := run([]string{"serve"}, env, noStdin(), &stdout, &stderr)
	if err == nil {
		t.Fatal("serve against an unmigrated database returned nil")
	}

	// The message names both the file it opened and the command to run, since
	// which database was touched is the thing an operator has to be sure of.
	for _, want := range []string{path, "travelmap migrate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("serve reported %q, want it to name %q", err, want)
		}
	}
}
