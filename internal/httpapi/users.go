package httpapi

import (
	"net/http"
	"time"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
)

// timestampFormat is how a time is written into a response body: RFC 3339 with
// milliseconds, which is what upstream's JSON encoder produces. A client
// parsing with a fixed format string would fail on a value without them, so
// the zeroes are sent rather than dropped.
const timestampFormat = "2006-01-02T15:04:05.000Z07:00"

// The settings GET /api/v1/users/me reports.
//
// Nothing here is stored yet: these are upstream's own defaults, from
// Users::SafeSettings::DEFAULT_VALUES, so that a client reading one finds the
// value it would find against a fresh upstream instance. The ones that will
// become real are noted in the steps that make them so — Step 8 for the
// timezone, Step 16 for the settings endpoints; the map and route-drawing ones
// belong to the web UI, and the Immich and Photoprism URLs to a non-goal, so
// they stay as they are.
const (
	defaultTimezone                 = "UTC"
	defaultDistanceUnit             = "km"
	defaultFogOfWarMeters           = 50
	defaultMetersBetweenRoutes      = 500
	defaultPreferredMapLayer        = "OpenStreetMap"
	defaultSpeedColoredRoutes       = false
	defaultPointsRenderingMode      = "raw"
	defaultMinutesBetweenRoutes     = 30
	defaultTimeThresholdMinutes     = 30
	defaultMergeThresholdMinutes    = 15
	defaultLiveMapEnabled           = true
	defaultRouteOpacity             = 0.6
	defaultVisitsSuggestionsEnabled = true
	defaultFogOfWarThreshold        = 50
	defaultGlobeProjection          = true
)

// defaultTheme is the web UI's colour scheme. It sits outside the block above
// because it is not one of the settings: upstream keeps it in a column of its
// own on the user, and it is reported beside the settings rather than inside
// them. Nothing here stores one, so it is upstream's default until the
// Milestone H web UI has a setting to report.
const defaultTheme = "dark"

// usersMe answers GET /api/v1/users/me with the account the request
// authenticated as. It is the endpoint a client calls to confirm that the API
// key it was configured with belongs to somebody.
func (a *api) usersMe(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable. Getting here means the
		// route was registered outside it, which is a bug in the router — and
		// answering anything but an error would mean answering with an empty
		// account.
		a.logger.Error("users/me was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.UserEnvelope{User: dto.User{
		Email:     user.Email,
		Theme:     defaultTheme,
		CreatedAt: formatTimestamp(user.CreatedAt),
		UpdatedAt: formatTimestamp(user.UpdatedAt),
		Settings: dto.UserSettings{
			Timezone:                 defaultTimezone,
			Maps:                     dto.MapsPref{DistanceUnit: defaultDistanceUnit},
			FogOfWarMeters:           defaultFogOfWarMeters,
			MetersBetweenRoutes:      defaultMetersBetweenRoutes,
			PreferredMapLayer:        defaultPreferredMapLayer,
			SpeedColoredRoutes:       defaultSpeedColoredRoutes,
			PointsRenderingMode:      defaultPointsRenderingMode,
			MinutesBetweenRoutes:     defaultMinutesBetweenRoutes,
			TimeThresholdMinutes:     defaultTimeThresholdMinutes,
			MergeThresholdMinutes:    defaultMergeThresholdMinutes,
			LiveMapEnabled:           defaultLiveMapEnabled,
			RouteOpacity:             defaultRouteOpacity,
			VisitsSuggestionsEnabled: defaultVisitsSuggestionsEnabled,
			FogOfWarThreshold:        defaultFogOfWarThreshold,
			GlobeProjection:          defaultGlobeProjection,
		},
	}})
}

// formatTimestamp writes a time the way the API reports one: in UTC, so that
// two servers configured differently report the same instant the same way.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(timestampFormat)
}
