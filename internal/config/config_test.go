package config_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/config"
)

// The values every test below uses for the settings [config.Load] requires:
// their content does not matter to what each test is pinning, only that
// they are present.
const (
	requiredBaseURL      = "https://travelmap.example"
	requiredClientID     = "the-client-id"
	requiredClientSecret = "the-client-secret"
	requiredPushSecret   = "the-push-secret"
)

// requiredTOML is the required settings on their own, for a test whose
// content does not otherwise touch [server] or [foursquare].
const requiredTOML = `
[server]
base_url = "https://travelmap.example"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`

// writeConfig writes content to a travelmap.toml in a fresh temporary
// directory and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "travelmap.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	return path
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
		want    config.Config
	}{
		"defaults when only the required settings are given": {
			content: requiredTOML,
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
				SessionLifetime: 720 * time.Hour, SessionCookieSecure: true,
				FoursquareSyncLookbackDays: 14, FoursquareAPIURL: "https://api.foursquare.com",
				FoursquareSyncInterval: time.Hour,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
		"every setting given": {
			content: `
[server]
addr = "127.0.0.1:8080"
base_url = "https://travelmap.example"
log_level = "debug"
debug_log_requests = true

[database]
path = "/var/lib/travelmap/travelmap.db"

[tracking]
timezone = "Asia/Tokyo"
track_break_minutes = 45

[session]
lifetime = "168h"
cookie_secure = false

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "shh-its-a-secret"
api_url = "http://127.0.0.1:9999"
sync_interval = "30m"
sync_lookback_days = 30
`,
			want: config.Config{
				Addr:                   "127.0.0.1:8080",
				LogLevel:               slog.LevelDebug,
				DatabasePath:           "/var/lib/travelmap/travelmap.db",
				DebugLogRequests:       true,
				Timezone:               "Asia/Tokyo",
				TrackBreakMinutes:      45,
				FoursquarePushSecret:   "shh-its-a-secret",
				FoursquareClientID:     "the-client-id",
				FoursquareClientSecret: "the-client-secret",
				BaseURL:                "https://travelmap.example",
				SessionLifetime:        168 * time.Hour,
				SessionCookieSecure:    false,

				FoursquareSyncLookbackDays: 30,
				FoursquareAPIURL:           "http://127.0.0.1:9999",
				FoursquareSyncInterval:     30 * time.Minute,
			},
		},
		// Info is where the request log writes, so a level above it would
		// have answered the switch with an empty capture.
		"the request log holds the log level down to info": {
			content: `
[server]
base_url = "https://travelmap.example"
log_level = "error"
debug_log_requests = true

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`,
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelInfo,
				DatabasePath:      "travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
				SessionLifetime:   720 * time.Hour, SessionCookieSecure: true,

				FoursquareSyncLookbackDays: 14,
				FoursquareAPIURL:           "https://api.foursquare.com",
				FoursquareSyncInterval:     time.Hour,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
		// Down, not to: a level below Info is what an operator debugging
		// something else asked for, and the request log comes through it.
		"the request log leaves a lower level alone": {
			content: `
[server]
base_url = "https://travelmap.example"
log_level = "debug"
debug_log_requests = true

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`,
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelDebug,
				DatabasePath:      "travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
				SessionLifetime:   720 * time.Hour, SessionCookieSecure: true,

				FoursquareSyncLookbackDays: 14,
				FoursquareAPIURL:           "https://api.foursquare.com",
				FoursquareSyncInterval:     time.Hour,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
		// The off switch is a setting an operator writes down, not just the
		// absence of one: a config that turns the log off has to keep it
		// off, and keep the level it asked for.
		"the request log switched off explicitly": {
			content: `
[server]
base_url = "https://travelmap.example"
log_level = "error"
debug_log_requests = false

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`,
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelError,
				DatabasePath:      "travelmap.db",
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
				SessionLifetime:   720 * time.Hour, SessionCookieSecure: true,

				FoursquareSyncLookbackDays: 14,
				FoursquareAPIURL:           "https://api.foursquare.com",
				FoursquareSyncInterval:     time.Hour,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
		"the log level is case-insensitive": {
			content: `
[server]
base_url = "https://travelmap.example"
log_level = "WARN"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`,
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelWarn, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
				SessionLifetime: 720 * time.Hour, SessionCookieSecure: true,
				FoursquareSyncLookbackDays: 14, FoursquareAPIURL: "https://api.foursquare.com",
				FoursquareSyncInterval: time.Hour,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
		// Unlike the durations above, 0 is a valid value here: it switches the
		// periodic fetch off rather than meaning "immediately" or "never
		// expires", which is why it gets its own case instead of a rejection
		// test.
		"a zero sync interval disables the periodic fetch": {
			content: `
[server]
base_url = "https://travelmap.example"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
sync_interval = "0"
`,
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
				SessionLifetime: 720 * time.Hour, SessionCookieSecure: true,
				FoursquareSyncLookbackDays: 14, FoursquareAPIURL: "https://api.foursquare.com",
				FoursquareSyncInterval: 0,

				BaseURL:                requiredBaseURL,
				FoursquareClientID:     requiredClientID,
				FoursquareClientSecret: requiredClientSecret,
				FoursquarePushSecret:   requiredPushSecret,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("Load returned %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Load differs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestLoadWithoutAnExplicitPathStillRequiresTheRequiredSettings pins that an
// empty path with no default file present does not silently succeed: the
// default path's own absence is not an error, but every required setting
// still has to come from somewhere, so this fails just as an explicit,
// incomplete file would.
func TestLoadWithoutAnExplicitPathStillRequiresTheRequiredSettings(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := config.Load("")
	if err == nil {
		t.Fatal("Load returned nil with no config file and no required settings set")
	}
}

// TestLoadReadsTheDefaultPathWhenPresent pins that a travelmap.toml sitting
// in the current directory is read even though no path was given
// explicitly.
func TestLoadReadsTheDefaultPathWhenPresent(t *testing.T) {
	t.Chdir(t.TempDir())

	content := `
[server]
addr = ":9999"
base_url = "https://travelmap.example"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`
	if err := os.WriteFile(config.DefaultPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", config.DefaultPath, err)
	}

	got, err := config.Load("")
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got.Addr != ":9999" {
		t.Errorf("Addr = %q, want the value from %s", got.Addr, config.DefaultPath)
	}
}

// TestLoadRejectsAMissingExplicitPath pins that a path given explicitly is
// expected to exist: a mistyped --config should be reported, not mistaken
// for one that was never given.
func TestLoadRejectsAMissingExplicitPath(t *testing.T) {
	t.Parallel()

	_, err := config.Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err == nil {
		t.Fatal("Load returned nil for a missing explicit path")
	}
}

// TestLoadRejectsInvalidTOML pins that a syntax error is reported rather than
// silently falling back to defaults.
func TestLoadRejectsInvalidTOML(t *testing.T) {
	t.Parallel()

	_, err := config.Load(writeConfig(t, "this is not toml"))
	if err == nil {
		t.Fatal("Load returned nil for a file that is not valid TOML")
	}
}

// TestLoadRejectsAnUnknownKey pins that a misspelled table or key is refused
// rather than silently kept at its default: a typo here is otherwise
// indistinguishable from a setting nobody meant to change.
func TestLoadRejectsAnUnknownKey(t *testing.T) {
	t.Parallel()

	content := "[trackingg]\ntimezone = \"Asia/Tokyo\"\n" + requiredTOML

	_, err := config.Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("Load returned nil for an unknown table")
	}
}

// TestLoadRejectsAnInvalidLogLevel pins that a typo stops the server instead
// of being silently ignored, which would leave the operator looking for logs
// that never arrive.
func TestLoadRejectsAnInvalidLogLevel(t *testing.T) {
	t.Parallel()

	content := `
[server]
log_level = "chatty"
base_url = "https://travelmap.example"

[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`

	_, err := config.Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("Load returned nil for an invalid log level")
	}

	if got := err.Error(); !strings.Contains(got, "server.log_level") {
		t.Errorf("error = %q, want it to name the key at fault", got)
	}
}

// TestLoadRejectsAnInvalidTimezone pins that a typo is refused at startup —
// travelmap recalculate is the only other place tracking.timezone gets
// resolved, and finding out there means the mistake already looked like a
// success.
func TestLoadRejectsAnInvalidTimezone(t *testing.T) {
	t.Parallel()

	content := "[tracking]\ntimezone = \"Nowhere/Nothing\"\n" + requiredTOML

	_, err := config.Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("Load returned nil for an invalid timezone")
	}

	if got := err.Error(); !strings.Contains(got, "tracking.timezone") {
		t.Errorf("error = %q, want it to name the key at fault", got)
	}
}

// TestLoadRejectsAnInvalidTrackBreakMinutes covers both a value that is not a
// number and one that is not positive: a zero or negative break would count
// every segment, however far apart in time.
func TestLoadRejectsAnInvalidTrackBreakMinutes(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"zero":     "0",
		"negative": "-5",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := "[tracking]\ntrack_break_minutes = " + value + "\n" + requiredTOML

			_, err := config.Load(writeConfig(t, content))
			if err == nil {
				t.Fatal("Load returned nil for an invalid track_break_minutes")
			}

			if got := err.Error(); !strings.Contains(got, "tracking.track_break_minutes") {
				t.Errorf("error = %q, want it to name the key at fault", got)
			}
		})
	}
}

// TestLoadRejectsAnInvalidSessionLifetime covers both a value that does not
// parse as a duration and one that is not positive: a zero or negative
// lifetime would make every session expire before it could be used.
func TestLoadRejectsAnInvalidSessionLifetime(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"not a duration": "soon",
		"zero":           "0h",
		"negative":       "-5h",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := "[session]\nlifetime = " + `"` + value + `"` + "\n" + requiredTOML

			_, err := config.Load(writeConfig(t, content))
			if err == nil {
				t.Fatal("Load returned nil for an invalid session lifetime")
			}

			if got := err.Error(); !strings.Contains(got, "session.lifetime") {
				t.Errorf("error = %q, want it to name the key at fault", got)
			}
		})
	}
}

