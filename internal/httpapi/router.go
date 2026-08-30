package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/tk0miya/travelmap/internal/store"
)

// defaultTrackBreak is what [Options.TrackBreak] defaults to when left zero,
// matching config's own TRAVELMAP_TRACK_BREAK_MINUTES default.
const defaultTrackBreak = 30 * time.Minute

// defaultSessionLifetime is what [Options.SessionLifetime] defaults to when
// left zero, matching config's own TRAVELMAP_SESSION_LIFETIME default.
const defaultSessionLifetime = 720 * time.Hour

// Options carries what the HTTP surface needs from its caller. Later steps add
// the ingest layer here; cmd/travelmap is the only place that fills it in.
type Options struct {
	// Logger receives everything the HTTP layer logs. Required.
	Logger *slog.Logger

	// Store is where the handlers read and write. Required on every route,
	// /api/v1/health included: authentication reaches it before the handler
	// on any request that carries a key, whichever route that request is for.
	Store store.Store

	// DebugLogRequests turns on the request log described on [api.logRequests]:
	// every request, unmatched routes included, with the credentials taken out.
	// Off unless TRAVELMAP_DEBUG_LOG_REQUESTS says otherwise.
	DebugLogRequests bool

	// Timezone is what GET /api/v1/users/me reports in settings.timezone —
	// TRAVELMAP_TIMEZONE, unvalidated here because config.Load already
	// refused an invalid one at startup. Left empty, it defaults to "UTC",
	// which is what every test in this package that does not set it gets.
	Timezone string

	// Location is Timezone resolved — [config.Config.Location] — which is
	// what internal/ingest actually cuts a point's day on. Kept apart from
	// Timezone because that field is a display string and this is the value
	// date arithmetic needs; a caller sets both from the same configured
	// zone. Left nil, it defaults to time.UTC, matching Timezone's own
	// default.
	Location *time.Location

	// TrackBreak is TRAVELMAP_TRACK_BREAK_MINUTES as a [time.Duration] —
	// [config.Config.TrackBreak] — passed to internal/ingest for the same
	// reason as Location. Left zero, it defaults to [defaultTrackBreak].
	TrackBreak time.Duration

	// FoursquarePushSecret is TRAVELMAP_FOURSQUARE_PUSH_SECRET — see
	// [config.Config.FoursquarePushSecret] for why it defaults to unset.
	// Left empty, POST /webhooks/foursquare is not registered at all.
	FoursquarePushSecret string

	// FoursquareClientID and FoursquareClientSecret are
	// TRAVELMAP_FOURSQUARE_CLIENT_ID and _CLIENT_SECRET — see
	// [config.Config.FoursquareClientID] for why both default to unset.
	// Left empty, GET /foursquare/oauth/start and its callback are not
	// registered at all, the same reasoning as FoursquarePushSecret.
	FoursquareClientID     string
	FoursquareClientSecret string

	// BaseURL is TRAVELMAP_BASE_URL — see [config.Config.BaseURL] for what
	// it is and why it is named generally rather than for its one current
	// reader. Left empty, GET /foursquare/oauth/start and its callback are
	// not registered at all, the same reasoning as FoursquarePushSecret.
	BaseURL string

	// FoursquareAPIURL is TRAVELMAP_FOURSQUARE_API_URL —
	// [config.Config.FoursquareAPIURL], the same setting the check-in fetch
	// client uses. Left empty, it defaults to [foursquare.DefaultAPIBaseURL].
	FoursquareAPIURL string

	// SessionLifetime is TRAVELMAP_SESSION_LIFETIME as a [time.Duration] —
	// [config.Config.SessionLifetime]. Left zero, it defaults to
	// [defaultSessionLifetime].
	SessionLifetime time.Duration

	// SessionCookieSecure is TRAVELMAP_SESSION_COOKIE_SECURE —
	// [config.Config.SessionCookieSecure], which defaults to true. Unlike
	// Timezone or TrackBreak this field's own zero value is not that
	// default: cmd/travelmap always passes config's already-resolved value,
	// and a caller building Options directly gets the cookie sent over
	// plain HTTP too unless it sets this itself.
	SessionCookieSecure bool
}

// api holds the dependencies shared by the handlers. Handlers are methods on
// it rather than free functions, so a new dependency is added in one place
// instead of being threaded through every signature.
type api struct {
	logger               *slog.Logger
	store                store.Store
	timezone             string
	loc                  *time.Location
	trackBreak           time.Duration
	debugLogRequests     bool
	foursquarePushSecret string
	sessions             *scs.SessionManager
	csrf                 *http.CrossOriginProtection
	foursquareOAuth      *foursquareOAuth
}

// newAPI builds the dependencies [New] wires into a router — split out so
// that an internal test can override the Foursquare OAuth endpoint URLs
// before calling [api.newRouter] itself, which New does not expose.
func newAPI(opts Options) *api {
	timezone := opts.Timezone
	if timezone == "" {
		timezone = defaultTimezone
	}

	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}

	trackBreak := opts.TrackBreak
	if trackBreak == 0 {
		trackBreak = defaultTrackBreak
	}

	sessionLifetime := opts.SessionLifetime
	if sessionLifetime == 0 {
		sessionLifetime = defaultSessionLifetime
	}

	return &api{
		logger:               opts.Logger,
		store:                opts.Store,
		timezone:             timezone,
		loc:                  loc,
		trackBreak:           trackBreak,
		debugLogRequests:     opts.DebugLogRequests,
		foursquarePushSecret: opts.FoursquarePushSecret,
		sessions:             newSessionManager(opts.Store, sessionLifetime, opts.SessionCookieSecure),
		csrf:                 http.NewCrossOriginProtection(),
		foursquareOAuth:      newFoursquareOAuth(opts),
	}
}

