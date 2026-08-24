package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// This file, alone in this package's tests, opens internal/store/sqlite
// directly: cmd/travelmap is the only package allowed to, per "Layering" in
// CLAUDE.md, and there is no CLI command that inserts a point to set one up
// through instead — that is Step 7's HTTP endpoints.

// seedPoints migrates env's database and inserts one user's points directly,
// standing in for what a device's uploads would have put there. It returns
// the id the user got.
func seedPoints(t *testing.T, env func(string) string, points []model.Point) int64 {
	t.Helper()

	var out bytes.Buffer
	if err := run([]string{"migrate"}, env, noStdin(), &out, &out); err != nil {
		t.Fatalf("migrate returned %v", err)
	}

	ctx := t.Context()

	db, _, err := openDatabase(ctx, env)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	user, err := db.Users().Create(ctx, model.User{
		Email:        "recalculate@example.com",
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "recalculate-test-key",
	})
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	for i := range points {
		points[i].UserID = user.ID
	}

	if _, err := db.Points().Create(ctx, points); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	return user.ID
}

// TestRecalculateCommand is the completion condition of Step 8 seen from
// outside: rebuilding from a set of points known to travel a fixed distance
// produces the daily_stats row that distance predicts.
func TestRecalculateCommand(t *testing.T) {
	t.Parallel()

	env, _ := tempDatabase(t)

	day := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	const (
		tokyoLat, tokyoLon = 35.6812, 139.7671
		osakaLat, osakaLon = 34.7025, 135.4959
	)

	// Ten minutes apart, well inside the default 30-minute
	// TRAVELMAP_TRACK_BREAK_MINUTES, so the segment is not excluded.
	userID := seedPoints(t, env, []model.Point{
		{Timestamp: day, Latitude: tokyoLat, Longitude: tokyoLon},
		{Timestamp: day.Add(10 * time.Minute), Latitude: osakaLat, Longitude: osakaLon},
	})

	var out bytes.Buffer
	if err := run([]string{"recalculate"}, env, noStdin(), &out, &out); err != nil {
		t.Fatalf("recalculate returned %v (output %q)", err, out.String())
	}

	if got := out.String(); !strings.Contains(got, "daily_stats recalculated") {
		t.Errorf("recalculate printed %q, want it to report the recalculation", got)
	}

	db, _, err := openDatabase(t.Context(), env)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	stat, err := db.DailyStats().Get(t.Context(), userID, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}

	if stat.Points != 2 {
		t.Errorf("Points = %d, want 2", stat.Points)
	}

	if stat.KM <= 0 {
		t.Errorf("KM = %v, want the Tokyo-Osaka segment counted", stat.KM)
	}
}

// TestRecalculateOnAnUnmigratedDatabase covers the same mistake `serve` and
// `user create` refuse: TRAVELMAP_DATABASE pointing at a file with no schema.
func TestRecalculateOnAnUnmigratedDatabase(t *testing.T) {
	t.Parallel()

	env, _ := tempDatabase(t)

	var out bytes.Buffer

	err := run([]string{"recalculate"}, env, noStdin(), &out, &out)
	if err == nil {
		t.Fatal("recalculate on an unmigrated database returned nil")
	}

	if !strings.Contains(err.Error(), `run "travelmap migrate" first`) {
		t.Errorf("recalculate failed with %v, want it to name the command to run", err)
	}
}

// TestRecalculateRejectsAnInvalidTimezone pins that a bad TRAVELMAP_TIMEZONE
// stops the command before it touches the database, the same way every other
// command refuses a configuration that fails to load.
func TestRecalculateRejectsAnInvalidTimezone(t *testing.T) {
	t.Parallel()

	base, _ := tempDatabase(t)

	env := func(name string) string {
		if name == "TRAVELMAP_TIMEZONE" {
			return "Nowhere/Nothing"
		}

		return base(name)
	}

	var out bytes.Buffer

	err := run([]string{"recalculate"}, env, noStdin(), &out, &out)
	if err == nil {
		t.Fatal("recalculate with an invalid TRAVELMAP_TIMEZONE returned nil")
	}

	if !strings.Contains(err.Error(), "TRAVELMAP_TIMEZONE") {
		t.Errorf("recalculate failed with %v, want it to name the variable at fault", err)
	}
}

// TestRecalculateDeletesStaleDays pins the reason DeleteAll runs first:
// rebuilding under a changed TRAVELMAP_TIMEZONE must not leave a day from
// the previous timezone's grouping behind.
func TestRecalculateDeletesStaleDays(t *testing.T) {
	t.Parallel()

	env, _ := tempDatabase(t)

	// 20:00 UTC, which recalculate under the default UTC groups onto June 1.
	ts := time.Date(2026, time.June, 1, 20, 0, 0, 0, time.UTC)

	userID := seedPoints(t, env, []model.Point{{Timestamp: ts, Latitude: 35.0, Longitude: 139.0}})

	var out bytes.Buffer
	if err := run([]string{"recalculate"}, env, noStdin(), &out, &out); err != nil {
		t.Fatalf("the first recalculate returned %v", err)
	}

	// The same instant is 05:00 JST on June 2 — a different calendar day, so
	// the second recalculation must not leave June 1's row from the first
	// behind.
	tzEnv := func(name string) string {
		if name == "TRAVELMAP_TIMEZONE" {
			return "Asia/Tokyo"
		}

		return env(name)
	}

	if err := run([]string{"recalculate"}, tzEnv, noStdin(), &out, &out); err != nil {
		t.Fatalf("the second recalculate returned %v", err)
	}

	db, _, err := openDatabase(t.Context(), env)
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	defer closeDatabase(db)

	if _, err := db.DailyStats().Get(t.Context(), userID, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound: the UTC-grouped row must not survive a Tokyo recalculation", err)
	}
}
