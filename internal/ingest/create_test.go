package ingest_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/ingest"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// point builds a minimal, valid point: the coordinates below are arbitrary,
// since none of these tests assert on distance — that is pinned separately,
// by tests directly against [store.DailyStatsRepository].
func point(userID int64, ts time.Time) model.Point {
	return model.Point{UserID: userID, Timestamp: ts, Latitude: 1, Longitude: 2}
}

// TestCreatePointsRebuildsEachAffectedDayOnce pins the ordinary case: points
// on two distinct days rebuild exactly those two days, and nothing else —
// there is no later point stored anywhere for NextTimestamp to find.
func TestCreatePointsRebuildsEachAffectedDayOnce(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{points: &fakePoints{}, dailyStats: dailyStats}

	points := []model.Point{
		point(1, day1.Add(time.Hour)),
		point(1, day2.Add(30*time.Minute)),
	}

	created, err := ingest.CreatePoints(t.Context(), st, points, time.UTC, 30*time.Minute)
	if err != nil {
		t.Fatalf("CreatePoints returned %v", err)
	}

	if created != 2 {
		t.Errorf("created = %d, want 2", created)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestCreatePointsRebuildsEachUserIndependently pins that the day grouping
// does not leak across users, matching Recalculate's own guarantee.
func TestCreatePointsRebuildsEachUserIndependently(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{points: &fakePoints{}, dailyStats: dailyStats}

	points := []model.Point{
		point(2, day.Add(time.Hour)),
		point(1, day.Add(2*time.Hour)),
	}

	if _, err := ingest.CreatePoints(t.Context(), st, points, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("CreatePoints returned %v", err)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day, TrackBreak: 30 * time.Minute},
		{UserID: 2, Day: day, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestCreatePointsPropagatesToTheDayHoldingTheNextPoint pins the boundary
// case affectedDays exists for: a point lands on a day that already has a
// later day's point stored, and that later day gets rebuilt too, even though
// none of its own points are in this batch — because it may have been
// computed against a predecessor this insert just replaced.
func TestCreatePointsPropagatesToTheDayHoldingTheNextPoint(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	points := &fakePoints{
		// Stands in for a point already stored on day2, from before this
		// batch — the one NextTimestamp is expected to find.
		existing: map[int64][]time.Time{1: {day2.Add(10 * time.Minute)}},
	}
	st := &fakeStore{points: points, dailyStats: dailyStats}

	batch := []model.Point{point(1, day1.Add(23*time.Hour))}

	if _, err := ingest.CreatePoints(t.Context(), st, batch, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("CreatePoints returned %v", err)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestCreatePointsDoesNotPropagateWithoutALaterPoint pins the flip side:
// nothing is stored after the batch, so NextTimestamp finds nothing and only
// the day the batch actually touched is rebuilt.
func TestCreatePointsDoesNotPropagateWithoutALaterPoint(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	st := &fakeStore{points: &fakePoints{}, dailyStats: dailyStats}

	batch := []model.Point{point(1, day.Add(time.Hour))}

	if _, err := ingest.CreatePoints(t.Context(), st, batch, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("CreatePoints returned %v", err)
	}

	want := []rebuildCall{{UserID: 1, Day: day, TrackBreak: 30 * time.Minute}}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestCreatePointsReturnsWhatCreateReported pins that the count CreatePoints
// answers with is Create's own report, not the size of the batch it was
// given — the two differ whenever a point is a duplicate.
func TestCreatePointsReturnsWhatCreateReported(t *testing.T) {
	t.Parallel()

	reported := 1

	var events []string

	st := &fakeStore{
		points:     &fakePoints{createResult: &reported},
		dailyStats: &fakeDailyStats{events: &events},
	}

	batch := []model.Point{point(1, time.Now()), point(1, time.Now().Add(time.Minute))}

	created, err := ingest.CreatePoints(t.Context(), st, batch, time.UTC, 30*time.Minute)
	if err != nil {
		t.Fatalf("CreatePoints returned %v", err)
	}

	if created != reported {
		t.Errorf("created = %d, want %d", created, reported)
	}
}

// TestCreatePointsReportsFailures covers every call CreatePoints makes
// through the store, pinning that a failure from any one of them is reported
// rather than answering success over points that were never actually
// committed.
func TestCreatePointsReportsFailures(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	batch := []model.Point{point(1, day.Add(time.Hour))}

	tests := map[string]struct {
		points     *fakePoints
		dailyStats *fakeDailyStats
	}{
		"Create fails": {
			points:     &fakePoints{createErr: errFake},
			dailyStats: &fakeDailyStats{},
		},
		"NextTimestamp fails": {
			points:     &fakePoints{nextTimestampErr: errFake},
			dailyStats: &fakeDailyStats{},
		},
		"Rebuild fails": {
			points:     &fakePoints{},
			dailyStats: &fakeDailyStats{rebuildErr: errFake},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var events []string
			tt.dailyStats.events = &events

			st := &fakeStore{points: tt.points, dailyStats: tt.dailyStats}

			created, err := ingest.CreatePoints(t.Context(), st, batch, time.UTC, 30*time.Minute)
			if !errors.Is(err, errFake) {
				t.Errorf("CreatePoints returned %v, want it to wrap %v", err, errFake)
			}

			if created != 0 {
				t.Errorf("created = %d, want 0 on failure", created)
			}
		})
	}
}

// TestCreatePointsAgreesWithRecalculate is the agreement test TODO.md calls
// for: against a real database, the daily_stats CreatePoints builds up
// incrementally, batch by batch, must equal what Recalculate produces from
// scratch over the same final points.
//
// The batches are deliberately out of order and split across days, including
// one that arrives late and lands right before a day an earlier batch already
// rebuilt — the case CreatePoints' propagation to the following day exists
// for. Without it, that earlier day's segment would stay computed against the
// wrong predecessor and this test would catch the disagreement.
func TestCreatePointsAgreesWithRecalculate(t *testing.T) {
	t.Parallel()

	loc := time.UTC
	trackBreak := 30 * time.Minute

	user := model.User{
		ID: 1, Email: "agree@example.com", PasswordHash: "x", APIKey: "y",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	st := storetest.New(t, user)

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	// Ten days later, separated from day2 by far more than trackBreak: this
	// is the "points separated by several days" case, confirming the
	// crossing segment is excluded from km on both paths alike.
	day12 := day2.AddDate(0, 0, 10)

	// Distinct coordinates throughout, not the shared placeholder [point]
	// uses elsewhere in this file: every segment below needs a non-zero
	// distance, or a predecessor mismatch would compute the same 0 km either
	// way and this test would not notice.
	at := func(userID int64, ts time.Time, lat, lon float64) model.Point {
		p := point(userID, ts)
		p.Latitude, p.Longitude = lat, lon

		return p
	}

	// Batch 1, first chronologically but sent second below: today's real-time
	// samples, just after midnight on day2.
	batch1 := []model.Point{
		at(user.ID, day2.Add(5*time.Minute), 35.00, 139.00),
		at(user.ID, day2.Add(15*time.Minute), 35.01, 139.01),
	}

	// Batch 2, sent first: a backfill landing on day1, 10 minutes before
	// batch1's first point above — inside trackBreak, so once it exists it
	// changes day2's first segment. Out-of-order and late-arriving both: it
	// is inserted before batch1 even though its own timestamp is earlier.
	batch2 := []model.Point{
		at(user.ID, day1.Add(23*time.Hour+55*time.Minute), 34.90, 138.90),
	}

	// Batch 3: a resumed-tracking cluster well over a week later, the other
	// side of a gap tracking was stopped across.
	batch3 := []model.Point{
		at(user.ID, day12.Add(9*time.Hour), 40.00, 116.00),
		at(user.ID, day12.Add(9*time.Hour+5*time.Minute), 40.01, 116.01),
	}

	if _, err := ingest.CreatePoints(t.Context(), st, batch1, loc, trackBreak); err != nil {
		t.Fatalf("creating batch1: %v", err)
	}

	if _, err := ingest.CreatePoints(t.Context(), st, batch2, loc, trackBreak); err != nil {
		t.Fatalf("creating batch2 (late-arriving): %v", err)
	}

	if _, err := ingest.CreatePoints(t.Context(), st, batch3, loc, trackBreak); err != nil {
		t.Fatalf("creating batch3: %v", err)
	}

	incremental := dailyStatsFor(t, st, user.ID, []time.Time{day1, day2, day12})

	if err := ingest.Recalculate(t.Context(), st, loc, trackBreak); err != nil {
		t.Fatalf("Recalculate returned %v", err)
	}

	recalculated := dailyStatsFor(t, st, user.ID, []time.Time{day1, day2, day12})

	if diff := cmp.Diff(recalculated, incremental); diff != "" {
		t.Errorf("incremental daily_stats differ from a full recalculation (-recalculated +incremental):\n%s", diff)
	}
}

// dailyStatsFor reads userID's daily_stats row for each of days, mapping a
// day with no row (store.ErrNotFound) to the zero value — CreatePoints and
// Recalculate must agree on which days have no row at all just as much as on
// the rows that exist.
func dailyStatsFor(t *testing.T, st store.Store, userID int64, days []time.Time) map[time.Time]model.DailyStat {
	t.Helper()

	got := make(map[time.Time]model.DailyStat, len(days))

	for _, day := range days {
		stat, err := st.DailyStats().Get(t.Context(), userID, day)

		switch {
		case errors.Is(err, store.ErrNotFound):
			got[day] = model.DailyStat{}
		case err != nil:
			t.Fatalf("getting daily_stats for %s: %v", day, err)
		default:
			got[day] = stat
		}
	}

	return got
}
