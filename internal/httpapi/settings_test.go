package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// TestGetMobileSettingsDefaults covers a user who has never PATCHed a
// setting: [model.DefaultSettings], stamped with the account's own
// registration time.
func TestGetMobileSettingsDefaults(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/settings/mobile?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	assertGolden(t, "settings_mobile_default.json", resp.body)
}

// TestGetMobileSettingsRequiresAuthentication pins that GET is behind
// requireUser like every other route serving one account's data.
func TestGetMobileSettingsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/settings/mobile")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// mobileSettingsBody is what a test decodes a settings/mobile response into.
type mobileSettingsBody struct {
	Settings struct {
		TrackingMode                    string `json:"tracking_mode"`
		TrackingVisits                  bool   `json:"tracking_visits"`
		TrackVisitsIndependently        bool   `json:"track_visits_independently"`
		AutoStart                       bool   `json:"auto_start"`
		DistanceFilter                  int    `json:"distance_filter"`
		TimeFilter                      int    `json:"time_filter"`
		TrackBreak                      int    `json:"track_break"`
		Accuracy                        int    `json:"accuracy"`
		ShowBackgroundLocationIndicator bool   `json:"show_background_location_indicator"`
		UploadAutomatically             bool   `json:"upload_automatically"`
		UploadAllOnTrackingStop         bool   `json:"upload_all_on_tracking_stop"`
		BatchSize                       int    `json:"batch_size"`
	} `json:"settings"`
	UpdatedAt string `json:"updated_at"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// TestPatchMobileSettings covers the happy path for both request shapes the
// published spec's schema and example disagree on: flat fields at the top
// level, and the same fields wrapped in "settings".
func TestPatchMobileSettings(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"flat":    `{"tracking_mode": "significant", "distance_filter": 250, "batch_size": 50}`,
		"wrapped": `{"settings": {"tracking_mode": "significant", "distance_filter": 250, "batch_size": 50}}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile?api_key="+testAPIKey, withBody(body))

			if resp.status != http.StatusOK {
				t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
			}

			var got mobileSettingsBody
			if err := json.Unmarshal(resp.body, &got); err != nil {
				t.Fatalf("decoding the response body: %v", err)
			}

			if got.Settings.TrackingMode != "significant" {
				t.Errorf("tracking_mode = %q, want %q", got.Settings.TrackingMode, "significant")
			}

			if got.Settings.DistanceFilter != 250 {
				t.Errorf("distance_filter = %d, want %d", got.Settings.DistanceFilter, 250)
			}

			if got.Settings.BatchSize != 50 {
				t.Errorf("batch_size = %d, want %d", got.Settings.BatchSize, 50)
			}

			// A field the request omitted keeps its default rather than
			// zeroing.
			if got.Settings.TimeFilter != 10 {
				t.Errorf("time_filter = %d, want the default %d unchanged", got.Settings.TimeFilter, 10)
			}

			if got.UpdatedAt == "" {
				t.Errorf("updated_at is empty, want it stamped")
			}

			if got.Status != "success" {
				t.Errorf("status = %q, want %q", got.Status, "success")
			}

			if got.Message != "Settings updated" {
				t.Errorf("message = %q, want %q", got.Message, "Settings updated")
			}
		})
	}
}

// TestPatchMobileSettingsPersists pins that a PATCH is reflected by a later
// GET, not just in the PATCH's own response.
func TestPatchMobileSettingsPersists(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	patch := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile?api_key="+testAPIKey,
		withBody(`{"accuracy": 6}`))
	if patch.status != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d (body: %s)", patch.status, http.StatusOK, patch.body)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/settings/mobile?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	var got mobileSettingsBody
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	if got.Settings.Accuracy != 6 {
		t.Errorf("accuracy = %d, want %d", got.Settings.Accuracy, 6)
	}

	if got.UpdatedAt == "" {
		t.Errorf("updated_at is empty, want it stamped")
	}
}

