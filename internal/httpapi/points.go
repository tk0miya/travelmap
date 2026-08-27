package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// createPoints answers POST /api/v1/points: a GeoJSON batch from the app
// becomes points belonging to the authenticated user.
//
// It shares its body with [api.createOverlandBatch] and differs only in the
// success status: 200 here, 201 there. See the POST /api/v1/points bullet
// under "Per-endpoint" in TODO.md.
func (a *api) createPoints(w http.ResponseWriter, r *http.Request) {
	a.ingestLocations(w, r, http.StatusOK)
}

// createOverlandBatch answers POST /api/v1/overland/batches. See
// [api.createPoints].
func (a *api) createOverlandBatch(w http.ResponseWriter, r *http.Request) {
	a.ingestLocations(w, r, http.StatusCreated)
}

// ingestLocations decodes a GeoJSON locations batch, stores the points it
// carries for the authenticated user, and answers with success on ok.
//
// It writes through [store.Store.Points] directly rather than through
// internal/ingest: there is no daily_stats yet for a mutation to keep in sync.
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

	var created int

	err := a.store.Tx(r.Context(), func(ctx context.Context, tx store.Store) error {
		var err error
		created, err = tx.Points().Create(ctx, points)

		return err
	})
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
