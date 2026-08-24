package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/config"
)

// env turns a map into the getenv function Load takes, so that a test never
// touches the process environment and every case can run in parallel.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		vars map[string]string
		want config.Config
	}{
		"defaults when nothing is set": {
			vars: nil,
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
			},
		},
		"every variable set": {
			vars: map[string]string{
				"TRAVELMAP_ADDR":                "127.0.0.1:8080",
				"TRAVELMAP_LOG_LEVEL":           "debug",
				"TRAVELMAP_DATABASE":            "/var/lib/travelmap/travelmap.db",
				"TRAVELMAP_DEBUG_LOG_REQUESTS":  "1",
				"TRAVELMAP_TIMEZONE":            "Asia/Tokyo",
				"TRAVELMAP_TRACK_BREAK_MINUTES": "45",
			},
			want: config.Config{
				Addr:              "127.0.0.1:8080",
				LogLevel:          slog.LevelDebug,
				DatabasePath:      "/var/lib/travelmap/travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "Asia/Tokyo",
				TrackBreakMinutes: 45,
			},
		},
		// Documented as =1, but a shell wrapper writes what it writes, and a
		// server that ignored "true" would be capturing nothing while the
		// operator watched the device.
		"the request log accepts the other spellings of true": {
			vars: map[string]string{"TRAVELMAP_DEBUG_LOG_REQUESTS": "true"},
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelInfo,
				DatabasePath:      "travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
			},
		},
		// Info is where the request log writes, so a level above it would
		// have answered the switch with an empty capture.
		"the request log holds the log level down to info": {
			vars: map[string]string{
				"TRAVELMAP_LOG_LEVEL":          "error",
				"TRAVELMAP_DEBUG_LOG_REQUESTS": "1",
			},
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelInfo,
				DatabasePath:      "travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
			},
		},
		// Down, not to: a level below Info is what an operator debugging
		// something else asked for, and the request log comes through it.
		"the request log leaves a lower level alone": {
			vars: map[string]string{
				"TRAVELMAP_LOG_LEVEL":          "debug",
				"TRAVELMAP_DEBUG_LOG_REQUESTS": "1",
			},
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelDebug,
				DatabasePath:      "travelmap.db",
				DebugLogRequests:  true,
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
			},
		},
		// The off switch is a setting an operator writes down, not just the
		// absence of one: a unit file that turns the log off has to keep it
		// off, and keep the level it asked for.
		"the request log switched off explicitly": {
			vars: map[string]string{
				"TRAVELMAP_LOG_LEVEL":          "error",
				"TRAVELMAP_DEBUG_LOG_REQUESTS": "0",
			},
			want: config.Config{
				Addr:              ":3000",
				LogLevel:          slog.LevelError,
				DatabasePath:      "travelmap.db",
				Timezone:          "UTC",
				TrackBreakMinutes: 30,
			},
		},
		"the log level is case-insensitive": {
			vars: map[string]string{"TRAVELMAP_LOG_LEVEL": "WARN"},
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelWarn, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
			},
		},
		// A variable a wrapper script left blank is not a listen address; an
		// unset one is covered by the first case, so this one is the trim.
		"a blank value falls back to the default": {
			vars: map[string]string{"TRAVELMAP_ADDR": "  "},
			want: config.Config{
				Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db",
				Timezone: "UTC", TrackBreakMinutes: 30,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(tt.vars))
			if err != nil {
				t.Fatalf("Load returned %v", err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Load differs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestLoadRejectsAnInvalidLogLevel pins that a typo stops the server instead
// of being silently ignored, which would leave the operator looking for logs
// that never arrive.
func TestLoadRejectsAnInvalidLogLevel(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"TRAVELMAP_LOG_LEVEL": "chatty"}))
	if err == nil {
		t.Fatal("Load returned nil for an invalid log level")
	}

	if got := err.Error(); !strings.Contains(got, "TRAVELMAP_LOG_LEVEL") {
		t.Errorf("error = %q, want it to name the variable at fault", got)
	}
}

// TestLoadRejectsAnInvalidDebugLogRequests pins that this one fails loudly
// too: a capture session that quietly logged nothing is a session to run
// again, with the device back in hand.
func TestLoadRejectsAnInvalidDebugLogRequests(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"TRAVELMAP_DEBUG_LOG_REQUESTS": "yes please"}))
	if err == nil {
		t.Fatal("Load returned nil for an invalid TRAVELMAP_DEBUG_LOG_REQUESTS")
	}

	if got := err.Error(); !strings.Contains(got, "TRAVELMAP_DEBUG_LOG_REQUESTS") {
		t.Errorf("error = %q, want it to name the variable at fault", got)
	}
}

// TestLoadRejectsAnInvalidTimezone pins that a typo is refused at startup —
// travelmap recalculate is the only other place TRAVELMAP_TIMEZONE gets
// resolved, and finding out there means the mistake already looked like a
// success.
func TestLoadRejectsAnInvalidTimezone(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{"TRAVELMAP_TIMEZONE": "Nowhere/Nothing"}))
	if err == nil {
		t.Fatal("Load returned nil for an invalid TRAVELMAP_TIMEZONE")
	}

	if got := err.Error(); !strings.Contains(got, "TRAVELMAP_TIMEZONE") {
		t.Errorf("error = %q, want it to name the variable at fault", got)
	}
}

// TestLoadRejectsAnInvalidTrackBreakMinutes covers both a value that is not a
// number and one that is not positive: a zero or negative break would count
// every segment, however far apart in time.
func TestLoadRejectsAnInvalidTrackBreakMinutes(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]string{
		"not a number": "soon",
		"zero":         "0",
		"negative":     "-5",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{"TRAVELMAP_TRACK_BREAK_MINUTES": value}))
			if err == nil {
				t.Fatal("Load returned nil for an invalid TRAVELMAP_TRACK_BREAK_MINUTES")
			}

			if got := err.Error(); !strings.Contains(got, "TRAVELMAP_TRACK_BREAK_MINUTES") {
				t.Errorf("error = %q, want it to name the variable at fault", got)
			}
		})
	}
}

// TestConfigLocation pins that it resolves the same zone Load already
// validated, for the one caller — travelmap recalculate — that needs a
// *time.Location rather than the name.
func TestConfigLocation(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{"TRAVELMAP_TIMEZONE": "Asia/Tokyo"}))
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
// in minutes because that is what the environment variable is documented in,
// and TrackBreak is what the SQL rebuild query actually wants.
func TestConfigTrackBreak(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(env(map[string]string{"TRAVELMAP_TRACK_BREAK_MINUTES": "45"}))
	if err != nil {
		t.Fatalf("Load returned %v", err)
	}

	if got, want := cfg.TrackBreak(), 45*time.Minute; got != want {
		t.Errorf("TrackBreak = %v, want %v", got, want)
	}
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	cfg, err := config.Load(env(map[string]string{"TRAVELMAP_LOG_LEVEL": "warn"}))
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
