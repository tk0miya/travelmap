package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// UpdatePoint overwrites one point's coordinates and rebuilds every
// daily_stats day the change touches, in the same transaction as the update.
// It is the only path that reaches store.PointRepository.Update, per "Every
// mutation of a point goes through internal/ingest" in CLAUDE.md.
//
// PATCH /api/v1/points/{id} never changes a point's timestamp, so
// [rebuildAffectedDays] is given only the point's own (unchanged) timestamp:
// its own day, and the day of whatever point is now next after it in time
// order.
func UpdatePoint(
	ctx context.Context, st store.Store, userID, id int64, latitude, longitude float64,
	loc *time.Location, trackBreak time.Duration,
) (model.Point, error) {
	var updated model.Point

	err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		var err error

		updated, err = tx.Points().Update(ctx, userID, id, latitude, longitude)
		if err != nil {
			return err
		}

		return rebuildAffectedDays(ctx, tx, []model.Point{{UserID: userID, Timestamp: updated.Timestamp}}, loc, trackBreak)
	})
	if err != nil {
		return model.Point{}, fmt.Errorf("ingest: updating point %d: %w", id, err)
	}

	return updated, nil
}

// DeletePoint removes one point and rebuilds every daily_stats day the
// deletion touches, in the same transaction as the delete. It is the only
// path that reaches store.PointRepository.Delete, for the same reason as
// UpdatePoint.
//
// [rebuildAffectedDays] runs after the delete, inside the same transaction,
// so its NextTimestamp lookup already reflects the point's absence: it finds
// the row that is now genuinely next, not the one just removed.
func DeletePoint(ctx context.Context, st store.Store, userID, id int64, loc *time.Location, trackBreak time.Duration) error {
	err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		timestamp, err := tx.Points().Delete(ctx, userID, id)
		if err != nil {
			return err
		}

		return rebuildAffectedDays(ctx, tx, []model.Point{{UserID: userID, Timestamp: timestamp}}, loc, trackBreak)
	})
	if err != nil {
		return fmt.Errorf("ingest: deleting point %d: %w", id, err)
	}

	return nil
}

// DeletePoints removes every point in ids that belongs to userID and
// rebuilds every daily_stats day the deletions touch, in the same
// transaction. It returns how many points were actually deleted, per
// store.PointRepository.DeleteBulk — an id that does not exist or belongs to
// someone else is silently skipped, not an error, matching
// DELETE /api/v1/points/bulk_destroy's own upstream behaviour.
func DeletePoints(
	ctx context.Context, st store.Store, userID int64, ids []int64, loc *time.Location, trackBreak time.Duration,
) (int, error) {
	var deleted int

	err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		timestamps, err := tx.Points().DeleteBulk(ctx, userID, ids)
		if err != nil {
			return err
		}

		deleted = len(timestamps)

		touched := make([]model.Point, len(timestamps))
		for i, ts := range timestamps {
			touched[i] = model.Point{UserID: userID, Timestamp: ts}
		}

		return rebuildAffectedDays(ctx, tx, touched, loc, trackBreak)
	})
	if err != nil {
		return 0, fmt.Errorf("ingest: deleting points: %w", err)
	}

	return deleted, nil
}
