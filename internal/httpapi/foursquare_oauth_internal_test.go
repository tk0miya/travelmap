package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

const (
	oauthTestClientID     = "the-client-id"
	oauthTestClientSecret = "the-client-secret"
	oauthTestBaseURL      = "https://travelmap.example"
	oauthTestRedirectURL  = oauthTestBaseURL + foursquareOAuthCallbackPath
)

// newOAuthTestUser returns the account the OAuth flow tests sign in as.
func newOAuthTestUser() model.User {
	return model.User{ //nolint:gosec // fixture values, not credentials
		ID:           7,
		Email:        "oauth@example.com",
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "oauth-test-key",
	}
}

// oauthFakeServer stands in for both Foursquare hosts the callback reaches:
// the token endpoint at "/token" and users/self at "/v2/users/self". tokenBody
// and tokenStatus, selfBody and selfStatus decide what each answers, so a test
// can drive either failure path independently.
type oauthFakeServer struct {
	tokenStatus int
	tokenBody   string
	selfStatus  int
	selfBody    string
}

func newOAuthFakeServer(t *testing.T, cfg oauthFakeServer) *httptest.Server {
	t.Helper()

	if cfg.tokenStatus == 0 {
		cfg.tokenStatus = http.StatusOK
	}

	if cfg.tokenBody == "" {
		cfg.tokenBody = `{"access_token": "the-access-token"}`
	}

	if cfg.selfStatus == 0 {
		cfg.selfStatus = http.StatusOK
	}

	if cfg.selfBody == "" {
		cfg.selfBody = `{"response": {"user": {"id": "1709193"}}}`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.WriteHeader(cfg.tokenStatus)
			_, _ = w.Write([]byte(cfg.tokenBody))
		case "/v2/users/self":
			w.WriteHeader(cfg.selfStatus)
			_, _ = w.Write([]byte(cfg.selfBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// newOAuthTestAPI builds the *api the OAuth flow tests exercise: a real
// router over st, with the Foursquare token and users/self endpoints pointed
// at fake instead of the real Foursquare hosts.
func newOAuthTestAPI(t *testing.T, st store.Store, fake *httptest.Server) *api {
	t.Helper()

	a := newAPI(Options{
		Logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:                  st,
		FoursquareClientID:     oauthTestClientID,
		FoursquareClientSecret: oauthTestClientSecret,
		BaseURL:                oauthTestBaseURL,
		FoursquareAPIURL:       fake.URL,
	})

	a.foursquareOAuth.tokenURL = fake.URL + "/token"
	a.foursquareOAuth.httpClient = fake.Client()

	return a
}

// newOAuthTestServer starts the real router over a, the way router_test.go's
// own helper does for the external package — duplicated here since that one
// cannot see newAPI or the unexported fields above it overrides.
func newOAuthTestServer(t *testing.T, a *api) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(a.newRouter())
	t.Cleanup(srv.Close)

	return srv
}

// noRedirectClient is an *http.Client that stops at the first redirect
// instead of following it, so a test can inspect the Location header itself.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// TestFoursquareOAuthStartRequiresASession pins that a browser with no
// session is sent to the login form rather than answered 401 — the
// difference between this route and /api/v1's own requireUser.
func TestFoursquareOAuthStartRequiresASession(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, newOAuthTestUser())
	a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{}))
	srv := newOAuthTestServer(t, a)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/settings/foursquare/connect", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("GET /settings/foursquare/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	if got := resp.Header.Get("Location"); got != "/login" {
		t.Errorf("Location = %q, want /login", got)
	}
}

// TestFoursquareOAuthStartRedirectsToFoursquare pins that a signed-in
// session is redirected to Foursquare with a state bound to that user.
func TestFoursquareOAuthStartRedirectsToFoursquare(t *testing.T) {
	t.Parallel()

	user := newOAuthTestUser()
	st := storetest.New(t, user)
	a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{}))
	srv := newOAuthTestServer(t, a)

	token := plantSession(t, st, user.ID)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/settings/foursquare/connect", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: token}) //nolint:gosec

	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("GET /settings/foursquare/connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location did not parse as a URL: %v", err)
	}

	if got := location.Scheme + "://" + location.Host + location.Path; got != "https://foursquare.com/oauth2/authenticate" {
		t.Errorf("redirect target = %q, want the Foursquare authenticate endpoint", got)
	}

	q := location.Query()
	if got := q.Get("client_id"); got != oauthTestClientID {
		t.Errorf("client_id = %q, want %q", got, oauthTestClientID)
	}

	if got := q.Get("redirect_uri"); got != oauthTestRedirectURL {
		t.Errorf("redirect_uri = %q, want %q", got, oauthTestRedirectURL)
	}

	state := q.Get("state")
	if state == "" {
		t.Fatal("the redirect carried no state")
	}

	// Consuming it here is destructive (Consume is single-use), which is
	// fine: this test's only interest in the state is that it names user.ID.
	if userID, ok := a.foursquareOAuth.states.Consume(state); !ok || userID != user.ID {
		t.Errorf("the state named user %d (ok=%v), want %d", userID, ok, user.ID)
	}
}

