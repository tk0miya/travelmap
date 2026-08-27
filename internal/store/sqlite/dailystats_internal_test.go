package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/geo"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// dailyStatsTestUser creates a fresh user for a daily_stats test, so that two
// subtests inserting points at the same timestamps never collide on the
// (user_id, timestamp) unique index.
func dailyStatsTestUser(t *testing.T, db *DB, email string) int64 {
	t.Helper()

	user, err := db.Users().Create(t.Context(), testUser(email))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	return user.ID
}

// insertPoint stores one point at (lat, lon) and ts, for a test to build the
// points a rebuild reads.
func insertPoint(t *testing.T, db *DB, userID int64, ts time.Time, lat, lon float64) {
	t.Helper()

	point := model.Point{UserID: userID, Timestamp: ts, Latitude: lat, Longitude: lon}

	if _, err := db.Points().Create(t.Context(), []model.Point{point}); err != nil {
		t.Fatalf("inserting a point: %v", err)
	}
}

// TestDailyStatsRebuild pins the plain case: a day with two points sums to
// the Haversine distance between them — the check against got.KM below is
// also the agreement test between the SQL formula and internal/geo's own.
func TestDailyStatsRebuild(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := dailyStatsTestUser(t, db, "rebuild@example.com")

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	const (
		tokyoLat, tokyoLon = 35.6812, 139.7671
		osakaLat, osakaLon = 34.7025, 135.4959
	)

	insertPoint(t, db, userID, day.Add(1*time.Hour), tokyoLat, tokyoLon)
	insertPoint(t, db, userID, day.Add(2*time.Hour), osakaLat, osakaLon)

	if err := db.DailyStats().Rebuild(t.Context(), userID, day, 2*time.Hour); err != nil {
		t.Fatalf("Rebuild returned %v", err)
	}

	got, err := db.DailyStats().Get(t.Context(), userID, day)
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}

	if got.Points != 2 {
		t.Errorf("Points = %d, want 2", got.Points)
	}

	want := geo.Haversine(tokyoLat, tokyoLon, osakaLat, osakaLon)
	if diff := got.KM - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("KM = %v, want %v (the SQL and Go Haversine must agree)", got.KM, want)
	}

	if got.ReverseGeocodedPoints != 0 {
		t.Errorf("ReverseGeocodedPoints = %d, want 0: reverse geocoding is off by default", got.ReverseGeocodedPoints)
	}

	if len(got.Countries) != 0 || len(got.Cities) != 0 {
		t.Errorf("Countries = %v, Cities = %v, want both empty", got.Countries, got.Cities)
	}
}

// TestDailyStatsRebuildDeletesTheEmptiedDay pins that rebuilding a day that
// has no points removes any row already there instead of leaving one behind
// at zero.
func TestDailyStatsRebuildDeletesTheEmptiedDay(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := dailyStatsTestUser(t, db, "emptied@example.com")

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	// A stray row planted directly, standing in for what a full rebuild
	// after a change of TRAVELMAP_TIMEZONE would otherwise leave behind: no
	// point in this database maps to this day any more.
	if _, err := db.q.ExecContext(t.Context(),
		`INSERT INTO daily_stats (user_id, day, points, reverse_geocoded_points, km, countries, cities)
		 VALUES (?, ?, 1, 0, 12.5, '[]', '[]')`,
		userID, day.Format(dayFormat),
	); err != nil {
		t.Fatalf("planting the stray row: %v", err)
	}

	if err := db.DailyStats().Rebuild(t.Context(), userID, day, 30*time.Minute); err != nil {
		t.Fatalf("Rebuild returned %v", err)
	}

	if _, err := db.DailyStats().Get(t.Context(), userID, day); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}

