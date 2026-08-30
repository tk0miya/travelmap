package track

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
)

// at builds a minimal point for build's tests: userID and coordinates are
// arbitrary except where a test cares about them.
func at(userID int64, ts time.Time, lat, lon float64) model.Point {
	return model.Point{UserID: userID, Timestamp: ts, Latitude: lat, Longitude: lon}
}

// TestBuildSplitsOnAGapExceedingTrackBreak pins the ordinary case: two points
// close together are one track, and a gap past trackBreak starts a new one.
func TestBuildSplitsOnAGapExceedingTrackBreak(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	trackBreak := 30 * time.Minute

	points := []model.Point{
		at(1, start, 35.00, 139.00),
		at(1, start.Add(10*time.Minute), 35.01, 139.01),
		// Past the break: a new track.
		at(1, start.Add(2*time.Hour), 36.00, 140.00),
		at(1, start.Add(2*time.Hour+10*time.Minute), 36.01, 140.01),
	}

	tracks := build(points, trackBreak)

	if len(tracks) != 2 {
		t.Fatalf("len(tracks) = %d, want 2", len(tracks))
	}

	if !tracks[0].StartAt.Equal(points[0].Timestamp) || !tracks[0].EndAt.Equal(points[1].Timestamp) {
		t.Errorf("track[0] = [%s, %s], want [%s, %s]",
			tracks[0].StartAt, tracks[0].EndAt, points[0].Timestamp, points[1].Timestamp)
	}

	if !tracks[1].StartAt.Equal(points[2].Timestamp) || !tracks[1].EndAt.Equal(points[3].Timestamp) {
		t.Errorf("track[1] = [%s, %s], want [%s, %s]",
			tracks[1].StartAt, tracks[1].EndAt, points[2].Timestamp, points[3].Timestamp)
	}
}

// TestBuildExactlyAtTheBreakStaysOneTrack pins the boundary: a gap equal to
// trackBreak does not split, matching daily_stats' own "<=" segment
// attribution — only a gap exceeding it does.
func TestBuildExactlyAtTheBreakStaysOneTrack(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	trackBreak := 30 * time.Minute

	points := []model.Point{
		at(1, start, 35.00, 139.00),
		at(1, start.Add(trackBreak), 35.01, 139.01),
	}

	tracks := build(points, trackBreak)

	if len(tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(tracks))
	}
}

// TestBuildDropsALonePoint pins that a run of a single point — a gap before
// and after exceeding trackBreak — is not kept as a degenerate track: a
// GeoJSON LineString needs at least two coordinates.
func TestBuildDropsALonePoint(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	trackBreak := 30 * time.Minute

	points := []model.Point{
		at(1, start, 35.00, 139.00),
		at(1, start.Add(time.Hour), 36.00, 140.00),
		at(1, start.Add(2*time.Hour), 37.00, 141.00),
	}

	tracks := build(points, trackBreak)

	if len(tracks) != 0 {
		t.Fatalf("len(tracks) = %d, want 0 (every point isolated)", len(tracks))
	}
}

// TestBuildComputesDistanceAndGeometry pins that the geometry is every
// point's coordinate in order, and the distance is the sum of the
// great-circle distance between consecutive points, in metres.
func TestBuildComputesDistanceAndGeometry(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	points := []model.Point{
		at(1, start, 35.6586, 139.7454),
		at(1, start.Add(time.Minute), 35.6595, 139.7454),
	}

	tracks := build(points, 30*time.Minute)
	if len(tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(tracks))
	}

	got := tracks[0]

	wantGeometry := []model.Coordinate{
		{Longitude: 139.7454, Latitude: 35.6586},
		{Longitude: 139.7454, Latitude: 35.6595},
	}
	if diff := cmp.Diff(wantGeometry, got.Geometry); diff != "" {
		t.Errorf("geometry differs (-want +got):\n%s", diff)
	}

	// About 100 metres apart (0.0009 degrees of latitude), so this pins the
	// right order of magnitude rather than an exact figure computed the same
	// way the code under test computes it.
	if got.DistanceMeters < 50 || got.DistanceMeters > 150 {
		t.Errorf("DistanceMeters = %v, want roughly 100", got.DistanceMeters)
	}
}

// TestBuildEmpty pins that no points produce no tracks, not a nil-vs-empty
// panic or a degenerate entry.
func TestBuildEmpty(t *testing.T) {
	t.Parallel()

	if tracks := build(nil, 30*time.Minute); len(tracks) != 0 {
		t.Errorf("len(tracks) = %d, want 0", len(tracks))
	}
}

// TestBuildSetsUserID pins that a track carries the user id of the points it
// was built from.
func TestBuildSetsUserID(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	points := []model.Point{
		at(7, start, 1, 1),
		at(7, start.Add(time.Minute), 2, 2),
	}

	tracks := build(points, 30*time.Minute)
	if len(tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(tracks))
	}

	if tracks[0].UserID != 7 {
		t.Errorf("UserID = %d, want 7", tracks[0].UserID)
	}
}
