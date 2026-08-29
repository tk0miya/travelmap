package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// pointColumns is the select list [List] reads, in the order [scanPoint]
// expects them.
const pointColumns = `id, user_id, timestamp, latitude, longitude, altitude,
	velocity, accuracy, vertical_accuracy, course, course_accuracy,
	battery_status, battery, ssid, tracker_id, created_at, updated_at`

// pointRepository implements [store.PointRepository].
type pointRepository struct {
	q querier
}

// Create implements [store.PointRepository].
//
// Rows are inserted one at a time rather than as a single multi-row INSERT,
// because ON CONFLICT ... DO NOTHING reports its own rows affected per
// statement — a multi-row insert would report the whole statement's count,
// not which rows were the duplicates. A batch is at most the mobile app's
// batch_size (up to 1000), so the round trips this costs are not worth
// avoiding on the one connection this server holds anyway.
func (r pointRepository) Create(ctx context.Context, points []model.Point) (int, error) {
	// Truncated to the second for the reason on userRepository.Create: it is
	// what a later lookup will find.
	now := time.Now().UTC().Truncate(time.Second)

	var inserted int

	for _, p := range points {
		result, err := r.q.ExecContext(ctx,
			`INSERT INTO points (
				user_id, timestamp, latitude, longitude, altitude, velocity,
				accuracy, vertical_accuracy, course, course_accuracy,
				battery_status, battery, ssid, tracker_id, created_at, updated_at
			 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (user_id, timestamp) DO NOTHING`,
			p.UserID, unixTime(p.Timestamp), p.Latitude, p.Longitude,
			p.Altitude, p.Velocity, p.Accuracy, p.VerticalAccuracy,
			p.Course, p.CourseAccuracy, p.BatteryStatus, p.Battery,
			p.SSID, p.TrackerID, unixTime(now), unixTime(now),
		)
		if err != nil {
			return inserted, fmt.Errorf("sqlite: inserting a point: %w", translate(err))
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return inserted, fmt.Errorf("sqlite: inserting a point: reading rows affected: %w", err)
		}

		inserted += int(affected)
	}

	return inserted, nil
}

// UserIDs implements [store.PointRepository].
func (r pointRepository) UserIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT DISTINCT user_id FROM points ORDER BY user_id`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the users with points: %w", err)
	}

	userIDs, err := collect(rows, func(rows *sql.Rows) (int64, error) {
		var userID int64

		err := rows.Scan(&userID)

		return userID, err
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the users with points: %w", err)
	}

	return userIDs, nil
}

// Timestamps implements [store.PointRepository].
func (r pointRepository) Timestamps(ctx context.Context, userID int64) ([]time.Time, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT timestamp FROM points WHERE user_id = ? ORDER BY timestamp`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the timestamps for user %d: %w", userID, err)
	}

	timestamps, err := collect(rows, func(rows *sql.Rows) (time.Time, error) {
		var ts unixTime

		err := rows.Scan(&ts)

		return time.Time(ts), err
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing the timestamps for user %d: %w", userID, err)
	}

	return timestamps, nil
}

// NextTimestamp implements [store.PointRepository].
//
// ORDER BY ... LIMIT 1 rather than MIN(timestamp): a MIN() over no matching
// rows still returns one row, holding NULL, so telling "no next point" apart
// from "the next point is at time zero" would need a nullable scan; this way
// the empty case is the ordinary sql.ErrNoRows every other lookup here uses.
func (r pointRepository) NextTimestamp(ctx context.Context, userID int64, after time.Time) (time.Time, bool, error) {
	var ts unixTime

	err := r.q.QueryRowContext(ctx,
		`SELECT timestamp FROM points WHERE user_id = ? AND timestamp > ? ORDER BY timestamp LIMIT 1`,
		userID, unixTime(after),
	).Scan(&ts)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, fmt.Errorf("sqlite: finding the next point for user %d after %s: %w",
			userID, after, err)
	default:
		return time.Time(ts), true, nil
	}
}

// List implements [store.PointRepository].
func (r pointRepository) List(
	ctx context.Context, userID int64, startAt *time.Time, endAt time.Time, ascending bool, page, perPage int,
) ([]model.Point, int, error) {
	where := `user_id = ? AND timestamp <= ?`
	args := []any{userID, unixTime(endAt)}

	if startAt != nil {
		where += ` AND timestamp >= ?`
		args = append(args, unixTime(*startAt))
	}

	var total int
	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM points WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("sqlite: counting points for user %d: %w", userID, err)
	}

	// The only place order becomes part of the query text: it is always one
	// of these two literals, never the client's own string, so there is
	// nothing here for a malformed order value to inject.
	direction := "DESC"
	if ascending {
		direction = "ASC"
	}

	rows, err := r.q.QueryContext(ctx,
		`SELECT `+pointColumns+` FROM points WHERE `+where+` ORDER BY timestamp `+direction+` LIMIT ? OFFSET ?`,
		append(args, perPage, (page-1)*perPage)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: listing points for user %d: %w", userID, err)
	}

	points, err := collect(rows, scanPoint)
	if err != nil {
		return nil, 0, fmt.Errorf("sqlite: listing points for user %d: %w", userID, err)
	}

	return points, total, nil
}

