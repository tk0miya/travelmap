package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// withFoursquareUser returns the environment of a migrated database holding
// one user, created with `user create` — the account `foursquare connect`
// expects to find.
func withFoursquareUser(t *testing.T) (func(string) string, string) {
	t.Helper()

	env := migrated(t)
	const email = "alice@example.com"

	args := []string{"user", "create", "--email", email, "--password", password}
	if err := run(args, env, noStdin(), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("user create returned %v", err)
	}

	return env, email
}

// TestFoursquareConnectCommand is Step 17's other half: linking an account
// and printing something an operator can confirm.
func TestFoursquareConnectCommand(t *testing.T) {
	t.Parallel()

	env, email := withFoursquareUser(t)

	var stdout, stderr bytes.Buffer

	args := []string{"foursquare", "connect", "--email", email, "--foursquare-user-id", "1709193"}
	if err := run(args, env, strings.NewReader("the-access-token\n"), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare connect returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "linked user 1 (alice@example.com) to Foursquare user 1709193") {
		t.Errorf("foursquare connect printed %q, want the link summary", got)
	}
}

// TestFoursquareConnectRequiresAnExistingUser covers the mistake the error
// message exists for: there is no account to link yet.
func TestFoursquareConnectRequiresAnExistingUser(t *testing.T) {
	t.Parallel()

	env := migrated(t)

	var stdout, stderr bytes.Buffer

	args := []string{"foursquare", "connect", "--email", "nobody@example.com", "--foursquare-user-id", "1709193"}

	err := run(args, env, strings.NewReader("the-access-token\n"), &stdout, &stderr)
	if err == nil {
		t.Fatal("foursquare connect returned nil for an email with no user")
	}

	if !strings.Contains(err.Error(), `run "travelmap user create" first`) {
		t.Errorf("foursquare connect failed with %v, want it to name the command to run", err)
	}
}

// TestFoursquareConnectRejectsADuplicate pins that a second link is refused
// rather than silently overwriting the first — a travelmap user has at most
// one Swarm account, and a Swarm account links to at most one travelmap user.
func TestFoursquareConnectRejectsADuplicate(t *testing.T) {
	t.Parallel()

	env, email := withFoursquareUser(t)

	args := []string{"foursquare", "connect", "--email", email, "--foursquare-user-id", "1709193"}

	var first bytes.Buffer
	if err := run(args, env, strings.NewReader("the-access-token\n"), &first, &first); err != nil {
		t.Fatalf("the first foursquare connect returned %v", err)
	}

	var second bytes.Buffer

	err := run(args, env, strings.NewReader("another-access-token\n"), &second, &second)
	if err == nil {
		t.Fatal("the second foursquare connect returned nil for an account already linked")
	}

	if !strings.Contains(err.Error(), "already linked") {
		t.Errorf("the second foursquare connect failed with %v, want it to say the account is already linked", err)
	}
}

// TestFoursquareConnectRejectsBadInput covers what has to fail before a
// database is touched at all, plus the token that never arrives.
func TestFoursquareConnectRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args  []string
		stdin string
	}{
		"no email": {
			args: []string{"foursquare", "connect", "--foursquare-user-id", "1709193"},
		},
		"an email that is not an address": {
			args: []string{"foursquare", "connect", "--email", "alice", "--foursquare-user-id", "1709193"},
		},
		"no foursquare user id": {
			args: []string{"foursquare", "connect", "--email", "alice@example.com"},
		},
		"a positional argument": {
			args: []string{"foursquare", "connect", "alice@example.com"},
		},
		"no access token anywhere": {
			args: []string{"foursquare", "connect", "--email", "alice@example.com", "--foursquare-user-id", "1709193"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env := migrated(t)

			var stdout, stderr bytes.Buffer

			err := run(tt.args, env, strings.NewReader(tt.stdin), &stdout, &stderr)
			if !errors.Is(err, errUsage) {
				t.Errorf("foursquare connect failed with %v, want errUsage", err)
			}
		})
	}
}
