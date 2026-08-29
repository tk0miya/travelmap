package httpapi_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/httpapi"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// The fixture's own identity: the secret it was built with, and the
// checkin.user.id / checkin.id it carries. Shared with internal/foursquare's
// own test of the same file.
const (
	foursquarePushSecret  = "shh-its-a-secret"
	foursquareUserID      = "1709193"
	foursquareCheckinID   = "5f2a1b3c4d5e6f708192a3b4"
	foursquareWebhookPath = "/webhooks/foursquare"
)

// foursquarePushBody reads the fixture built to match a Swarm User Push
// notification's documented shape — see internal/foursquare/testdata for why
// it is synthetic rather than a live capture.
func foursquarePushBody(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "foursquare", "testdata", "push_body.txt"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	// The file ends in the trailing newline every text file does; the body
	// it represents does not.
	return strings.TrimRight(string(raw), "\n")
}

// withFormBody sends body as an application/x-www-form-urlencoded request
// body, the content type a Swarm User Push notification arrives with.
func withFormBody(body string) requestOption {
	return func(r *http.Request) {
		withBody(body)(r)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
}

// linkFoursquareAccount links foursquareUserID to testUser, which is what
// lets a pushed check-in for it resolve onto a travelmap account.
func linkFoursquareAccount(t *testing.T, st store.Store) {
	t.Helper()

	if _, err := st.FoursquareAccounts().Create(t.Context(), model.FoursquareAccount{
		UserID:           1,
		FoursquareUserID: foursquareUserID,
		AccessToken:      "the-access-token",
	}); err != nil {
		t.Fatalf("linking the Foursquare account: %v", err)
	}
}

// probeCheckinSource upserts a throwaway write for foursquareCheckinID and
// returns the source the row comes back with. Upsert never changes source on
// a conflict, so this is a read of what the first write left behind without
// the store interface needing a dedicated lookup: an unexpected "probe" back
// says nothing arrived through the webhook at all, and the id on the
// returned row is what a second call compares against to confirm one row.
func probeCheckin(t *testing.T, st store.Store) model.Checkin {
	t.Helper()

	got, err := st.Checkins().Upsert(t.Context(), model.Checkin{
		UserID:              1,
		FoursquareCheckinID: foursquareCheckinID,
		CheckedInAt:         testCreatedAt,
		Source:              "probe",
		Raw:                 "{}",
	})
	if err != nil {
		t.Fatalf("probing the stored check-in: %v", err)
	}

	return got
}

// TestFoursquareWebhookIsNotRegisteredWithoutASecret pins that an
// unconfigured server answers 404 rather than 401 to every request, which is
// what lets a server with the feature unconfigured be told apart from one
// refusing a request — see "An endpoint this server does not implement
// answers 404" in docs/api-notes.md.
func TestFoursquareWebhookIsNotRegisteredWithoutASecret(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(foursquarePushBody(t)))

	if resp.status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
	}
}

// newFoursquareTestServer starts the router with the webhook configured,
// over a store holding testUser.
func newFoursquareTestServer(t *testing.T, st store.Store) *httptest.Server {
	t.Helper()

	return newTestServerWithOptions(t, httpapi.Options{Store: st, FoursquarePushSecret: foursquarePushSecret})
}

// TestFoursquareWebhookStoresAndIsIdempotent is this route's own completion
// condition: replaying the recorded payload stores one check-in, and
// replaying it again still leaves one.
func TestFoursquareWebhookStoresAndIsIdempotent(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	srv := newFoursquareTestServer(t, st)
	body := foursquarePushBody(t)

	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(body))
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty", resp.body)
	}

	first := probeCheckin(t, st)
	if first.Source != "push" {
		t.Fatalf("Source = %q, want %q — nothing reached the webhook", first.Source, "push")
	}

	resp = do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(body))
	if resp.status != http.StatusOK {
		t.Fatalf("the replay's status = %d, want %d", resp.status, http.StatusOK)
	}

	second := probeCheckin(t, st)
	if second.ID != first.ID {
		t.Errorf("the replay landed on id %d, want the first write's %d", second.ID, first.ID)
	}
}

