package timeline_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store/storetest"
	"github.com/tk0miya/travelmap/internal/timeline"
)

// TestEntriesAssemblesARange pins the round trip: check-ins stored in a
// range come back as entries ordered by CheckedInAt.
func TestEntriesAssemblesARange(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	start := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

	for i, at := range []time.Time{start, start.Add(time.Hour)} {
		_, err := st.Checkins().Upsert(t.Context(), model.Checkin{
			UserID:              1,
			FoursquareCheckinID: fmt.Sprintf("entries-%d", i),
			CheckedInAt:         at,
			Source:              "push",
			Raw:                 "{}",
		})
		if err != nil {
			t.Fatalf("upserting a check-in: %v", err)
		}
	}

	entries, err := timeline.Entries(t.Context(), st, 1, start, start.Add(2*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("Entries returned %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if !entries[0].At.Equal(start) || !entries[1].At.Equal(start.Add(time.Hour)) {
		t.Errorf("entries = %+v, want ordered by CheckedInAt starting at %v", entries, start)
	}
}

// TestEntriesFailuresReportTheUnderlyingError pins that a database that
// cannot reach either table Entries reads reports a failure, rather than
// hanging or panicking — the path a future handler turns into a 500.
func TestEntriesFailuresReportTheUnderlyingError(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	t.Run("checkins unavailable", func(t *testing.T) {
		t.Parallel()

		st := storetest.UnavailableCheckins(t, testUser())

		if _, err := timeline.Entries(t.Context(), st, 1, from, to, time.UTC); err == nil {
			t.Error("Entries returned nil for a store that cannot reach checkins")
		}
	})

	t.Run("points unavailable", func(t *testing.T) {
		t.Parallel()

		st := storetest.UnavailablePoints(t, testUser())

		if _, err := timeline.Entries(t.Context(), st, 1, from, to, time.UTC); err == nil {
			t.Error("Entries returned nil for a store that cannot reach points")
		}
	})
}
