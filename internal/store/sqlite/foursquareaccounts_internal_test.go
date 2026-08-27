package sqlite

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// testFoursquareAccount builds an account as `travelmap foursquare connect`
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
// rather than a zero account, since the push webhook (Step 18) turns exactly
// this into "log and drop", per "Webhook responses" in TODO.md.
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
