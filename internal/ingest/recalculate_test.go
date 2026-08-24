package ingest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/ingest"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// errFake stands in for whatever a real repository could fail with — a
// closed connection, SQLITE_BUSY, and so on. Recalculate has to report it
// rather than swallow it, whichever call it came from.
var errFake = errors.New("fake: it broke")

// rebuildCall is one call Recalculate made to
// [store.DailyStatsRepository.Rebuild], recorded so a test can assert on
// which days it walked and in what order.
type rebuildCall struct {
	UserID     int64
	Day        time.Time
	TrackBreak time.Duration
}

// fakeDailyStats implements [store.DailyStatsRepository] by recording every
// call rather than storing anything: what Recalculate does with the store is
// what these tests are about, not the SQL, which internal/store/sqlite tests
// against a real database.
type fakeDailyStats struct {
	events *[]string

	rebuilt []rebuildCall

	// rebuildErr and deleteAllErr, when set, are what Rebuild and DeleteAll
	// fail with, for a test asserting that Recalculate reports the failure
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
// sets up, since Recalculate only ever reads through it.
type fakePoints struct {
	userIDs    []int64
	timestamps map[int64][]time.Time

	// userIDsErr and timestampsErr, when set, are what UserIDs and
	// Timestamps fail with.
	userIDsErr, timestampsErr error
}

func (f *fakePoints) Create(context.Context, []model.Point) (int, error) {
	return 0, nil
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

// fakeStore implements [store.Store] over [fakePoints] and [fakeDailyStats].
// Users is never reached by Recalculate, so it panics rather than being
// implemented, which would fail a test that reaches it just as loudly.
type fakeStore struct {
	points     *fakePoints
	dailyStats *fakeDailyStats
}

func (s *fakeStore) Users() store.UserRepository { panic("fakeStore: Users is not implemented") }

func (s *fakeStore) Points() store.PointRepository { return s.points }

func (s *fakeStore) DailyStats() store.DailyStatsRepository { return s.dailyStats }

// Tx implements [store.Store]. There is nothing here to roll back, so fn
// always runs against this same store.
func (s *fakeStore) Tx(ctx context.Context, fn func(context.Context, store.Store) error) error {
	return fn(ctx, s)
}

// TestRecalculateRebuildsEachDistinctDayOnce pins the grouping Recalculate
// does itself: three timestamps spanning two UTC days rebuild exactly those
// two days, each once, in ascending order — not once per point.
func TestRecalculateRebuildsEachDistinctDayOnce(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{
		points: &fakePoints{
			userIDs: []int64{1},
			timestamps: map[int64][]time.Time{
				1: {
					day1.Add(1 * time.Hour),
					day1.Add(2 * time.Hour),
					day2.Add(30 * time.Minute),
				},
			},
		},
		dailyStats: dailyStats,
	}

	if err := ingest.Recalculate(t.Context(), st, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("Recalculate returned %v", err)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestRecalculateDeletesBeforeRebuilding pins that DeleteAll runs before any
// Rebuild: changing TRAVELMAP_TIMEZONE reshuffles which days exist, and a
// rebuild that ran first would leave the old grouping's rows behind.
func TestRecalculateDeletesBeforeRebuilding(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	st := &fakeStore{
		points: &fakePoints{
			userIDs:    []int64{1},
			timestamps: map[int64][]time.Time{1: {day.Add(time.Hour)}},
		},
		dailyStats: &fakeDailyStats{events: &events},
	}

	if err := ingest.Recalculate(t.Context(), st, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("Recalculate returned %v", err)
	}

	want := []string{"delete_all", "rebuild"}
	if diff := cmp.Diff(want, events); diff != "" {
		t.Errorf("event order differs (-want +got):\n%s", diff)
	}
}

// TestRecalculateEachUserIndependently pins that the day grouping does not
// leak across users: two users with points on the same UTC calendar day get
// one Rebuild call each, for their own id.
func TestRecalculateEachUserIndependently(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{
		points: &fakePoints{
			userIDs: []int64{1, 2},
			timestamps: map[int64][]time.Time{
				1: {day.Add(time.Hour)},
				2: {day.Add(2 * time.Hour)},
			},
		},
		dailyStats: dailyStats,
	}

	if err := ingest.Recalculate(t.Context(), st, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("Recalculate returned %v", err)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day, TrackBreak: 30 * time.Minute},
		{UserID: 2, Day: day, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestRecalculateAsiaTokyo pins the case every other test passes under UTC
// without exercising: a point at 00:30 JST is 15:30 the previous day in UTC,
// so grouping on the UTC calendar date instead of the configured timezone
// would attribute it to the wrong day.
func TestRecalculateAsiaTokyo(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("loading Asia/Tokyo: %v", err)
	}

	// 2026-06-02T00:30:00+09:00, which is 2026-06-01T15:30:00Z.
	ts := time.Date(2026, time.June, 1, 15, 30, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{
		points: &fakePoints{
			userIDs:    []int64{1},
			timestamps: map[int64][]time.Time{1: {ts}},
		},
		dailyStats: dailyStats,
	}

	if err := ingest.Recalculate(t.Context(), st, tokyo, 30*time.Minute); err != nil {
		t.Fatalf("Recalculate returned %v", err)
	}

	if len(dailyStats.rebuilt) != 1 {
		t.Fatalf("Rebuild was called %d times, want 1", len(dailyStats.rebuilt))
	}

	want := time.Date(2026, time.June, 2, 0, 0, 0, 0, tokyo)
	if got := dailyStats.rebuilt[0].Day; !got.Equal(want) {
		t.Errorf("day = %v, want %v (local midnight in Asia/Tokyo)", got, want)
	}
}

// TestRecalculateReportsFailures covers every call Recalculate makes through
// the store, pinning that a failure from any one of them is reported rather
// than swallowed — a `travelmap recalculate` that printed success over a
// database it never finished rebuilding would be worse than one that failed
// loudly.
func TestRecalculateReportsFailures(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		points     *fakePoints
		dailyStats *fakeDailyStats
	}{
		"DeleteAll fails": {
			points:     &fakePoints{userIDs: []int64{1}, timestamps: map[int64][]time.Time{1: {day}}},
			dailyStats: &fakeDailyStats{deleteAllErr: errFake},
		},
		"UserIDs fails": {
			points:     &fakePoints{userIDsErr: errFake},
			dailyStats: &fakeDailyStats{},
		},
		"Timestamps fails": {
			points:     &fakePoints{userIDs: []int64{1}, timestampsErr: errFake},
			dailyStats: &fakeDailyStats{},
		},
		"Rebuild fails": {
			points:     &fakePoints{userIDs: []int64{1}, timestamps: map[int64][]time.Time{1: {day}}},
			dailyStats: &fakeDailyStats{rebuildErr: errFake},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var events []string
			tt.dailyStats.events = &events

			st := &fakeStore{points: tt.points, dailyStats: tt.dailyStats}

			err := ingest.Recalculate(t.Context(), st, time.UTC, 30*time.Minute)
			if !errors.Is(err, errFake) {
				t.Errorf("Recalculate returned %v, want it to wrap %v", err, errFake)
			}
		})
	}
}
