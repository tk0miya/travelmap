package foursquare_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/foursquare"
)

// pageLimit is the `limit` the client asks for, repeated here because a test
// of the paging rules has to build a page of exactly that size: a full page
// and a short one are what the two rules are told apart by.
const pageLimit = 250

// discardLogger is the logger a test that is not about logging hands the
// client.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// checkinsResponse reads the recorded response fixture: a
// GET /v2/users/self/checkins body built to match the documented envelope and
// the fields an observed check-in carries, since no live capture was
// available to record one from.
func checkinsResponse(t *testing.T) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", "checkins_response.json"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return body
}

// serve starts a server answering every request with handler and returns a
// client pointed at it.
func serve(t *testing.T, handler http.HandlerFunc) *foursquare.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return foursquare.NewClient(server.URL, discardLogger())
}

// TestCheckinsSendsTheDocumentedRequest pins what goes on the wire: the
// pinned version, the maximum page size, the offset, and the token in a
// header rather than in the query.
func TestCheckinsSendsTheDocumentedRequest(t *testing.T) {
	t.Parallel()

	var got *http.Request

	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())

		_, _ = w.Write(checkinsResponse(t))
	})

	if _, err := client.Checkins(t.Context(), "the-token",
		foursquare.CheckinsQuery{Offset: 500}); err != nil {
		t.Fatalf("Checkins returned %v", err)
	}

	if got.URL.Path != "/v2/users/self/checkins" {
		t.Errorf("path = %q, want %q", got.URL.Path, "/v2/users/self/checkins")
	}

	if auth := got.Header.Get("Authorization"); auth != "Bearer the-token" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer the-token")
	}

	if raw := got.URL.Query().Get("oauth_token"); raw != "" {
		t.Errorf("oauth_token = %q, want the token sent in the header only", raw)
	}

	want := map[string]string{
		"limit":  strconv.Itoa(pageLimit),
		"offset": "500",
	}

	for name, value := range want {
		if sent := got.URL.Query().Get(name); sent != value {
			t.Errorf("%s = %q, want %q", name, sent, value)
		}
	}

	// The exact date is this client's own to pin; that one is sent at all is
	// what the API requires.
	if v := got.URL.Query().Get("v"); len(v) != len("20260824") {
		t.Errorf("v = %q, want a YYYYMMDD date", v)
	}
}

// TestCheckinsOmitsTheOffsetOnTheFirstRequest covers the other half of the
// query: a run's first request asks for the newest page, with nothing to skip
// yet, and says so by leaving the parameter off rather than sending a zero.
func TestCheckinsOmitsTheOffsetOnTheFirstRequest(t *testing.T) {
	t.Parallel()

	var got *http.Request

	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())

		_, _ = w.Write(checkinsResponse(t))
	})

	if _, err := client.Checkins(t.Context(), "the-token", foursquare.CheckinsQuery{}); err != nil {
		t.Fatalf("Checkins returned %v", err)
	}

	if _, ok := got.URL.Query()["offset"]; ok {
		t.Errorf("offset = %q, want it absent", got.URL.Query().Get("offset"))
	}
}

// TestCheckinsDecodesTheRecordedResponse pins the wire shape a page parses
// to, the fixture playing the part a golden file plays for a response this
// server writes. Raw is checked separately, since it is the one field the
// decoder does not fill in itself.
func TestCheckinsDecodesTheRecordedResponse(t *testing.T) {
	t.Parallel()

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(checkinsResponse(t))
	})

	page, err := client.Checkins(t.Context(), "the-token", foursquare.CheckinsQuery{})
	if err != nil {
		t.Fatalf("Checkins returned %v", err)
	}

	if len(page) != 2 {
		t.Fatalf("the page holds %d check-ins, want 2", len(page))
	}

	offset := 540
	shout := "lunch"

	want := foursquare.Checkin{
		ID:             "5f2a1b3c4d5e6f708192a3b5",
		CreatedAt:      1767139506,
		TimeZoneOffset: &offset,
		Shout:          &shout,
		Venue: &foursquare.Venue{
			ID:   "4b0588a2f964a520f66422e3",
			Name: "Tsukiji Market",
			Location: foursquare.Location{
				Lat: 35.6654, Lng: 139.7707,
				CC: "JP", City: "中央区", State: "東京都", Country: "日本",
			},
			Categories: []foursquare.Category{
				{ID: "4bf58dd8d48988d1fa941735", Name: "Market", Primary: true},
			},
		},
		Raw: page[1].Raw,
	}

	if diff := cmp.Diff(want, page[1]); diff != "" {
		t.Errorf("the second check-in differs (-want +got):\n%s", diff)
	}

	// A fetched check-in keeps its own bytes, which is what the checkins
	// table stores: the same item, decoded again, has to come back out.
	var raw struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(page[1].Raw, &raw); err != nil {
		t.Fatalf("the check-in's Raw is not the JSON it arrived as: %v", err)
	}

	if raw.ID != page[1].ID {
		t.Errorf("Raw holds the check-in %q, want %q", raw.ID, page[1].ID)
	}
}

