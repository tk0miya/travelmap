package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// DefaultPath is the config file [Load] reads when no path is given
// explicitly. Its own absence is not an error, though a required setting
// missing because of it still is; a path given explicitly must exist.
const DefaultPath = "travelmap.toml"

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
	// It defaults to a file in the working directory, so a config file that
	// only sets the settings that are required does not have to name it too.
	// A service running from a unit file wants an absolute path under a
	// directory it owns: SQLite creates the file but not the directories
	// above it, and a relative path would follow the process's working
	// directory.
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
	// carries in its own `secret` form field. Required: travelmap always
	// runs with Swarm check-in collection active, so there is no state for
	// this to default into.
	FoursquarePushSecret string

	// FoursquareClientID and FoursquareClientSecret identify the Foursquare
	// application the OAuth flow (GET /settings/foursquare/connect and its
	// callback) runs against. Required, the same reasoning as
	// FoursquarePushSecret above.
	FoursquareClientID     string
	FoursquareClientSecret string

	// BaseURL is this server's own externally reachable URL, with no
	// trailing path — e.g. "https://travelmap.example.com". Required: it is
	// what the Foursquare OAuth callback URL (this plus the fixed
	// /foursquare/oauth/callback path) is derived from, and that flow is
	// always registered. The setting names this server rather than that one
	// feature, so a second consumer names the same setting instead of
	// adding its own. The derived callback URL has to match the one
	// registered on the Foursquare application exactly.
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
	// entirely, leaving only the push webhook to collect check-ins going
	// forward — `travelmap foursquare sync` still runs the same fetch by
	// hand regardless.
	FoursquareSyncInterval time.Duration
}

// fileConfig is the shape of the TOML config file. Every field is a pointer
// so that Load can tell a key the file omits from one set to its zero value
// — a distinction plain values cannot carry, and the one an overlay onto
// Config's own defaults needs.
type fileConfig struct {
	Server struct {
		Addr             *string `toml:"addr"`
		BaseURL          *string `toml:"base_url"`
		LogLevel         *string `toml:"log_level"`
		DebugLogRequests *bool   `toml:"debug_log_requests"`
	} `toml:"server"`

	Database struct {
		Path *string `toml:"path"`
	} `toml:"database"`

	Tracking struct {
		Timezone          *string `toml:"timezone"`
		TrackBreakMinutes *int    `toml:"track_break_minutes"`
	} `toml:"tracking"`

	Session struct {
		Lifetime     *string `toml:"lifetime"`
		CookieSecure *bool   `toml:"cookie_secure"`
	} `toml:"session"`

	Foursquare struct {
		ClientID         *string `toml:"client_id"`
		ClientSecret     *string `toml:"client_secret"`
		PushSecret       *string `toml:"push_secret"`
		APIURL           *string `toml:"api_url"`
		SyncInterval     *string `toml:"sync_interval"`
		SyncLookbackDays *int    `toml:"sync_lookback_days"`
	} `toml:"foursquare"`
}

