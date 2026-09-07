package track_test

import (
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
	"github.com/tk0miya/travelmap/internal/track"
)

// testUser is the account every test in this package seeds points for.
func testUser() model.User {
	now := time.Now().UTC()

	return model.User{
		ID: 1, Email: "track@example.com", PasswordHash: "x", APIKey: "y",
		CreatedAt: now, UpdatedAt: now,
	}
}

// seedPoints inserts points directly through the store, bypassing
// internal/ingest: these tests are about internal/track's own rebuild, not
// about what enqueues one.
func seedPoints(t *testing.T, st store.Store, points []model.Point) {
	t.Helper()

	if _, err := st.Points().Create(t.Context(), points); err != nil {
		t.Fatalf("seeding points: %v", err)
	}
}

// TestRebuildUserReplacesWhatWasStoredBefore pins that a rebuild starts from
// scratch: a point added after the first rebuild changes the stored track
// rather than leaving the old row beside a new one.
func TestRebuildUserReplacesWhatWasStoredBefore(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	seedPoints(t, st, []model.Point{
		{UserID: 1, Timestamp: start, Latitude: 1, Longitude: 1},
		{UserID: 1, Timestamp: start.Add(5 * time.Minute), Latitude: 2, Longitude: 2},
	})

	if err := track.RebuildUser(t.Context(), st, 1, 30*time.Minute); err != nil {
		t.Fatalf("RebuildUser returned %v", err)
	}

	first, _, err := st.Tracks().List(t.Context(), 1, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("listing tracks: %v", err)
	}

	if len(first) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(first))
	}

	if !first[0].EndAt.Equal(start.Add(5 * time.Minute)) {
		t.Fatalf("first EndAt = %s, want %s", first[0].EndAt, start.Add(5*time.Minute))
	}

	// Within trackBreak of the existing track's last point, so the rebuild
	// extends it rather than leaving the old two-point track beside a new one.
	seedPoints(t, st, []model.Point{
		{UserID: 1, Timestamp: start.Add(10 * time.Minute), Latitude: 3, Longitude: 3},
	})

	if err := track.RebuildUser(t.Context(), st, 1, 30*time.Minute); err != nil {
		t.Fatalf("RebuildUser returned %v", err)
	}

	second, _, err := st.Tracks().List(t.Context(), 1, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("listing tracks: %v", err)
	}

	if len(second) != 1 {
		t.Fatalf("len(tracks) = %d, want 1 (still one track, now extended)", len(second))
	}

	if !second[0].EndAt.Equal(start.Add(10 * time.Minute)) {
		t.Errorf("second EndAt = %s, want %s (the old row, not replaced)", second[0].EndAt, start.Add(10*time.Minute))
	}

	if second[0].DistanceMeters <= first[0].DistanceMeters {
		t.Errorf("DistanceMeters = %v, want it to have grown past the first rebuild's %v",
			second[0].DistanceMeters, first[0].DistanceMeters)
	}
}

// TestRebuildUserOnlyTouchesItsOwnUser pins that rebuilding one user's tracks
// leaves another user's tracks alone.
func TestRebuildUserOnlyTouchesItsOwnUser(t *testing.T) {
	t.Parallel()

	other := model.User{
		ID: 2, Email: "other@example.com", PasswordHash: "x", APIKey: "z",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}

	st := storetest.New(t, testUser(), other)
	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	seedPoints(t, st, []model.Point{
		{UserID: 1, Timestamp: start, Latitude: 1, Longitude: 1},
		{UserID: 1, Timestamp: start.Add(time.Minute), Latitude: 2, Longitude: 2},
		{UserID: 2, Timestamp: start, Latitude: 1, Longitude: 1},
		{UserID: 2, Timestamp: start.Add(time.Minute), Latitude: 2, Longitude: 2},
	})

	if err := track.RebuildUser(t.Context(), st, 1, 30*time.Minute); err != nil {
		t.Fatalf("RebuildUser returned %v", err)
	}

	otherTracks, _, err := st.Tracks().List(t.Context(), 2, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("listing the other user's tracks: %v", err)
	}

	if len(otherTracks) != 0 {
		t.Errorf("len(otherTracks) = %d, want 0 (untouched)", len(otherTracks))
	}
}

// TestRebuildUserReportsFailures pins that a failure reading points is
// reported rather than silently rebuilding from an incomplete history.
func TestRebuildUserReportsFailures(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailablePoints(t, testUser())

	if err := track.RebuildUser(t.Context(), st, 1, 30*time.Minute); err == nil {
		t.Fatal("RebuildUser returned nil for a store that cannot read points")
	}
}

// TestRebuildUserWithNoPointsClearsExistingTracks pins that a user left with
// no points at all ends up with no tracks — the "rebuild from scratch" rule
// applied to the empty case.
func TestRebuildUserWithNoPointsClearsExistingTracks(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	if err := track.RebuildUser(t.Context(), st, 1, 30*time.Minute); err != nil {
		t.Fatalf("RebuildUser returned %v", err)
	}

	tracks, total, err := st.Tracks().List(t.Context(), 1, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("listing tracks: %v", err)
	}

	if total != 0 || len(tracks) != 0 {
		t.Errorf("tracks = %v (total %d), want none", tracks, total)
	}
}
