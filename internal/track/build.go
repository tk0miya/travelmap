package track

import (
	"time"

	"github.com/tk0miya/travelmap/internal/geo"
	"github.com/tk0miya/travelmap/internal/model"
)

// build splits points — one user's whole history, already ordered by
// timestamp ascending — into tracks, starting a new one wherever the gap to
// the previous point exceeds trackBreak.
//
// A run of a single point is not a track: a GeoJSON LineString needs at
// least two coordinates, and a lone point measures no distance or duration.
// It is dropped rather than kept as a degenerate one-point track.
func build(points []model.Point, trackBreak time.Duration) []model.Track {
	var (
		tracks  []model.Track
		current []model.Point
		prev    *model.Point
	)

	flush := func() {
		if t, ok := finish(current); ok {
			tracks = append(tracks, t)
		}

		current = nil
	}

	for i, p := range points {
		if prev != nil && p.Timestamp.Sub(prev.Timestamp) > trackBreak {
			flush()
		}

		current = append(current, p)
		prev = &points[i]
	}

	flush()

	return tracks
}

// finish turns one run of points into the [model.Track] it describes,
// reporting false for a run of fewer than two points.
func finish(points []model.Point) (model.Track, bool) {
	if len(points) < 2 {
		return model.Track{}, false
	}

	geometry := make([]model.Coordinate, len(points))

	var (
		distanceKm float64
		prev       *model.Point
	)

	for i, p := range points {
		geometry[i] = model.Coordinate{Longitude: p.Longitude, Latitude: p.Latitude}

		if prev != nil {
			distanceKm += geo.Haversine(prev.Latitude, prev.Longitude, p.Latitude, p.Longitude)
		}

		prev = &points[i]
	}

	return model.Track{
		UserID:         points[0].UserID,
		StartAt:        points[0].Timestamp,
		EndAt:          points[len(points)-1].Timestamp,
		DistanceMeters: distanceKm * 1000,
		Geometry:       geometry,
	}, true
}