// TestPatchMobileSettingsInvalidBody pins that a body the decoder cannot read
// is a bad request, the same answer every other PATCH endpoint gives.
func TestPatchMobileSettingsInvalidBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile?api_key="+testAPIKey, withBody(`{"tracking_mode":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestPatchMobileSettingsValidation covers the range/enum a field outside it
// is refused with, one field at a time so the message names the field that
// actually failed.
func TestPatchMobileSettingsValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want string
	}{
		"tracking_mode":   {body: `{"tracking_mode": "sometimes"}`, want: "invalid tracking_mode"},
		"distance_filter": {body: `{"distance_filter": 0}`, want: "invalid distance_filter"},
		"time_filter":     {body: `{"time_filter": 3601}`, want: "invalid time_filter"},
		"track_break":     {body: `{"track_break": 1441}`, want: "invalid track_break"},
		"accuracy":        {body: `{"accuracy": 7}`, want: "invalid accuracy"},
		"batch_size":      {body: `{"batch_size": 1001}`, want: "invalid batch_size"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile?api_key="+testAPIKey, withBody(tt.body))

			if resp.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusUnprocessableEntity, resp.body)
			}

			var got struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(resp.body, &got); err != nil {
				t.Fatalf("decoding the response body: %v", err)
			}

			if got.Error != tt.want {
				t.Errorf("error = %q, want %q", got.Error, tt.want)
			}
		})
	}
}

// TestPatchMobileSettingsRequiresAuthentication pins that PATCH is behind
// requireUser like every other route serving one account's data.
func TestPatchMobileSettingsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile", withBody(`{"accuracy": 1}`))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestGetMobileSettingsStoreFailure covers what a client sees when the store
// cannot be read, rather than a 200 with a made-up result.
func TestGetMobileSettingsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.UnavailableSettings(t, testUser(t)))
	resp := do(t, srv, http.MethodGet, "/api/v1/settings/mobile?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestPatchMobileSettingsStoreFailure covers what a client sees when the
// store cannot be written to, rather than a 200 claiming a change that was
// never saved.
func TestPatchMobileSettingsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.UnavailableSettings(t, testUser(t)))
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings/mobile?api_key="+testAPIKey, withBody(`{"accuracy": 1}`))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestGetSettingsDefaults covers a user who has never PATCHed a setting:
// [model.DefaultSettings].
func TestGetSettingsDefaults(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/settings?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	assertGolden(t, "settings_default.json", resp.body)
}

// TestGetSettingsRequiresAuthentication pins that GET is behind requireUser
// like every other route serving one account's data.
func TestGetSettingsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/settings")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// settingsBody is what a test decodes a GET /api/v1/settings response into.
type settingsBody struct {
	Settings struct {
		RouteOpacity      float64 `json:"route_opacity"`
		PreferredMapLayer string  `json:"preferred_map_layer"`
		ImmichURL         *string `json:"immich_url"`
		ImmichAPIKey      *string `json:"immich_api_key"`
		PhotoprismURL     *string `json:"photoprism_url"`
		PhotoprismAPIKey  *string `json:"photoprism_api_key"`
		Maps              struct {
			DistanceUnit string `json:"distance_unit"`
		} `json:"maps"`
	} `json:"settings"`
}

