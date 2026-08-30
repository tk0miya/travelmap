package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
)

// withFoursquareUser returns the environment of a migrated database holding
// one user, seeded directly — the account `foursquare connect` expects to
// find.
func withFoursquareUser(t *testing.T) (func(string) string, string) {
	t.Helper()

	env := migrated(t)
	const email = "alice@example.com"

	seedUser(t, env, email)

	return env, email
}

// seedUser inserts one user directly into env's already-migrated database —
// the same reasoning as recalculate_test.go's own seedPoints, since there is
// no CLI command left that creates a user: signing up needs a browser, which
// these tests don't drive. The email doubles as the API key; nothing here
// authenticates with it, so only uniqueness matters.
func seedUser(t *testing.T, env func(string) string, email string) {
	t.Helper()

	ctx := t.Context()

	db, _, err := openDatabase(ctx, env)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	if _, err := db.Users().Create(ctx, model.User{
		Email:        email,
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       email,
	}); err != nil {
		t.Fatalf("creating the user %s: %v", email, err)
	}
}

// TestFoursquareConnectCommand covers the command's own completion
// condition: linking an account and printing something an operator can
// confirm.
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

	if !strings.Contains(err.Error(), "sign up at /signup first") {
		t.Errorf("foursquare connect failed with %v, want it to name where to sign up", err)
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

// withFoursquareAPI returns the environment of a migrated database holding
// one user with a Swarm account already linked, pointed at a test server
// answering with checkins — the one variable that exists so that a fetch can
// be run against something other than Foursquare itself.
func withFoursquareAPI(t *testing.T, checkins string) func(string) string {
	t.Helper()

	env, email := withFoursquareUser(t)

	args := []string{"foursquare", "connect", "--email", email, "--foursquare-user-id", "1709193"}
	if err := run(args, env, strings.NewReader("the-access-token\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatalf("foursquare connect returned %v", err)
	}

	body := `{"meta":{"code":200},"response":{"checkins":{"count":1,"items":[` + checkins + `]}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return func(name string) string {
		if name == "TRAVELMAP_FOURSQUARE_API_URL" {
			return server.URL
		}

		return env(name)
	}
}

// recentCheckin is one check-in dated relative to now. A run computes its
// window from its own start, so a fixed date would drift out of that window
// as time passed; dating it from now is what keeps a fixture on the side of
// the window its test means it to be on.
func recentCheckin(id string, ago time.Duration) string {
	return fmt.Sprintf(`{"id":%q,"createdAt":%d}`, id, time.Now().UTC().Add(-ago).Unix())
}

// TestFoursquareSyncCommand covers the command's own completion condition: a
// run against a linked account collects what the window holds and reports it
// per account, naming the database it wrote to like every other command here.
func TestFoursquareSyncCommand(t *testing.T) {
	t.Parallel()

	env := withFoursquareAPI(t, recentCheckin("5f2a1b3c4d5e6f708192a3b4", time.Hour))

	var stdout, stderr bytes.Buffer

	if err := run([]string{"foursquare", "sync"}, env, noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare sync returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "1 check-in collected for user 1 (Foursquare user 1709193)") {
		t.Errorf("foursquare sync printed %q, want the run summary", got)
	}
}

// TestFoursquareSyncTakesAWiderWindow covers --lookback-days, which is how a
// backfill reaches further back than the window a routine run takes. The
// check-in is 30 days old, so the default fortnight would leave it alone and
// only the wider window collects it.
func TestFoursquareSyncTakesAWiderWindow(t *testing.T) {
	t.Parallel()

	env := withFoursquareAPI(t, recentCheckin("5f2a1b3c4d5e6f708192a3b4", 30*24*time.Hour))

	var stdout, stderr bytes.Buffer

	args := []string{"foursquare", "sync", "--lookback-days", "365"}
	if err := run(args, env, noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare sync returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "1 check-in collected") {
		t.Errorf("foursquare sync printed %q, want the run summary", got)
	}
}

// TestFoursquareSyncWithoutALinkedAccount pins the answer to the state every
// fresh server is in: nothing to fetch for, said in the words that name the
// command that fixes it, and not treated as a failure.
func TestFoursquareSyncWithoutALinkedAccount(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	if err := run([]string{"foursquare", "sync"}, migrated(t), noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare sync returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, `run "travelmap foursquare connect" first`) {
		t.Errorf("foursquare sync printed %q, want it to name the command that links an account", got)
	}
}

// TestFoursquareSyncRejectsBadInput covers the misuses that are answered
// before any database is opened.
func TestFoursquareSyncRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"an argument it takes none of": {"foursquare", "sync", "yesterday"},
		"a negative window":            {"foursquare", "sync", "--lookback-days", "-1"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			err := run(args, migrated(t), noStdin(), &stdout, &stderr)
			if !errors.Is(err, errUsage) {
				t.Fatalf("foursquare sync returned %v, want a usage error", err)
			}
		})
	}
}

// TestFoursquareSyncContinuesAfterOneAccountFails pins the one thing the
// per-account loop promises: a refused request for one linked account does
// not stop the run before the rest are tried. Two accounts are linked to the
// same test server, which tells them apart by the token each one presents —
// the shape a rate limit or a revoked authorisation take, since they are
// per-token rather than per-server.
func TestFoursquareSyncContinuesAfterOneAccountFails(t *testing.T) {
	t.Parallel()

	env := migrated(t)

	body := `{"meta":{"code":200},"response":{"checkins":{"count":1,` +
		`"items":[` + recentCheckin("5f2a1b3c4d5e6f708192a3b4", time.Hour) + `]}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer the-failing-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"meta":{"code":403,"errorType":"rate_limit_exceeded"},"response":{}}`))

			return
		}

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	link := func(email, foursquareUserID, token string) {
		seedUser(t, env, email)

		args := []string{"foursquare", "connect", "--email", email, "--foursquare-user-id", foursquareUserID}
		if err := run(args, env, strings.NewReader(token+"\n"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("foursquare connect returned %v", err)
		}
	}

	link("first@example.com", "1111111", "the-failing-token")
	link("second@example.com", "2222222", "the-working-token")

	withAPIURL := func(name string) string {
		if name == "TRAVELMAP_FOURSQUARE_API_URL" {
			return server.URL
		}

		return env(name)
	}

	var stdout, stderr bytes.Buffer

	err := run([]string{"foursquare", "sync"}, withAPIURL, noStdin(), &stdout, &stderr)
	if err == nil {
		t.Fatal("foursquare sync returned nil, want the failing account's error")
	}

	if got := stdout.String(); !strings.Contains(got, "1 check-in collected for user 2 (Foursquare user 2222222)") {
		t.Errorf("foursquare sync printed %q, want the working account's own summary", got)
	}

	if got := stdout.String(); strings.Contains(got, "user 1 (Foursquare user 1111111)") {
		t.Errorf("foursquare sync printed %q, want no summary line for the failing account", got)
	}
}