// New builds the server's HTTP handler.
func New(opts Options) http.Handler {
	return newAPI(opts).newRouter()
}

// newRouter builds the router over a, split out of New so that an internal
// test can build a over [newAPI] with its Foursquare OAuth URLs overridden
// first — see the comment on those fields.
func (a *api) newRouter() http.Handler {
	r := chi.NewRouter()

	// First, so that the line records what the client was actually answered:
	// the 500 the recovery below writes for a panic, and the 404 for a route
	// nothing matched. See [api.logRequests] for why that matters.
	if a.debugLogRequests {
		a.logger.Warn("logging every request, credentials redacted; " +
			"unset TRAVELMAP_DEBUG_LOG_REQUESTS when the capture is done")

		r.Use(a.logRequests)
	}

	// The recovery covers the whole server, not just the API: it is about a
	// bug not taking the process down. The Dawarich headers are compatibility,
	// so they stay on the group they describe.
	r.Use(a.recoverer)

	r.NotFound(a.notFound)
	r.MethodNotAllowed(a.methodNotAllowed(r))

	// Registered at the top level rather than under /api/v1: this is not a
	// Dawarich-compatible endpoint, so it carries none of that group's
	// middleware — no Dawarich headers, no api_key, no requireUser. It
	// authenticates the caller with its own shared secret instead, and only
	// exists at all once one is configured.
	if a.foursquarePushSecret != "" {
		r.Post("/webhooks/foursquare", a.foursquareWebhook)
	}

	// The browser's own group, beside /api/v1 rather than under it: CSRF
	// protection is attached here and only here — /api/v1 is Bearer /
	// api_key only, so nothing it serves can be driven by a cross-origin
	// form.
	r.Group(func(r chi.Router) {
		r.Use(a.sessions.LoadAndSave)
		r.Use(a.loadSessionUser)
		r.Use(a.csrf.Handler)

		r.Get("/", a.index)
		r.Get("/login", a.loginPage)
		r.Post("/login", a.loginSubmit)
		r.Post("/logout", a.logout)
		r.Get("/signup", a.signupPage)
		r.Post("/signup", a.signupSubmit)

		// The Foursquare OAuth flow, in the browser's own session group: the
		// callback is a top-level GET navigation back from Foursquare, which
		// carries the session cookie, so it can require the session's user
		// and the state's own user to be the same one rather than resting on
		// state alone.
		if a.foursquareOAuth.configured() {
			r.Group(func(r chi.Router) {
				r.Use(requireSessionUser)

				r.Get("/foursquare/oauth/start", a.foursquareOAuthStart)
			})

			r.Get(foursquareOAuthCallbackPath, a.foursquareOAuthCallback)
		}
	})

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(staticFiles)))

	r.Route("/api/v1", func(r chi.Router) {
		// authenticate first, for the reason on dawarichHeaders.
		r.Use(a.authenticate)
		r.Use(dawarichHeaders)

		// The two routes reachable without credentials: health is what a
		// client checks a server URL with, and login is where a client with
		// only a password gets a key.
		get(r, "/health", a.health)
		r.Post("/auth/login", a.login)

		// Everything else needs a user. A route registered outside this group
		// is a route that serves one account's data to whoever asks.
		r.Group(func(r chi.Router) {
			r.Use(requireUser)

			get(r, "/users/me", a.usersMe)
			get(r, "/points", a.listPoints)
			r.Post("/points", a.createPoints)
			r.Patch("/points/{id}", a.patchPoint)
			r.Delete("/points/bulk_destroy", a.bulkDeletePoints)
			r.Delete("/points/{id}", a.deletePoint)
			r.Post("/overland/batches", a.createOverlandBatch)
			get(r, "/points/tracked_months", a.trackedMonths)
			get(r, "/stats", a.stats)
		})
	})

	return r
}

// get registers h for both GET and HEAD.
//
// chi answers 405 to the HEAD of a route declared with Get, where upstream
// answers it like any GET; a client that probes with HEAD reads the 405 as
// "nothing to fetch". Registering both here means no later handler has to
// remember it. net/http discards the body of a HEAD response, so h needs no
// branch.
func get(r chi.Router, pattern string, h http.HandlerFunc) {
	r.Get(pattern, h)
	r.Head(pattern, h)
}

// notFound answers a request for a route this server does not serve.
//
// It has to stay a 404 with no usable body: Dawarich has no version
// negotiation, so clients probe an endpoint and read 404 as "this server does
// not have the feature". Answering 200 with an empty array instead tells the
// client the feature exists and it will show UI that then misbehaves.
func (a *api) notFound(w http.ResponseWriter, r *http.Request) {
	a.writeError(w, r, http.StatusNotFound, "not found")
}

// methodNotAllowed answers a known route addressed with a method it does not
// serve. chi registers methods explicitly, which is what makes this reachable.
//
// It takes the router because a 405 owes the client an Allow header (RFC 9110
// makes it a MUST), and chi's own list of allowed methods is unexported: a
// replacement handler cannot read it, so the methods are recovered by asking
// the router which ones would match this path. Those extra lookups only ever
// happen on a request that is already failing.
func (a *api) methodNotAllowed(routes chi.Routes) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if allowed := allowedMethods(routes, r.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}

		a.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// probedMethods are the methods an Allow header is built out of: every method
// this server registers anywhere, so that the answer is complete.
var probedMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
}

// allowedMethods reports which of probedMethods the router would route to a
// handler for path.
func allowedMethods(routes chi.Routes, path string) []string {
	allowed := make([]string, 0, len(probedMethods))

	for _, method := range probedMethods {
		if routes.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}

	return allowed
}
