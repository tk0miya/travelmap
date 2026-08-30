package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// testFoursquareAccount builds an account as the settings page's OAuth flow
// creates one: no synced_through yet, since nothing has been fetched.
func testFoursquareAccount(userID int64, foursquareUserID string) model.FoursquareAccount {
	return model.FoursquareAccount{
		UserID:           userID,
		FoursquareUserID: foursquareUserID,
		AccessToken:      "the-access-token",
	}
}

// TestFoursquareAccountCreate covers what Create fills in, and that the
// stored row is found again by the lookup a push resolves check-ins with.
func TestFoursquareAccountCreate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("swarm@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	before := time.Now().UTC().Truncate(time.Second)

	created, err := db.FoursquareAccounts().Create(t.Context(), testFoursquareAccount(user.ID, "1709193"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if created.CreatedAt.Before(before) || created.CreatedAt.After(time.Now().UTC()) {
		t.Errorf("CreatedAt = %v, want a time from this test's run", created.CreatedAt)
	}

	if !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want CreatedAt %v on a freshly linked account", created.UpdatedAt, created.CreatedAt)
	}

	if created.SyncedThrough != nil {
		t.Errorf("SyncedThrough = %v, want nil until the first fetch succeeds", created.SyncedThrough)
	}

	got, err := db.FoursquareAccounts().ByFoursquareUserID(t.Context(), "1709193")
	if err != nil {
		t.Fatalf("ByFoursquareUserID returned %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("the account differs (-want +got):\n%s", diff)
	}
}

// TestFoursquareAccountByFoursquareUserIDReportsMissing pins ErrNotFound
// rather than a zero account, since the push webhook turns exactly this into
// a check-in it logs and drops rather than one it stores.
func TestFoursquareAccountByFoursquareUserIDReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if _, err := db.FoursquareAccounts().ByFoursquareUserID(t.Context(), "no-such-account"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByFoursquareUserID returned %v, want ErrNotFound", err)
	}
}

// TestFoursquareAccountCreateRejectsDuplicates pins ErrConflict on both
// unique columns: a second Swarm account cannot be linked to a travelmap user
// that already has one, and one Swarm account cannot link to two travelmap
// users.
func TestFoursquareAccountCreateRejectsDuplicates(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		second func(userID, otherID int64) model.FoursquareAccount
	}{
		"the same travelmap user": {
			second: func(userID, _ int64) model.FoursquareAccount {
				return testFoursquareAccount(userID, "another-foursquare-id")
			},
		},
		"the same Foursquare user id": {
			second: func(_, otherID int64) model.FoursquareAccount {
				return testFoursquareAccount(otherID, "1709193")
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)

			user, err := db.Users().Create(t.Context(), testUser("first-"+name+"@example.com"))
			if err != nil {
				t.Fatalf("creating the first user: %v", err)
			}

			other, err := db.Users().Create(t.Context(), testUser("other-"+name+"@example.com"))
			if err != nil {
				t.Fatalf("creating the other user: %v", err)
			}

			if _, err := db.FoursquareAccounts().Create(t.Context(), testFoursquareAccount(user.ID, "1709193")); err != nil {
				t.Fatalf("the first Create returned %v", err)
			}

			if _, err := db.FoursquareAccounts().Create(t.Context(), tt.second(user.ID, other.ID)); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("the second Create returned %v, want ErrConflict", err)
			}
		})
	}
}

// TestFoursquareAccountByUserID covers the Swarm connection page's own
// lookup, the reverse direction of ByFoursquareUserID.
func TestFoursquareAccountByUserID(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("swarm@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	created, err := db.FoursquareAccounts().Create(t.Context(), testFoursquareAccount(user.ID, "1709193"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	got, err := db.FoursquareAccounts().ByUserID(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("ByUserID returned %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("the account differs (-want +got):\n%s", diff)
	}
}

