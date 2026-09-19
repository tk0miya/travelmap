package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// tripsTestUser creates a fresh user for a trips test, for the same reason
// [tracksTestUser] does.
func tripsTestUser(t *testing.T, db *DB, email string) int64 {
	t.Helper()

	user, err := db.Users().Create(t.Context(), testUser(email))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	return user.ID
}

// aTrip builds a trip spanning [start, end) with an arbitrary title and
// description, for a test that does not care about their exact content.
func aTrip(userID int64, start, end time.Time) model.Trip {
	return model.Trip{
		UserID:      userID,
		Title:       "A trip",
		StartedAt:   start,
		EndedAt:     end,
		Description: "Somewhere, sometime",
	}
}

// TestTripsCreateAndByID pins the round trip: a created trip is found again
// by its own id, scoped to the user that created it.
func TestTripsCreateAndByID(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tripsTestUser(t, db, "create@example.com")
	otherID := tripsTestUser(t, db, "other-create@example.com")

	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := db.Trips().Create(t.Context(), aTrip(userID, start, start.Add(24*time.Hour)))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if created.ID == 0 {
		t.Error("Create left ID unset")
	}

	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("Create left CreatedAt/UpdatedAt unset")
	}

	got, err := db.Trips().ByID(t.Context(), userID, created.ID)
	if err != nil {
		t.Fatalf("ByID returned %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("ByID differs from the created row (-want +got):\n%s", diff)
	}

	if _, err := db.Trips().ByID(t.Context(), userID, created.ID+999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a nonexistent id returned %v, want ErrNotFound", err)
	}

	if _, err := db.Trips().ByID(t.Context(), otherID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another user's id returned %v, want ErrNotFound", err)
	}
}

// TestTripsListOrdersByStartedAt pins that List returns only the requesting
// user's trips, ordered by StartedAt ascending regardless of creation order.
func TestTripsListOrdersByStartedAt(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tripsTestUser(t, db, "list@example.com")
	otherID := tripsTestUser(t, db, "other-list@example.com")

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)

	later, err := db.Trips().Create(t.Context(), aTrip(userID, day2, day2.Add(24*time.Hour)))
	if err != nil {
		t.Fatalf("creating the later trip: %v", err)
	}

	earlier, err := db.Trips().Create(t.Context(), aTrip(userID, day1, day1.Add(24*time.Hour)))
	if err != nil {
		t.Fatalf("creating the earlier trip: %v", err)
	}

	if _, err := db.Trips().Create(t.Context(), aTrip(otherID, day1, day1.Add(24*time.Hour))); err != nil {
		t.Fatalf("creating the other user's trip: %v", err)
	}

	trips, err := db.Trips().List(t.Context(), userID)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if len(trips) != 2 || trips[0].ID != earlier.ID || trips[1].ID != later.ID {
		t.Fatalf("List = %+v, want [earlier, later]", trips)
	}
}

// TestTripsUpdate pins that Update overwrites every mutable field and bumps
// UpdatedAt, scoped to the owning user.
func TestTripsUpdate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tripsTestUser(t, db, "update@example.com")
	otherID := tripsTestUser(t, db, "other-update@example.com")

	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := db.Trips().Create(t.Context(), aTrip(userID, start, start.Add(24*time.Hour)))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	newStart := start.Add(48 * time.Hour)
	toUpdate := created
	toUpdate.Title = "Renamed"
	toUpdate.StartedAt = newStart
	toUpdate.EndedAt = newStart.Add(24 * time.Hour)
	toUpdate.Description = "Updated description"

	updated, err := db.Trips().Update(t.Context(), toUpdate)
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}

	if updated.Title != "Renamed" || updated.Description != "Updated description" || !updated.StartedAt.Equal(newStart) {
		t.Errorf("Update = %+v, want the new fields stored", updated)
	}

	if !updated.UpdatedAt.After(created.UpdatedAt) && !updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("UpdatedAt = %s, want it no earlier than the original %s", updated.UpdatedAt, created.UpdatedAt)
	}

	wrongUser := created
	wrongUser.UserID = otherID

	if _, err := db.Trips().Update(t.Context(), wrongUser); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("updating with another user's id returned %v, want ErrNotFound", err)
	}
}

// TestTripsDelete pins that Delete removes the row and reports
// [store.ErrNotFound] for an id that does not exist or belongs to a
// different user.
func TestTripsDelete(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tripsTestUser(t, db, "delete@example.com")
	otherID := tripsTestUser(t, db, "other-delete@example.com")

	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	created, err := db.Trips().Create(t.Context(), aTrip(userID, start, start.Add(24*time.Hour)))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if err := db.Trips().Delete(t.Context(), otherID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting with another user's id returned %v, want ErrNotFound", err)
	}

	if err := db.Trips().Delete(t.Context(), userID, created.ID); err != nil {
		t.Fatalf("Delete returned %v", err)
	}

	if _, err := db.Trips().ByID(t.Context(), userID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ByID after Delete returned %v, want ErrNotFound", err)
	}

	if err := db.Trips().Delete(t.Context(), userID, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting an already-deleted id returned %v, want ErrNotFound", err)
	}
}
