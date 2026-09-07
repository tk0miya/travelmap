package track_test

import (
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store/storetest"
	"github.com/tk0miya/travelmap/internal/track"
)

// TestRecalculateAllRebuildsEveryUser pins that a full recalculation reaches
// every user with points, not only whichever one is asked about explicitly.
func TestRecalculateAllRebuildsEveryUser(t *testing.T) {
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
		{UserID: 2, Timestamp: start, Latitude: 3, Longitude: 3},
		{UserID: 2, Timestamp: start.Add(time.Minute), Latitude: 4, Longitude: 4},
	})

	if err := track.RecalculateAll(t.Context(), st, 30*time.Minute); err != nil {
		t.Fatalf("RecalculateAll returned %v", err)
	}

	for _, userID := range []int64{1, 2} {
		tracks, _, err := st.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
		if err != nil {
			t.Fatalf("listing tracks for user %d: %v", userID, err)
		}

		if len(tracks) != 1 {
			t.Errorf("len(tracks) for user %d = %d, want 1", userID, len(tracks))
		}
	}
}

// TestRecalculateAllWithNoPointsIsANoop pins that a database with no points
// at all does nothing rather than failing.
func TestRecalculateAllWithNoPointsIsANoop(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	if err := track.RecalculateAll(t.Context(), st, 30*time.Minute); err != nil {
		t.Fatalf("RecalculateAll returned %v", err)
	}
}

// TestRecalculateAllReportsUserIDsFailure pins that a failure listing which
// users have points is reported rather than silently doing nothing.
func TestRecalculateAllReportsUserIDsFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailablePoints(t, testUser())

	if err := track.RecalculateAll(t.Context(), st, 30*time.Minute); err == nil {
		t.Fatal("RecalculateAll returned nil for a store that cannot list users")
	}
}

// TestRecalculateAllReportsRebuildFailure pins that a failure rebuilding one
// user's tracks stops the run and is reported, rather than silently skipping
// that user.
func TestRecalculateAllReportsRebuildFailure(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableTracks(t, testUser())

	seedPoints(t, st, []model.Point{
		{UserID: 1, Timestamp: time.Now(), Latitude: 1, Longitude: 1},
		{UserID: 1, Timestamp: time.Now().Add(time.Minute), Latitude: 2, Longitude: 2},
	})

	if err := track.RecalculateAll(t.Context(), st, 30*time.Minute); err == nil {
		t.Fatal("RecalculateAll returned nil for a store that cannot write tracks")
	}
}
