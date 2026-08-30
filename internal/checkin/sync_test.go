package checkin_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
// page.
func fetchServer(t *testing.T, checkins ...string) *foursquare.Client {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	client := fetchServer(t,
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

	if _, err := checkin.Sync(t.Context(), st, fetchServer(t, fetched), account, testLookback); err != nil {
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

// pagingServer answers like the real endpoint does: newest first, at most
// the limit the request asks for, skipping the requested offset. After the
// first request it inserts a check-in at the front, which is what an arriving
// check-in does to an offset walk underneath it — the page boundary then
// hands one check-in back a second time.
// fakeCheckin is one check-in the paging server below serves, before it is
// rendered as the JSON a page carries.
type fakeCheckin struct {
	id        string
	createdAt time.Time
}

func pagingServer(t *testing.T, pages *int, checkins func(limit int) []fakeCheckin) *foursquare.Client {
	t.Helper()

	arrived := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*pages++

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			t.Errorf("limit = %q, want a number", r.URL.Query().Get("limit"))

			limit = 1
		}

		offset := 0

		if raw := r.URL.Query().Get("offset"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				t.Errorf("offset = %q, want a number", raw)
			}

			offset = parsed
		}

		all := checkins(limit)

		// The check-in that arrives mid-walk, pushing everything one place
		// later and so back across the boundary the last page ended on.
		if arrived {
			all = append([]fakeCheckin{{id: "arrived", createdAt: all[0].createdAt.Add(time.Minute)}}, all...)
		}

		arrived = true

		items := make([]string, 0, limit)

		for i := offset; i < len(all) && len(items) < limit; i++ {
			items = append(items, checkinJSON(all[i].id, all[i].createdAt))
		}

		body := fmt.Sprintf(`{"meta":{"code":200},"response":{"checkins":{"count":%d,"items":[%s]}}}`,
			len(all), strings.Join(items, ","))

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return foursquare.NewClient(server.URL, slog.New(slog.DiscardHandler))
}

// TestSyncCountsACheckinSeenTwiceOnce pins what the run reports across a page
// boundary. Paging walks a list by offset, so a check-in arriving mid-walk
// pushes one back across the boundary the last page ended on, and it is
// fetched twice. The upsert makes that harmless for the stored rows; this is
// about the count, which is a count of check-ins collected and not of items
// fetched.
func TestSyncCountsACheckinSeenTwiceOnce(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	account := linkedAccount(t, st)
	now := time.Now().UTC().Truncate(time.Second)

	total := 0

	// One full page and a short one after it, whatever page size the client
	// asks for.
	checkins := func(limit int) []fakeCheckin {
		total = limit + 3

		all := make([]fakeCheckin, total)

		for i := range all {
			all[i].id = fmt.Sprintf("page%03d", i)
			all[i].createdAt = now.Add(-time.Duration(i+1) * time.Minute)
		}

		return all
	}

	pages := 0

	collected, err := checkin.Sync(t.Context(), st, pagingServer(t, &pages, checkins), account, testLookback)
	if err != nil {
		t.Fatalf("Sync returned %v", err)
	}

	// Without a second request there is no boundary, and the count below
	// would hold however the repeat was handled.
	if pages < 2 {
		t.Fatalf("the run made %d requests, want the second page a boundary needs", pages)
	}

	// total, not total + 1: the check-in pushed back across the boundary is
	// fetched twice and counted once. The one that arrived mid-walk is never
	// seen, having been pushed off the front of a page already read.
	if collected != total {
		t.Errorf("Sync collected %d check-ins, want %d", collected, total)
	}
}

// TestSyncFetchesAWindowRatherThanResumingACursor is the test the whole fetch
// design rests on: a check-in dated before the last successful run, added
// after it, is still collected by the next one. The window is computed from
// the run's own start, so synced_through is a report of how current an
// account is and never the lower bound of what is asked for — the one change
// that would silently defeat the fetch path.
//
// The lower bound is applied to the pages rather than sent, so the same run
// has to show it still holds: a check-in older than the window is left alone.
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

	client := fetchServer(t,
		// Inside the window but behind the stored high-water mark, which is
		// what a cursor would skip and this design collects.
		checkinJSON("retro01", now.Add(-72*time.Hour)),
		// Outside it, by a day: the bound is real, not merely computed.
		checkinJSON("ancient", now.Add(-testLookback-24*time.Hour)),
	)

	collected, err := checkin.Sync(t.Context(), st, client, account, testLookback)
	if err != nil {
		t.Fatalf("Sync returned %v", err)
	}

	if collected != 1 {
		t.Errorf("Sync collected %d check-ins, want only the one inside the window", collected)
	}

	if stored := probeCheckin(t, st, "retro01"); stored.Source != checkin.SourceSync {
		t.Errorf("the retroactive check-in was not collected: source = %q", stored.Source)
	}

	if stored := probeCheckin(t, st, "ancient"); stored.Source == checkin.SourceSync {
		t.Error("a check-in older than the window was collected")
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
