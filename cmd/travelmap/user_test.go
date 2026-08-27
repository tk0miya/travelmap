package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// password is one long enough for internal/auth to accept.
const password = "correct horse battery"

// migrated returns the environment of a database the schema has been applied to,
// which is the state every `user create` below starts from.
func migrated(t *testing.T) func(string) string {
	t.Helper()

	env, _ := tempDatabase(t)

	var out bytes.Buffer
	if err := run([]string{"migrate"}, env, noStdin(), &out, &out); err != nil {
		t.Fatalf("migrate returned %v", err)
	}

	return env
}

// TestUserCreateCommand verifies travelmap user create: a user and an API
// key, printed where a script can read them.
func TestUserCreateCommand(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	args := []string{"user", "create", "--email", "Alice@Example.com", "--password", password}
	if err := run(args, migrated(t), noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("user create returned %v (stderr %q)", err, stderr.String())
	}

	// The address is stored lowercased, so it is reported that way too: the
	// output is what tells the operator which identity to log in with.
	if got := stdout.String(); !strings.Contains(got, "created user 1: alice@example.com") {
		t.Errorf("user create printed %q, want the id and the email", got)
	}

	// That the key is 64 hex characters is pinned in internal/auth; what matters
	// here is that it reaches stdout whole, on a line a setup script can read.
	if key := apiKeyFrom(t, stdout.String()); len(key) != 64 {
		t.Errorf("the API key printed was %q, want the whole key", key)
	}
}

// TestUserCreateReadsThePasswordFromStdin covers the way a script or a unit file
// passes a password without it showing up in the process list.
func TestUserCreateReadsThePasswordFromStdin(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	args := []string{"user", "create", "--email", "alice@example.com"}
	if err := run(args, migrated(t), strings.NewReader(password+"\n"), &stdout, &stderr); err != nil {
		t.Fatalf("user create returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "api_key:") {
		t.Errorf("user create printed %q, want the API key", got)
	}
}

// TestUserCreateRejectsADuplicate pins that the second attempt is refused rather
// than issuing a second account nobody asked for. The email differs in case,
// which is the way it happens by accident.
func TestUserCreateRejectsADuplicate(t *testing.T) {
	t.Parallel()

	env := migrated(t)

	var first bytes.Buffer
	if err := run([]string{"user", "create", "--email", "alice@example.com", "--password", password}, env, noStdin(), &first, &first); err != nil {
		t.Fatalf("the first user create returned %v", err)
	}

	var second bytes.Buffer

	err := run([]string{"user", "create", "--email", "Alice@example.com", "--password", password}, env, noStdin(), &second, &second)
	if err == nil {
		t.Fatal("the second user create returned nil for an email already taken")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the second user create failed with %v, want it to say the user exists", err)
	}
}

// TestUserCreateOnAnUnmigratedDatabase covers the mistake the error message
// exists for: TRAVELMAP_DATABASE pointing somewhere the schema is not, which
// SQLite answers by creating an empty file rather than by failing.
func TestUserCreateOnAnUnmigratedDatabase(t *testing.T) {
	t.Parallel()

	env, _ := tempDatabase(t)

	var out bytes.Buffer

	err := run([]string{"user", "create", "--email", "alice@example.com", "--password", password}, env, noStdin(), &out, &out)
	if err == nil {
		t.Fatal("user create on an unmigrated database returned nil")
	}

	if !strings.Contains(err.Error(), `run "travelmap migrate" first`) {
		t.Errorf("user create failed with %v, want it to name the command to run", err)
	}
}

// TestUserCreateRejectsBadInput covers what has to fail before a database is
// touched at all.
func TestUserCreateRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args      []string
		stdin     string
		wantUsage bool
	}{
		"no email": {
			args:      []string{"user", "create", "--password", password},
			wantUsage: true,
		},
		"an email that is not an address": {
			args:      []string{"user", "create", "--email", "alice", "--password", password},
			wantUsage: true,
		},
		"a positional argument": {
			args:      []string{"user", "create", "alice@example.com"},
			wantUsage: true,
		},
		// Not a usage error: the invocation was right and the password was not,
		// which a script tells apart by the exit status.
		"a password shorter than the minimum": {
			args: []string{"user", "create", "--email", "alice@example.com", "--password", "short"},
		},
		// A missing password is a misuse like a missing email, so it gets the
		// usage text and the exit status that goes with one.
		"no password anywhere": {
			args:      []string{"user", "create", "--email", "alice@example.com"},
			wantUsage: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, _ := tempDatabase(t)

			var stdout, stderr bytes.Buffer

			err := run(tt.args, env, strings.NewReader(tt.stdin), &stdout, &stderr)
			if err == nil {
				t.Fatalf("user create returned nil, want an error (stdout %q)", stdout.String())
			}

			if errors.Is(err, errUsage) != tt.wantUsage {
				t.Errorf("user create failed with %v, wantUsage = %v", err, tt.wantUsage)
			}
		})
	}
}

// apiKeyFrom takes the key out of what `user create` printed, the way a script
// setting up a device would.
func apiKeyFrom(t *testing.T, out string) string {
	t.Helper()

	const prefix = "api_key: "

	for line := range strings.SplitSeq(out, "\n") {
		if key, found := strings.CutPrefix(line, prefix); found {
			return key
		}
	}

	t.Fatalf("no %q line in %q", prefix, out)

	return ""
}
