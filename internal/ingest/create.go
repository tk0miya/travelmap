package ingest

import (
	"context"
	"fmt"
	"sort"
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

		days, err := affectedDays(ctx, tx, points, loc)
		if err != nil {
			return err
		}

		for _, day := range days {
			if err := tx.DailyStats().Rebuild(ctx, day.userID, day.day, trackBreak); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("ingest: creating points: %w", err)
	}

	return created, nil
}

// dayKey is one user's calendar day touched by a batch of points.
type dayKey struct {
	userID int64
	day    time.Time
}

// affectedDays returns every (user, day) pair a rebuild is owed after
// inserting points: the day each point falls on, cut in loc per
// [localMidnight], plus — for each of those days — the day of the next
// already-stored point after the latest point this batch put there, if that
// falls on a different day.
//
// The second half is what makes a late-arriving batch correct. Rebuild's day
// D reads the single point immediately preceding D as context from outside D
// itself (see its own doc), so inserting a point on day D can change day D+1
// even though D+1 gets none of this batch's own points — if the new point on
// D lands closer to D+1's start than whatever was the nearest predecessor
// before. Using each day's own latest inserted point as the search's lower
// bound is enough to catch this: an earlier point in the same batch cannot be
// nearer to a later day's start than that day's own latest point is, so it
// cannot matter to any day this search would miss. The result can include a
// day nothing here actually changed the predecessor for — the batch's latest
// point for D was not necessarily D's true latest point — but
// [store.DailyStatsRepository.Rebuild] is cheap to run again, and finding out
// requires the same query anyway.
func affectedDays(ctx context.Context, tx store.Store, points []model.Point, loc *time.Location) ([]dayKey, error) {
	latest := make(map[dayKey]time.Time, len(points))
	order := make([]dayKey, 0, len(points))

	for _, p := range points {
		key := dayKey{userID: p.UserID, day: localMidnight(p.Timestamp, loc)}

		cur, ok := latest[key]
		if !ok {
			order = append(order, key)
		}

		if !ok || p.Timestamp.After(cur) {
			latest[key] = p.Timestamp
		}
	}

	seen := make(map[dayKey]bool, 2*len(order))
	days := make([]dayKey, 0, 2*len(order))

	add := func(key dayKey) {
		if seen[key] {
			return
		}

		seen[key] = true

		days = append(days, key)
	}

	for _, key := range order {
		add(key)

		next, ok, err := tx.Points().NextTimestamp(ctx, key.userID, latest[key])
		if err != nil {
			return nil, err
		}

		if ok {
			add(dayKey{userID: key.userID, day: localMidnight(next, loc)})
		}
	}

	sort.Slice(days, func(i, j int) bool {
		if days[i].userID != days[j].userID {
			return days[i].userID < days[j].userID
		}

		return days[i].day.Before(days[j].day)
	})

	return days, nil
}
