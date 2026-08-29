package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/ingest"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// createPoints answers POST /api/v1/points: a GeoJSON batch from the app
// becomes points belonging to the authenticated user.
//
// It shares its body with [api.createOverlandBatch] and differs only in the
// success status: 200 here, 201 there.
func (a *api) createPoints(w http.ResponseWriter, r *http.Request) {
	a.ingestLocations(w, r, http.StatusOK)
}

// createOverlandBatch answers POST /api/v1/overland/batches. See
// [api.createPoints].
func (a *api) createOverlandBatch(w http.ResponseWriter, r *http.Request) {
	a.ingestLocations(w, r, http.StatusCreated)
}

// ingestLocations decodes a GeoJSON locations batch, stores the points it
// carries for the authenticated user through [ingest.CreatePoints], and
// answers with success on ok.
func (a *api) ingestLocations(w http.ResponseWriter, r *http.Request, success int) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("points ingest was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	var req dto.LocationsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("a locations body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	points := parseLocations(req, user.ID)

	created, err := ingest.CreatePoints(r.Context(), a.store, points, a.loc, a.trackBreak)
	if err != nil {
		a.logger.Error("storing points failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, success, dto.LocationsCreated{Created: created})
}

// parseLocations converts req into the points it describes for user, silently
// dropping a Feature whose coordinates or timestamp cannot be read.
//
// Upstream does the same (Points::Params#params_valid?): a batch is a stream
// of device samples, and refusing the whole batch for one bad sample would
// lose the rest of it too.
func parseLocations(req dto.LocationsRequest, userID int64) []model.Point {
	points := make([]model.Point, 0, len(req.Locations))

	for _, feature := range req.Locations {
		point, ok := pointFromFeature(feature, userID)
		if !ok {
			continue
		}

		points = append(points, point)
	}

	return points
}

// timestampLayouts are the encodings a Feature's `timestamp` property has been
// seen arriving in: RFC 3339 (colon in the zone offset), which is what
// Dawarich's own examples use, and the original Overland iOS app's own layout
// (no colon, e.g. "2015-10-01T08:00:00-0700" — see its README's example
// payload), which /api/v1/overland/batches exists to accept. Go's Z0700
// reference also matches a literal "Z", so this one layout alone would cover
// both zero-offset forms; RFC3339Nano stays first because it is the
// documented format and covers a fractional second either layout after it
// would also parse.
var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999Z0700",
}

// parseTimestamp reads a Feature's `timestamp` property, trying every layout
// in [timestampLayouts] in turn.
func parseTimestamp(s string) (time.Time, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

// pointFromFeature converts one GeoJSON Feature into a [model.Point] for
// userID, reporting false if it carries no usable coordinates or timestamp.
//
// SpeedAccuracy and TrackID are accepted but not stored: upstream itself does
// not persist them to a column either, keeping them only in the raw device
// payload it archives and this server does not.
func pointFromFeature(f dto.Feature, userID int64) (model.Point, bool) {
	// GeoJSON allows a third coordinate (elevation), which is ignored here:
	// altitude is read from Properties.Altitude instead, matching upstream.
	if len(f.Geometry.Coordinates) < 2 {
		return model.Point{}, false
	}

	timestamp, ok := parseTimestamp(f.Properties.Timestamp)
	if !ok {
		return model.Point{}, false
	}

	return model.Point{
		UserID:    userID,
		Timestamp: timestamp,
		Longitude: f.Geometry.Coordinates[0],
		Latitude:  f.Geometry.Coordinates[1],

		Altitude:         f.Properties.Altitude,
		Velocity:         f.Properties.Speed,
		Accuracy:         f.Properties.HorizontalAccuracy,
		VerticalAccuracy: f.Properties.VerticalAccuracy,
		Course:           f.Properties.Course,
		CourseAccuracy:   f.Properties.CourseAccuracy,
		BatteryStatus:    f.Properties.BatteryState,
		Battery:          f.Properties.BatteryLevel,
		SSID:             f.Properties.Wifi,
		TrackerID:        f.Properties.DeviceID,
	}, true
}

// defaultPointsPerPage is what per_page defaults to when absent or not a
// positive integer, matching upstream's own controller default.
const defaultPointsPerPage = 100

// listPoints answers GET /api/v1/points: the authenticated user's points,
// optionally narrowed by start_at/end_at, paginated by page/per_page, sorted
// per the order parameter.
func (a *api) listPoints(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("points listing was reached without an authenticated user",
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

	// Upstream defaults end_at to the request time rather than leaving the
	// range open-ended, so a client that never sends it still gets an upper
	// bound.
	end := time.Now().UTC()
	if endAt != nil {
		end = *endAt
	}

	page := positiveInt(q.Get("page"), 1)
	perPage := positiveInt(q.Get("per_page"), defaultPointsPerPage)
	ascending := q.Get("order") == "asc"

	points, total, err := a.store.Points().List(r.Context(), user.ID, startAt, end, ascending, page, perPage)
	if err != nil {
		a.logger.Error("listing points failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	// Integer ceiling division: perPage is always positive, from
	// [positiveInt] above, so this never divides by zero.
	totalPages := (total + perPage - 1) / perPage

	w.Header().Set("X-Current-Page", strconv.Itoa(page))
	w.Header().Set("X-Total-Pages", strconv.Itoa(totalPages))

	a.writeJSON(w, r, http.StatusOK, pointsToDTO(points))
}

// parsePointID reads the {id} path parameter PATCH and DELETE
// /api/v1/points/{id} take. The published spec types it as an opaque
// string, but every id this server hands out is one of its own
// AUTOINCREMENT primary keys, so anything that does not parse as one can
// never match a stored point — reported the same way [ingest.UpdatePoint]
// and [ingest.DeletePoint] report an id that parses but does not exist.
func parsePointID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)

	return id, err == nil
}

// patchPoint answers PATCH /api/v1/points/{id}: the body's latitude and
// longitude, wrapped in a top-level "point" object per upstream's own
// request shape, overwrite the point's coordinates. The timestamp is never
// part of the request and never changes.
func (a *api) patchPoint(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a point update was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	id, ok := parsePointID(r)
	if !ok {
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	}

	var req dto.PointUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("a point update body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	if req.Point.Latitude == nil || req.Point.Longitude == nil {
		a.writeError(w, r, http.StatusUnprocessableEntity, "invalid point")

		return
	}

	updated, err := ingest.UpdatePoint(
		r.Context(), a.store, user.ID, id, *req.Point.Latitude, *req.Point.Longitude, a.loc, a.trackBreak,
	)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.writeError(w, r, http.StatusNotFound, "not found")
	case err != nil:
		a.logger.Error("updating a point failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")
	default:
		a.writeJSON(w, r, http.StatusOK, pointToDTO(updated))
	}
}

// deletePoint answers DELETE /api/v1/points/{id}.
func (a *api) deletePoint(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a point deletion was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	id, ok := parsePointID(r)
	if !ok {
		a.writeError(w, r, http.StatusNotFound, "not found")

		return
	}

	err := ingest.DeletePoint(r.Context(), a.store, user.ID, id, a.loc, a.trackBreak)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.writeError(w, r, http.StatusNotFound, "not found")
	case err != nil:
		a.logger.Error("deleting a point failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")
	default:
		a.writeJSON(w, r, http.StatusOK, dto.PointDeleted{Message: "Point deleted successfully"})
	}
}

// bulkDeletePoints answers DELETE /api/v1/points/bulk_destroy: point_ids
// that do not exist, or belong to another user, are silently skipped rather
// than failing the request — matching upstream's own
// `where(id: point_ids).destroy_all` scoping.
func (a *api) bulkDeletePoints(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a bulk point deletion was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	var req dto.PointsBulkDeleteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("a bulk point deletion body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	if len(req.PointIDs) == 0 {
		a.writeError(w, r, http.StatusUnprocessableEntity, "no points selected")

		return
	}

	deleted, err := ingest.DeletePoints(r.Context(), a.store, user.ID, req.PointIDs, a.loc, a.trackBreak)
	if err != nil {
		a.logger.Error("bulk deleting points failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.PointsBulkDeleted{
		Message: "Points were successfully destroyed",
		Count:   deleted,
	})
}

// positiveInt parses s as a positive integer, reporting def for anything
// else — absent, not a number, zero or negative — matching upstream's own
// `to_i; default unless positive?` handling of page and per_page.
func positiveInt(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}

	return n
}

