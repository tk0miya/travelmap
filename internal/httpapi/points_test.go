package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// validLocationsBody is one GeoJSON Feature carrying every property this
// server stores, for the tests that only care that ingest works.
const validLocationsBody = `{
	"locations": [{
		"type": "Feature",
		"geometry": {"type": "Point", "coordinates": [13.356718, 52.502397]},
		"properties": {
			"timestamp": "2021-06-01T12:00:00Z",
			"altitude": 10,
			"speed": 5,
			"horizontal_accuracy": 4,
			"vertical_accuracy": 3,
			"course": 90,
			"course_accuracy": 1,
			"battery_state": "charging",
			"battery_level": 0.5,
			"wifi": "home",
			"track_id": "track-1",
			"device_id": "device-1"
		}
	}]
}`

// TestCreatePoints covers the endpoint the app uploads tracked locations
// through, which answers 200 unlike its overland/batches twin.
func TestCreatePoints(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "points_created.json", resp.body)
}

// TestCreateOverlandBatch covers the endpoint's other name: the same body,
// answered with 201 instead of 200.
func TestCreateOverlandBatch(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/overland/batches?api_key="+testAPIKey, withBody(validLocationsBody))

	if resp.status != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.status, http.StatusCreated)
	}

	assertGolden(t, "overland_batch_created.json", resp.body)
}

// TestCreateOverlandBatchAcceptsTheOverlandTimestampFormat pins that a
// timestamp in the original Overland iOS app's own layout — no colon in the
// zone offset, e.g. "-0700" rather than "-07:00" — is accepted rather than
// silently dropped as an unparseable Feature. Without this, a real Overland
// client's batches would all come back empty behind a 201.
func TestCreateOverlandBatchAcceptsTheOverlandTimestampFormat(t *testing.T) {
	t.Parallel()

	body := `{
		"locations": [{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [-122.4183, 37.7758]},
			"properties": {"timestamp": "2015-10-01T08:00:00-0700"}
		}]
	}`

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/overland/batches?api_key="+testAPIKey, withBody(body))

	if resp.status != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.status, http.StatusCreated)
	}

	assertGolden(t, "points_created.json", resp.body)
}

// TestCreatePointsDeduplicates pins that a point already stored at the same
// (user, timestamp) is silently dropped rather than duplicated or refused —
// the batch_size settings.mobile allows means a client is expected to resend
// overlapping ranges.
func TestCreatePointsDeduplicates(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	first := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))
	if first.status != http.StatusOK {
		t.Fatalf("the first request: status = %d, want %d", first.status, http.StatusOK)
	}

	second := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))
	if second.status != http.StatusOK {
		t.Fatalf("the second request: status = %d, want %d", second.status, http.StatusOK)
	}

	assertGolden(t, "points_created_none.json", second.body)
}

// TestCreatePointsSkipsInvalidFeatures pins that a Feature with no usable
// coordinates or timestamp is dropped rather than failing the whole batch —
// the same tolerance upstream's own parser has, so that one bad sample in a
// batch does not cost the rest of it.
func TestCreatePointsSkipsInvalidFeatures(t *testing.T) {
	t.Parallel()

	body := `{
		"locations": [
			{"type": "Feature", "geometry": {"type": "Point", "coordinates": [1]}, "properties": {"timestamp": "2021-06-01T12:00:00Z"}},
			{"type": "Feature", "geometry": {"type": "Point", "coordinates": [1, 2]}, "properties": {}},
			{"type": "Feature", "geometry": {"type": "Point", "coordinates": [1, 2]}, "properties": {"timestamp": "2021-06-01T12:00:00Z"}}
		]
	}`

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(body))

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "points_created.json", resp.body)
}