// Update implements [store.PointRepository].
//
// RETURNING hands back the row as it now stands without a second round trip,
// the same reason [checkinRepository.Upsert] uses it, and doubles as the
// existence check: a WHERE that matches nothing returns no row, which
// [scanPointRow] turns into [store.ErrNotFound] via [translate].
func (r pointRepository) Update(ctx context.Context, userID, id int64, latitude, longitude float64) (model.Point, error) {
	now := time.Now().UTC().Truncate(time.Second)

	row := r.q.QueryRowContext(ctx,
		`UPDATE points SET latitude = ?, longitude = ?, updated_at = ?
		 WHERE id = ? AND user_id = ?
		 RETURNING `+pointColumns,
		latitude, longitude, unixTime(now), id, userID,
	)

	p, err := scanPointRow(row)
	if err != nil {
		return model.Point{}, fmt.Errorf("sqlite: updating point %d for user %d: %w", id, userID, err)
	}

	return p, nil
}

// Delete implements [store.PointRepository].
func (r pointRepository) Delete(ctx context.Context, userID, id int64) (time.Time, error) {
	var ts unixTime

	err := r.q.QueryRowContext(ctx,
		`DELETE FROM points WHERE id = ? AND user_id = ? RETURNING timestamp`, id, userID,
	).Scan(&ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("sqlite: deleting point %d for user %d: %w", id, userID, translate(err))
	}

	return time.Time(ts), nil
}

// DeleteBulk implements [store.PointRepository].
func (r pointRepository) DeleteBulk(ctx context.Context, userID int64, ids []int64) ([]time.Time, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)

	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}

	rows, err := r.q.QueryContext(ctx,
		`DELETE FROM points WHERE user_id = ? AND id IN (`+strings.Join(placeholders, ",")+`) RETURNING timestamp`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: bulk deleting points for user %d: %w", userID, err)
	}

	timestamps, err := collect(rows, func(rows *sql.Rows) (time.Time, error) {
		var ts unixTime

		err := rows.Scan(&ts)

		return time.Time(ts), err
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: bulk deleting points for user %d: %w", userID, err)
	}

	return timestamps, nil
}

// scanPoint reads one row of [pointColumns].
func scanPoint(rows *sql.Rows) (model.Point, error) {
	var (
		p                    model.Point
		timestamp            unixTime
		createdAt, updatedAt unixTime
	)

	err := rows.Scan(
		&p.ID, &p.UserID, &timestamp, &p.Latitude, &p.Longitude, &p.Altitude,
		&p.Velocity, &p.Accuracy, &p.VerticalAccuracy, &p.Course, &p.CourseAccuracy,
		&p.BatteryStatus, &p.Battery, &p.SSID, &p.TrackerID, &createdAt, &updatedAt,
	)
	if err != nil {
		return model.Point{}, err
	}

	p.Timestamp = time.Time(timestamp)
	p.CreatedAt = time.Time(createdAt)
	p.UpdatedAt = time.Time(updatedAt)

	return p, nil
}

// scanPointRow is [scanPoint] for the single-row RETURNING queries Update
// runs: same [pointColumns] order, but over a *sql.Row, whose Scan error is
// translated so a WHERE that matched nothing comes back as
// [store.ErrNotFound] rather than a bare sql.ErrNoRows.
func scanPointRow(row *sql.Row) (model.Point, error) {
	var (
		p                    model.Point
		timestamp            unixTime
		createdAt, updatedAt unixTime
	)

	err := row.Scan(
		&p.ID, &p.UserID, &timestamp, &p.Latitude, &p.Longitude, &p.Altitude,
		&p.Velocity, &p.Accuracy, &p.VerticalAccuracy, &p.Course, &p.CourseAccuracy,
		&p.BatteryStatus, &p.Battery, &p.SSID, &p.TrackerID, &createdAt, &updatedAt,
	)
	if err != nil {
		return model.Point{}, translate(err)
	}

	p.Timestamp = time.Time(timestamp)
	p.CreatedAt = time.Time(createdAt)
	p.UpdatedAt = time.Time(updatedAt)

	return p, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.PointRepository = pointRepository{}
