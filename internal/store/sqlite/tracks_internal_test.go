package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// tracksTestUser creates a fresh user for a tracks test, for the same reason
// [dailyStatsTestUser] does.
func tracksTestUser(t *testing.T, db *DB, email string) int64 {
	t.Helper()

	user, err := db.Users().Create(t.Context(), testUser(email))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	return user.ID
}

// aTrack builds a track spanning [start, end) with an arbitrary two-point
// geometry, for a test that does not care about its exact shape.
func aTrack(userID int64, start, end time.Time) model.Track {
	return model.Track{
		UserID:  userID,
		StartAt: start,
		EndAt:   end,
		Geometry: []model.Coordinate{
			{Longitude: 1, Latitude: 2},
			{Longitude: 3, Latitude: 4},
		},
		DistanceMeters: 123.45,
	}
}

// TestTracksReplaceAllStoresAndReplaces pins the round trip and that a
// second ReplaceAll call removes what the first one wrote rather than
// appending to it.
func TestTracksReplaceAllStoresAndReplaces(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "replace@example.com")

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{
		aTrack(userID, start, start.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("ReplaceAll returned %v", err)
	}

	first, total, err := db.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 1 || len(first) != 1 {
		t.Fatalf("total, len(first) = %d, %d, want 1, 1", total, len(first))
	}

	if diff := cmp.Diff(aTrack(userID, start, start.Add(time.Hour)).Geometry, first[0].Geometry); diff != "" {
		t.Errorf("geometry differs (-want +got):\n%s", diff)
	}

	if first[0].DistanceMeters != 123.45 {
		t.Errorf("DistanceMeters = %v, want 123.45", first[0].DistanceMeters)
	}

	// A second call with a different track replaces the first entirely.
	newStart := start.Add(24 * time.Hour)
	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{
		aTrack(userID, newStart, newStart.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("the second ReplaceAll returned %v", err)
	}

	second, total, err := db.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 1 || len(second) != 1 {
		t.Fatalf("total, len(second) = %d, %d, want 1, 1", total, len(second))
	}

	if !second[0].StartAt.Equal(newStart) {
		t.Errorf("StartAt = %s, want %s (the old row replaced)", second[0].StartAt, newStart)
	}
}

// TestTracksReplaceAllWithNoTracksClearsExisting pins that replacing with an
// empty slice leaves the user with no tracks at all.
func TestTracksReplaceAllWithNoTracksClearsExisting(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "clear@example.com")

	start := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)

	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{
		aTrack(userID, start, start.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("ReplaceAll returned %v", err)
	}

	if err := db.Tracks().ReplaceAll(t.Context(), userID, nil); err != nil {
		t.Fatalf("the clearing ReplaceAll returned %v", err)
	}

	tracks, total, err := db.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 0 || len(tracks) != 0 {
		t.Errorf("total, len(tracks) = %d, %d, want 0, 0", total, len(tracks))
	}
}

// TestTracksListFiltersByOverlap pins that List returns a track whenever its
// own [StartAt, EndAt] overlaps the requested range, not only one wholly
// inside it, and excludes one that does not overlap at all.
func TestTracksListFiltersByOverlap(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "overlap@example.com")

	day1 := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	day3 := day1.AddDate(0, 0, 2)

	// Straddles the boundary between day1 and day2, entirely inside day2, and
	// entirely on day3.
	straddling := aTrack(userID, day1.Add(23*time.Hour), day2.Add(time.Hour))
	insideDay2 := aTrack(userID, day2.Add(2*time.Hour), day2.Add(3*time.Hour))
	onDay3 := aTrack(userID, day3.Add(time.Hour), day3.Add(2*time.Hour))

	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{straddling, insideDay2, onDay3}); err != nil {
		t.Fatalf("ReplaceAll returned %v", err)
	}

	tracks, total, err := db.Tracks().List(t.Context(), userID, &day2, &day3, 1, 10)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 2 {
		t.Errorf("total = %d, want 2 (day3 excluded)", total)
	}

	if len(tracks) != 2 || !tracks[0].StartAt.Equal(straddling.StartAt) || !tracks[1].StartAt.Equal(insideDay2.StartAt) {
		t.Errorf("List returned %d tracks starting %v, %v, want the straddling and inside-day2 tracks in order",
			len(tracks), tracks[0].StartAt, tracks[1].StartAt)
	}
}

// TestTracksListPaginates pins page/perPage slicing and that the reported
// total reflects every matching row, not just the page fetched.
func TestTracksListPaginates(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "paginate@example.com")

	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{
		aTrack(userID, start, start.Add(time.Hour)),
		aTrack(userID, start.Add(2*time.Hour), start.Add(3*time.Hour)),
	}); err != nil {
		t.Fatalf("ReplaceAll returned %v", err)
	}

	tracks, total, err := db.Tracks().List(t.Context(), userID, nil, nil, 2, 1)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}

	if len(tracks) != 1 || !tracks[0].StartAt.Equal(start.Add(2*time.Hour)) {
		t.Errorf("page 2 = %v, want the second track alone", tracks)
	}
}

