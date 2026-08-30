package httpapi

import (
	"net/http"
	"time"

	"github.com/tk0miya/travelmap/internal/foursquare"
)

// foursquareHTTPTimeout bounds a call to Foursquare made while a browser is
// waiting on the OAuth flow to finish — long enough for a slow response, not
// long enough to hold the request open indefinitely if Foursquare never
// answers at all.
const foursquareHTTPTimeout = 10 * time.Second

// foursquareOAuthCallbackPath is GET /foursquare/oauth/callback's path,
// named once so that registering the route and deriving its full external
// URL from [Options.BaseURL] can't drift apart.
const foursquareOAuthCallbackPath = "/foursquare/oauth/callback"

// foursquareOAuth holds everything the Foursquare OAuth flow needs, grouped
// on its own rather than flattened onto [api] — the same reasoning that
// already keeps sessions and csrf their own fields there, and it is what
// keeps that struct's own field list from growing one line per setting a
// single feature happens to need.
//
// baseURL is kept as given rather than turned into the derived callback URL
// once and cached: configured is cheap to compute from the one fact this
// type actually holds, and the callback URL itself is the handlers' own to
// build — see foursquareOAuthCallbackURL in foursquare.go.
type foursquareOAuth struct {
	clientID     string
	clientSecret string
	baseURL      string

	httpClient *http.Client
	states     *oauthStateStore

	// apiBaseURL is TRAVELMAP_FOURSQUARE_API_URL — [config.Config.FoursquareAPIURL]
	// — the same setting the check-in fetch client points at a fake server
	// with, reused here rather than a second setting of its own.
	apiBaseURL string

	// tokenURL is the one Foursquare endpoint this type calls that has no
	// real setting to reuse: nothing else ever has a reason to point
	// Foursquare's own OAuth token exchange anywhere but Foursquare, so an
	// internal test overrides this field directly, built through newAPI
	// rather than New. There is no equivalent field for the authenticate
	// endpoint [foursquare.AuthenticateURL] redirects to: that function
	// takes no endpoint argument at all, so there is nothing here to store
	// or override for it.
	tokenURL string
}

// newFoursquareOAuth builds the OAuth dependencies from opts.
func newFoursquareOAuth(opts Options) *foursquareOAuth {
	apiBaseURL := opts.FoursquareAPIURL
	if apiBaseURL == "" {
		apiBaseURL = foursquare.DefaultAPIBaseURL
	}

	return &foursquareOAuth{
		clientID:     opts.FoursquareClientID,
		clientSecret: opts.FoursquareClientSecret,
		baseURL:      opts.BaseURL,
		httpClient:   &http.Client{Timeout: foursquareHTTPTimeout},
		states:       newOAuthStateStore(),
		apiBaseURL:   apiBaseURL,
		tokenURL:     foursquare.DefaultTokenURL,
	}
}

// configured reports whether enough is set to run the OAuth flow at all:
// the client id, the client secret, and BaseURL to derive the callback URL
// from. GET /settings/foursquare/connect and its callback are registered
// only when this is true, the same reasoning as FoursquarePushSecret.
func (f *foursquareOAuth) configured() bool {
	return f.clientID != "" && f.clientSecret != "" && f.baseURL != ""
}
