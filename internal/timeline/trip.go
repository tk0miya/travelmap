package timeline

import (
	"context"
	"fmt"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// CreateTrip stores trip and returns it as stored, with ID, CreatedAt and
// UpdatedAt filled in.
func CreateTrip(ctx context.Context, st store.Store, trip model.Trip) (model.Trip, error) {
	created, err := st.Trips().Create(ctx, trip)
	if err != nil {
		return model.Trip{}, fmt.Errorf("timeline: creating a trip for user %d: %w", trip.UserID, err)
	}

	return created, nil
}

// TripByID returns one of userID's trips, and [store.ErrNotFound] if there
// is none or it belongs to a different user.
func TripByID(ctx context.Context, st store.Store, userID, id int64) (model.Trip, error) {
	trip, err := st.Trips().ByID(ctx, userID, id)
	if err != nil {
		return model.Trip{}, fmt.Errorf("timeline: finding trip %d for user %d: %w", id, userID, err)
	}

	return trip, nil
}

// Trips returns every one of userID's trips, ordered by StartedAt ascending.
func Trips(ctx context.Context, st store.Store, userID int64) ([]model.Trip, error) {
	trips, err := st.Trips().List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("timeline: listing trips for user %d: %w", userID, err)
	}

	return trips, nil
}

// UpdateTrip overwrites one of userID's trips and returns it as stored, with
// UpdatedAt bumped. It returns [store.ErrNotFound] if trip.ID does not exist
// or belongs to a different user.
func UpdateTrip(ctx context.Context, st store.Store, trip model.Trip) (model.Trip, error) {
	updated, err := st.Trips().Update(ctx, trip)
	if err != nil {
		return model.Trip{}, fmt.Errorf("timeline: updating trip %d: %w", trip.ID, err)
	}

	return updated, nil
}

// DeleteTrip removes one of userID's trips. It returns [store.ErrNotFound]
// if id does not exist or belongs to a different user.
func DeleteTrip(ctx context.Context, st store.Store, userID, id int64) error {
	if err := st.Trips().Delete(ctx, userID, id); err != nil {
		return fmt.Errorf("timeline: deleting trip %d for user %d: %w", id, userID, err)
	}

	return nil
}