// TestCreatePointsRejectsAnUnreadableBody pins that a body the decoder cannot
// read is a bad request, the same answer POST /api/v1/auth/login gives.
func TestCreatePointsRejectsAnUnreadableBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(`{"locations":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestCreatePointsRequiresAuthentication pins that ingest is behind
// requireUser like every other route serving one account's data.
func TestCreatePointsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPost, "/api/v1/points", withBody(validLocationsBody))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestCreatePointsUpdatesDailyStats pins that POST /api/v1/points goes
// through ingest: the point it stores also produces a daily_stats row for the
// day it falls on, in the same request, rather than daily_stats staying stale
// until a `travelmap recalculate`.
func TestCreatePointsUpdatesDailyStats(t *testing.T) {
	t.Parallel()

	st := newTestStore(t)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	day := time.Date(2021, time.June, 1, 0, 0, 0, 0, time.UTC)

	stat, err := st.DailyStats().Get(t.Context(), testUser(t).ID, day)
	if err != nil {
		t.Fatalf("getting daily_stats for %s: %v", day, err)
	}

	if stat.Points != 1 {
		t.Errorf("daily_stats.points = %d, want 1", stat.Points)
	}
}

// TestCreatePointsStoreFailure covers what a client sees when the store
// cannot be written to, rather than a 200 claiming points that were never
// saved.
func TestCreatePointsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailablePoints(t))
	resp := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// twoPointsBody seeds two points an hour apart: one carrying every property
// this server stores, the other only what a Feature must have — a timestamp
// and coordinates — so a listing test's golden file shows both a populated
// and an empty device property.
const twoPointsBody = `{
	"locations": [
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [13.356718, 52.502397]},
			"properties": {
				"timestamp": "2021-06-01T12:00:00Z",
				"altitude": 10,
				"speed": 5,
				"horizontal_accuracy": 4,
				"vertical_accuracy": 3,
				"course": 90,
				"course_accuracy": 1,
				"battery_state": "charging",
				"battery_level": 0.5,
				"wifi": "home",
				"track_id": "track-1",
				"device_id": "device-1"
			}
		},
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [10, 20]},
			"properties": {"timestamp": "2021-06-01T13:00:00Z"}
		}
	]
}`

// ptr returns a pointer to a copy of v, for building a [model.Point] literal
// whose optional fields take an address rather than a variable of their own.
func ptr[T any](v T) *T {
	return &v
}

// twoTestPoints is [twoPointsBody] as the [model.Point] values it describes,
// for a test that seeds the store directly rather than through POST
// /api/v1/points — so that created_at/updated_at are the fixed
// [testCreatedAt]/[testUpdatedAt] a golden file can pin, not whichever
// instant the test happened to run at.
func twoTestPoints(userID int64) []model.Point {
	return []model.Point{
		{
			ID:        1,
			UserID:    userID,
			Timestamp: time.Date(2021, time.June, 1, 12, 0, 0, 0, time.UTC),
			Latitude:  52.502397,
			Longitude: 13.356718,

			Altitude:         ptr(10.0),
			Velocity:         ptr(5.0),
			Accuracy:         ptr(4.0),
			VerticalAccuracy: ptr(3.0),
			Course:           ptr(90.0),
			CourseAccuracy:   ptr(1.0),
			BatteryStatus:    ptr("charging"),
			Battery:          ptr(0.5),
			SSID:             ptr("home"),
			TrackerID:        ptr("device-1"),

			CreatedAt: testCreatedAt,
			UpdatedAt: testUpdatedAt,
		},
		{
			ID:        2,
			UserID:    userID,
			Timestamp: time.Date(2021, time.June, 1, 13, 0, 0, 0, time.UTC),
			Latitude:  20,
			Longitude: 10,

			CreatedAt: testCreatedAt,
			UpdatedAt: testUpdatedAt,
		},
	}
}

// TestListPoints covers GET /api/v1/points: the shape of one point carrying
// every property this server stores, one carrying none of them, and that the
// default order is newest first.
func TestListPoints(t *testing.T) {
	t.Parallel()

	st := storetest.NewWithPoints(t, []model.User{testUser(t)}, twoTestPoints(testUser(t).ID))
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Current-Page"), "1"; got != want {
		t.Errorf("X-Current-Page = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "1"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	assertGolden(t, "points_listed.json", resp.body)
}

// TestListPointsAnswersHead pins that HEAD carries the same pagination
// headers as GET, with no body — the headers are what a client reads to
// decide whether to fetch any pages at all.
func TestListPointsAnswersHead(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
	if seed.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
	}

	resp := do(t, srv, http.MethodHead, "/api/v1/points?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "1"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a HEAD response", resp.body)
	}
}

// TestListPointsRequiresAuthentication pins that listing is behind
// requireUser like every other route serving one account's data.
func TestListPointsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/points")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestListPointsEmptyResultIsAnEmptyArray pins that no matching points still
// answers a JSON array, never `null` — a client iterating the body would fail
// on `null` where it expects a (possibly empty) list.
func TestListPointsEmptyResultIsAnEmptyArray(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "0"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	if got, want := string(resp.body), "[]\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// pointIDs decodes body as a list of points and returns their ids in order,
// for a test that cares which points came back and in what order rather than
// the shape of one — that is [TestListPoints]'s job.
func pointIDs(t *testing.T, body []byte) []int64 {
	t.Helper()

	var points []struct {
		ID int64 `json:"id"`
	}

	if err := json.Unmarshal(body, &points); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	ids := make([]int64, len(points))
	for i, p := range points {
		ids[i] = p.ID
	}

	return ids
}

// TestListPointsOrder pins that order=asc reverses the default newest-first
// order, and that anything else — including leaving it unset — keeps that
// default rather than erroring.
func TestListPointsOrder(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
	if seed.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
	}

	tests := map[string]struct {
		query   string
		wantIDs []int64
	}{
		"default is newest first": {query: "", wantIDs: []int64{2, 1}},
		"desc is newest first":    {query: "&order=desc", wantIDs: []int64{2, 1}},
		"asc is oldest first":     {query: "&order=asc", wantIDs: []int64{1, 2}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey+tt.query)

			if resp.status != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
			}

			ids := pointIDs(t, resp.body)
			if diff := cmp.Diff(ids, tt.wantIDs); diff != "" {
				t.Errorf("ids differ (-want +got):\n%s", diff)
			}
		})
	}
}

// TestListPointsFiltersByTimeRange pins that start_at/end_at narrow the
// result to the points falling inside the range, both bounds inclusive.
func TestListPointsFiltersByTimeRange(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
	if seed.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
	}

	// The first point is at 12:00Z, the second at 13:00Z; a range covering
	// only the second's own instant should exclude the first.
	resp := do(t, srv, http.MethodGet,
		"/api/v1/points?api_key="+testAPIKey+"&start_at=2021-06-01T13:00:00Z&end_at=2021-06-01T13:00:00Z")

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if diff := cmp.Diff(pointIDs(t, resp.body), []int64{2}); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}
}

// TestListPointsPaginates pins that per_page/page slice the result and that
// X-Total-Pages reflects the whole range, not just the page fetched.
func TestListPointsPaginates(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
	if seed.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey+"&per_page=1&page=2")

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Current-Page"), "2"; got != want {
		t.Errorf("X-Current-Page = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "2"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	// Newest first by default, one per page: page 2 is the older point.
	if diff := cmp.Diff(pointIDs(t, resp.body), []int64{1}); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}
}

// TestListPointsStoreFailure covers what a client sees when the store cannot
// be read, rather than a 200 with an incomplete or empty result.
func TestListPointsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailablePoints(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestListPointsExcludesOtherUsersPoints pins that the listing is scoped to
// the authenticated user: a request must not read another account's location
// history back, which id alone would not catch since ids are not
// per-user.
func TestListPointsExcludesOtherUsersPoints(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID:        2,
		Email:     "bob@example.com",
		APIKey:    otherAPIKey,
		CreatedAt: testCreatedAt,
		UpdatedAt: testUpdatedAt,
	}

	st := storetest.New(t, testUser(t), other)
	srv := newTestServerWith(t, st)

	if _, err := st.Points().Create(t.Context(), []model.Point{
		{
			UserID:    testUser(t).ID,
			Timestamp: time.Date(2021, time.June, 1, 12, 0, 0, 0, time.UTC),
			Latitude:  1,
			Longitude: 1,
		},
		{
			UserID:    other.ID,
			Timestamp: time.Date(2021, time.June, 1, 13, 0, 0, 0, time.UTC),
			Latitude:  2,
			Longitude: 2,
		},
	}); err != nil {
		t.Fatalf("seeding points: %v", err)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	// The seeded point belonging to testUser is the first row inserted, so
	// its id is 1 regardless of which user owns it — id alone would not
	// catch a query that forgot to scope by user_id.
	if diff := cmp.Diff([]int64{1}, pointIDs(t, resp.body)); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}
}

// TestListPointsRejectsUnparseableTime pins that a start_at/end_at this
// server cannot read is a 400, not silently treated as absent — a client
// whose date landed in a range it did not ask for would have no way to tell.
func TestListPointsRejectsUnparseableTime(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query      string
		wantGolden string
	}{
		"start_at": {query: "start_at=not-a-date", wantGolden: "invalid_start_at.json"},
		"end_at":   {query: "end_at=not-a-date", wantGolden: "invalid_end_at.json"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey+"&"+tt.query)

			if resp.status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
			}

			assertGolden(t, tt.wantGolden, resp.body)
		})
	}
}

// TestListPointsAcceptsAlternateTimeFormats pins that start_at/end_at also
// accept a bare YYYY-MM-DD date and a purely numeric Unix-seconds value, not
// only RFC 3339 — both deliberately, for existing clients that send them,
// not merely because [time.Parse] happens to accept them.
func TestListPointsAcceptsAlternateTimeFormats(t *testing.T) {
	t.Parallel()

	// Both seeded points are on 2021-06-01, at 12:00Z and 13:00Z; both
	// queries below are chosen to include both of them.
	tests := map[string]string{
		"bare date as start_at": "start_at=2021-06-01&end_at=2021-06-01T13:00:00Z",
		"Unix seconds":          "start_at=1622548800&end_at=1622552400",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)

			seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
			if seed.status != http.StatusOK {
				t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
			}

			resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey+"&"+query)

			if resp.status != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if diff := cmp.Diff([]int64{2, 1}, pointIDs(t, resp.body)); diff != "" {
				t.Errorf("ids differ (-want +got):\n%s", diff)
			}
		})
	}
}

// TestListPointsInvalidPaginationFallsBackToDefaults pins that a page or
// per_page that is not a positive integer falls back to the default (1 and
// 100 respectively) rather than answering an error or an empty page — for
// both ways it can fail to be one: zero/negative, and not a number at all.
func TestListPointsInvalidPaginationFallsBackToDefaults(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"zero and negative": "page=0&per_page=-5",
		"not a number":      "page=abc&per_page=xyz",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)

			seed := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(twoPointsBody))
			if seed.status != http.StatusOK {
				t.Fatalf("seeding points: status = %d, want %d", seed.status, http.StatusOK)
			}

			resp := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey+"&"+query)

			if resp.status != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
			}

			if got, want := resp.header.Get("X-Current-Page"), "1"; got != want {
				t.Errorf("X-Current-Page = %q, want %q", got, want)
			}

			if got, want := resp.header.Get("X-Total-Pages"), "1"; got != want {
				t.Errorf("X-Total-Pages = %q, want %q", got, want)
			}

			if diff := cmp.Diff([]int64{2, 1}, pointIDs(t, resp.body)); diff != "" {
				t.Errorf("ids differ (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPatchPoint covers the happy path: the body's coordinates, wrapped in
// "point", overwrite the stored point.
//
// The response is not compared against a golden file the way every other
// endpoint here is: it echoes updated_at, which Update stamps with the
// request's own wall-clock time, so the body is not byte-stable across test
// runs the way a golden file needs. The fields that are stable are checked
// directly instead.
func TestPatchPoint(t *testing.T) {
	t.Parallel()

	st := storetest.NewWithPoints(t, []model.User{testUser(t)}, twoTestPoints(testUser(t).ID))
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodPatch, "/api/v1/points/1?api_key="+testAPIKey,
		withBody(`{"point": {"latitude": 9.5, "longitude": -10.5}}`))

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	var got struct {
		ID        int64  `json:"id"`
		Latitude  string `json:"latitude"`
		Longitude string `json:"longitude"`
		Timestamp int64  `json:"timestamp"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	if got.ID != 1 {
		t.Errorf("id = %d, want 1", got.ID)
	}

	if got.Latitude != "9.5" || got.Longitude != "-10.5" {
		t.Errorf("latitude, longitude = %q, %q, want %q, %q", got.Latitude, got.Longitude, "9.5", "-10.5")
	}

	// PATCH never touches the timestamp.
	if want := twoTestPoints(testUser(t).ID)[0].Timestamp.Unix(); got.Timestamp != want {
		t.Errorf("timestamp = %d, want %d (unchanged)", got.Timestamp, want)
	}

	if got.CreatedAt == "" {
		t.Errorf("created_at is empty")
	}

	if got.UpdatedAt == got.CreatedAt {
		t.Errorf("updated_at = %q, want it to differ from the unchanged created_at %q", got.UpdatedAt, got.CreatedAt)
	}

	// The change is reflected back through GET too, not just in PATCH's own
	// response.
	list := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)
	if list.status != http.StatusOK {
		t.Fatalf("listing points: status = %d, want %d", list.status, http.StatusOK)
	}

	var points []struct {
		ID       int64  `json:"id"`
		Latitude string `json:"latitude"`
	}
	if err := json.Unmarshal(list.body, &points); err != nil {
		t.Fatalf("decoding the listed points: %v", err)
	}

	for _, p := range points {
		if p.ID == 1 && p.Latitude != "9.5" {
			t.Errorf("listed point 1's latitude = %q, want %q", p.Latitude, "9.5")
		}
	}
}