// TestCheckinsReportsARefusal covers reading `meta` rather than the status
// alone: Foursquare's rate limit arrives as a 403, so what a caller has to
// branch on is errorType, and requestId is what a support question quotes.
func TestCheckinsReportsARefusal(t *testing.T) {
	t.Parallel()

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"meta":{"code":403,"errorType":"rate_limit_exceeded",` +
			`"errorDetail":"Quota exceeded","requestId":"abc123"},"response":{}}`))
	})

	_, err := client.Checkins(t.Context(), "the-token", foursquare.CheckinsQuery{})

	var apiErr *foursquare.APIError

	if !errors.As(err, &apiErr) {
		t.Fatalf("Checkins returned %v, want an *APIError", err)
	}

	if apiErr.StatusCode != http.StatusForbidden || apiErr.ErrorType != "rate_limit_exceeded" {
		t.Errorf("the error is %+v, want the 403 rate_limit_exceeded it was sent", apiErr)
	}

	if apiErr.RequestID != "abc123" {
		t.Errorf("RequestID = %q, want %q", apiErr.RequestID, "abc123")
	}
}

// TestCheckinsLogsADeprecationNotice covers the one case the documentation
// names as arriving on a 200: the pinned version, or a field read out of it,
// being on its way out. It is the only warning this client gets before a
// pinned version stops answering, so it is logged rather than dropped.
func TestCheckinsLogsADeprecationNotice(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"meta":{"code":200,"errorType":"deprecated",` +
			`"errorDetail":"the v parameter is deprecated"},` +
			`"response":{"checkins":{"count":0,"items":[]}}}`))
	}))
	t.Cleanup(server.Close)

	client := foursquare.NewClient(server.URL,
		slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))

	if _, err := client.Checkins(t.Context(), "the-token", foursquare.CheckinsQuery{}); err != nil {
		t.Fatalf("Checkins returned %v, want the response to be accepted", err)
	}

	if !strings.Contains(logged.String(), "deprecated") {
		t.Errorf("the log holds %q, want the deprecation notice in it", logged.String())
	}
}

// page renders a response holding count check-ins, the newest at newest and
// each one a second older than the last — the shape the client pages
// backwards through.
func page(t *testing.T, newest time.Time, count int) []byte {
	t.Helper()

	items := make([]string, 0, count)

	for i := range count {
		items = append(items, fmt.Sprintf(`{"id":"checkin-%d","createdAt":%d}`, i, newest.Unix()-int64(i)))
	}

	return fmt.Appendf(nil, `{"meta":{"code":200},"response":{"checkins":{"count":%d,"items":[%s]}}}`,
		count, strings.Join(items, ","))
}

// TestEachCheckinPageStopsOnAShortPage pins the ordinary end of a walk: a
// page shorter than the limit is all there is, and the walk stops without
// asking for another. It is why a fetch of a quiet fortnight costs one
// request rather than two.
func TestEachCheckinPageStopsOnAShortPage(t *testing.T) {
	t.Parallel()

	requests := 0
	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++

		_, _ = w.Write(page(t, newest, 3))
	})

	var collected []foursquare.Checkin

	err := client.EachCheckinPage(t.Context(), "the-token", newest.AddDate(0, 0, -14),
		func(page []foursquare.Checkin) error {
			collected = append(collected, page...)

			return nil
		})
	if err != nil {
		t.Fatalf("EachCheckinPage returned %v", err)
	}

	if requests != 1 {
		t.Errorf("the walk made %d requests, want 1", requests)
	}

	if len(collected) != 3 {
		t.Errorf("the walk collected %d check-ins, want 3", len(collected))
	}
}

// TestEachCheckinPageWalksByOffset pins how the walk moves: each request
// skips the pages already read, by the documented offset, and the offsets go
// up by a whole page at a time.
func TestEachCheckinPageWalksByOffset(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	oldest := newest.Add(-time.Duration(pageLimit-1) * time.Second)

	var offsets []string

	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))

		if len(offsets) == 1 {
			_, _ = w.Write(page(t, newest, pageLimit))

			return
		}

		_, _ = w.Write(page(t, oldest.Add(-time.Second), 1))
	})

	pages := 0

	err := client.EachCheckinPage(t.Context(), "the-token", newest.AddDate(0, 0, -14),
		func([]foursquare.Checkin) error {
			pages++

			return nil
		})
	if err != nil {
		t.Fatalf("EachCheckinPage returned %v", err)
	}

	if pages != 2 {
		t.Errorf("the walk delivered %d pages, want 2", pages)
	}

	want := []string{"", strconv.Itoa(pageLimit)}

	if diff := cmp.Diff(want, offsets); diff != "" {
		t.Errorf("the offsets sent differ (-want +got):\n%s", diff)
	}
}

