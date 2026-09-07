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

// oneTestTrack is a single track spanning two points, with everything a
// golden file can pin fixed rather than computed at test time.
func oneTestTrack(userID int64) ([]model.Track, []model.Point) {
	start := time.Date(2021, time.June, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)

	points := []model.Point{
		{ID: 1, UserID: userID, Timestamp: start, Latitude: 52.502397, Longitude: 13.356718, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 2, UserID: userID, Timestamp: end, Latitude: 52.6, Longitude: 13.4, Velocity: ptr(5.0), CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
	}

	tracks := []model.Track{{
		ID:      1,
		UserID:  userID,
		StartAt: start,
		EndAt:   end,
		Geometry: []model.Coordinate{
			{Longitude: points[0].Longitude, Latitude: points[0].Latitude},
			{Longitude: points[1].Longitude, Latitude: points[1].Latitude},
		},
		DistanceMeters: 12345.6,
		CreatedAt:      testCreatedAt,
		UpdatedAt:      testUpdatedAt,
	}}

	return tracks, points
}

// TestListTracks covers GET /api/v1/tracks: the GeoJSON FeatureCollection
// shape, one track carrying every property this server sends.
func TestListTracks(t *testing.T) {
	t.Parallel()

	tracks, points := oneTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Current-Page"), "1"; got != want {
		t.Errorf("X-Current-Page = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "1"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	assertGolden(t, "tracks_listed.json", resp.body)
}

// TestListTracksRequiresAuthentication pins that listing is behind
// requireUser like every other route serving one account's data.
func TestListTracksRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestListTracksEmptyResultIsAnEmptyFeatureCollection pins that no tracks
// still answers a FeatureCollection with an empty (never null) features
// array — the GeoJSON shape of [TestListPointsEmptyResultIsAnEmptyArray]'s
// own rule.
func TestListTracksEmptyResultIsAnEmptyFeatureCollection(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "0"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	assertGolden(t, "tracks_listed_empty.json", resp.body)
}

// trackIDs decodes body as a FeatureCollection and returns each feature's
// track id in order, for a test that cares which tracks came back rather
// than the shape of one.
func trackIDs(t *testing.T, body []byte) []int64 {
	t.Helper()

	var decoded struct {
		Features []struct {
			Properties struct {
				ID int64 `json:"id"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	ids := make([]int64, len(decoded.Features))
	for i, f := range decoded.Features {
		ids[i] = f.Properties.ID
	}

	return ids
}

// twoTracksTwoDays seeds two tracks a day apart, each a single point pair,
// for the tests below that filter or paginate across them.
func twoTracksTwoDays(userID int64) ([]model.Track, []model.Point) {
	day1 := time.Date(2021, time.June, 1, 12, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)

	points := []model.Point{
		{ID: 1, UserID: userID, Timestamp: day1, Latitude: 1, Longitude: 1, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 2, UserID: userID, Timestamp: day1.Add(time.Minute), Latitude: 2, Longitude: 2, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 3, UserID: userID, Timestamp: day2, Latitude: 3, Longitude: 3, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 4, UserID: userID, Timestamp: day2.Add(time.Minute), Latitude: 4, Longitude: 4, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
	}

	tracks := []model.Track{
		{
			ID: 1, UserID: userID, StartAt: day1, EndAt: day1.Add(time.Minute),
			Geometry:       []model.Coordinate{{Longitude: 1, Latitude: 1}, {Longitude: 2, Latitude: 2}},
			DistanceMeters: 100,
			CreatedAt:      testCreatedAt, UpdatedAt: testUpdatedAt,
		},
		{
			ID: 2, UserID: userID, StartAt: day2, EndAt: day2.Add(time.Minute),
			Geometry:       []model.Coordinate{{Longitude: 3, Latitude: 3}, {Longitude: 4, Latitude: 4}},
			DistanceMeters: 200,
			CreatedAt:      testCreatedAt, UpdatedAt: testUpdatedAt,
		},
	}

	return tracks, points
}

// TestListTracksFiltersByTimeRange pins that start_at/end_at narrow the
// result to tracks overlapping the range.
func TestListTracksFiltersByTimeRange(t *testing.T) {
	t.Parallel()

	tracks, points := twoTracksTwoDays(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet,
		"/api/v1/tracks?api_key="+testAPIKey+"&start_at=2021-06-02T00:00:00Z&end_at=2021-06-03T00:00:00Z")

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if diff := cmp.Diff([]int64{2}, trackIDs(t, resp.body)); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}
}

// TestListTracksPaginates pins that per_page/page slice the result and that
// X-Total-Pages reflects the whole range, not just the page fetched.
func TestListTracksPaginates(t *testing.T) {
	t.Parallel()

	tracks, points := twoTracksTwoDays(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks?api_key="+testAPIKey+"&per_page=1&page=2")

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Current-Page"), "2"; got != want {
		t.Errorf("X-Current-Page = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "2"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	if diff := cmp.Diff([]int64{2}, trackIDs(t, resp.body)); diff != "" {
		t.Errorf("ids differ (-want +got):\n%s", diff)
	}
}

// TestListTracksRejectsUnparseableTime pins that a start_at/end_at this
// server cannot read is a 400, matching GET /api/v1/points' own behaviour.
func TestListTracksRejectsUnparseableTime(t *testing.T) {
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
			resp := do(t, srv, http.MethodGet, "/api/v1/tracks?api_key="+testAPIKey+"&"+tt.query)

			if resp.status != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", resp.status, http.StatusBadRequest)
			}

			assertGolden(t, tt.wantGolden, resp.body)
		})
	}
}

// TestListTracksStoreFailure covers what a client sees when tracks cannot be
// read, rather than a 200 with an incomplete or empty result.
func TestListTracksStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailableTracks(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestGetTrack covers GET /api/v1/tracks/{id}: a single-Feature
// FeatureCollection, the same per-feature shape GET /api/v1/tracks answers
// with.
func TestGetTrack(t *testing.T) {
	t.Parallel()

	tracks, points := oneTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "track.json", resp.body)
}

// TestGetTrackNotFound covers every way a track is unreachable through GET:
// an id nothing stored, an id that exists but belongs to a different user,
// and an id that is not one of this server's own integer ids at all.
func TestGetTrackNotFound(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID: 2, Email: "bob@example.com", APIKey: otherAPIKey,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	}

	tracks, points := oneTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t), other}, points, tracks)
	srv := newTestServerWith(t, st)

	tests := map[string]struct {
		path   string
		apiKey string
	}{
		"a nonexistent id":             {path: "/api/v1/tracks/999", apiKey: testAPIKey},
		"another user's own id":        {path: "/api/v1/tracks/1", apiKey: otherAPIKey},
		"an id that is not an integer": {path: "/api/v1/tracks/not-a-number", apiKey: testAPIKey},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := do(t, srv, http.MethodGet, tt.path+"?api_key="+tt.apiKey)

			if resp.status != http.StatusNotFound {
				t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
			}

			assertGolden(t, "not_found.json", resp.body)
		})
	}
}

// TestGetTrackRequiresAuthentication pins that GET is behind requireUser
// like every other route serving one account's data.
func TestGetTrackRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestGetTrackStoreFailure covers what a client sees when tracks cannot be
// read, rather than a 404 that looks like the track simply does not exist.
func TestGetTrackStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailableTracks(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestTrackPoints covers GET /api/v1/tracks/{track_id}/points: the narrower
// per-point shape this endpoint answers with, in timestamp order.
func TestTrackPoints(t *testing.T) {
	t.Parallel()

	tracks, points := oneTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1/points?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "1"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	assertGolden(t, "track_points.json", resp.body)
}

// threePointTestTrack is a single track spanning three points, for the
// pagination test below — [oneTestTrack]'s own two points fit on one page,
// which would not exercise a second one.
func threePointTestTrack(userID int64) ([]model.Track, []model.Point) {
	start := time.Date(2021, time.June, 1, 12, 0, 0, 0, time.UTC)

	points := []model.Point{
		{ID: 1, UserID: userID, Timestamp: start, Latitude: 1, Longitude: 1, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 2, UserID: userID, Timestamp: start.Add(time.Minute), Latitude: 2, Longitude: 2, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
		{ID: 3, UserID: userID, Timestamp: start.Add(2 * time.Minute), Latitude: 3, Longitude: 3, CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt},
	}

	tracks := []model.Track{{
		ID: 1, UserID: userID, StartAt: start, EndAt: start.Add(2 * time.Minute),
		Geometry: []model.Coordinate{
			{Longitude: 1, Latitude: 1},
			{Longitude: 2, Latitude: 2},
			{Longitude: 3, Latitude: 3},
		},
		DistanceMeters: 300,
		CreatedAt:      testCreatedAt, UpdatedAt: testUpdatedAt,
	}}

	return tracks, points
}

// TestTrackPointsPaginates pins that per_page/page slice a track's own
// points and that X-Total-Pages reflects the whole track, not just the page
// fetched — the same pagination [TestListTracksPaginates] and
// [TestListPointsPaginates] already pin for the other listing endpoints.
func TestTrackPointsPaginates(t *testing.T) {
	t.Parallel()

	tracks, points := threePointTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t)}, points, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1/points?api_key="+testAPIKey+"&per_page=1&page=2")

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	if got, want := resp.header.Get("X-Current-Page"), "2"; got != want {
		t.Errorf("X-Current-Page = %q, want %q", got, want)
	}

	if got, want := resp.header.Get("X-Total-Pages"), "3"; got != want {
		t.Errorf("X-Total-Pages = %q, want %q", got, want)
	}

	var decoded []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(resp.body, &decoded); err != nil {
		t.Fatalf("decoding the response body: %v", err)
	}

	if len(decoded) != 1 || decoded[0].ID != 2 {
		t.Errorf("page 2 = %v, want the middle point alone", decoded)
	}
}

// TestTrackPointsNotFound pins that a track id nothing stored, or one
// belonging to another user, answers 404 rather than an empty points list —
// the same distinction [TestGetTrackNotFound] pins for the track itself.
func TestTrackPointsNotFound(t *testing.T) {
	t.Parallel()

	// A test fixture, not a credential — see testAPIKey.
	const otherAPIKey = "1f0e2d3c4b5a69788796a5b4c3d2e1f01f0e2d3c4b5a69788796a5b4c3d2e1f0" //nolint:gosec // see above

	other := model.User{
		ID: 2, Email: "bob@example.com", APIKey: otherAPIKey,
		CreatedAt: testCreatedAt, UpdatedAt: testUpdatedAt,
	}

	tracks, points := oneTestTrack(testUser(t).ID)
	st := storetest.NewWithTracks(t, []model.User{testUser(t), other}, points, tracks)
	srv := newTestServerWith(t, st)

	tests := map[string]struct {
		path   string
		apiKey string
	}{
		"a nonexistent track":  {path: "/api/v1/tracks/999/points", apiKey: testAPIKey},
		"another user's track": {path: "/api/v1/tracks/1/points", apiKey: otherAPIKey},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := do(t, srv, http.MethodGet, tt.path+"?api_key="+tt.apiKey)

			if resp.status != http.StatusNotFound {
				t.Errorf("status = %d, want %d", resp.status, http.StatusNotFound)
			}

			assertGolden(t, "not_found.json", resp.body)
		})
	}
}

// TestTrackPointsRequiresAuthentication pins that this endpoint is behind
// requireUser like every other route serving one account's data.
func TestTrackPointsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1/points")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestTrackPointsStoreFailure covers what a client sees when the track
// lookup itself cannot be read, rather than a 404 that looks like the track
// simply does not exist.
func TestTrackPointsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailableTracks(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1/points?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestTrackPointsStoreFailureListingPoints covers the other store call this
// endpoint makes: the track lookup succeeds, but listing its points fails —
// the same 500 as [TestTrackPointsStoreFailure], reached through a different
// call.
func TestTrackPointsStoreFailureListingPoints(t *testing.T) {
	t.Parallel()

	tracks, _ := oneTestTrack(testUser(t).ID)
	st := storetest.UnavailablePointsWithTracks(t, []model.User{testUser(t)}, tracks)
	srv := newTestServerWith(t, st)

	resp := do(t, srv, http.MethodGet, "/api/v1/tracks/1/points?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}