// TestPatchPointNotFound covers both ways a point is unreachable through
// PATCH: an id nothing stored, and an id that exists but belongs to a
// different user — the same 404 either way, matching upstream's own
// current_api_user.points scoping.
func TestPatchPointNotFound(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID: 2, Email: "bob@example.com", APIKey: otherAPIKey,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	}

	st := storetest.NewWithPoints(t, []model.User{testUser(t), other}, twoTestPoints(testUser(t).ID))
	srv := newTestServerWith(t, st)

	tests := map[string]struct {
		path   string
		apiKey string
	}{
		"a nonexistent id":      {path: "/api/v1/points/999", apiKey: testAPIKey},
		"another user's own id": {path: "/api/v1/points/1", apiKey: otherAPIKey},
		// Not one of this server's own AUTOINCREMENT ids at all, so it can
		// never match a stored point either — see [parsePointID].
		"an id that is not an integer": {path: "/api/v1/points/not-a-number", apiKey: testAPIKey},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := do(t, srv, http.MethodPatch, tt.path+"?api_key="+tt.apiKey,
				withBody(`{"point": {"latitude": 1, "longitude": 2}}`))

			if resp.status != http.StatusNotFound {
				t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
			}

			assertGolden(t, "not_found.json", resp.body)
		})
	}
}

