package config

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// prefix is prepended to every environment variable this server reads.
const prefix = "TRAVELMAP_"

// Defaults for the settings below. Port 3000 matches the port upstream
// Dawarich listens on, so an existing client configuration keeps working.
const (
	defaultAddr         = ":3000"
	defaultLogLevel     = slog.LevelInfo
	defaultDatabasePath = "travelmap.db"

	// defaultTimezone and defaultTrackBreakMinutes are also the values
	// GET /api/v1/users/me and the daily_stats rebuild fall back to, per
	// "Configuration affecting stored aggregates" in TODO.md.
	defaultTimezone          = "UTC"
	defaultTrackBreakMinutes = 30
)

// Config is the server configuration.
type Config struct {
	// Addr is the TCP address the HTTP server listens on, in the form
	// accepted by net.Listen ("host:port", or ":port" for every interface).
	Addr string

	// LogLevel is the lowest level the logger emits.
	LogLevel slog.Level

	// DatabasePath is the SQLite file holding everything this server stores.
	//
	// It defaults to a file in the working directory, which is what makes
	// `travelmap serve` in a checkout work with nothing configured. A service
	// running from a unit file wants an absolute path under a directory it
	// owns: SQLite creates the file but not the directories above it, and a
	// relative path would follow the process's working directory.
	DatabasePath string

	// DebugLogRequests turns on the request log: one line per request,
	// unmatched routes included, with the credentials taken out.
	//
	// It is off by default and belongs on a server being pointed at a client,
	// not on one in service. Every request the client makes ends up in the log
	// — which is the point, and also why it is not something to leave running.
	//
	// Turning it on holds [Config.LogLevel] down to Info, since that is the
	// level those lines are written at.
	DebugLogRequests bool

	// Timezone is the IANA zone name day boundaries in daily_stats are cut
	// on, and what GET /api/v1/users/me reports in settings.timezone.
	// Changing it invalidates every stored daily_stats row and requires
	// `travelmap recalculate`; see "Configuration affecting stored
	// aggregates" in TODO.md.
	Timezone string

	// TrackBreakMinutes is the gap, in minutes, above which a segment
	// between two consecutive points is excluded entirely from a day's km
	// rather than counted as travelled distance. Distinct from
	// settings/mobile's own track_break, which is the device's own
	// track-splitting setting; see "Configuration affecting stored
	// aggregates" in TODO.md. Changing it likewise requires `travelmap
	// recalculate`.
	TrackBreakMinutes int
}

// Load reads the configuration from the TRAVELMAP_* environment variables,
// filling in the default of every variable that is unset or empty.
//
// The environment is read through getenv rather than [os.Getenv] so that tests
// can supply one without mutating the process environment, which would stop
// them from running in parallel. Callers outside tests pass [os.Getenv].
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:              lookup(getenv, "ADDR", defaultAddr),
		LogLevel:          defaultLogLevel,
		DatabasePath:      lookup(getenv, "DATABASE", defaultDatabasePath),
		Timezone:          lookup(getenv, "TIMEZONE", defaultTimezone),
		TrackBreakMinutes: defaultTrackBreakMinutes,
	}

	if raw := lookup(getenv, "LOG_LEVEL", ""); raw != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(raw)); err != nil {
			return Config{}, fmt.Errorf("%sLOG_LEVEL: %w", prefix, err)
		}
	}

	if raw := lookup(getenv, "DEBUG_LOG_REQUESTS", ""); raw != "" {
		// A typo stops the server rather than leaving the setting off: this is
		// switched on to capture traffic that has to be reproduced to be
		// captured again, and finding out afterwards that the log was never on
		// costs that whole session.
		on, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%sDEBUG_LOG_REQUESTS: %w", prefix, err)
		}

		cfg.DebugLogRequests = on
	}

	// The request log is written at Info, so a level above it would answer the
	// switch with an empty capture — and two variables that have to agree is
	// the second switch that switch was meant not to be. It comes down only:
	// a level below Info was asked for by someone debugging something else,
	// and these lines come through it either way.
	if cfg.DebugLogRequests && cfg.LogLevel > slog.LevelInfo {
		cfg.LogLevel = slog.LevelInfo
	}

	// Resolved and discarded rather than kept on Config: what daily_stats
	// needs is a *time.Location, but every caller of Load runs in the same
	// process as the one that will use it, so resolving it again there costs
	// nothing and keeps Config itself made of plain, comparable values.
	// Doing it here means a typo is refused at startup rather than the first
	// time `travelmap recalculate` runs.
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return Config{}, fmt.Errorf("%sTIMEZONE: %w", prefix, err)
	}

	if raw := lookup(getenv, "TRACK_BREAK_MINUTES", ""); raw != "" {
		minutes, err := strconv.Atoi(raw)
		if err != nil || minutes <= 0 {
			return Config{}, fmt.Errorf("%sTRACK_BREAK_MINUTES: must be a positive number of minutes", prefix)
		}

		cfg.TrackBreakMinutes = minutes
	}

	return cfg, nil
}

// Location resolves [Config.Timezone] into a *time.Location.
//
// Load already resolves it once, to refuse a typo at startup rather than the
// first time it is needed; this does the same lookup again for the caller
// that actually wants the value; see the comment in Load for why it is not
// carried on Config itself instead.
func (c Config) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%sTIMEZONE: %w", prefix, err)
	}

	return loc, nil
}

// TrackBreak is [Config.TrackBreakMinutes] as a [time.Duration], which is
// the form the daily_stats rebuild takes it in.
func (c Config) TrackBreak() time.Duration {
	return time.Duration(c.TrackBreakMinutes) * time.Minute
}

// NewLogger builds the logger described by the configuration.
//
// The output is logfmt rather than JSON: a self-hosted server is most often
// read straight from a terminal or `journalctl`, and structured logs shipped
// somewhere else are a Milestone G concern.
func (c Config) NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: c.LogLevel}))
}

// lookup returns the value of the TRAVELMAP_-prefixed variable name, or
// fallback when it is unset or empty. An empty value is treated as unset
// because a shell wrapper blanks a variable it does not want to pass on, and
// an empty address would send the server to an arbitrary port chosen by the
// kernel rather than to the one a client is configured for.
func lookup(getenv func(string) string, name, fallback string) string {
	if v := strings.TrimSpace(getenv(prefix + name)); v != "" {
		return v
	}

	return fallback
}