// parseQueryTime reads a start_at/end_at query value, reporting (nil, true)
// for one left empty. Besides RFC 3339 and a bare date, a purely numeric
// value is read as Unix seconds — upstream's own SafeTimestampParser treats
// one as such deliberately, not as a leniency, so a client already relying on
// it is not left with a 400 when it works upstream.
func parseQueryTime(s string) (*time.Time, bool) {
	if s == "" {
		return nil, true
	}

	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		t := time.Unix(secs, 0).UTC()

		return &t, true
	}

	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()

			return &t, true
		}
	}

	return nil, false
}

// pointsToDTO converts points into the shape GET /api/v1/points answers with,
// in the same order. It never returns nil: an empty result is still a JSON
// array, since a client that gets `null` where it expects an array of points
// would fail iterating it rather than seeing an empty list.
func pointsToDTO(points []model.Point) []dto.Point {
	out := make([]dto.Point, 0, len(points))

	for _, p := range points {
		out = append(out, pointToDTO(p))
	}

	return out
}

// pointToDTO converts one point into the shape [pointsToDTO] answers with —
// also what PATCH /api/v1/points/{id} answers with, upstream's own
// point_serializer.new(point.reload).call.
func pointToDTO(p model.Point) dto.Point {
	return dto.Point{
		ID: p.ID,

		Latitude:  formatFloat(p.Latitude),
		Longitude: formatFloat(p.Longitude),
		Timestamp: p.Timestamp.Unix(),

		Altitude:         p.Altitude,
		Velocity:         formatFloatPtr(p.Velocity),
		Accuracy:         p.Accuracy,
		VerticalAccuracy: p.VerticalAccuracy,
		Course:           formatFloatPtr(p.Course),
		CourseAccuracy:   formatFloatPtr(p.CourseAccuracy),
		BatteryStatus:    p.BatteryStatus,
		Battery:          p.Battery,
		SSID:             p.SSID,
		TrackerID:        p.TrackerID,

		CreatedAt: formatTimestamp(p.CreatedAt),
		UpdatedAt: formatTimestamp(p.UpdatedAt),
		UserID:    p.UserID,

		InRegions: []string{},
		InRIDs:    []string{},
		Geodata:   map[string]any{},
	}
}

// formatFloat renders v the way upstream's serializer renders a coordinate:
// as a string rather than a JSON number. [strconv.FormatFloat]'s shortest
// round-trippable form is not a byte-for-byte match for Ruby's Float#to_s
// (which always keeps a ".0" on a whole number, for one) — nothing here
// parses this back as a float to check — but it round-trips through the same
// `strconv.ParseFloat`/Dart `double.parse` a client would use, which is what
// compatibility actually rests on.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatFloatPtr is [formatFloat] for a device property that may not have
// been reported.
func formatFloatPtr(v *float64) *string {
	if v == nil {
		return nil
	}

	s := formatFloat(*v)

	return &s
}
