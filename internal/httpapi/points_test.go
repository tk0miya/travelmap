package httpapi_test

import (
	"net/http"
	"testing"
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
// answered with 201 instead of 200. See "Per-endpoint" in TODO.md.
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
// overlapping ranges. See "Deduplication" under "points" in TODO.md.
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
