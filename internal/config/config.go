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
	// GET /api/v1/users/me and the daily_stats rebuild fall back to.
	defaultTimezone          = "UTC"
	defaultTrackBreakMinutes = 30

	defaultSessionLifetime     = 720 * time.Hour
	defaultSessionCookieSecure = true

	// defaultFoursquareSyncLookbackDays is the fortnight the periodic fetch
	// re-reads on every run. It is a window rather than a cursor because a
	// check-in can be added or edited after the fact, and a fortnight of one
	// account's check-ins is expected to fit in a single request.
	defaultFoursquareSyncLookbackDays = 14

	// defaultFoursquareSyncInterval is how often the periodic fetch repeats.
	defaultFoursquareSyncInterval = time.Hour

	// defaultFoursquareAPIURL is the API itself, which every deployment
	// talks to: it is configurable so that a test can point the client at a
	// local server, not because there is a second Foursquare. The value sits
	// here rather than in internal/foursquare because that package is a leaf
	// this one cannot import, and one default is better in the file that
	// documents every other one.
	defaultFoursquareAPIURL = "https://api.foursquare.com"
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
	// Running in Japan without "Asia/Tokyo" attributes movement between
	// 00:00 and 09:00 to the previous day, making /stats and tracked_months
	// disagree with the app. Changing it invalidates every stored
	// daily_stats row and requires `travelmap recalculate`.
	Timezone string

	// TrackBreakMinutes is the gap, in minutes, above which a segment
	// between two consecutive points is excluded entirely from a day's km
	// rather than counted as travelled distance. Distinct from
	// settings/mobile's own track_break, which is the device's own
	// track-splitting setting: using that one here would change the
	// meaning of past aggregates whenever the user changes it in the app.
	// Changing this one likewise requires `travelmap recalculate`.
	TrackBreakMinutes int

	// FoursquarePushSecret is the shared secret a Swarm User Push notification
	// carries in its own `secret` form field. Empty by default, which is what
	// keeps POST /webhooks/foursquare unregistered: a route answering 401 to
	// every request would still confirm the feature exists, where 404 says it
	// does not (see "An endpoint this server does not implement answers 404"
	// in docs/api-notes.md, which this route follows even though it is not a
	// Dawarich-compatible one).
	FoursquarePushSecret string

	// FoursquareClientID and FoursquareClientSecret identify the Foursquare
	// application the OAuth flow (GET /settings/foursquare/connect and its
	// callback) runs against. Both empty by default, which is what keeps
	// those routes unregistered, the same reasoning as FoursquarePushSecret
	// above.
	FoursquareClientID     string
	FoursquareClientSecret string

	// BaseURL is this server's own externally reachable URL, with no
	// trailing path — e.g. "https://travelmap.example.com". Empty by
	// default, which along with the two Foursquare settings above keeps
	// GET /settings/foursquare/connect and its callback unregistered: deriving
	// the callback URL (BaseURL plus the fixed
	// /foursquare/oauth/callback path) is its only consumer today, but the
	// setting names this server rather than that one feature, so a second
	// consumer names the same setting instead of adding its own. The
	// derived callback URL has to match the one registered on the
	// Foursquare application exactly.
	BaseURL string

	// SessionLifetime is how long a browser session lasts before it needs a
	// fresh login. Defaults to 30 days.
	SessionLifetime time.Duration

	// SessionCookieSecure sets the session cookie's Secure attribute.
	// Defaults to on: over plain HTTP a login then visibly bounces back to
	// the form, where off would let the cookie cross a plain-HTTP LAN
	// unreported. Browsers treat http://localhost as a secure context, so
	// the default costs a developer nothing there.
	SessionCookieSecure bool

	// FoursquareSyncLookbackDays is how far back a check-in fetch looks,
	// counted from the moment the run starts. Every run re-reads the whole
	// window and lets the upsert absorb the overlap, so raising it costs
	// requests rather than correctness; `travelmap foursquare sync` takes a
	// wider one for a backfill.
	FoursquareSyncLookbackDays int

	// FoursquareAPIURL is the base URL of the Foursquare API, without a
	// trailing path. Read by both the check-in fetch client and the OAuth
	// flow's own call to /v2/users/self — one setting for the one
	// Foursquare API host, rather than one per reader of it.
	FoursquareAPIURL string

	// FoursquareSyncInterval is how often the periodic check-in fetch repeats,
	// once for every linked account. Defaults to an hour; 0 disables it
	// entirely, leaving only the push webhook (if configured) to collect
	// check-ins going forward — `travelmap foursquare sync` still runs the
	// same fetch by hand regardless.
	FoursquareSyncInterval time.Duration
}

