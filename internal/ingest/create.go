package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// CreatePoints inserts points and rebuilds every daily_stats day they touch,
// in the same transaction as the insert. It is the only path that reaches
// store.PointRepository.Create, per "Every mutation of a point goes through
// internal/ingest" in CLAUDE.md.
//
// It returns how many of points were actually inserted, per
// store.PointRepository.Create — a point whose (user_id, timestamp) is
// already stored is silently dropped rather than counted. A dropped point
// still contributes its day to the rebuild: the day already reflects it, so
// rebuilding is redundant but not wrong, and telling a duplicate apart from a
// fresh point up front would cost a lookup this design has no other use for.
//
// loc and trackBreak are TRAVELMAP_TIMEZONE and TRAVELMAP_TRACK_BREAK_MINUTES,
// resolved by the caller for the reason on [Recalculate].
func CreatePoints(ctx context.Context, st store.Store, points []model.Point, loc *time.Location, trackBreak time.Duration) (int, error) {
	var created int

	err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		var err error

		created, err = tx.Points().Create(ctx, points)
		if err != nil {
			return err
		}

		return rebuildAffectedDays(ctx, tx, points, loc, trackBreak)
	})
	if err != nil {
		return 0, fmt.Errorf("ingest: creating points: %w", err)
	}

	return created, nil
}