// TestLoadRejectsAnInvalidFoursquareSyncLookback covers the same two shapes
// as the track break above: a window that is not positive, which would ask
// the API for a window ending before it starts.
func TestLoadRejectsAnInvalidFoursquareSyncLookback(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"zero":     "0",
		"negative": "-14",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := "[foursquare]\nsync_lookback_days = " + value + "\n" +
				"client_id = \"" + requiredClientID + "\"\n" +
				"client_secret = \"" + requiredClientSecret + "\"\n" +
				"push_secret = \"" + requiredPushSecret + "\"\n" +
				"\n[server]\nbase_url = \"" + requiredBaseURL + "\"\n"

			_, err := config.Load(writeConfig(t, content))
			if err == nil {
				t.Fatal("Load returned nil for an invalid sync_lookback_days")
			}

			if got := err.Error(); !strings.Contains(got, "foursquare.sync_lookback_days") {
				t.Errorf("error = %q, want it to name the key at fault", got)
			}
		})
	}
}

// TestLoadRejectsAnInvalidFoursquareSyncInterval covers a value that does not
// parse as a duration and one that is negative. Unlike the session lifetime
// and the lookback window, zero is valid here — it disables the periodic
// fetch — so it is not one of the cases this test rejects.
func TestLoadRejectsAnInvalidFoursquareSyncInterval(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"not a duration": "soon",
		"negative":       "-1h",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := "[foursquare]\nsync_interval = " + `"` + value + `"` + "\n" +
				"client_id = \"" + requiredClientID + "\"\n" +
				"client_secret = \"" + requiredClientSecret + "\"\n" +
				"push_secret = \"" + requiredPushSecret + "\"\n" +
				"\n[server]\nbase_url = \"" + requiredBaseURL + "\"\n"

			_, err := config.Load(writeConfig(t, content))
			if err == nil {
				t.Fatal("Load returned nil for an invalid sync_interval")
			}

			if got := err.Error(); !strings.Contains(got, "foursquare.sync_interval") {
				t.Errorf("error = %q, want it to name the key at fault", got)
			}
		})
	}
}

