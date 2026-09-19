package timeline_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
	"github.com/tk0miya/travelmap/internal/timeline"
)

// testUser is the account every test in this package creates a trip for.
func testUser() model.User {
	now := time.Now().UTC()

	return model.User{
		ID: 1, Email: "timeline@example.com", PasswordHash: "x", APIKey: "y",
		CreatedAt: now, UpdatedAt: now,
	}
}

// aTrip builds a trip for userID, for a test that does not care about its
// exact content.
func aTrip(userID int64, start time.Time) model.Trip {
	return model.Trip{
		UserID:      userID,
		Title:       "A trip",
		StartedAt:   start,
		EndedAt:     start.Add(24 * time.Hour),
		Description: "Somewhere, sometime",
	}
}

// TestTripRoundTrip pins that a created trip is found again by TripByID, and
// listed by Trips.
func TestTripRoundTrip(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := timeline.CreateTrip(t.Context(), st, aTrip(1, start))
	if err != nil {
		t.Fatalf("CreateTrip returned %v", err)
	}

	got, err := timeline.TripByID(t.Context(), st, 1, created.ID)
	if err != nil {
		t.Fatalf("TripByID returned %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("TripByID differs from the created trip (-want +got):\n%s", diff)
	}

	trips, err := timeline.Trips(t.Context(), st, 1)
	if err != nil {
		t.Fatalf("Trips returned %v", err)
	}

	if len(trips) != 1 || trips[0].ID != created.ID {
		t.Fatalf("Trips = %+v, want just the created trip", trips)
	}
}

// TestUpdateTrip pins that UpdateTrip overwrites the stored row and reports
// the update.
func TestUpdateTrip(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := timeline.CreateTrip(t.Context(), st, aTrip(1, start))
	if err != nil {
		t.Fatalf("CreateTrip returned %v", err)
	}

	toUpdate := created
	toUpdate.Title = "Renamed"

	updated, err := timeline.UpdateTrip(t.Context(), st, toUpdate)
	if err != nil {
		t.Fatalf("UpdateTrip returned %v", err)
	}

	if updated.Title != "Renamed" {
		t.Errorf("Title = %q, want %q", updated.Title, "Renamed")
	}
}

// TestDeleteTrip pins that DeleteTrip removes the row, so a later lookup
// reports [store.ErrNotFound].
func TestDeleteTrip(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := timeline.CreateTrip(t.Context(), st, aTrip(1, start))
	if err != nil {
		t.Fatalf("CreateTrip returned %v", err)
	}

	if err := timeline.DeleteTrip(t.Context(), st, 1, created.ID); err != nil {
		t.Fatalf("DeleteTrip returned %v", err)
	}

	if _, err := timeline.TripByID(t.Context(), st, 1, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("TripByID after DeleteTrip returned %v, want ErrNotFound", err)
	}
}

// TestTripFailuresReportTheUnderlyingError pins that a database that cannot
// reach the trips table reports a failure from every operation rather than
// hanging or panicking — the path a future handler turns into a 500.
func TestTripFailuresReportTheUnderlyingError(t *testing.T) {
	t.Parallel()

	st := storetest.UnavailableTrips(t, testUser())

	if _, err := timeline.CreateTrip(t.Context(), st, aTrip(1, time.Now())); err == nil {
		t.Error("CreateTrip returned nil for a store that cannot reach trips")
	}

	if _, err := timeline.TripByID(t.Context(), st, 1, 1); err == nil {
		t.Error("TripByID returned nil for a store that cannot reach trips")
	}

	if _, err := timeline.Trips(t.Context(), st, 1); err == nil {
		t.Error("Trips returned nil for a store that cannot reach trips")
	}

	if _, err := timeline.UpdateTrip(t.Context(), st, aTrip(1, time.Now())); err == nil {
		t.Error("UpdateTrip returned nil for a store that cannot reach trips")
	}

	if err := timeline.DeleteTrip(t.Context(), st, 1, 1); err == nil {
		t.Error("DeleteTrip returned nil for a store that cannot reach trips")
	}
}
