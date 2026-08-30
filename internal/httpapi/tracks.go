package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// trackColor is what every track's "color" property answers. This server
// infers no transport mode to color-code by — dominant_mode and
// dominant_mode_emoji are always null, for the same reason — so there is
// nothing for the color to vary with.
const trackColor = "#3B82F6"

// listTracks answers GET /api/v1/tracks: the authenticated user's tracks,
// optionally narrowed by start_at/end_at, paginated by page/per_page, as a
// GeoJSON FeatureCollection.
func (a *api) listTracks(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("tracks listing was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	q := r.URL.Query()

	startAt, ok := parseQueryTime(q.Get("start_at"))
	if !ok {
		a.writeError(w, r, http.StatusBadRequest, "invalid start_at")

		return
	}

	endAt, ok := parseQueryTime(q.Get("end_at"))
	if !ok {
		a.writeError(w, r, http.StatusBadRequest, "invalid end_at")

		return
	}

	page := positiveInt(q.Get("page"), 1)
	perPage := positiveInt(q.Get("per_page"), defaultPointsPerPage)

	tracks, total, err := a.store.Tracks().List(r.Context(), user.ID, startAt, endAt, page, perPage)
	if err != nil {
		a.logger.Error("listing tracks failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	// Integer ceiling division: perPage is always positive, from
	// [positiveInt], so this never divides by zero.
	totalPages := (total + perPage - 1) / perPage

	w.Header().Set("X-Current-Page", strconv.Itoa(page))
	w.Header().Set("X-Total-Pages", strconv.Itoa(totalPages))

	a.writeJSON(w, r, http.StatusOK, tracksToDTO(tracks))
}

// parseIDParam reads the path parameter name as one of this server's own
// AUTOINCREMENT primary keys — the same reasoning as [parsePointID], for a
// route whose id path parameter is not named "id".
func parseIDParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)

	return id, err == nil
}

// getTrack answers GET /api/v1/tracks/{id}: one track, as a GeoJSON
// FeatureCollection of a single Feature — the same per-feature shape
// GET /api/v1/tracks answers with.
func (a *api) getTrack(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("getting a track was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	id, ok := parseIDParam(r, "id")
	if !ok {
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	}

	t, err := a.store.Tracks().ByID(r.Context(), user.ID, id)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.writeError(w, r, http.StatusNotFound, "not found")
	case err != nil:
		a.logger.Error("getting a track failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")
	default:
		a.writeJSON(w, r, http.StatusOK, dto.TracksResponse{
			Type:     "FeatureCollection",
			Features: []dto.TrackFeature{trackToFeature(t)},
		})
	}
}

// trackPoints answers GET /api/v1/tracks/{track_id}/points: the points
// belonging to one of the authenticated user's tracks, ordered by timestamp
// ascending, paginated by page/per_page — a narrower shape than
// GET /api/v1/points, matching upstream's own documented schema for this
// endpoint.
//
// A track's points are exactly [store.PointRepository.List]'s result for its
// own [StartAt, EndAt] range: internal/track builds every track from the
// same ordered walk over one user's points, so no other track's points ever
// fall inside another track's own span.
func (a *api) trackPoints(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a track's points were reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	trackID, ok := parseIDParam(r, "track_id")
	if !ok {
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	}

	t, err := a.store.Tracks().ByID(r.Context(), user.ID, trackID)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	case err != nil:
		a.logger.Error("finding the track for its points failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	q := r.URL.Query()
	page := positiveInt(q.Get("page"), 1)
	perPage := positiveInt(q.Get("per_page"), defaultPointsPerPage)

	points, total, err := a.store.Points().List(r.Context(), user.ID, &t.StartAt, t.EndAt, true, page, perPage)
	if err != nil {
		a.logger.Error("listing a track's points failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	// Integer ceiling division: perPage is always positive, from
	// [positiveInt], so this never divides by zero.
	totalPages := (total + perPage - 1) / perPage

	w.Header().Set("X-Current-Page", strconv.Itoa(page))
	w.Header().Set("X-Total-Pages", strconv.Itoa(totalPages))

	a.writeJSON(w, r, http.StatusOK, trackPointsToDTO(points))
}

// tracksToDTO converts tracks into the shape GET /api/v1/tracks answers
// with. It never returns a nil Features slice, for the reason
// [pointsToDTO] does not return a nil slice.
func tracksToDTO(tracks []model.Track) dto.TracksResponse {
	features := make([]dto.TrackFeature, 0, len(tracks))

	for _, t := range tracks {
		features = append(features, trackToFeature(t))
	}

	return dto.TracksResponse{Type: "FeatureCollection", Features: features}
}

// trackToFeature converts one track into the GeoJSON Feature
// [tracksToDTO]/[api.getTrack] answer with. Duration and avg_speed are
// derived here rather than stored: both follow from StartAt, EndAt and
// DistanceMeters alone.
func trackToFeature(t model.Track) dto.TrackFeature {
	coordinates := make([][]float64, len(t.Geometry))
	for i, c := range t.Geometry {
		coordinates[i] = []float64{c.Longitude, c.Latitude}
	}

	// Never zero for a stored track: [build] only keeps a run of two or more
	// points, each at a distinct timestamp (points_user_id_timestamp_key).
	duration := int64(t.EndAt.Sub(t.StartAt).Seconds())

	return dto.TrackFeature{
		Type:     "Feature",
		Geometry: dto.TrackGeometry{Type: "LineString", Coordinates: coordinates},
		Properties: dto.TrackProperties{
			ID:       t.ID,
			Color:    trackColor,
			StartAt:  formatTimestamp(t.StartAt),
			EndAt:    formatTimestamp(t.EndAt),
			Distance: t.DistanceMeters,
			AvgSpeed: (t.DistanceMeters / 1000) / (float64(duration) / 3600),
			Duration: duration,
		},
	}
}

// trackPointsToDTO converts points into the shape
// GET /api/v1/tracks/{track_id}/points answers with. CountryName is always
// nil: reverse geocoding is not implemented yet, the same reason
// GET /api/v1/points' own City and Country fields are always nil too.
func trackPointsToDTO(points []model.Point) []dto.TrackPoint {
	out := make([]dto.TrackPoint, 0, len(points))

	for _, p := range points {
		out = append(out, dto.TrackPoint{
			ID: p.ID,

			Latitude:  formatFloat(p.Latitude),
			Longitude: formatFloat(p.Longitude),
			Timestamp: p.Timestamp.Unix(),

			Velocity:    p.Velocity,
			CountryName: nil,
		})
	}

	return out
}
