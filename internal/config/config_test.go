package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

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
			want: config.Config{Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db"},
		},
		"every variable set": {
			vars: map[string]string{
				"TRAVELMAP_ADDR":      "127.0.0.1:8080",
				"TRAVELMAP_LOG_LEVEL": "debug",
				"TRAVELMAP_DATABASE":  "/var/lib/travelmap/travelmap.db",
			},
			want: config.Config{
				Addr:         "127.0.0.1:8080",
				LogLevel:     slog.LevelDebug,
				DatabasePath: "/var/lib/travelmap/travelmap.db",
			},
		},
		"the log level is case-insensitive": {
			vars: map[string]string{"TRAVELMAP_LOG_LEVEL": "WARN"},
			want: config.Config{Addr: ":3000", LogLevel: slog.LevelWarn, DatabasePath: "travelmap.db"},
		},
		// A variable a wrapper script left blank is not a listen address; an
		// unset one is covered by the first case, so this one is the trim.
		"a blank value falls back to the default": {
			vars: map[string]string{"TRAVELMAP_ADDR": "  "},
			want: config.Config{Addr: ":3000", LogLevel: slog.LevelInfo, DatabasePath: "travelmap.db"},
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