// TestLoadRejectsAMissingRequiredSetting covers each setting [config.Load]
// treats as required on its own: an empty travelmap.toml has none of them,
// so leaving one out of an otherwise-complete file is what each case here
// pins.
func TestLoadRejectsAMissingRequiredSetting(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
		wantKey string
	}{
		"base_url": {
			content: "[foursquare]\nclient_id = \"" + requiredClientID + "\"\n" +
				"client_secret = \"" + requiredClientSecret + "\"\n" +
				"push_secret = \"" + requiredPushSecret + "\"\n",
			wantKey: "server.base_url",
		},
		"client_id": {
			content: "[server]\nbase_url = \"" + requiredBaseURL + "\"\n" +
				"[foursquare]\nclient_secret = \"" + requiredClientSecret + "\"\n" +
				"push_secret = \"" + requiredPushSecret + "\"\n",
			wantKey: "foursquare.client_id",
		},
		"client_secret": {
			content: "[server]\nbase_url = \"" + requiredBaseURL + "\"\n" +
				"[foursquare]\nclient_id = \"" + requiredClientID + "\"\n" +
				"push_secret = \"" + requiredPushSecret + "\"\n",
			wantKey: "foursquare.client_secret",
		},
		"push_secret": {
			content: "[server]\nbase_url = \"" + requiredBaseURL + "\"\n" +
				"[foursquare]\nclient_id = \"" + requiredClientID + "\"\n" +
				"client_secret = \"" + requiredClientSecret + "\"\n",
			wantKey: "foursquare.push_secret",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(writeConfig(t, tt.content))
			if err == nil {
				t.Fatalf("Load returned nil with %s missing", tt.wantKey)
			}

			if got := err.Error(); !strings.Contains(got, tt.wantKey) {
				t.Errorf("error = %q, want it to name %s", got, tt.wantKey)
			}
		})
	}
}

