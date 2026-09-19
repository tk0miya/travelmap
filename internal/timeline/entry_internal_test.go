package timeline

import (
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
)

// checkinAt builds a check-in for assemble's tests: only CheckedInAt and
// TimezoneOffset matter to it, everything else is arbitrary.
func checkinAt(at time.Time, offsetMinutes *int) model.Checkin {
	return model.Checkin{
		UserID:              1,
		FoursquareCheckinID: at.String(),
		CheckedInAt:         at,
		TimezoneOffset:      offsetMinutes,
		Source:              "push",
		Raw:                 `{}`,
	}
}

// pointAt builds a point for assemble's tests: only Timestamp and
// coordinates matter to it.
func pointAt(at time.Time, lat, lon float64) model.Point {
	return model.Point{UserID: 1, Timestamp: at, Latitude: lat, Longitude: lon}
}

// TestAssembleOneEntryPerCheckin pins the ordinary case: two check-ins
// produce two entries, the first carrying the elapsed time and the distance
// summed from the point recorded between them, the last carrying neither
// since it has no next entry to measure against.
func TestAssembleOneEntryPerCheckin(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

	checkins := []model.Checkin{
		checkinAt(start, nil),
		checkinAt(start.Add(time.Hour), nil),
	}
	points := []model.Point{
		pointAt(start.Add(10*time.Minute), 35.6586, 139.7454),
		pointAt(start.Add(20*time.Minute), 35.6595, 139.7454),
	}

	entries := assemble(sources{Checkins: checkins, Points: points}, time.UTC)

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if entries[0].Elapsed != time.Hour {
		t.Errorf("entries[0].Elapsed = %v, want 1h", entries[0].Elapsed)
	}

	// About 100 metres apart (0.0009 degrees of latitude), so this pins the
	// right order of magnitude rather than an exact figure computed the same
	// way the code under test computes it.
	if entries[0].DistanceMeters < 50 || entries[0].DistanceMeters > 150 {
		t.Errorf("entries[0].DistanceMeters = %v, want roughly 100", entries[0].DistanceMeters)
	}

	if entries[1].Elapsed != 0 || entries[1].DistanceMeters != 0 {
		t.Errorf("entries[1] = %+v, want the last entry to carry neither", entries[1])
	}
}

// TestAssembleACheckinWithNoPointsAround pins that a check-in with nothing
// recorded around it still gets an entry, with zero distance rather than a
// panic or a missing entry.
func TestAssembleACheckinWithNoPointsAround(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

	checkins := []model.Checkin{
		checkinAt(start, nil),
		checkinAt(start.Add(time.Hour), nil),
	}

	entries := assemble(sources{Checkins: checkins}, time.UTC)

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if entries[0].Elapsed != time.Hour {
		t.Errorf("entries[0].Elapsed = %v, want 1h", entries[0].Elapsed)
	}

	if entries[0].DistanceMeters != 0 {
		t.Errorf("entries[0].DistanceMeters = %v, want 0", entries[0].DistanceMeters)
	}
}

// TestAssemblePointsWithNoCheckin pins that points with no check-in at all —
// before the only one, after the only one, or the whole set when there are
// none — produce no entry and are otherwise silently dropped.
func TestAssemblePointsWithNoCheckin(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

	t.Run("no check-ins at all", func(t *testing.T) {
		t.Parallel()

		points := []model.Point{pointAt(start, 35.0, 139.0)}

		if entries := assemble(sources{Points: points}, time.UTC); len(entries) != 0 {
			t.Errorf("len(entries) = %d, want 0", len(entries))
		}
	})

	t.Run("points before the first and after the last", func(t *testing.T) {
		t.Parallel()

		checkins := []model.Checkin{checkinAt(start, nil)}
		points := []model.Point{
			pointAt(start.Add(-time.Hour), 35.0, 139.0),
			pointAt(start.Add(time.Hour), 36.0, 140.0),
		}

		entries := assemble(sources{Checkins: checkins, Points: points}, time.UTC)

		if len(entries) != 1 {
			t.Fatalf("len(entries) = %d, want 1", len(entries))
		}

		if entries[0].DistanceMeters != 0 {
			t.Errorf("DistanceMeters = %v, want 0 (no next check-in to bound an interval with)", entries[0].DistanceMeters)
		}
	})
}

// TestLocalTimeCrossesMidnight pins that a check-in's own TimezoneOffset can
// move At onto a different calendar day than its UTC instant.
func TestLocalTimeCrossesMidnight(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.June, 1, 23, 50, 0, 0, time.UTC)
	offset := 600 // UTC+10

	got := localTime(at, &offset, time.UTC)

	if got.Day() != 2 {
		t.Errorf("Day() = %d, want 2 (past midnight in UTC+10)", got.Day())
	}

	if !got.Equal(at) {
		t.Errorf("got %v, want the same instant as %v", got, at)
	}
}

// TestLocalTimeFallsBackWithNoOffset pins that a check-in with no
// TimezoneOffset renders in the caller's fallback location.
func TestLocalTimeFallsBackWithNoOffset(t *testing.T) {
	t.Parallel()

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("loading Asia/Tokyo: %v", err)
	}

	at := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)

	got := localTime(at, nil, tokyo)

	if got.Location() != tokyo {
		t.Errorf("Location() = %v, want %v", got.Location(), tokyo)
	}

	if !got.Equal(at) {
		t.Errorf("got %v, want the same instant as %v", got, at)
	}
}
