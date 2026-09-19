package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// tripColumns is the select list every lookup shares, in the order
// [scanTrip] reads them.
const tripColumns = `id, user_id, title, started_at, ended_at, description, created_at, updated_at`

// tripRepository implements [store.TripRepository].
type tripRepository struct {
	q querier
}

// Create implements [store.TripRepository].
func (r tripRepository) Create(ctx context.Context, trip model.Trip) (model.Trip, error) {
	now := time.Now().UTC().Truncate(time.Second)

	result, err := r.q.ExecContext(ctx,
		`INSERT INTO trips (user_id, title, started_at, ended_at, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		trip.UserID, trip.Title, unixTime(trip.StartedAt), unixTime(trip.EndedAt), trip.Description,
		unixTime(now), unixTime(now),
	)
	if err != nil {
		return model.Trip{}, fmt.Errorf("sqlite: creating a trip for user %d: %w", trip.UserID, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return model.Trip{}, fmt.Errorf("sqlite: creating a trip for user %d: reading the id: %w", trip.UserID, err)
	}

	trip.ID = id
	trip.CreatedAt = now
	trip.UpdatedAt = now

	return trip, nil
}

// ByID implements [store.TripRepository].
func (r tripRepository) ByID(ctx context.Context, userID, id int64) (model.Trip, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+tripColumns+` FROM trips WHERE id = ? AND user_id = ?`, id, userID)

	trip, err := scanTripRow(row)
	if err != nil {
		return model.Trip{}, fmt.Errorf("sqlite: finding trip %d for user %d: %w", id, userID, err)
	}

	return trip, nil
}

// List implements [store.TripRepository].
func (r tripRepository) List(ctx context.Context, userID int64) ([]model.Trip, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+tripColumns+` FROM trips WHERE user_id = ? ORDER BY started_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing trips for user %d: %w", userID, err)
	}

	trips, err := collect(rows, scanTrip)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing trips for user %d: %w", userID, err)
	}

	return trips, nil
}

// Update implements [store.TripRepository].
//
// RETURNING hands back the row as it now stands without a second round trip,
// the same reason [pointRepository.Update] uses it, and doubles as the
// existence check: a WHERE that matches nothing returns no row, which
// [scanTripRow] turns into [store.ErrNotFound] via [translate].
func (r tripRepository) Update(ctx context.Context, trip model.Trip) (model.Trip, error) {
	now := time.Now().UTC().Truncate(time.Second)

	row := r.q.QueryRowContext(ctx,
		`UPDATE trips SET title = ?, started_at = ?, ended_at = ?, description = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?
		 RETURNING `+tripColumns,
		trip.Title, unixTime(trip.StartedAt), unixTime(trip.EndedAt), trip.Description, unixTime(now),
		trip.ID, trip.UserID,
	)

	updated, err := scanTripRow(row)
	if err != nil {
		return model.Trip{}, fmt.Errorf("sqlite: updating trip %d: %w", trip.ID, err)
	}

	return updated, nil
}

// Delete implements [store.TripRepository].
func (r tripRepository) Delete(ctx context.Context, userID, id int64) error {
	var discard int64

	err := r.q.QueryRowContext(ctx,
		`DELETE FROM trips WHERE id = ? AND user_id = ? RETURNING id`, id, userID,
	).Scan(&discard)
	if err != nil {
		return fmt.Errorf("sqlite: deleting trip %d for user %d: %w", id, userID, translate(err))
	}

	return nil
}

// scanTrip reads one row of [tripColumns].
func scanTrip(rows *sql.Rows) (model.Trip, error) {
	var (
		t                    model.Trip
		startedAt, endedAt   unixTime
		createdAt, updatedAt unixTime
	)

	err := rows.Scan(&t.ID, &t.UserID, &t.Title, &startedAt, &endedAt, &t.Description, &createdAt, &updatedAt)
	if err != nil {
		return model.Trip{}, err
	}

	t.StartedAt = time.Time(startedAt)
	t.EndedAt = time.Time(endedAt)
	t.CreatedAt = time.Time(createdAt)
	t.UpdatedAt = time.Time(updatedAt)

	return t, nil
}

// scanTripRow is [scanTrip] for the single-row queries ByID and Update rely
// on: same [tripColumns] order, but over a *sql.Row, whose Scan error is
// translated so a WHERE that matched nothing comes back as
// [store.ErrNotFound] rather than a bare sql.ErrNoRows.
func scanTripRow(row *sql.Row) (model.Trip, error) {
	var (
		t                    model.Trip
		startedAt, endedAt   unixTime
		createdAt, updatedAt unixTime
	)

	err := row.Scan(&t.ID, &t.UserID, &t.Title, &startedAt, &endedAt, &t.Description, &createdAt, &updatedAt)
	if err != nil {
		return model.Trip{}, translate(err)
	}

	t.StartedAt = time.Time(startedAt)
	t.EndedAt = time.Time(endedAt)
	t.CreatedAt = time.Time(createdAt)
	t.UpdatedAt = time.Time(updatedAt)

	return t, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.TripRepository = tripRepository{}
