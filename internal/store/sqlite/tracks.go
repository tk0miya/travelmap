package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// trackColumns is the select list [trackRepository.List] and
// [trackRepository.ByID] read, in the order [scanTrack] expects them.
const trackColumns = `id, user_id, start_at, end_at, distance, geometry, created_at, updated_at`

// jsonCoordinates scans and stores [model.Coordinate] as the JSON array of
// [longitude, latitude] pairs tracks.geometry holds — GeoJSON's own
// coordinate order, so no reordering is needed to answer GET /api/v1/tracks.
type jsonCoordinates []model.Coordinate

// Scan implements [sql.Scanner].
func (c *jsonCoordinates) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("sqlite: scanning a track geometry: %T is not a string", src)
	}

	var pairs [][2]float64
	if err := json.Unmarshal([]byte(text), &pairs); err != nil {
		return err
	}

	coords := make([]model.Coordinate, len(pairs))
	for i, pair := range pairs {
		coords[i] = model.Coordinate{Longitude: pair[0], Latitude: pair[1]}
	}

	*c = coords

	return nil
}

// Value implements [driver.Valuer].
func (c jsonCoordinates) Value() (driver.Value, error) {
	pairs := make([][2]float64, len(c))
	for i, coord := range c {
		pairs[i] = [2]float64{coord.Longitude, coord.Latitude}
	}

	b, err := json.Marshal(pairs)
	if err != nil {
		return nil, err
	}

	return string(b), nil
}

// trackRepository implements [store.TrackRepository].
type trackRepository struct {
	q querier
}

// ReplaceAll implements [store.TrackRepository].
func (r trackRepository) ReplaceAll(ctx context.Context, userID int64, tracks []model.Track) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM tracks WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("sqlite: replacing tracks for user %d: %w", userID, err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	for _, t := range tracks {
		_, err := r.q.ExecContext(ctx,
			`INSERT INTO tracks (user_id, start_at, end_at, distance, geometry, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			userID, unixTime(t.StartAt), unixTime(t.EndAt), t.DistanceMeters, jsonCoordinates(t.Geometry),
			unixTime(now), unixTime(now),
		)
		if err != nil {
			return fmt.Errorf("sqlite: replacing tracks for user %d: %w", userID, err)
		}
	}

	return nil
}

// List implements [store.TrackRepository].
func (r trackRepository) List(
	ctx context.Context, userID int64, startAt, endAt *time.Time, page, perPage int,
) ([]model.Track, int, error) {
	where := `user_id = ?`
	args := []any{userID}

	// A track overlaps [startAt, endAt) when it starts before the range ends
	// and ends after the range starts.
	if startAt != nil {
		where += ` AND end_at >= ?`
		args = append(args, unixTime(*startAt))
	}

	if endAt != nil {
		where += ` AND start_at < ?`
		args = append(args, unixTime(*endAt))
	}

	var total int
	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tracks WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sqlite: counting tracks for user %d: %w", userID, err)
	}

	rows, err := r.q.QueryContext(ctx,
		`SELECT `+trackColumns+` FROM tracks WHERE `+where+` ORDER BY start_at LIMIT ? OFFSET ?`,
		append(args, perPage, (page-1)*perPage)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: listing tracks for user %d: %w", userID, err)
	}

	tracks, err := collect(rows, scanTrack)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: listing tracks for user %d: %w", userID, err)
	}

	return tracks, total, nil
}

// ByID implements [store.TrackRepository].
func (r trackRepository) ByID(ctx context.Context, userID, id int64) (model.Track, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+trackColumns+` FROM tracks WHERE id = ? AND user_id = ?`, id, userID)

	t, err := scanTrackRow(row)
	if err != nil {
		return model.Track{}, fmt.Errorf("sqlite: finding track %d for user %d: %w", id, userID, err)
	}

	return t, nil
}

// Enqueue implements [store.TrackRepository].
func (r trackRepository) Enqueue(ctx context.Context, userID int64) error {
	now := time.Now().UTC().Truncate(time.Second)

	_, err := r.q.ExecContext(ctx,
		`INSERT INTO track_split_jobs (user_id, requested_at) VALUES (?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET requested_at = excluded.requested_at`,
		userID, unixTime(now),
	)
	if err != nil {
		return fmt.Errorf("sqlite: enqueuing a track rebuild for user %d: %w", userID, err)
	}

	return nil
}

// NextPending implements [store.TrackRepository].
func (r trackRepository) NextPending(ctx context.Context) (int64, bool, error) {
	var userID int64

	// Ordered by id, not requested_at: two requests coalescing onto the same
	// second would otherwise tie, and id — assigned once, on first insert,
	// and never reassigned by the ON CONFLICT DO UPDATE in Enqueue — is what
	// breaks the tie in the order they actually arrived.
	err := r.q.QueryRowContext(ctx,
		`DELETE FROM track_split_jobs
		 WHERE id = (SELECT id FROM track_split_jobs ORDER BY id LIMIT 1)
		 RETURNING user_id`,
	).Scan(&userID)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("sqlite: claiming the next pending track rebuild: %w", err)
	default:
		return userID, true, nil
	}
}

// scanTrack reads one row of [trackColumns].
func scanTrack(rows *sql.Rows) (model.Track, error) {
	var (
		t                    model.Track
		startAt, endAt       unixTime
		geometry             jsonCoordinates
		createdAt, updatedAt unixTime
	)

	err := rows.Scan(&t.ID, &t.UserID, &startAt, &endAt, &t.DistanceMeters, &geometry, &createdAt, &updatedAt)
	if err != nil {
		return model.Track{}, err
	}

	t.StartAt = time.Time(startAt)
	t.EndAt = time.Time(endAt)
	t.Geometry = geometry
	t.CreatedAt = time.Time(createdAt)
	t.UpdatedAt = time.Time(updatedAt)

	return t, nil
}

// scanTrackRow is [scanTrack] for the single-row ByID lookup: same
// [trackColumns] order, but over a *sql.Row, whose Scan error is translated
// so a WHERE that matched nothing comes back as [store.ErrNotFound] rather
// than a bare sql.ErrNoRows.
func scanTrackRow(row *sql.Row) (model.Track, error) {
	var (
		t                    model.Track
		startAt, endAt       unixTime
		geometry             jsonCoordinates
		createdAt, updatedAt unixTime
	)

	err := row.Scan(&t.ID, &t.UserID, &startAt, &endAt, &t.DistanceMeters, &geometry, &createdAt, &updatedAt)
	if err != nil {
		return model.Track{}, translate(err)
	}

	t.StartAt = time.Time(startAt)
	t.EndAt = time.Time(endAt)
	t.Geometry = geometry
	t.CreatedAt = time.Time(createdAt)
	t.UpdatedAt = time.Time(updatedAt)

	return t, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.TrackRepository = trackRepository{}
