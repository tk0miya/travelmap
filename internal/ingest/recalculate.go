package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/store"
)

// Recalculate rebuilds daily_stats for every user, from points. It is what
// `travelmap recalculate` runs: for recovery after an import or an
// inconsistency, and after tracking.timezone or tracking.track_break_minutes
// changes — both invalidate every existing row.
//
// Each user is rebuilt in its own transaction, deliberately not the whole run
// in one: a single transaction would hold SQLite's one write lock for as long
// as the largest deployment's entire history takes to rebuild — long enough
// for a concurrent POST /api/v1/points to exhaust busyTimeout and answer 500,
// which is not the brief CLI/server overlap that timeout is sized for. The
// cost is Recalculate's own atomicity: a run interrupted partway leaves some
// users rebuilt and others not, and daily_stats can be observed empty for a
// user not yet reached. Recalculate is idempotent, so re-running it is the
// recovery from either case, and that is a better trade than the single lock.
//
// loc is the timezone day boundaries are cut on and trackBreak the gap above
// which a segment is excluded from km; both come from config, which this
// package does not otherwise depend on, so they are passed in rather than
// read here.
func Recalculate(ctx context.Context, st store.Store, loc *time.Location, trackBreak time.Duration) error {
	// First, not last, and in its own transaction: changing tracking.timezone
	// reshuffles which days exist, and a per-user rebuild committed ahead of
	// this would leave rows from the old grouping behind.
	if err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		return tx.DailyStats().DeleteAll(ctx)
	}); err != nil {
		return fmt.Errorf("ingest: recalculating: %w", err)
	}

	userIDs, err := st.Points().UserIDs(ctx)
	if err != nil {
		return fmt.Errorf("ingest: recalculating: %w", err)
	}

	for _, userID := range userIDs {
		err := st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
			return recalculateUser(ctx, tx, userID, loc, trackBreak)
		})
		if err != nil {
			return fmt.Errorf("ingest: recalculating user %d: %w", userID, err)
		}
	}

	return nil
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