// TestFoursquareWebhookRejectsTheWrongSecret pins the 401 case documented in
// docs/api-notes.md: a request whose secret does not match is refused with
// an empty body, before anything is parsed or stored.
func TestFoursquareWebhookRejectsTheWrongSecret(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	srv := newFoursquareTestServer(t, st)

	body := url.Values{
		"secret":  {"not-the-secret"},
		"checkin": {`{"id":"` + foursquareCheckinID + `","user":{"id":"` + foursquareUserID + `"}}`},
	}.Encode()

	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(body))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty", resp.body)
	}
}

// TestFoursquareWebhookAnswersOKForAnUnlinkedAccount pins the other 200 row:
// a push naming a Foursquare user id nothing here has linked is handled
// correctly, not refused, since the check-in is not this server's to store.
func TestFoursquareWebhookAnswersOKForAnUnlinkedAccount(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newTestStore(t))

	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(foursquarePushBody(t)))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}
}

// TestFoursquareWebhookAnswersOKForAMissingCheckinParameter pins the row a
// User Push notification never actually triggers: the form parsed, the
// secret matched, but there is nothing to store, which is handled rather
// than refused since nothing is documented about what a non-2xx does.
func TestFoursquareWebhookAnswersOKForAMissingCheckinParameter(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newTestStore(t))

	body := url.Values{"secret": {foursquarePushSecret}}.Encode()
	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(body))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}
}

// TestFoursquareWebhookAnswersOKForAnUnparseableCheckin covers a "checkin"
// value that is not JSON, answered the same way as one missing entirely:
// logged and dropped rather than refused.
func TestFoursquareWebhookAnswersOKForAnUnparseableCheckin(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newTestStore(t))

	body := url.Values{"secret": {foursquarePushSecret}, "checkin": {"not json"}}.Encode()
	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(body))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}
}

// TestFoursquareWebhookRejectsAnUnparseableForm pins the 400 row: a body that
// is not form-encoded at all, which is a bug in whatever sent it rather than
// something to handle.
func TestFoursquareWebhookRejectsAnUnparseableForm(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newTestStore(t))

	// A percent-encoding escape that decodes to nothing valid, which is what
	// makes net/http.Request.ParseForm itself fail rather than the JSON it
	// eventually carries.
	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody("checkin=%zz"))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty", resp.body)
	}
}

// TestFoursquareWebhookReportsAStoreFailure pins the 500 row: a database
// that cannot be read is this server's own fault, not a reason to tell
// Foursquare the push was refused.
func TestFoursquareWebhookReportsAStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newUnavailableStore(t))

	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(foursquarePushBody(t)))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}
}

// TestFoursquareWebhookCarriesNoDawarichHeaders pins that this route sits
// outside the Dawarich-compatible group entirely: none of that group's
// middleware runs here.
func TestFoursquareWebhookCarriesNoDawarichHeaders(t *testing.T) {
	t.Parallel()

	srv := newFoursquareTestServer(t, newTestStore(t))
	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(foursquarePushBody(t)))

	if got := resp.header.Get("X-Dawarich-Response"); got != "" {
		t.Errorf("X-Dawarich-Response = %q, want it unset", got)
	}

	if got := resp.header.Get("X-Dawarich-Version"); got != "" {
		t.Errorf("X-Dawarich-Version = %q, want it unset", got)
	}
}

// TestFoursquareWebhookRequestBodyIsNeverLogged pins the one thing this
// server's request logger promises every route (see
// internal/httpapi/requestlog.go), exercised here by the one route whose
// body carries a credential: the secret must never reach the log, and
// neither may anything else from the body.
func TestFoursquareWebhookRequestBodyIsNeverLogged(t *testing.T) {
	t.Parallel()

	logs := &logBuffer{}
	st := newTestStore(t)
	linkFoursquareAccount(t, st)

	handler := httpapi.New(httpapi.Options{
		Logger:               slog.New(slog.NewTextHandler(logs, nil)),
		Store:                st,
		DebugLogRequests:     true,
		FoursquarePushSecret: foursquarePushSecret,
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp := do(t, srv, http.MethodPost, foursquareWebhookPath, withFormBody(foursquarePushBody(t)))
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	got := logs.String()

	if strings.Contains(got, foursquarePushSecret) {
		t.Errorf("the push secret reached the log: %q", got)
	}

	if strings.Contains(got, foursquareCheckinID) || strings.Contains(got, foursquareUserID) {
		t.Errorf("the request body reached the log: %q", got)
	}

	if !strings.Contains(got, "path="+foursquareWebhookPath) {
		t.Errorf("log = %q, want it to record the request was made", got)
	}
}
