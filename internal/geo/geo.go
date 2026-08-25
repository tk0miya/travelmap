package geo

import "math"

// EarthRadiusKm is the mean radius of the Earth in kilometres, used by
// [Haversine]. The SQL rebuild query in internal/store/sqlite computes the
// same formula and is handed this same constant, per "Distance calculation"
// in TODO.md, rather than carrying the literal in two places where it could
// drift.
const EarthRadiusKm = 6371.0088

// Haversine returns the great-circle distance in kilometres between two
// points given as decimal-degree coordinates.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }

	dLat := rad(lat2 - lat1)
	dLon := rad(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)

	return EarthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
