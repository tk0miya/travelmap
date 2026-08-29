package httpapi_test

import (
	"net/http"
	"testing"
)

// statsFixtureBody seeds four points across two years:
//   - 2021-06-01: two points 15 minutes apart (Tokyo, then Osaka), within
//     the default track break, so the day carries a real distance.
//   - 2021-08-15: one isolated point (London), too far in time from the
//     June points to count any segment — a month with data but zero
//     distance.
//   - 2022-01-10: one isolated point (New York) in a different year.
//
// This is enough to exercise both GET /api/v1/stats' yearly/monthly
// breakdown and GET /api/v1/points/tracked_months' year and month grouping.
const statsFixtureBody = `{
	"locations": [
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [139.7671, 35.6812]},
			"properties": {"timestamp": "2021-06-01T00:00:00Z"}
		},
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [135.4959, 34.7025]},
			"properties": {"timestamp": "2021-06-01T00:15:00Z"}
		},
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [-0.1278, 51.5074]},
			"properties": {"timestamp": "2021-08-15T12:00:00Z"}
		},
		{
			"type": "Feature",
			"geometry": {"type": "Point", "coordinates": [-74.0060, 40.7128]},
			"properties": {"timestamp": "2022-01-10T08:00:00Z"}
		}
	]
}`

// TestStats covers the happy path: distance, point counts and the
// yearly/monthly breakdown, all read from daily_stats.
func TestStats(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	created := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(statsFixtureBody))
	if created.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", created.status, http.StatusOK)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/stats?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "stats.json", resp.body)
}

// TestStatsEmpty covers a user with no points at all: every total is zero
// and yearlyStats is an empty array, never null.
func TestStatsEmpty(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/stats?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "stats_empty.json", resp.body)
}

// TestStatsRequiresAuthentication pins that /stats is behind requireUser
// like every other route serving one account's data.
func TestStatsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/stats")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestStatsStoreFailure covers what a client sees when daily_stats cannot be
// read, rather than a 200 with an incomplete or empty result.
func TestStatsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailableDailyStats(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/stats?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}

// TestTrackedMonths covers the happy path: the years and months carrying at
// least one point, most recent year first and calendar order within a year.
func TestTrackedMonths(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)

	created := do(t, srv, http.MethodPost, "/api/v1/points?api_key="+testAPIKey, withBody(statsFixtureBody))
	if created.status != http.StatusOK {
		t.Fatalf("seeding points: status = %d, want %d", created.status, http.StatusOK)
	}

	resp := do(t, srv, http.MethodGet, "/api/v1/points/tracked_months?api_key="+testAPIKey)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "tracked_months.json", resp.body)
}

// TestTrackedMonthsEmpty covers a user with no points at all: the response
// is an empty array, never null.
func TestTrackedMonthsEmpty(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/points/tracked_months?api_key="+testAPIKey)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusOK)
	}

	assertGolden(t, "tracked_months_empty.json", resp.body)
}

// TestTrackedMonthsRequiresAuthentication pins that tracked_months is behind
// requireUser like every other route serving one account's data.
func TestTrackedMonthsRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	resp := do(t, srv, http.MethodGet, "/api/v1/points/tracked_months")

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.status, http.StatusUnauthorized)
	}

	if len(resp.body) != 0 {
		t.Errorf("body = %q, want it empty on a 401", resp.body)
	}
}

// TestTrackedMonthsStoreFailure covers what a client sees when daily_stats
// cannot be read, rather than a 200 with an incomplete or empty result.
func TestTrackedMonthsStoreFailure(t *testing.T) {
	t.Parallel()

	srv := newTestServerWith(t, newStoreWithUnavailableDailyStats(t))
	resp := do(t, srv, http.MethodGet, "/api/v1/points/tracked_months?api_key="+testAPIKey)

	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.status, http.StatusInternalServerError)
	}

	assertGolden(t, "internal_server_error.json", resp.body)
}