// Load reads the configuration from the TRAVELMAP_* environment variables,
// filling in the default of every variable that is unset or empty.
//
// The environment is read through getenv rather than [os.Getenv] so that tests
// can supply one without mutating the process environment, which would stop
// them from running in parallel. Callers outside tests pass [os.Getenv].
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:                       lookup(getenv, "ADDR", defaultAddr),
		LogLevel:                   defaultLogLevel,
		DatabasePath:               lookup(getenv, "DATABASE", defaultDatabasePath),
		Timezone:                   lookup(getenv, "TIMEZONE", defaultTimezone),
		TrackBreakMinutes:          defaultTrackBreakMinutes,
		FoursquarePushSecret:       lookup(getenv, "FOURSQUARE_PUSH_SECRET", ""),
		FoursquareClientID:         lookup(getenv, "FOURSQUARE_CLIENT_ID", ""),
		FoursquareClientSecret:     lookup(getenv, "FOURSQUARE_CLIENT_SECRET", ""),
		BaseURL:                    lookup(getenv, "BASE_URL", ""),
		SessionLifetime:            defaultSessionLifetime,
		SessionCookieSecure:        defaultSessionCookieSecure,
		FoursquareSyncLookbackDays: defaultFoursquareSyncLookbackDays,
		FoursquareAPIURL:           lookup(getenv, "FOURSQUARE_API_URL", defaultFoursquareAPIURL),
		FoursquareSyncInterval:     defaultFoursquareSyncInterval,
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

	if raw := lookup(getenv, "SESSION_LIFETIME", ""); raw != "" {
		lifetime, err := time.ParseDuration(raw)
		if err != nil || lifetime <= 0 {
			return Config{}, fmt.Errorf("%sSESSION_LIFETIME: must be a positive duration", prefix)
		}

		cfg.SessionLifetime = lifetime
	}

	if raw := lookup(getenv, "SESSION_COOKIE_SECURE", ""); raw != "" {
		on, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("%sSESSION_COOKIE_SECURE: %w", prefix, err)
		}

		cfg.SessionCookieSecure = on
	}

	if raw := lookup(getenv, "FOURSQUARE_SYNC_LOOKBACK_DAYS", ""); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil || days <= 0 {
			return Config{}, fmt.Errorf("%sFOURSQUARE_SYNC_LOOKBACK_DAYS: must be a positive number of days", prefix)
		}

		cfg.FoursquareSyncLookbackDays = days
	}

	if raw := lookup(getenv, "FOURSQUARE_SYNC_INTERVAL", ""); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 0 {
			return Config{}, fmt.Errorf("%sFOURSQUARE_SYNC_INTERVAL: must be a non-negative duration", prefix)
		}

		cfg.FoursquareSyncInterval = interval
	}

	return cfg, nil
}

// FoursquareSyncLookback is [Config.FoursquareSyncLookbackDays] as a
// [time.Duration], which is the form a fetch takes its window in.
func (c Config) FoursquareSyncLookback() time.Duration {
	return time.Duration(c.FoursquareSyncLookbackDays) * 24 * time.Hour
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
