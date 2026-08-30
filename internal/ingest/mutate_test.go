package ingest_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/ingest"
	"github.com/tk0miya/travelmap/internal/model"
)

// TestUpdatePointRebuildsAffectedDays pins that an update rebuilds the
// point's own day and the day of whatever point is next after it in time
// order — the same propagation CreatePoints exercises, driven this time by
// Update's own returned timestamp rather than a Create argument.
func TestUpdatePointRebuildsAffectedDays(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	updated := model.Point{ID: 1, UserID: 1, Timestamp: day1.Add(time.Hour), Latitude: 9, Longitude: 10}
	points := &fakePoints{
		updateResult: &updated,
		// Stands in for a point already stored on day2, the one
		// NextTimestamp is expected to find after the update.
		existing: map[int64][]time.Time{1: {day2.Add(10 * time.Minute)}},
	}
	st := &fakeStore{points: points, dailyStats: dailyStats}

	got, err := ingest.UpdatePoint(t.Context(), st, 1, 1, 9, 10, time.UTC, 30*time.Minute)
	if err != nil {
		t.Fatalf("UpdatePoint returned %v", err)
	}

	if diff := cmp.Diff(updated, got); diff != "" {
		t.Errorf("UpdatePoint result differs from what Update reported (-want +got):\n%s", diff)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestUpdatePointDoesNotPropagateWithoutALaterPoint pins the flip side:
// nothing else is stored, so only the updated point's own day is rebuilt.
func TestUpdatePointDoesNotPropagateWithoutALaterPoint(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	updated := model.Point{ID: 1, UserID: 1, Timestamp: day.Add(time.Hour)}
	st := &fakeStore{points: &fakePoints{updateResult: &updated}, dailyStats: dailyStats}

	if _, err := ingest.UpdatePoint(t.Context(), st, 1, 1, 9, 10, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("UpdatePoint returned %v", err)
	}

	want := []rebuildCall{{UserID: 1, Day: day, TrackBreak: 30 * time.Minute}}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestUpdatePointReportsFailures covers every call UpdatePoint makes through
// the store, pinning that a failure from any one of them is reported rather
// than answering success over a change that was never actually committed.
func TestUpdatePointReportsFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		points     *fakePoints
		dailyStats *fakeDailyStats
		tracks     *fakeTracks
	}{
		"Update fails": {
			points:     &fakePoints{updateErr: errFake},
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
		"Enqueue fails": {
			points:     &fakePoints{},
			dailyStats: &fakeDailyStats{},
			tracks:     &fakeTracks{enqueueErr: errFake},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var events []string
			tt.dailyStats.events = &events

			st := &fakeStore{points: tt.points, dailyStats: tt.dailyStats, tracks: tt.tracks}

			if _, err := ingest.UpdatePoint(t.Context(), st, 1, 1, 9, 10, time.UTC, 30*time.Minute); !errors.Is(err, errFake) {
				t.Errorf("UpdatePoint returned %v, want it to wrap %v", err, errFake)
			}
		})
	}
}

// TestUpdatePointEnqueuesTrackRebuild pins that an update enqueues a track
// rebuild for the point's owner.
func TestUpdatePointEnqueuesTrackRebuild(t *testing.T) {
	t.Parallel()

	var events []string

	updated := model.Point{ID: 1, UserID: 1, Timestamp: time.Now()}
	tracks := &fakeTracks{}
	st := &fakeStore{
		points:     &fakePoints{updateResult: &updated},
		dailyStats: &fakeDailyStats{events: &events},
		tracks:     tracks,
	}

	if _, err := ingest.UpdatePoint(t.Context(), st, 1, 1, 9, 10, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("UpdatePoint returned %v", err)
	}

	if diff := cmp.Diff([]int64{1}, tracks.enqueued); diff != "" {
		t.Errorf("enqueued users differ (-want +got):\n%s", diff)
	}
}

// TestDeletePointRebuildsAffectedDays pins that a delete rebuilds the
// deleted point's own day and the day of whatever point is now next after
// it in time order — NextTimestamp is expected to already reflect its
// absence, which [fakePoints.existing] models simply by never having
// contained the deleted point's own timestamp.
func TestDeletePointRebuildsAffectedDays(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	points := &fakePoints{
		deleteResult: day1.Add(time.Hour),
		existing:     map[int64][]time.Time{1: {day2.Add(10 * time.Minute)}},
	}
	st := &fakeStore{points: points, dailyStats: dailyStats}

	if err := ingest.DeletePoint(t.Context(), st, 1, 1, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("DeletePoint returned %v", err)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestDeletePointReportsFailures is [TestUpdatePointReportsFailures] for
// DeletePoint.
func TestDeletePointReportsFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		points     *fakePoints
		dailyStats *fakeDailyStats
		tracks     *fakeTracks
	}{
		"Delete fails": {
			points:     &fakePoints{deleteErr: errFake},
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
		"Enqueue fails": {
			points:     &fakePoints{},
			dailyStats: &fakeDailyStats{},
			tracks:     &fakeTracks{enqueueErr: errFake},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var events []string
			tt.dailyStats.events = &events

			st := &fakeStore{points: tt.points, dailyStats: tt.dailyStats, tracks: tt.tracks}

			if err := ingest.DeletePoint(t.Context(), st, 1, 1, time.UTC, 30*time.Minute); !errors.Is(err, errFake) {
				t.Errorf("DeletePoint returned %v, want it to wrap %v", err, errFake)
			}
		})
	}
}

// TestDeletePointEnqueuesTrackRebuild pins that a delete enqueues a track
// rebuild for the deleted point's owner.
func TestDeletePointEnqueuesTrackRebuild(t *testing.T) {
	t.Parallel()

	var events []string

	tracks := &fakeTracks{}
	st := &fakeStore{
		points:     &fakePoints{deleteResult: time.Now()},
		dailyStats: &fakeDailyStats{events: &events},
		tracks:     tracks,
	}

	if err := ingest.DeletePoint(t.Context(), st, 1, 1, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("DeletePoint returned %v", err)
	}

	if diff := cmp.Diff([]int64{1}, tracks.enqueued); diff != "" {
		t.Errorf("enqueued users differ (-want +got):\n%s", diff)
	}
}

// TestDeletePointsRebuildsEachAffectedDayOnce pins that a bulk delete
// spanning two distinct days rebuilds exactly those two days, deduplicated —
// the same grouping [ingest.CreatePoints] exercises, over DeleteBulk's own
// reported timestamps this time.
func TestDeletePointsRebuildsEachAffectedDayOnce(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	var events []string

	dailyStats := &fakeDailyStats{events: &events}
	points := &fakePoints{
		deleteBulkResult: []time.Time{day1.Add(time.Hour), day2.Add(30 * time.Minute)},
	}
	st := &fakeStore{points: points, dailyStats: dailyStats}

	deleted, err := ingest.DeletePoints(t.Context(), st, 1, []int64{1, 2}, time.UTC, 30*time.Minute)
	if err != nil {
		t.Fatalf("DeletePoints returned %v", err)
	}

	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	want := []rebuildCall{
		{UserID: 1, Day: day1, TrackBreak: 30 * time.Minute},
		{UserID: 1, Day: day2, TrackBreak: 30 * time.Minute},
	}
	if diff := cmp.Diff(want, dailyStats.rebuilt); diff != "" {
		t.Errorf("Rebuild calls differ (-want +got):\n%s", diff)
	}
}

// TestDeletePointsReturnsCountActuallyDeleted pins that the count
// DeletePoints answers with is however many DeleteBulk actually removed,
// not the number of ids it was asked to delete — the two differ whenever an
// id does not exist or belongs to another user, which DeleteBulk silently
// skips rather than treating as an error.
func TestDeletePointsReturnsCountActuallyDeleted(t *testing.T) {
	t.Parallel()

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	var events []string

	st := &fakeStore{
		points:     &fakePoints{deleteBulkResult: []time.Time{day.Add(time.Hour)}},
		dailyStats: &fakeDailyStats{events: &events},
	}

	deleted, err := ingest.DeletePoints(t.Context(), st, 1, []int64{1, 2, 3}, time.UTC, 30*time.Minute)
	if err != nil {
		t.Fatalf("DeletePoints returned %v", err)
	}

	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only what DeleteBulk actually removed)", deleted)
	}
}

