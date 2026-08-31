package httpapi

import (
	"net/http"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
)

// getMobileSettings answers GET /api/v1/settings/mobile.
func (a *api) getMobileSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("mobile settings were reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	settings, err := a.store.Settings().Get(r.Context(), user.ID)
	if err != nil {
		a.logger.Error("loading mobile settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.MobileSettingsEnvelope{
		Settings:  mobileSettingsToDTO(settings),
		UpdatedAt: formatTimestamp(settings.UpdatedAt),
		Status:    "success",
	})
}

// patchMobileSettings answers PATCH /api/v1/settings/mobile: a field the body
// carries overwrites the stored value, and one it omits is left as it was.
// [dto.MobileSettingsUpdate] accepts both forms the published spec's own
// schema and example disagree on.
func (a *api) patchMobileSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a mobile settings update was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	var req dto.MobileSettingsUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("a mobile settings body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	current, err := a.store.Settings().Get(r.Context(), user.ID)
	if err != nil {
		a.logger.Error("loading mobile settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	updated := applyMobileSettingsUpdate(current, req)

	if message, ok := validateMobileSettings(updated); !ok {
		a.writeError(w, r, http.StatusUnprocessableEntity, message)

		return
	}

	stored, err := a.store.Settings().Update(r.Context(), updated)
	if err != nil {
		a.logger.Error("storing mobile settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.MobileSettingsEnvelope{
		Settings:  mobileSettingsToDTO(stored),
		UpdatedAt: formatTimestamp(stored.UpdatedAt),
		Status:    "success",
		Message:   "Settings updated",
	})
}

// applyMobileSettingsUpdate overlays the fields req carries onto current,
// leaving every field it omits as it was.
func applyMobileSettingsUpdate(current model.Settings, req dto.MobileSettingsUpdate) model.Settings {
	updated := current

	if req.TrackingMode != nil {
		updated.TrackingMode = *req.TrackingMode
	}

	if req.TrackingVisits != nil {
		updated.TrackingVisits = *req.TrackingVisits
	}

	if req.TrackVisitsIndependently != nil {
		updated.TrackVisitsIndependently = *req.TrackVisitsIndependently
	}

	if req.AutoStart != nil {
		updated.AutoStart = *req.AutoStart
	}

	if req.DistanceFilter != nil {
		updated.DistanceFilter = *req.DistanceFilter
	}

	if req.TimeFilter != nil {
		updated.TimeFilter = *req.TimeFilter
	}

	if req.TrackBreak != nil {
		updated.TrackBreak = *req.TrackBreak
	}

	if req.Accuracy != nil {
		updated.Accuracy = *req.Accuracy
	}

	if req.ShowBackgroundLocationIndicator != nil {
		updated.ShowBackgroundLocationIndicator = *req.ShowBackgroundLocationIndicator
	}

	if req.UploadAutomatically != nil {
		updated.UploadAutomatically = *req.UploadAutomatically
	}

	if req.UploadAllOnTrackingStop != nil {
		updated.UploadAllOnTrackingStop = *req.UploadAllOnTrackingStop
	}

	if req.BatchSize != nil {
		updated.BatchSize = *req.BatchSize
	}

	return updated
}

// validateMobileSettings reports the first field of s outside the range
// GET/PATCH /api/v1/settings/mobile documents for it, or ("", true) if every
// field is in range.
func validateMobileSettings(s model.Settings) (string, bool) {
	switch {
	case s.TrackingMode != "precise" && s.TrackingMode != "significant":
		return "invalid tracking_mode", false
	case s.DistanceFilter < 1 || s.DistanceFilter > 10000:
		return "invalid distance_filter", false
	case s.TimeFilter < 1 || s.TimeFilter > 3600:
		return "invalid time_filter", false
	case s.TrackBreak < 1 || s.TrackBreak > 1440:
		return "invalid track_break", false
	case s.Accuracy < 1 || s.Accuracy > 6:
		return "invalid accuracy", false
	case s.BatchSize < 1 || s.BatchSize > 1000:
		return "invalid batch_size", false
	default:
		return "", true
	}
}

// mobileSettingsToDTO converts s into the shape
// GET/PATCH /api/v1/settings/mobile answers with.
func mobileSettingsToDTO(s model.Settings) dto.MobileSettings {
	return dto.MobileSettings{
		TrackingMode:                    s.TrackingMode,
		TrackingVisits:                  s.TrackingVisits,
		TrackVisitsIndependently:        s.TrackVisitsIndependently,
		AutoStart:                       s.AutoStart,
		DistanceFilter:                  s.DistanceFilter,
		TimeFilter:                      s.TimeFilter,
		TrackBreak:                      s.TrackBreak,
		Accuracy:                        s.Accuracy,
		ShowBackgroundLocationIndicator: s.ShowBackgroundLocationIndicator,
		UploadAutomatically:             s.UploadAutomatically,
		UploadAllOnTrackingStop:         s.UploadAllOnTrackingStop,
		BatchSize:                       s.BatchSize,
	}
}

// getSettings answers GET /api/v1/settings.
func (a *api) getSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("settings were reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	settings, err := a.store.Settings().Get(r.Context(), user.ID)
	if err != nil {
		a.logger.Error("loading settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.SettingsEnvelope{Settings: settingsToDTO(settings)})
}

// patchSettings answers PATCH /api/v1/settings: a field the body carries
// overwrites the stored value, and one it omits is left as it was.
// immich_url, immich_api_key, photoprism_url and photoprism_api_key are
// accepted, per [decodeJSON] ignoring unknown fields, but stored nowhere:
// see [dto.Settings].
//
// The spec documents no response body for this one ("settings updated", no
// schema), so it answers `{}`.
func (a *api) patchSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		// requireUser is what makes this unreachable; see usersMe for why
		// answering anything but an error here would be wrong regardless.
		a.logger.Error("a settings update was reached without an authenticated user",
			"method", r.Method,
			"path", r.URL.Path,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	var req dto.SettingsUpdate
	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("a settings body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	current, err := a.store.Settings().Get(r.Context(), user.ID)
	if err != nil {
		a.logger.Error("loading settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	updated := applySettingsUpdate(current, req)

	if _, err := a.store.Settings().Update(r.Context(), updated); err != nil {
		a.logger.Error("storing settings failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.writeJSON(w, r, http.StatusOK, struct{}{})
}

// applySettingsUpdate overlays the fields req carries onto current, leaving
// every field it omits as it was. RouteOpacity is scaled ÷100: req carries
// the 0-100 percentage PATCH /api/v1/settings speaks, and current stores the
// fraction GET /api/v1/users/me's settings.route_opacity also uses.
func applySettingsUpdate(current model.Settings, req dto.SettingsUpdate) model.Settings {
	updated := current

	if req.RouteOpacity != nil {
		updated.RouteOpacity = *req.RouteOpacity / 100
	}

	if req.MetersBetweenRoutes != nil {
		updated.MetersBetweenRoutes = *req.MetersBetweenRoutes
	}

	if req.MinutesBetweenRoutes != nil {
		updated.MinutesBetweenRoutes = *req.MinutesBetweenRoutes
	}

	if req.FogOfWarMeters != nil {
		updated.FogOfWarMeters = *req.FogOfWarMeters
	}

	if req.TimeThresholdMinutes != nil {
		updated.TimeThresholdMinutes = *req.TimeThresholdMinutes
	}

	if req.MergeThresholdMinutes != nil {
		updated.MergeThresholdMinutes = *req.MergeThresholdMinutes
	}

	if req.PreferredMapLayer != nil {
		updated.PreferredMapLayer = *req.PreferredMapLayer
	}

	if req.SpeedColoredRoutes != nil {
		updated.SpeedColoredRoutes = *req.SpeedColoredRoutes
	}

	if req.PointsRenderingMode != nil {
		updated.PointsRenderingMode = *req.PointsRenderingMode
	}

	if req.LiveMapEnabled != nil {
		updated.LiveMapEnabled = *req.LiveMapEnabled
	}

	if req.SpeedColorScale != nil {
		updated.SpeedColorScale = req.SpeedColorScale
	}

	if req.FogOfWarThreshold != nil {
		updated.FogOfWarThreshold = *req.FogOfWarThreshold
	}

	if req.Maps != nil && req.Maps.DistanceUnit != nil {
		updated.DistanceUnit = *req.Maps.DistanceUnit
	}

	return updated
}

// settingsToDTO converts s into the shape GET /api/v1/settings answers with.
// RouteOpacity is scaled ×100 — see [applySettingsUpdate] — and ImmichURL,
// ImmichAPIKey, PhotoprismURL and PhotoprismAPIKey stay nil: see
// [dto.Settings].
func settingsToDTO(s model.Settings) dto.Settings {
	return dto.Settings{
		RouteOpacity:          s.RouteOpacity * 100,
		MetersBetweenRoutes:   s.MetersBetweenRoutes,
		MinutesBetweenRoutes:  s.MinutesBetweenRoutes,
		FogOfWarMeters:        s.FogOfWarMeters,
		TimeThresholdMinutes:  s.TimeThresholdMinutes,
		MergeThresholdMinutes: s.MergeThresholdMinutes,
		PreferredMapLayer:     s.PreferredMapLayer,
		SpeedColoredRoutes:    s.SpeedColoredRoutes,
		PointsRenderingMode:   s.PointsRenderingMode,
		LiveMapEnabled:        s.LiveMapEnabled,
		SpeedColorScale:       s.SpeedColorScale,
		FogOfWarThreshold:     s.FogOfWarThreshold,
		Maps:                  dto.MapsPref{DistanceUnit: s.DistanceUnit},
	}
}
