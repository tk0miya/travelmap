package geo_test

import (
	"math"
	"testing"

	"github.com/tk0miya/travelmap/internal/geo"
)

// TestHaversineSamePointIsZero pins the degenerate case: no movement is no
// distance, whatever the coordinates.
func TestHaversineSamePointIsZero(t *testing.T) {
	t.Parallel()

	if got := geo.Haversine(35.6812, 139.7671, 35.6812, 139.7671); got != 0 {
		t.Errorf("Haversine(same point) = %v, want 0", got)
	}
}

// TestHaversineKnownDistance pins the formula against a distance that can be
// checked independently: Tokyo Station to Osaka Station is approximately
// 400 km great-circle.
func TestHaversineKnownDistance(t *testing.T) {
	t.Parallel()

	const (
		tokyoLat, tokyoLon = 35.6812, 139.7671
		osakaLat, osakaLon = 34.7025, 135.4959
	)

	got := geo.Haversine(tokyoLat, tokyoLon, osakaLat, osakaLon)

	const want = 403.0

	if math.Abs(got-want) > 5 {
		t.Errorf("Haversine(Tokyo, Osaka) = %v, want approximately %v", got, want)
	}
}

// TestHaversineIsSymmetric pins that the order of the two points does not
// matter, which the SQL rebuild query relies on: it always orders by
// timestamp, never by which point came first in the request.
func TestHaversineIsSymmetric(t *testing.T) {
	t.Parallel()

	a := geo.Haversine(35.6812, 139.7671, 34.7025, 135.4959)
	b := geo.Haversine(34.7025, 135.4959, 35.6812, 139.7671)

	if a != b {
		t.Errorf("Haversine(a, b) = %v, Haversine(b, a) = %v, want equal", a, b)
	}
}