// Load reads the TOML configuration file at path, filling in the default of
// every setting the file omits.
//
// An empty path reads [DefaultPath] instead; that file's absence is not
// itself an error, but every required setting still has to come from
// somewhere, so a config file is effectively needed regardless. A path given
// explicitly is expected to exist, so its absence is an error rather than a
// silent fall-back to defaults: a mistyped --config should not be mistaken
// for one that was never given.
func Load(path string) (Config, error) {
	cfg := Config{
		Addr:                       defaultAddr,
		LogLevel:                   defaultLogLevel,
		DatabasePath:               defaultDatabasePath,
		Timezone:                   defaultTimezone,
		TrackBreakMinutes:          defaultTrackBreakMinutes,
		SessionLifetime:            defaultSessionLifetime,
		SessionCookieSecure:        defaultSessionCookieSecure,
		FoursquareSyncLookbackDays: defaultFoursquareSyncLookbackDays,
		FoursquareAPIURL:           defaultFoursquareAPIURL,
		FoursquareSyncInterval:     defaultFoursquareSyncInterval,
	}

	explicit := path != ""
	if !explicit {
		path = DefaultPath
	}

	var file fileConfig

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		decoder := toml.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()

		// An unrecognised table or key is refused rather than quietly kept at
		// its default: the same typo that TIMEZONE guards against below, just
		// one level up.
		if err := decoder.Decode(&file); err != nil {
			return Config{}, fmt.Errorf("parsing %s: %w", path, err)
		}
	case explicit || !errors.Is(err, os.ErrNotExist):
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	// The remaining case is the default path absent and no path given
	// explicitly: file stays its zero value, and every setting below falls
	// through to its own default — except the required ones, which have none
	// to fall back to and are what turns this into an error further down.

	if v := file.Server.Addr; v != nil {
		cfg.Addr = *v
	}

	if v := file.Server.BaseURL; v != nil {
		cfg.BaseURL = *v
	}

	if v := file.Database.Path; v != nil {
		cfg.DatabasePath = *v
	}

	if v := file.Foursquare.PushSecret; v != nil {
		cfg.FoursquarePushSecret = *v
	}

	if v := file.Foursquare.ClientID; v != nil {
		cfg.FoursquareClientID = *v
	}

	if v := file.Foursquare.ClientSecret; v != nil {
		cfg.FoursquareClientSecret = *v
	}

	if v := file.Foursquare.APIURL; v != nil {
		cfg.FoursquareAPIURL = *v
	}

	if v := file.Server.DebugLogRequests; v != nil {
		cfg.DebugLogRequests = *v
	}

	if v := file.Server.LogLevel; v != nil {
		if err := cfg.LogLevel.UnmarshalText([]byte(*v)); err != nil {
			return Config{}, fmt.Errorf("server.log_level: %w", err)
		}
	}

	// The request log is written at Info, so a level above it would answer the
	// switch with an empty capture — and two settings that have to agree is
	// the second switch that switch was meant not to be. It comes down only:
	// a level below Info was asked for by someone debugging something else,
	// and these lines come through it either way.
	if cfg.DebugLogRequests && cfg.LogLevel > slog.LevelInfo {
		cfg.LogLevel = slog.LevelInfo
	}

	if v := file.Tracking.Timezone; v != nil {
		cfg.Timezone = *v
	}

	// Resolved and discarded rather than kept on Config: what daily_stats
	// needs is a *time.Location, but every caller of Load runs in the same
	// process as the one that will use it, so resolving it again there costs
	// nothing and keeps Config itself made of plain, comparable values.
	// Doing it here means a typo is refused at startup rather than the first
	// time `travelmap recalculate` runs.
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return Config{}, fmt.Errorf("tracking.timezone: %w", err)
	}

	if v := file.Tracking.TrackBreakMinutes; v != nil {
		if *v <= 0 {
			return Config{}, errors.New("tracking.track_break_minutes: must be a positive number of minutes")
		}

		cfg.TrackBreakMinutes = *v
	}

	if v := file.Session.Lifetime; v != nil {
		lifetime, err := time.ParseDuration(*v)
		if err != nil || lifetime <= 0 {
			return Config{}, errors.New("session.lifetime: must be a positive duration")
		}

		cfg.SessionLifetime = lifetime
	}

	if v := file.Session.CookieSecure; v != nil {
		cfg.SessionCookieSecure = *v
	}

	if v := file.Foursquare.SyncLookbackDays; v != nil {
		if *v <= 0 {
			return Config{}, errors.New("foursquare.sync_lookback_days: must be a positive number of days")
		}

		cfg.FoursquareSyncLookbackDays = *v
	}

	if v := file.Foursquare.SyncInterval; v != nil {
		interval, err := time.ParseDuration(*v)
		if err != nil || interval < 0 {
			return Config{}, errors.New("foursquare.sync_interval: must be a non-negative duration")
		}

		cfg.FoursquareSyncInterval = interval
	}

	// Required rather than defaulted: travelmap always runs with Swarm
	// check-in collection active, so there is no meaningful empty value for
	// any of these to fall back to.
	switch {
	case cfg.BaseURL == "":
		return Config{}, errors.New("server.base_url: required")
	case cfg.FoursquareClientID == "":
		return Config{}, errors.New("foursquare.client_id: required")
	case cfg.FoursquareClientSecret == "":
		return Config{}, errors.New("foursquare.client_secret: required")
	case cfg.FoursquarePushSecret == "":
		return Config{}, errors.New("foursquare.push_secret: required")
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
		return nil, fmt.Errorf("tracking.timezone: %w", err)
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
