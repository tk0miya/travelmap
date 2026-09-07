package ingest

import (
	"context"
	"sort"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// dayKey is one user's calendar day touched by a batch of points.
type dayKey struct {
	userID int64
	day    time.Time
}

// affectedDays returns every (user, day) pair a rebuild is owed after the
// rows at points changed — inserted, updated or deleted; each [model.Point]
// need only carry the UserID and Timestamp a change touched, not a row that
// still exists. For each touch: its own day, cut in loc per [localMidnight],
// plus the day of whatever point is now next after it in time order, if that
// falls on a different day. Run this after the write, so NextTimestamp
// reflects the result.
//
// The second half exists because Rebuild's day D reads the point immediately
// preceding D as context (see its own doc): a change on day D can therefore
// change day D+1's own rebuild even though D+1 gets none of the change's
// points. Using each day's own latest touch as the lower bound is enough to
// catch this — an earlier touch in the same day can never be nearer to D+1's
// start than the latest one is. The result can include a day nothing here
// actually changed the predecessor for, but [store.DailyStatsRepository.Rebuild]
// is cheap to rerun, and finding out costs the same query anyway.
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

// rebuildAffectedDays rebuilds every day [affectedDays] finds owed by
// touched, and enqueues a track rebuild for every distinct user among
// touched — the shared second half of every point mutation, insert, update
// or delete alike. It must run after the write touched describes, in the
// same transaction, so [affectedDays]' own NextTimestamp lookups see the
// result.
func rebuildAffectedDays(ctx context.Context, tx store.Store, touched []model.Point, loc *time.Location, trackBreak time.Duration) error {
	days, err := affectedDays(ctx, tx, touched, loc)
	if err != nil {
		return err
	}

	for _, day := range days {
		if err := tx.DailyStats().Rebuild(ctx, day.userID, day.day, trackBreak); err != nil {
			return err
		}
	}

	return enqueueTrackRebuilds(ctx, tx, touched)
}

// enqueueTrackRebuilds records a pending track rebuild for every distinct
// user among touched, so internal/track's worker recomputes their tracks
// once a point anywhere in their history has changed. touched is usually all
// one user's points — every ingest caller acts on a single authenticated
// user — but this holds regardless.
func enqueueTrackRebuilds(ctx context.Context, tx store.Store, touched []model.Point) error {
	seen := make(map[int64]bool, len(touched))

	for _, p := range touched {
		if seen[p.UserID] {
			continue
		}

		seen[p.UserID] = true

		if err := tx.Tracks().Enqueue(ctx, p.UserID); err != nil {
			return err
		}
	}

	return nil
}