// TestFoursquareOAuthRoutesAbsentWhenUnconfigured pins that the routes do
// not exist at all unless every one of the three settings is present — the
// same reasoning as POST /webhooks/foursquare's own FoursquarePushSecret.
// Each case leaves exactly one of the three unset, including BaseURL on its
// own: that is the one combination foursquareOAuth.configured has to reject
// correctly, since the callback URL it derives from BaseURL is never itself
// empty (it would just be the bare callback path).
func TestFoursquareOAuthRoutesAbsentWhenUnconfigured(t *testing.T) {
	t.Parallel()

	tests := map[string]Options{
		"nothing set": {},
		"BaseURL missing": {
			FoursquareClientID:     oauthTestClientID,
			FoursquareClientSecret: oauthTestClientSecret,
		},
		"FoursquareClientID missing": {
			FoursquareClientSecret: oauthTestClientSecret,
			BaseURL:                oauthTestBaseURL,
		},
		"FoursquareClientSecret missing": {
			FoursquareClientID: oauthTestClientID,
			BaseURL:            oauthTestBaseURL,
		},
	}

	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			opts.Store = storetest.New(t)

			a := newAPI(opts)
			srv := httptest.NewServer(a.newRouter())
			t.Cleanup(srv.Close)

			for _, path := range []string{"/settings/foursquare/connect", "/foursquare/oauth/callback"} {
				resp, err := srv.Client().Get(srv.URL + path)
				if err != nil {
					t.Fatalf("GET %s: %v", path, err)
				}

				if err := resp.Body.Close(); err != nil {
					t.Errorf("closing the response body: %v", err)
				}

				if resp.StatusCode != http.StatusNotFound {
					t.Errorf("GET %s: status = %d, want %d", path, resp.StatusCode, http.StatusNotFound)
				}
			}
		})
	}
}

// TestFoursquareOAuthStartTrimsBaseURLTrailingSlash pins that a BaseURL an
// operator copy-pasted with a trailing slash (plausible, out of a reverse
// proxy config) does not produce a doubled slash in redirect_uri — a value
// that would then no longer match what is registered on the Foursquare
// application.
func TestFoursquareOAuthStartTrimsBaseURLTrailingSlash(t *testing.T) {
	t.Parallel()

	user := newOAuthTestUser()
	st := storetest.New(t, user)

	a := newAPI(Options{
		Logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:                  st,
		FoursquareClientID:     oauthTestClientID,
		FoursquareClientSecret: oauthTestClientSecret,
		BaseURL:                oauthTestBaseURL + "/",
	})
	srv := newOAuthTestServer(t, a)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/settings/foursquare/connect", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: plantSession(t, st, user.ID)}) //nolint:gosec

	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("GET /settings/foursquare/connect: %v", err)
	}
	defer resp.Body.Close()

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("Location did not parse as a URL: %v", err)
	}

	if got := location.Query().Get("redirect_uri"); got != oauthTestRedirectURL {
		t.Errorf("redirect_uri = %q, want %q (a single slash between BaseURL and the callback path)",
			got, oauthTestRedirectURL)
	}
}

// startOAuthFlow mints a state the way foursquareOAuthStart does, so a
// callback test can present one without driving the whole redirect through
// an HTTP round trip.
func startOAuthFlow(t *testing.T, a *api, userID int64) string {
	t.Helper()

	state, err := a.foursquareOAuth.states.New(userID)
	if err != nil {
		t.Fatalf("minting a state: %v", err)
	}

	return state
}

// callbackURL builds the callback request URL carrying state and code.
func callbackURL(base, state, code string) string {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}

	if code != "" {
		q.Set("code", code)
	}

	return base + "/foursquare/oauth/callback?" + q.Encode()
}

