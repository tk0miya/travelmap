package ingest_test

import (
	"context"
	"errors"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// errFake stands in for whatever a real repository could fail with — a
// closed connection, SQLITE_BUSY, and so on. Every ingest function has to
// report it rather than swallow it, whichever call it came from.
var errFake = errors.New("fake: it broke")

// rebuildCall is one call made to [store.DailyStatsRepository.Rebuild],
// recorded so a test can assert on which days it walked and in what order.
type rebuildCall struct {
	UserID     int64
	Day        time.Time
	TrackBreak time.Duration
}

// fakeDailyStats implements [store.DailyStatsRepository] by recording every
// call rather than storing anything: what a caller does with the store is
// what these tests are about, not the SQL, which internal/store/sqlite tests
// against a real database.
//
// The embedded interface is left nil: a method this type does not override
// panics on the nil call rather than needing a stub here, so a method added
// to store.DailyStatsRepository that these tests never exercise does not
// force a change to this file.
type fakeDailyStats struct {
	store.DailyStatsRepository

	events *[]string

	rebuilt []rebuildCall

	// rebuildErr and deleteAllErr, when set, are what Rebuild and DeleteAll
	// fail with, for a test asserting that a caller reports the failure
	// rather than swallowing it.
	rebuildErr, deleteAllErr error
}

func (f *fakeDailyStats) Rebuild(_ context.Context, userID int64, day time.Time, trackBreak time.Duration) error {
	if f.rebuildErr != nil {
		return f.rebuildErr
	}

	*f.events = append(*f.events, "rebuild")
	f.rebuilt = append(f.rebuilt, rebuildCall{UserID: userID, Day: day, TrackBreak: trackBreak})

	return nil
}

func (f *fakeDailyStats) DeleteAll(context.Context) error {
	if f.deleteAllErr != nil {
		return f.deleteAllErr
	}

	*f.events = append(*f.events, "delete_all")

	return nil
}

func (f *fakeDailyStats) Get(context.Context, int64, time.Time) (model.DailyStat, error) {
	return model.DailyStat{}, store.ErrNotFound
}

// fakePoints implements [store.PointRepository] over the fixed data a test
// sets up.
//
// The embedded interface is left nil, for the same reason as
// [fakeDailyStats]'s: a method added to store.PointRepository that no test
// exercises panics on the nil call instead of needing a stub here.
type fakePoints struct {
	store.PointRepository

	userIDs    []int64
	timestamps map[int64][]time.Time

	// existing is every timestamp recorded for a user, standing in for the
	// points table NextTimestamp searches: seeded with what a test treats as
	// already stored before its batch, and appended to by Create so a
	// second NextTimestamp call sees what the first Create just inserted.
	existing map[int64][]time.Time

	// created records every point Create was given, across calls, for a test
	// to assert on.
	created []model.Point

	// createResult, when set, is what Create reports as inserted instead of
	// len(points) — for a test pinning that CreatePoints returns exactly what
	// Create reported rather than the size of the batch it was given.
	createResult *int

	// userIDsErr, timestampsErr, createErr and nextTimestampErr, when set,
	// are what UserIDs, Timestamps, Create and NextTimestamp fail with.
	userIDsErr, timestampsErr, createErr, nextTimestampErr error

	// updateResult, when set, is what Update reports instead of a point
	// built from its own arguments — for a test pinning that UpdatePoint
	// returns exactly what Update reported.
	updateResult *model.Point

	// deleteResult is the timestamp Delete reports removed.
	deleteResult time.Time

	// deleteBulkResult is the timestamps DeleteBulk reports removed —
	// possibly fewer than the ids it was given, for a test pinning that
	// DeletePoints counts only what was actually deleted.
	deleteBulkResult []time.Time

	// updateErr, deleteErr and deleteBulkErr, when set, are what Update,
	// Delete and DeleteBulk fail with.
	updateErr, deleteErr, deleteBulkErr error
}

func (f *fakePoints) Create(_ context.Context, points []model.Point) (int, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}

	f.created = append(f.created, points...)

	if f.existing == nil {
		f.existing = make(map[int64][]time.Time)
	}

	for _, p := range points {
		f.existing[p.UserID] = append(f.existing[p.UserID], p.Timestamp)
	}

	if f.createResult != nil {
		return *f.createResult, nil
	}

	return len(points), nil
}

func (f *fakePoints) UserIDs(context.Context) ([]int64, error) {
	if f.userIDsErr != nil {
		return nil, f.userIDsErr
	}

	return f.userIDs, nil
}

func (f *fakePoints) Timestamps(_ context.Context, userID int64) ([]time.Time, error) {
	if f.timestampsErr != nil {
		return nil, f.timestampsErr
	}

	return f.timestamps[userID], nil
}

// NextTimestamp implements [store.PointRepository] over [fakePoints.existing],
// the smallest recorded timestamp after after, exactly as the real query
// would find it.
func (f *fakePoints) NextTimestamp(_ context.Context, userID int64, after time.Time) (time.Time, bool, error) {
	if f.nextTimestampErr != nil {
		return time.Time{}, false, f.nextTimestampErr
	}

	var (
		next  time.Time
		found bool
	)

	for _, ts := range f.existing[userID] {
		if !ts.After(after) {
			continue
		}

		if !found || ts.Before(next) {
			next = ts
			found = true
		}
	}

	return next, found, nil
}

func (f *fakePoints) Update(_ context.Context, userID, id int64, latitude, longitude float64) (model.Point, error) {
	if f.updateErr != nil {
		return model.Point{}, f.updateErr
	}

	if f.updateResult != nil {
		return *f.updateResult, nil
	}

	return model.Point{ID: id, UserID: userID, Latitude: latitude, Longitude: longitude}, nil
}

func (f *fakePoints) Delete(context.Context, int64, int64) (time.Time, error) {
	if f.deleteErr != nil {
		return time.Time{}, f.deleteErr
	}

	return f.deleteResult, nil
}

func (f *fakePoints) DeleteBulk(context.Context, int64, []int64) ([]time.Time, error) {
	if f.deleteBulkErr != nil {
		return nil, f.deleteBulkErr
	}

	return f.deleteBulkResult, nil
}

// fakeStore implements [store.Store] over [fakePoints] and [fakeDailyStats].
//
// The embedded interface is left nil, for the same reason as
// [fakeDailyStats]'s: Users, and any repository that store.Store grows
// later, are never reached by these tests, so a call to one of them panics
// on the nil call instead of needing a stub here.
type fakeStore struct {
	store.Store

	points     *fakePoints
	dailyStats *fakeDailyStats
}

func (s *fakeStore) Points() store.PointRepository { return s.points }

func (s *fakeStore) DailyStats() store.DailyStatsRepository { return s.dailyStats }

// Tx implements [store.Store]. There is nothing here to roll back, so fn
// always runs against this same store.
func (s *fakeStore) Tx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return fn(ctx, s)
}