// TestPatchPointInvalidBody pins that a body the decoder cannot read is a
// bad request, the same answer POST /api/v1/points gives.
func TestPatchPointInvalidBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/points/1?api_key="+testAPIKey, withBody(`{"point":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestPatchPointMissingCoordinates pins that a well-formed body missing
// either coordinate is refused with upstream's own documented 422, rather
// than reaching the store with half of what a point needs.
func TestPatchPointMissingCoordinates(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing both":      `{"point": {}}`,
		"missing longitude": `{"point": {"latitude": 1}}`,
		"missing latitude":  `{"point": {"longitude": 1}}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(t)
			resp := do(t, srv, http.MethodPatch, "/api/v1/points/1?api_key="+testAPIKey, withBody(body))

			if resp.status != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want %d", resp.status, http.StatusUnprocessableEntity)
			}

			assertGolden(t, "invalid_point.json", resp.body)
		})
	}
}

// TestPatchPointRequiresAuthentication pins that PATCH is behind
// requireUser like every other route serving one account's data.
func TestPatchPointRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodPatch, "/api/v1/points/1", withBody(`{"point": {"latitude": 1, "longitude": 2}}`))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestPatchPointStoreFailure covers what a client sees when the store cannot
// be written to, rather than a 200 claiming a change that was never saved.
func TestPatchPointStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailablePoints(t))
	resp := do(t, srv, http.MethodPatch, "/api/v1/points/1?api_key="+testAPIKey,
		withBody(`{"point": {"latitude": 1, "longitude": 2}}`))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestDeletePoint covers the happy path, and pins the Milestone E "Done
// when": a deletion is reflected in GET /api/v1/stats immediately, not after
// some later recalculation.
func TestDeletePoint(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	created := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(validLocationsBody))
	if created.status != http.StatusOK {
		t.Fatalf("seeding a point: status = %d, want %d", created.status, http.StatusOK)
	}

	before := do(t, srv, http.MethodGet, "/api/v1/stats?api_key="+testAPIKey)
	if before.status != http.StatusOK {
		t.Fatalf("reading stats before delete: status = %d, want %d", before.status, http.StatusOK)
	}

	var beforeStats struct {
		TotalPointsTracked int `json:"totalPointsTracked"`
	}
	if err := json.Unmarshal(before.body, &beforeStats); err != nil {
		t.Fatalf("decoding stats: %v", err)
	}

	if beforeStats.TotalPointsTracked != 1 {
		t.Fatalf("totalPointsTracked before delete = %d, want 1", beforeStats.TotalPointsTracked)
	}

	resp := do(t, srv, http.MethodDelete, "/api/v1/points/1?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "point_deleted.json", resp.body)

	list := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+testAPIKey)
	if list.status != http.StatusOK {
		t.Fatalf("listing points: status = %d, want %d", list.status, http.StatusOK)
	}

	if diff := cmp.Diff([]int64{}, pointIDs(t, list.body)); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}

	after := do(t, srv, http.MethodGet, "/api/v1/stats?api_key="+testAPIKey)
	if after.status != http.StatusOK {
		t.Fatalf("reading stats after delete: status = %d, want %d", after.status, http.StatusOK)
	}

	var afterStats struct {
		TotalPointsTracked int `json:"totalPointsTracked"`
	}
	if err := json.Unmarshal(after.body, &afterStats); err != nil {
		t.Fatalf("decoding stats: %v", err)
	}

	if afterStats.TotalPointsTracked != 0 {
		t.Errorf("totalPointsTracked after delete = %d, want 0 (reflected immediately)", afterStats.TotalPointsTracked)
	}
}

// TestDeletePointNotFound is [TestPatchPointNotFound] for DELETE.
func TestDeletePointNotFound(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID: 2, Email: "bob@example.com", APIKey: otherAPIKey,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	}

	st := storetest.NewWithPoints(t, []model.User{testUser(t), other}, twoTestPoints(testUser(t).ID))
	srv := newTestServerWith(t, st)

	tests := map[string]struct {
		path   string
		apiKey string
	}{
		"a nonexistent id":      {path: "/api/v1/points/999", apiKey: testAPIKey},
		"another user's own id": {path: "/api/v1/points/1", apiKey: otherAPIKey},
		// Not one of this server's own AUTOINCREMENT ids at all, so it can
		// never match a stored point either — see [parsePointID].
		"an id that is not an integer": {path: "/api/v1/points/not-a-number", apiKey: testAPIKey},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := do(t, srv, http.MethodDelete, tt.path+"?api_key="+tt.apiKey)

			if resp.status != http.StatusNotFound {
				t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
			}

			assertGolden(t, "not_found.json", resp.body)
		})
	}
}

// TestDeletePointRequiresAuthentication pins that DELETE is behind
// requireUser like every other route serving one account's data.
func TestDeletePointRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/1")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestDeletePointStoreFailure covers what a client sees when the store
// cannot be written to, rather than a 200 claiming a deletion that never
// happened.
func TestDeletePointStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailablePoints(t))
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/1?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestBulkDeletePoints covers the happy path: point_ids naming a mix of the
// caller's own points, another user's point, and an id nothing stored — only
// the caller's own points are deleted and counted, matching upstream's own
// `where(id: point_ids).destroy_all` scoping.
func TestBulkDeletePoints(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID: 2, Email: "bob@example.com", APIKey: otherAPIKey,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	}

	points := twoTestPoints(testUser(t).ID)
	points = append(points, model.Point{
		ID: 3, UserID: other.ID,
		Timestamp: time.Date(2021, time.June, 1, 14, 0, 0, 0, time.UTC),
		Latitude:  1, Longitude: 1,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	})

	st := storetest.NewWithPoints(t, []model.User{testUser(t), other}, points)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodDelete, "/api/v1/points/bulk_destroy?api_key="+testAPIKey,
		withBody(`{"point_ids": [1, 2, 3, 999]}`))

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", resp.status, http.StatusOK, resp.body)
	}

	assertGolden(t, "points_bulk_deleted.json", resp.body)

	remaining := do(t, srv, http.MethodGet, "/api/v1/points?api_key="+otherAPIKey)
	if remaining.status != http.StatusOK {
		t.Fatalf("listing the other user's points: status = %d, want %d", remaining.status, http.StatusOK)
	}

	if diff := cmp.Diff([]int64{3}, pointIDs(t, remaining.body)); diff != "" {
		t.Errorf("the other user's point was touched by someone else's bulk delete (-want +got):\n%s", diff)
	}
}

// TestBulkDeletePointsEmpty pins upstream's own 422 for a request naming no
// points to delete, rather than a 200 that deleted nothing silently.
func TestBulkDeletePointsEmpty(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/bulk_destroy?api_key="+testAPIKey,
		withBody(`{"point_ids": []}`))

	if resp.status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnprocessableEntity)
	}

	assertGolden(t, "no_points_selected.json", resp.body)
}

// TestBulkDeletePointsInvalidBody pins that a body the decoder cannot read
// is a bad request, the same answer POST /api/v1/points gives.
func TestBulkDeletePointsInvalidBody(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/bulk_destroy?api_key="+testAPIKey, withBody(`{"point_ids":`))

	if resp.status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
	}

	assertGolden(t, "invalid_request_body.json", resp.body)
}

// TestBulkDeletePointsRequiresAuthentication pins that bulk_destroy is
// behind requireUser like every other route serving one account's data.
func TestBulkDeletePointsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/bulk_destroy", withBody(`{"point_ids": [1]}`))

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestBulkDeletePointsStoreFailure covers what a client sees when the store
// cannot be written to, rather than a 200 claiming a deletion that never
// happened.
func TestBulkDeletePointsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailablePoints(t))
	resp := do(t, srv, http.MethodDelete, "/api/v1/points/bulk_destroy?api_key="+testAPIKey,
		withBody(`{"point_ids": [1]}`))

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}
