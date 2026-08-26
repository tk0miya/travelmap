package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// Recalculate rebuilds daily_stats for every user, from scratch, in one
// transaction. It is what `travelmap recalculate` runs: for recovery after an
// import or an inconsistency, and after TRAVELMAP_TIMEZONE or
// TRAVELMAP_TRACK_BREAK_MINUTES changes — both invalidate every existing row.
//
// loc is the timezone day boundaries are cut on and trackBreak the gap above
// which a segment is excluded from km; both come from config, which this
// package does not otherwise depend on, so they are passed in rather than
// read here.
func Recalculate(ctx context.Context, st store.Store, loc *time.Location, trackBreak time.Duration) error {
	return st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		// First, not last: changing TRAVELMAP_TIMEZONE reshuffles which days
		// exist, and rebuilding only the days the new grouping produces would
		// leave rows from the old grouping behind forever.
		if err := tx.DailyStats().DeleteAll(ctx); err != nil {
			return fmt.Errorf("ingest: recalculating: %w", err)
		}

		userIDs, err := tx.Points().UserIDs(ctx)
		if err != nil {
			return fmt.Errorf("ingest: recalculating: %w", err)
		}

		for _, userID := range userIDs {
			if err := recalculateUser(ctx, tx, userID, loc, trackBreak); err != nil {
				return fmt.Errorf("ingest: recalculating user %d: %w", userID, err)
			}
		}

		return nil
	})
}

// recalculateUser rebuilds every calendar day, cut in loc, on which userID has
// at least one point.
//
// Which day a timestamp falls on depends on loc, and SQLite carries no
// timezone database to compute that itself — see
// store.PointRepository.Timestamps — so the grouping happens here, in Go,
// over the timestamps alone. Timestamps returns them in ascending order, so a
// day change is detected by comparing each one against the last day seen,
// without collecting the whole list into a set first.
func recalculateUser(ctx context.Context, tx store.Store, userID int64, loc *time.Location, trackBreak time.Duration) error {
	timestamps, err := tx.Points().Timestamps(ctx, userID)
	if err != nil {
		return err
	}

	var lastDay time.Time

	for i, ts := range timestamps {
		day := localMidnight(ts, loc)

		if i > 0 && day.Equal(lastDay) {
			continue
		}

		if err := tx.DailyStats().Rebuild(ctx, userID, day, trackBreak); err != nil {
			return err
		}

		lastDay = day
	}

	return nil
}

// localMidnight returns local midnight, in loc, of the calendar day ts falls
// on.
func localMidnight(ts time.Time, loc *time.Location) time.Time {
	local := ts.In(loc)
	y, m, d := local.Date()

	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
