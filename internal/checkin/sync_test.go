package checkin_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// testLookback is the window these runs take, the default a deployment gets.
const testLookback = 14 * 24 * time.Hour

// linkedAccount is [linkedUser]'s account row, which is what Sync takes:
// nothing is collected for a user without one.
func linkedAccount(t *testing.T, st store.Store) model.FoursquareAccount {
	t.Helper()

	linkedUser(t, st)

	account, err := st.FoursquareAccounts().ByFoursquareUserID(t.Context(), foursquareUserID)
	if err != nil {
		t.Fatalf("reading the linked account: %v", err)
	}

	return account
}

// checkinJSON is one check-in as the API sends it, and — the same object
// under a different wrapper — as a push carries it.
func checkinJSON(id string, createdAt time.Time) string {
	return fmt.Sprintf(
		`{"id":%q,"createdAt":%d,"timeZoneOffset":540,"user":{"id":%q},`+
			`"venue":{"id":"4b4429abf964a520a80f25e3","name":"Tokyo Tower",`+
			`"location":{"lat":35.6586,"lng":139.7454,"cc":"JP","city":"Minato","state":"Tokyo","country":"Japan"},`+
			`"categories":[{"id":"4bf58dd8d48988d12d941735","name":"Monument / Landmark","primary":true}]}}`,
		id, createdAt.Unix(), foursquareUserID)
}