// TestDailyStatsRebuildTrackBreakBoundary pins the boundary of
// TRAVELMAP_TRACK_BREAK_MINUTES: a segment of exactly the configured gap is
// counted, and one second more is not, which is what catches a ">" versus
// ">=" mix-up in the SQL.
func TestDailyStatsRebuildTrackBreakBoundary(t *testing.T) {
	t.Parallel()

	const (
		lat1, lon1 = 35.6812, 139.7671
		lat2, lon2 = 35.6586, 139.7454
	)

	trackBreak := 30 * time.Minute

	tests := map[string]struct {
		gap      time.Duration
		wantSome bool
	}{
		"exactly the break is counted": {gap: trackBreak, wantSome: true},
		"one second over is not":       {gap: trackBreak + time.Second, wantSome: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)
			userID := dailyStatsTestUser(t, db, "boundary-"+name+"@example.com")

			day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

			insertPoint(t, db, userID, day.Add(time.Hour), lat1, lon1)
			insertPoint(t, db, userID, day.Add(time.Hour).Add(tt.gap), lat2, lon2)

			if err := db.DailyStats().Rebuild(t.Context(), userID, day, trackBreak); err != nil {
				t.Fatalf("Rebuild returned %v", err)
			}

			got, err := db.DailyStats().Get(t.Context(), userID, day)
			if err != nil {
				t.Fatalf("Get returned %v", err)
			}

			if tt.wantSome && got.KM <= 0 {
				t.Errorf("KM = %v, want the segment counted (> 0)", got.KM)
			}

			if !tt.wantSome && got.KM != 0 {
				t.Errorf("KM = %v, want the segment excluded (0)", got.KM)
			}
		})
	}
}

// TestDailyStatsRebuildCrossMidnightSegment pins that the distance between
// the previous day's last point and the current day's first point is
// attributed to the current day — an agreement test between a full and an
// incremental rebuild cannot catch this, because both would drop the segment
// the same way if it were missed.
func TestDailyStatsRebuildCrossMidnightSegment(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := dailyStatsTestUser(t, db, "midnight@example.com")

	day := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)

	const (
		lastLat, lastLon = 35.6812, 139.7671
		nextLat, nextLon = 35.6586, 139.7454
	)

	// The previous day's last point, ten minutes before midnight.
	insertPoint(t, db, userID, day.Add(-10*time.Minute), lastLat, lastLon)
	// This day's first point, five minutes after midnight.
	insertPoint(t, db, userID, day.Add(5*time.Minute), nextLat, nextLon)

	if err := db.DailyStats().Rebuild(t.Context(), userID, day, 30*time.Minute); err != nil {
		t.Fatalf("Rebuild returned %v", err)
	}

	got, err := db.DailyStats().Get(t.Context(), userID, day)
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}

	want := geo.Haversine(lastLat, lastLon, nextLat, nextLon)
	if diff := got.KM - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("KM = %v, want %v (the cross-midnight segment)", got.KM, want)
	}

	if got.Points != 1 {
		t.Errorf("Points = %d, want 1: the previous day's point does not belong to this day", got.Points)
	}
}

// TestDailyStatsGetNotFound pins that a day with no row at all — never
// rebuilt, or rebuilt down to zero — is reported as [store.ErrNotFound]
// rather than a zeroed [model.DailyStat].
func TestDailyStatsGetNotFound(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := dailyStatsTestUser(t, db, "not-found@example.com")

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	if _, err := db.DailyStats().Get(t.Context(), userID, day); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}

// TestDailyStatsDeleteAll pins that DeleteAll clears every user's rows, which
// a full recalculation relies on before rebuilding under a changed
// TRAVELMAP_TIMEZONE or TRAVELMAP_TRACK_BREAK_MINUTES.
func TestDailyStatsDeleteAll(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := dailyStatsTestUser(t, db, "delete-all@example.com")

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	insertPoint(t, db, userID, day.Add(time.Hour), 35.6812, 139.7671)

	if err := db.DailyStats().Rebuild(t.Context(), userID, day, 30*time.Minute); err != nil {
		t.Fatalf("Rebuild returned %v", err)
	}

	if err := db.DailyStats().DeleteAll(t.Context()); err != nil {
		t.Fatalf("DeleteAll returned %v", err)
	}

	if _, err := db.DailyStats().Get(t.Context(), userID, day); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}
