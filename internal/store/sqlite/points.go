package sqlite

import (
	"context"
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
			p.UserID, p.Timestamp.Unix(), p.Latitude, p.Longitude,
			p.Altitude, p.Velocity, p.Accuracy, p.VerticalAccuracy,
			p.Course, p.CourseAccuracy, p.BatteryStatus, p.Battery,
			p.SSID, p.TrackerID, now.Unix(), now.Unix(),
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

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.PointRepository = pointRepository{}