// TestDeletePointsReportsFailures is [TestUpdatePointReportsFailures] for
// DeletePoints.
func TestDeletePointsReportsFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		points     *fakePoints
		dailyStats *fakeDailyStats
		tracks     *fakeTracks
	}{
		"DeleteBulk fails": {
			points:     &fakePoints{deleteBulkErr: errFake},
			dailyStats: &fakeDailyStats{},
		},
		"Rebuild fails": {
			points:     &fakePoints{deleteBulkResult: []time.Time{time.Now()}},
			dailyStats: &fakeDailyStats{rebuildErr: errFake},
		},
		"Enqueue fails": {
			points:     &fakePoints{deleteBulkResult: []time.Time{time.Now()}},
			dailyStats: &fakeDailyStats{},
			tracks:     &fakeTracks{enqueueErr: errFake},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var events []string
			tt.dailyStats.events = &events

			st := &fakeStore{points: tt.points, dailyStats: tt.dailyStats, tracks: tt.tracks}

			if _, err := ingest.DeletePoints(t.Context(), st, 1, []int64{1}, time.UTC, 30*time.Minute); !errors.Is(err, errFake) {
				t.Errorf("DeletePoints returned %v, want it to wrap %v", err, errFake)
			}
		})
	}
}

// TestDeletePointsEnqueuesTrackRebuild pins that a bulk delete enqueues one
// track rebuild for the caller's user, regardless of how many points it
// touched.
func TestDeletePointsEnqueuesTrackRebuild(t *testing.T) {
	t.Parallel()

	var events []string

	tracks := &fakeTracks{}
	st := &fakeStore{
		points:     &fakePoints{deleteBulkResult: []time.Time{time.Now(), time.Now().Add(time.Hour)}},
		dailyStats: &fakeDailyStats{events: &events},
		tracks:     tracks,
	}

	if _, err := ingest.DeletePoints(t.Context(), st, 1, []int64{1, 2}, time.UTC, 30*time.Minute); err != nil {
		t.Fatalf("DeletePoints returned %v", err)
	}

	if diff := cmp.Diff([]int64{1}, tracks.enqueued); diff != "" {
		t.Errorf("enqueued users differ (-want +got):\n%s", diff)
	}
}