// TestConfigLocation pins that it resolves the same zone Load already
// validated, for the one caller — travelmap recalculate — that needs a
// *time.Location rather than the name.
func TestConfigLocation(t *testing.T) {
	t.Parallel()

	content := "[tracking]\ntimezone = \"Asia/Tokyo\"\n" + requiredTOML

	cfg, err := config.Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	loc, err := cfg.Location()
	if err != nil {
		t.Fatalf("Location returned %v", err)
	}

	if got := loc.String(); got != "Asia/Tokyo" {
		t.Errorf("Location = %q, want Asia/Tokyo", got)
	}
}

// TestConfigTrackBreak pins the unit conversion: TrackBreakMinutes is stored
// in minutes because that is what the config file is documented in, and
// TrackBreak is what the SQL rebuild query actually wants.
func TestConfigTrackBreak(t *testing.T) {
	t.Parallel()

	content := "[tracking]\ntrack_break_minutes = 45\n" + requiredTOML

	cfg, err := config.Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got, want := cfg.TrackBreak(), 45*time.Minute; got != want {
		t.Errorf("TrackBreak = %v, want %v", got, want)
	}
}

// TestConfigFoursquareSyncLookback pins the other unit conversion: the
// window is configured in days, because that is the unit a fortnight of
// history is thought about in, and a fetch takes it as a duration.
func TestConfigFoursquareSyncLookback(t *testing.T) {
	t.Parallel()

	content := "[foursquare]\nsync_lookback_days = 3\n" +
		"client_id = \"" + requiredClientID + "\"\n" +
		"client_secret = \"" + requiredClientSecret + "\"\n" +
		"push_secret = \"" + requiredPushSecret + "\"\n" +
		"\n[server]\nbase_url = \"" + requiredBaseURL + "\"\n"

	cfg, err := config.Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got, want := cfg.FoursquareSyncLookback(), 72*time.Hour; got != want {
		t.Errorf("FoursquareSyncLookback = %v, want %v", got, want)
	}
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	content := "[server]\nlog_level = \"warn\"\nbase_url = \"" + requiredBaseURL + "\"\n" + `
[foursquare]
client_id = "the-client-id"
client_secret = "the-client-secret"
push_secret = "the-push-secret"
`

	cfg, err := config.Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	logger := cfg.NewLogger(&out)
	logger.Info("dropped")
	logger.Warn("kept")

	got := out.String()
	if strings.Contains(got, "dropped") {
		t.Errorf("a message below the configured level was logged: %q", got)
	}

	if !strings.Contains(got, "kept") {
		t.Errorf("a message at the configured level was not logged: %q", got)
	}
}
