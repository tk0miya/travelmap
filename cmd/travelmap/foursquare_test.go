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

// seedUser inserts one user directly into configPath's already-migrated
// database — the same reasoning as recalculate_test.go's own seedPoints,
// since there is no CLI command left that creates a user: signing up needs
// a browser, which these tests don't drive. The email doubles as the API
// key; nothing here authenticates with it, so only uniqueness matters.
func seedUser(t *testing.T, configPath, email string) {
	t.Helper()

	ctx := t.Context()

	db, _, err := openDatabase(ctx, configPath)
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

// seedFoursquareAccount links email to foursquareUserID directly in
// configPath's already-migrated database, standing in for what the
// settings page's OAuth flow does — there is no CLI command left that
// creates a link either.
func seedFoursquareAccount(t *testing.T, configPath, email, foursquareUserID, accessToken string) {
	t.Helper()

	ctx := t.Context()

	db, _, err := openDatabase(ctx, configPath)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	user, err := db.Users().ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("looking up %s: %v", email, err)
	}

	if _, err := db.FoursquareAccounts().Create(ctx, model.FoursquareAccount{
		UserID:           user.ID,
		FoursquareUserID: foursquareUserID,
		AccessToken:      accessToken,
	}); err != nil {
		t.Fatalf("linking %s to Foursquare user %s: %v", email, foursquareUserID, err)
	}
}

// withFoursquareAPI returns the config path of a migrated database holding
// one user with a Swarm account already linked, pointed at a test server
// answering with checkins — the one setting that exists so that a fetch can
// be run against something other than Foursquare itself.
//
// The config file is rewritten with the server's URL only after every
// command that does not care about it has already run against the original
// content.
func withFoursquareAPI(t *testing.T, checkins string) string {
	t.Helper()

	configPath, dbPath := migrated(t)
	const email = "alice@example.com"

	seedUser(t, configPath, email)
	seedFoursquareAccount(t, configPath, email, "1709193", "the-access-token")

	body := `{"meta":{"code":200},"response":{"checkins":{"count":1,"items":[` + checkins + `]}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	// No [foursquare] header of its own: requiredTOML's own [foursquare]
	// table is still open at this point in the generated file, and TOML
	// refuses a table declared twice.
	extra := fmt.Sprintf("api_url = %q\n", server.URL)
	if err := rewriteConfigWithDB(configPath, dbPath, extra); err != nil {
		t.Fatalf("rewriting %s: %v", configPath, err)
	}

	return configPath
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

	configPath := withFoursquareAPI(t, recentCheckin("5f2a1b3c4d5e6f708192a3b4", time.Hour))

	var stdout, stderr bytes.Buffer

	if err := run(withConfig(configPath, "foursquare", "sync"), noStdin(), &stdout, &stderr); err != nil {
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

	configPath := withFoursquareAPI(t, recentCheckin("5f2a1b3c4d5e6f708192a3b4", 30*24*time.Hour))

	var stdout, stderr bytes.Buffer

	args := withConfig(configPath, "foursquare", "sync", "--lookback-days", "365")
	if err := run(args, noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare sync returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "1 check-in collected") {
		t.Errorf("foursquare sync printed %q, want the run summary", got)
	}
}

// TestFoursquareSyncWithoutALinkedAccount pins the answer to the state every
// fresh server is in: nothing to fetch for, said in words that point at where
// to fix it, and not treated as a failure.
func TestFoursquareSyncWithoutALinkedAccount(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	configPath, _ := migrated(t)

	args := withConfig(configPath, "foursquare", "sync")
	if err := run(args, noStdin(), &stdout, &stderr); err != nil {
		t.Fatalf("foursquare sync returned %v (stderr %q)", err, stderr.String())
	}

	if got := stdout.String(); !strings.Contains(got, "connect one from Settings first") {
		t.Errorf("foursquare sync printed %q, want it to say where to link an account", got)
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

			configPath, _ := migrated(t)

			err := run(withConfig(configPath, args...), noStdin(), &bytes.Buffer{}, &bytes.Buffer{})
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

	configPath, dbPath := migrated(t)

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
		seedUser(t, configPath, email)
		seedFoursquareAccount(t, configPath, email, foursquareUserID, token)
	}

	link("first@example.com", "1111111", "the-failing-token")
	link("second@example.com", "2222222", "the-working-token")

	// Rewritten only now, after both links — which do not care about it —
	// have already run against the original content. No [foursquare] header
	// of its own: see the comment on the same pattern in withFoursquareAPI.
	extra := fmt.Sprintf("api_url = %q\n", server.URL)
	if err := rewriteConfigWithDB(configPath, dbPath, extra); err != nil {
		t.Fatalf("rewriting %s: %v", configPath, err)
	}

	var stdout, stderr bytes.Buffer

	err := run(withConfig(configPath, "foursquare", "sync"), noStdin(), &stdout, &stderr)
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

// TestFoursquareSyncReportsRevokedAuthorization covers the one thing a status
// code alone cannot say: a 403 with errorType not_authorized is a revoked
// authorisation, not the rate limit the same status would otherwise suggest,
// and the run reports it as one rather than the other.
func TestFoursquareSyncReportsRevokedAuthorization(t *testing.T) {
	t.Parallel()

	configPath, dbPath := migrated(t)
	const email = "alice@example.com"

	seedUser(t, configPath, email)
	seedFoursquareAccount(t, configPath, email, "1709193", "the-access-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"meta":{"code":403,"errorType":"not_authorized"},"response":{}}`))
	}))
	t.Cleanup(server.Close)

	extra := fmt.Sprintf("api_url = %q\n", server.URL)
	if err := rewriteConfigWithDB(configPath, dbPath, extra); err != nil {
		t.Fatalf("rewriting %s: %v", configPath, err)
	}

	var stdout, stderr bytes.Buffer

	err := run(withConfig(configPath, "foursquare", "sync"), noStdin(), &stdout, &stderr)
	if err == nil {
		t.Fatal("foursquare sync returned nil, want the revoked account's error")
	}

	if got := stderr.String(); !strings.Contains(got, "was revoked, reconnect Swarm from Settings") {
		t.Errorf("foursquare sync printed stderr %q, want it to report the revoked authorisation", got)
	}

	if got := stderr.String(); strings.Contains(got, "rate limit") {
		t.Errorf("foursquare sync printed stderr %q, want it not to call a revoked authorisation a rate limit", got)
	}
}