// TestFoursquareOAuthCallbackLinksTheAccount covers the success path end to
// end: a valid state naming the signed-in session's own user, a token
// exchange and a users/self call both answered by the fake server, and the
// foursquare_accounts row it leaves behind.
func TestFoursquareOAuthCallbackLinksTheAccount(t *testing.T) {
	t.Parallel()

	user := newOAuthTestUser()
	st := storetest.New(t, user)
	a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{}))
	srv := newOAuthTestServer(t, a)

	sessionToken := plantSession(t, st, user.ID)
	state := startOAuthFlow(t, a, user.ID)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		callbackURL(srv.URL, state, "the-code"), nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken}) //nolint:gosec

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /foursquare/oauth/callback: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, http.StatusOK, body)
	}

	account, err := st.FoursquareAccounts().ByFoursquareUserID(context.Background(), "1709193")
	if err != nil {
		t.Fatalf("looking up the linked account: %v", err)
	}

	if account.UserID != user.ID {
		t.Errorf("the linked account's UserID = %d, want %d", account.UserID, user.ID)
	}

	if account.AccessToken != "the-access-token" {
		t.Errorf("AccessToken = %q, want %q", account.AccessToken, "the-access-token")
	}
}

// TestFoursquareOAuthCallbackRefuses covers every way the callback has to
// say no before it ever reaches Foursquare, plus the one failure that comes
// back from the fake server instead.
func TestFoursquareOAuthCallbackRefuses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		noState      bool
		mismatchUser bool
		noSession    bool
		noCode       bool
		tokenStatus  int
		selfStatus   int
		wantStatus   int
	}{
		"no state at all": {
			noState:    true,
			wantStatus: http.StatusForbidden,
		},
		"a state naming a different user than the session": {
			mismatchUser: true,
			wantStatus:   http.StatusForbidden,
		},
		"no session at all": {
			noSession:  true,
			wantStatus: http.StatusForbidden,
		},
		"no code parameter": {
			noCode:     true,
			wantStatus: http.StatusBadRequest,
		},
		"the token exchange fails": {
			tokenStatus: http.StatusBadRequest,
			wantStatus:  http.StatusBadGateway,
		},
		"the users/self call fails": {
			selfStatus: http.StatusUnauthorized,
			wantStatus: http.StatusBadGateway,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			user := newOAuthTestUser()
			st := storetest.New(t, user)
			a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{
				tokenStatus: tt.tokenStatus,
				selfStatus:  tt.selfStatus,
			}))
			srv := newOAuthTestServer(t, a)

			stateUserID := user.ID
			if tt.mismatchUser {
				stateUserID = user.ID + 1
			}

			state := startOAuthFlow(t, a, stateUserID)
			if tt.noState {
				state = ""
			}

			code := "the-code"
			if tt.noCode {
				code = ""
			}

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
				callbackURL(srv.URL, state, code), nil)
			if err != nil {
				t.Fatalf("building the request: %v", err)
			}

			if !tt.noSession {
				req.AddCookie(&http.Cookie{Name: "session", Value: plantSession(t, st, user.ID)}) //nolint:gosec
			}

			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET /foursquare/oauth/callback: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestFoursquareOAuthCallbackConflict pins that linking an account that is
// already linked to someone answers 409 rather than 500 — [store.ErrConflict]
// is an operator's mistake (or a retry), not a server fault.
func TestFoursquareOAuthCallbackConflict(t *testing.T) {
	t.Parallel()

	user := newOAuthTestUser()
	st := storetest.New(t, user)

	if _, err := st.FoursquareAccounts().Create(context.Background(), model.FoursquareAccount{
		UserID:           user.ID,
		FoursquareUserID: "1709193",
		AccessToken:      "already-linked",
	}); err != nil {
		t.Fatalf("seeding the existing link: %v", err)
	}

	a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{}))
	srv := newOAuthTestServer(t, a)

	sessionToken := plantSession(t, st, user.ID)
	state := startOAuthFlow(t, a, user.ID)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		callbackURL(srv.URL, state, "the-code"), nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken}) //nolint:gosec

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /foursquare/oauth/callback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

// TestFoursquareOAuthCallbackStoreFailure pins the 500 row: a database that
// cannot write foursquare_accounts, but can still resolve the session's
// user — [storetest.Unavailable] closes the whole database, which would
// fail the session lookup [loadSessionUser] does before this handler ever
// runs, so this needs the narrower failure only the write itself hits.
func TestFoursquareOAuthCallbackStoreFailure(t *testing.T) {
	t.Parallel()

	user := newOAuthTestUser()
	st := storetest.UnavailableFoursquareAccounts(t, user)
	a := newOAuthTestAPI(t, st, newOAuthFakeServer(t, oauthFakeServer{}))
	srv := newOAuthTestServer(t, a)

	sessionToken := plantSession(t, st, user.ID)
	state := startOAuthFlow(t, a, user.ID)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		callbackURL(srv.URL, state, "the-code"), nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken}) //nolint:gosec

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /foursquare/oauth/callback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}
