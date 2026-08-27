package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

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
// batch_size (Step 16, up to 1000), so the round trips this costs are not
// worth avoiding on the one connection this server holds anyway.
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

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.PointRepository = pointRepository{}