// TestTracksByID pins the found, not-found and wrong-user cases, all through
// the same lookup — a track belonging to a different user answers
// [store.ErrNotFound] exactly like an id nothing stored.
func TestTracksByID(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "byid@example.com")
	otherID := tracksTestUser(t, db, "other-byid@example.com")

	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	if err := db.Tracks().ReplaceAll(t.Context(), userID, []model.Track{
		aTrack(userID, start, start.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("ReplaceAll returned %v", err)
	}

	stored, _, err := db.Tracks().List(t.Context(), userID, nil, nil, 1, 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("listing the seeded track: %v, %v", stored, err)
	}

	got, err := db.Tracks().ByID(t.Context(), userID, stored[0].ID)
	if err != nil {
		t.Fatalf("ByID returned %v", err)
	}

	if diff := cmp.Diff(stored[0], got); diff != "" {
		t.Errorf("ByID differs from List's own row (-want +got):\n%s", diff)
	}

	if _, err := db.Tracks().ByID(t.Context(), userID, stored[0].ID+999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a nonexistent id returned %v, want ErrNotFound", err)
	}

	if _, err := db.Tracks().ByID(t.Context(), otherID, stored[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another user's id returned %v, want ErrNotFound", err)
	}
}

// TestTracksEnqueueCoalesces pins that a user already queued is left alone
// rather than duplicated: NextPending reports the user once regardless of
// how many times Enqueue was called for it.
func TestTracksEnqueueCoalesces(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "enqueue@example.com")

	if err := db.Tracks().Enqueue(t.Context(), userID); err != nil {
		t.Fatalf("the first Enqueue returned %v", err)
	}

	if err := db.Tracks().Enqueue(t.Context(), userID); err != nil {
		t.Fatalf("the second Enqueue returned %v", err)
	}

	first, ok, err := db.Tracks().NextPending(t.Context())
	if err != nil || !ok || first != userID {
		t.Fatalf("the first NextPending = (%d, %v, %v), want (%d, true, nil)", first, ok, err, userID)
	}

	_, ok, err = db.Tracks().NextPending(t.Context())
	if err != nil {
		t.Fatalf("the second NextPending returned %v", err)
	}

	if ok {
		t.Error("a second pending request existed, want the coalesced Enqueue to have left only one")
	}
}

// TestTracksNextPendingIsFIFO pins the claim order: the user enqueued first
// is claimed first.
func TestTracksNextPendingIsFIFO(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	first := tracksTestUser(t, db, "fifo-first@example.com")
	second := tracksTestUser(t, db, "fifo-second@example.com")

	if err := db.Tracks().Enqueue(t.Context(), second); err != nil {
		t.Fatalf("enqueuing second: %v", err)
	}

	if err := db.Tracks().Enqueue(t.Context(), first); err != nil {
		t.Fatalf("enqueuing first: %v", err)
	}

	got, ok, err := db.Tracks().NextPending(t.Context())
	if err != nil || !ok {
		t.Fatalf("NextPending = (%d, %v, %v)", got, ok, err)
	}

	if got != second {
		t.Errorf("NextPending = %d, want %d (enqueued first)", got, second)
	}
}

// TestTracksNextPendingRemovesTheClaim pins that claiming a request removes
// it: a second call finds nothing left, and Enqueue after the claim starts a
// fresh request rather than being silently absorbed into the one already
// claimed.
func TestTracksNextPendingRemovesTheClaim(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	userID := tracksTestUser(t, db, "claim@example.com")

	if err := db.Tracks().Enqueue(t.Context(), userID); err != nil {
		t.Fatalf("enqueuing: %v", err)
	}

	if _, ok, err := db.Tracks().NextPending(t.Context()); err != nil || !ok {
		t.Fatalf("the first NextPending = (%v, %v)", ok, err)
	}

	if _, ok, err := db.Tracks().NextPending(t.Context()); err != nil || ok {
		t.Errorf("the second NextPending = (%v, %v), want (false, nil)", ok, err)
	}

	if err := db.Tracks().Enqueue(t.Context(), userID); err != nil {
		t.Fatalf("re-enqueuing after the claim: %v", err)
	}

	if _, ok, err := db.Tracks().NextPending(t.Context()); err != nil || !ok {
		t.Errorf("NextPending after re-enqueuing = (%v, %v), want (true, nil)", ok, err)
	}
}

// TestTracksNextPendingEmpty pins the empty case: no pending request reports
// false rather than an error.
func TestTracksNextPendingEmpty(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	_, ok, err := db.Tracks().NextPending(t.Context())
	if err != nil {
		t.Fatalf("NextPending returned %v", err)
	}

	if ok {
		t.Error("NextPending reported a pending request in an empty database")
	}
}