// TestFoursquareAccountByUserIDReportsMissing pins ErrNotFound rather than a
// zero account, so the connection page can tell a fresh account apart from
// one that failed to load.
func TestFoursquareAccountByUserIDReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if _, err := db.FoursquareAccounts().ByUserID(t.Context(), 404); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByUserID returned %v, want ErrNotFound", err)
	}
}

// TestFoursquareAccountDelete covers that Delete removes the row a re-link
// needs gone.
func TestFoursquareAccountDelete(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("swarm@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	if _, err := db.FoursquareAccounts().Create(t.Context(), testFoursquareAccount(user.ID, "1709193")); err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if err := db.FoursquareAccounts().Delete(t.Context(), user.ID); err != nil {
		t.Fatalf("Delete returned %v", err)
	}

	if _, err := db.FoursquareAccounts().ByUserID(t.Context(), user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ByUserID after Delete returned %v, want ErrNotFound", err)
	}
}

// TestFoursquareAccountDeleteMissingIsNotAnError pins that deleting a userID
// with no linked account is not an error, the same reasoning as
// [store.SessionRepository.Delete].
func TestFoursquareAccountDeleteMissingIsNotAnError(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	if err := db.FoursquareAccounts().Delete(t.Context(), 404); err != nil {
		t.Fatalf("Delete returned %v, want nil for a userID with nothing linked", err)
	}
}

// TestFoursquareAccountAll covers the listing the fetch iterates over: every
// linked account, in a defined order, and an empty result — the ordinary
// state of a server nobody has linked an account on — rather than an error.
func TestFoursquareAccountAll(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	empty, err := db.FoursquareAccounts().All(t.Context())
	if err != nil {
		t.Fatalf("All returned %v", err)
	}

	if len(empty) != 0 {
		t.Errorf("All returned %d accounts, want none before any is linked", len(empty))
	}

	var want []model.FoursquareAccount

	for _, email := range []string{"first@example.com", "second@example.com"} {
		user, err := db.Users().Create(t.Context(), testUser(email))
		if err != nil {
			t.Fatalf("creating the user: %v", err)
		}

		account, err := db.FoursquareAccounts().Create(t.Context(),
			testFoursquareAccount(user.ID, "swarm-"+email))
		if err != nil {
			t.Fatalf("Create returned %v", err)
		}

		want = append(want, account)
	}

	got, err := db.FoursquareAccounts().All(t.Context())
	if err != nil {
		t.Fatalf("All returned %v", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("All differs (-want +got):\n%s", diff)
	}
}

// TestFoursquareAccountUpdateSyncedThrough covers what a successful fetch
// leaves behind: the window's end, truncated to the second the column holds,
// and a bumped updated_at.
func TestFoursquareAccountUpdateSyncedThrough(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("swarm@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	created, err := db.FoursquareAccounts().Create(t.Context(), testFoursquareAccount(user.ID, "1709193"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	through := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if err := db.FoursquareAccounts().UpdateSyncedThrough(t.Context(), user.ID, through); err != nil {
		t.Fatalf("UpdateSyncedThrough returned %v", err)
	}

	got, err := db.FoursquareAccounts().ByFoursquareUserID(t.Context(), "1709193")
	if err != nil {
		t.Fatalf("ByFoursquareUserID returned %v", err)
	}

	if got.SyncedThrough == nil || !got.SyncedThrough.Equal(through) {
		t.Errorf("SyncedThrough = %v, want %v", got.SyncedThrough, through)
	}

	if got.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want it at or after the link's own %v", got.UpdatedAt, created.UpdatedAt)
	}
}

// TestFoursquareAccountUpdateSyncedThroughReportsMissing pins that an update
// matching no row is the same ErrNotFound the lookup reports, rather than a
// silent success that would leave a fetch believing it had recorded its run.
func TestFoursquareAccountUpdateSyncedThroughReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	err := db.FoursquareAccounts().UpdateSyncedThrough(t.Context(), 404, time.Now())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateSyncedThrough returned %v, want ErrNotFound", err)
	}
}