// fetchServer answers every check-in request with the given check-ins, as one
// page, and records the afterTimestamp each request asked for.
func fetchServer(t *testing.T, windows *[]string, checkins ...string) *foursquare.Client {
	t.Helper()

	body := `{"meta":{"code":200},"response":{"checkins":{"count":` +
		strconv.Itoa(len(checkins)) + `,"items":[`

	for i, c := range checkins {
		if i > 0 {
			body += ","
		}

		body += c
	}

	body += `]}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if windows != nil {
			*windows = append(*windows, r.URL.Query().Get("afterTimestamp"))
		}

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return foursquare.NewClient(server.URL, slog.New(slog.DiscardHandler))
}

// probeCheckin upserts a throwaway write for id and returns the row that
// comes back. Upsert never changes source or created_at on a conflict, so
// those two are a read of what the first write left behind, without the store
// needing a lookup nothing else wants yet; the returned id is what tells one
// row from two. Every other column is the probe's own by then, so nothing
// else on the returned row says anything about what was stored before it.
func probeCheckin(t *testing.T, st store.Store, id string) model.Checkin {
	t.Helper()

	got, err := st.Checkins().Upsert(t.Context(), model.Checkin{
		UserID:              1,
		FoursquareCheckinID: id,
		CheckedInAt:         time.Unix(0, 0).UTC(),
		Source:              "probe",
		Raw:                 "{}",
	})
	if err != nil {
		t.Fatalf("probing the stored check-in: %v", err)
	}

	return got
}

// TestSyncStoresFetchedCheckins is the ordinary run: every check-in the
// window holds is written, and the account records how current it now is.
func TestSyncStoresFetchedCheckins(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	account := linkedAccount(t, st)
	now := time.Now().UTC().Truncate(time.Second)

	client := fetchServer(t, nil,
		checkinJSON("aaa111", now.Add(-time.Hour)),
		checkinJSON("bbb222", now.Add(-48*time.Hour)),
	)

	collected, err := checkin.Sync(t.Context(), st, client, account, testLookback)
	if err != nil {
		t.Fatalf("Sync returned %v", err)
	}

	if collected != 2 {
		t.Errorf("Sync collected %d check-ins, want 2", collected)
	}

	for _, id := range []string{"aaa111", "bbb222"} {
		if stored := probeCheckin(t, st, id); stored.Source != checkin.SourceSync {
			t.Errorf("the check-in %s has source %q, want %q", id, stored.Source, checkin.SourceSync)
		}
	}

	synced, err := st.FoursquareAccounts().ByFoursquareUserID(t.Context(), foursquareUserID)
	if err != nil {
		t.Fatalf("reading the account back: %v", err)
	}

	if synced.SyncedThrough == nil || synced.SyncedThrough.Before(now) {
		t.Errorf("SyncedThrough = %v, want the run's own start or later", synced.SyncedThrough)
	}
}

// TestSyncKeepsThePathThatSawTheCheckinFirst pins the two collection paths
// meeting on one row: a check-in pushed and then fetched is one row, and it
// still names push as the path that saw it. Which fields a repeat write
// refreshes is the store's own contract, pinned where the upsert is.
func TestSyncKeepsThePathThatSawTheCheckinFirst(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	account := linkedAccount(t, st)
	checkedInAt := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)

	pushed, err := checkin.WritePush(t.Context(), st, checkinJSON("aaa111", checkedInAt))
	if err != nil {
		t.Fatalf("WritePush returned %v", err)
	}

	// The same check-in as the fetch renders it: the same id, so the fetch
	// has to land on the row the push already wrote.
	fetched := `{"id":"aaa111","createdAt":` + strconv.FormatInt(checkedInAt.Unix(), 10) +
		`,"shout":"added later","user":{"id":"` + foursquareUserID + `"}}`

	if _, err := checkin.Sync(t.Context(), st, fetchServer(t, nil, fetched), account, testLookback); err != nil {
		t.Fatalf("Sync returned %v", err)
	}

	stored := probeCheckin(t, st, "aaa111")

	if stored.ID != pushed.ID {
		t.Errorf("the fetch stored row %d, want the pushed row %d", stored.ID, pushed.ID)
	}

	if stored.Source != checkin.SourcePush {
		t.Errorf("Source = %q, want the first path's %q", stored.Source, checkin.SourcePush)
	}
}

// TestSyncFetchesAWindowRatherThanResumingACursor is the test the whole fetch
// design rests on: a check-in dated before the last successful run, added
// after it, is still collected by the next one. The window is computed from
// the run's own start, so synced_through is a report of how current an
// account is and never the lower bound of what is asked for — the one change
// that would silently defeat the fetch path.
func TestSyncFetchesAWindowRatherThanResumingACursor(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	account := linkedAccount(t, st)
	now := time.Now().UTC().Truncate(time.Second)

	// A run that already succeeded a moment ago: a cursor would ask for
	// nothing older than this.
	if err := st.FoursquareAccounts().UpdateSyncedThrough(t.Context(), account.UserID, now); err != nil {
		t.Fatalf("recording the earlier run: %v", err)
	}

	account, err := st.FoursquareAccounts().ByFoursquareUserID(t.Context(), foursquareUserID)
	if err != nil {
		t.Fatalf("reading the account back: %v", err)
	}

	var windows []string

	retro := now.Add(-72 * time.Hour)
	client := fetchServer(t, &windows, checkinJSON("retro01", retro))

	if _, err := checkin.Sync(t.Context(), st, client, account, testLookback); err != nil {
		t.Fatalf("Sync returned %v", err)
	}

	if len(windows) != 1 {
		t.Fatalf("the run made %d requests, want 1", len(windows))
	}

	asked, err := strconv.ParseInt(windows[0], 10, 64)
	if err != nil {
		t.Fatalf("afterTimestamp %q is not a Unix timestamp: %v", windows[0], err)
	}

	// The window starts a lookback before the run, not at the stored
	// synced_through — the request itself is where the two designs differ.
	if want := now.Add(-testLookback).Unix(); asked > want+5 || asked < want-5 {
		t.Errorf("afterTimestamp = %d, want about %d (a lookback before the run)", asked, want)
	}

	if stored := probeCheckin(t, st, "retro01"); stored.Source != checkin.SourceSync {
		t.Errorf("the retroactive check-in was not collected: source = %q", stored.Source)
	}
}

// TestSyncLeavesSyncedThroughAloneWhenTheWalkFails pins the other half of
// that column's meaning: a run that could not page does not claim to have
// covered its window, so the account still reports how current it actually
// is.
func TestSyncLeavesSyncedThroughAloneWhenTheWalkFails(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	account := linkedAccount(t, st)

	// A server that answers 403 the way a rate limit or a revoked
	// authorisation does.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"meta":{"code":403,"errorType":"rate_limit_exceeded"},"response":{}}`))
	}))
	t.Cleanup(server.Close)

	client := foursquare.NewClient(server.URL, slog.New(slog.DiscardHandler))

	if _, err := checkin.Sync(t.Context(), st, client, account, testLookback); err == nil {
		t.Fatal("Sync returned nil for a refused request")
	}

	stored, err := st.FoursquareAccounts().ByFoursquareUserID(t.Context(), foursquareUserID)
	if err != nil {
		t.Fatalf("reading the account back: %v", err)
	}

	if stored.SyncedThrough != nil {
		t.Errorf("SyncedThrough = %v, want it left unset by a failed run", stored.SyncedThrough)
	}
}