// TestPatchSettings covers the happy path for both request shapes — flat and
// "settings"-wrapped — the same contradiction settings/mobile's spec has,
// and that route_opacity is accepted as the 0-100 percentage this endpoint
// speaks rather than the fraction GET /api/v1/users/me reports.
func TestPatchSettings(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"flat":    `{"route_opacity": 25, "preferred_map_layer": "Satellite", "maps": {"distance_unit": "mi"}}`,
		"wrapped": `{"settings": {"route_opacity": 25, "preferred_map_layer": "Satellite", "maps": {"distance_unit": "mi"}}}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)

			patch := do(t, srv, http.MethodPatch, "/api/v1/settings?api_key="+testAPIKey, withBody(body))
			if patch.status != http.StatusOK {
				t.Fatalf("PATCH status = %d, want %d (body: %s)", patch.status, http.StatusOK, patch.body)
			}

			if got, want := string(patch.body), "{}\n"; got != want {
				t.Errorf("PATCH body = %q, want %q", got, want)
			}

			resp := do(t, srv, http.MethodGet, "/api/v1/settings?api_key="+testAPIKey)
			if resp.status != http.StatusOK {
				t.Fatalf("GET status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
			}

			var got settingsBody
			if err := json.Unmarshal(resp.body, &got); err != nil {
				t.Fatalf("decoding the response body: %v", err)
			}

			if got.Settings.RouteOpacity != 25 {
				t.Errorf("route_opacity = %v, want %v", got.Settings.RouteOpacity, 25)
			}

			if got.Settings.PreferredMapLayer != "Satellite" {
				t.Errorf("preferred_map_layer = %q, want %q", got.Settings.PreferredMapLayer, "Satellite")
			}

			if got.Settings.Maps.DistanceUnit != "mi" {
				t.Errorf("maps.distance_unit = %q, want %q", got.Settings.Maps.DistanceUnit, "mi")
			}
		})
	}
}

// TestPatchSettingsIgnoresNonGoalFields pins that immich_url, immich_api_key,
// photoprism_url and photoprism_api_key are accepted without failing the
// request, but change nothing: that integration is a declared non-goal.
func TestPatchSettingsIgnoresNonGoalFields(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	body := `{
		"immich_url": "https://immich.example.com",
		"immich_api_key": "secret",
		"photoprism_url": "https://photoprism.example.com",
		"photoprism_api_key": "secret"
	}`

	patch := do(t, srv, http.MethodPatch, "/api/v1/settings?api_key="+testAPIKey, withBody(body))
	if patch.status != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d (body: %s)", patch.status, http.StatusOK, patch.body)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/settings?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	var got settingsBody
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	if got.Settings.ImmichURL != nil {
		t.Errorf("immich_url = %q, want nil", *got.Settings.ImmichURL)
	}

	if got.Settings.ImmichAPIKey != nil {
		t.Errorf("immich_api_key = %q, want nil", *got.Settings.ImmichAPIKey)
	}

	if got.Settings.PhotoprismURL != nil {
		t.Errorf("photoprism_url = %q, want nil", *got.Settings.PhotoprismURL)
	}

	if got.Settings.PhotoprismAPIKey != nil {
		t.Errorf("photoprism_api_key = %q, want nil", *got.Settings.PhotoprismAPIKey)
	}
}

// TestPatchSettingsInvalidBody pins that a body the decoder cannot read is a
// bad request, the same answer every other PATCH endpoint gives.
func TestPatchSettingsInvalidBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings?api_key="+testAPIKey, withBody(`{"route_opacity":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestPatchSettingsRequiresAuthentication pins that PATCH is behind
// requireUser like every other route serving one account's data.
func TestPatchSettingsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings", withBody(`{"route_opacity": 50}`))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestGetSettingsStoreFailure covers what a client sees when the store
// cannot be read, rather than a 200 with a made-up result.
func TestGetSettingsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.UnavailableSettings(t, testUser(t)))
	resp := do(t, srv, http.MethodGet, "/api/v1/settings?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestPatchSettingsStoreFailure covers what a client sees when the store
// cannot be written to, rather than a 200 claiming a change that was never
// saved.
func TestPatchSettingsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, storetest.UnavailableSettings(t, testUser(t)))
	resp := do(t, srv, http.MethodPatch, "/api/v1/settings?api_key="+testAPIKey, withBody(`{"route_opacity": 50}`))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestUsersMeReflectsPatchedSettings pins that GET /api/v1/users/me is fed
// from the same stored settings PATCH /api/v1/settings writes, rather than
// constants that would never change no matter what was PATCHed.
func TestUsersMeReflectsPatchedSettings(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	patch := do(t, srv, http.MethodPatch, "/api/v1/settings?api_key="+testAPIKey,
		withBody(`{"preferred_map_layer": "Satellite", "maps": {"distance_unit": "mi"}}`))
	if patch.status != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d (body: %s)", patch.status, http.StatusOK, patch.body)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/users/me?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	var envelope struct {
		User struct {
			Settings struct {
				PreferredMapLayer string `json:"preferred_map_layer"`
				Maps              struct {
					DistanceUnit string `json:"distance_unit"`
				} `json:"maps"`
			} `json:"settings"`
		} `json:"user"`
	}
	if err := json.Unmarshal(resp.body, &envelope); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	got := envelope.User

	if got.Settings.PreferredMapLayer != "Satellite" {
		t.Errorf("settings.preferred_map_layer = %q, want %q", got.Settings.PreferredMapLayer, "Satellite")
	}

	if got.Settings.Maps.DistanceUnit != "mi" {
		t.Errorf("settings.maps.distance_unit = %q, want %q", got.Settings.Maps.DistanceUnit, "mi")
	}
}