// TestEachCheckinPageStopsAndTrimsAtTheWindowStart pins where the window's
// lower bound is applied: to the pages here. A page reaching past it ends the
// walk, and the check-ins older than it are not handed over.
func TestEachCheckinPageStopsAndTrimsAtTheWindowStart(t *testing.T) {
	t.Parallel()

	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	// A full page one second apart per check-in, so the window can be put
	// inside it: the newest half is in, the oldest half is out.
	inside := pageLimit / 2
	after := newest.Add(-time.Duration(inside-1) * time.Second)

	requests := 0

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++

		_, _ = w.Write(page(t, newest, pageLimit))
	})

	var collected []foursquare.Checkin

	err := client.EachCheckinPage(t.Context(), "the-token", after,
		func(page []foursquare.Checkin) error {
			collected = append(collected, page...)

			return nil
		})
	if err != nil {
		t.Fatalf("EachCheckinPage returned %v", err)
	}

	// One: the page already reaches past the window, so there is nothing
	// older left to ask for.
	if requests != 1 {
		t.Errorf("the walk made %d requests, want 1", requests)
	}

	if len(collected) != inside {
		t.Errorf("the walk collected %d check-ins, want the %d inside the window", len(collected), inside)
	}

	for _, checkin := range collected {
		if checkin.CreatedAt < after.Unix() {
			t.Errorf("a check-in created at %d is older than the window start %d", checkin.CreatedAt, after.Unix())
		}
	}
}

// TestEachCheckinPageFailsWhenTheWalkCannotAdvance is the other half of the
// pair above, and the reason the conditions are kept apart: a server
// answering every request with the same full page — the shape an offset that
// is accepted and ignored produces — makes the walk fail, rather than quietly
// reporting a successful sync of that one page.
//
// The assertion is the failure, not merely that the walk ends: terminating
// alone is what this would still do with the progress check missing.
func TestEachCheckinPageFailsWhenTheWalkCannotAdvance(t *testing.T) {
	t.Parallel()

	requests := 0
	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++

		// The same page every time, offset or no offset.
		_, _ = w.Write(page(t, newest, pageLimit))
	})

	err := client.EachCheckinPage(t.Context(), "the-token", newest.AddDate(0, 0, -14),
		func([]foursquare.Checkin) error { return nil })

	if !errors.Is(err, foursquare.ErrNoProgress) {
		t.Fatalf("EachCheckinPage returned %v, want ErrNoProgress", err)
	}

	// Two: the first request has no page before it to have failed to reach
	// past, so the second is the earliest one the check can judge.
	if requests != 2 {
		t.Errorf("the walk made %d requests, want 2", requests)
	}
}

// TestEachCheckinPageEndsOnAShortPageThatDidNotAdvance is the case that
// separates the end of the data from a walk that cannot move. Check-ins
// arriving mid-walk push the list along, so a last page can hold only
// check-ins newer than the page before it and still be the end of the data. A
// short page is that end whatever its timestamps say, and judging it by them
// would fail a walk that finished correctly.
func TestEachCheckinPageEndsOnAShortPageThatDidNotAdvance(t *testing.T) {
	t.Parallel()

	requests := 0
	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++

		if requests == 1 {
			_, _ = w.Write(page(t, newest, pageLimit))

			return
		}

		// Newer than everything the first page held, which is what an
		// arriving check-in leaves behind at a boundary.
		_, _ = w.Write(page(t, newest.Add(time.Hour), 2))
	})

	pages := 0

	err := client.EachCheckinPage(t.Context(), "the-token", newest.AddDate(0, 0, -14),
		func([]foursquare.Checkin) error {
			pages++

			return nil
		})
	if err != nil {
		t.Fatalf("EachCheckinPage returned %v, want the short page to end the walk", err)
	}

	if pages != 2 {
		t.Errorf("the walk delivered %d pages, want 2", pages)
	}
}

// TestEachCheckinPageStopsOnACallbackError covers the walk's other exit: what
// the caller reports stops it where it stands, which is how a failed write
// leaves synced_through alone.
func TestEachCheckinPageStopsOnACallbackError(t *testing.T) {
	t.Parallel()

	requests := 0
	newest := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	client := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++

		_, _ = w.Write(page(t, newest, pageLimit))
	})

	wanted := errors.New("the store is unavailable")

	err := client.EachCheckinPage(t.Context(), "the-token", newest.AddDate(0, 0, -14),
		func([]foursquare.Checkin) error { return wanted })

	if !errors.Is(err, wanted) {
		t.Fatalf("EachCheckinPage returned %v, want the callback's own error", err)
	}

	if requests != 1 {
		t.Errorf("the walk made %d requests, want 1", requests)
	}
}
