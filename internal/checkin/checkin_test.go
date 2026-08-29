package checkin_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// testUser is the account every check-in in this package's tests belongs to.
func testUser() model.User {
	return model.User{
		ID:           1,
		Email:        "checkin@example.com",
		PasswordHash: "$2a$10$notarealbcryptdigestnotarealbcryptdigestnotarealbcryptdig",
		APIKey:       "the-api-key",
		CreatedAt:    time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
		UpdatedAt:    time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
	}
}

// TestWriteStoresAndUpserts pins the one thing this package exists for: every
// write goes through here, and a repeat write for the same
// FoursquareCheckinID lands on the same row rather than a second one, keeping
// Source rather than taking the repeat write's.
func TestWriteStoresAndUpserts(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	first := model.Checkin{
		UserID:              1,
		FoursquareCheckinID: "abc123",
		CheckedInAt:         time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
		Source:              "push",
		Raw:                 `{"id":"abc123"}`,
	}

	stored, err := checkin.Write(t.Context(), st, first)
	if err != nil {
		t.Fatalf("Write returned %v", err)
	}

	if stored.ID == 0 {
		t.Fatal("Write returned a check-in with no id")
	}

	second := first
	second.Source = "sync"
	second.Raw = `{"id":"abc123","shout":"updated"}`

	repeat, err := checkin.Write(t.Context(), st, second)
	if err != nil {
		t.Fatalf("the second Write returned %v", err)
	}

	if repeat.ID != stored.ID {
		t.Errorf("the second Write returned id %d, want the first row's %d", repeat.ID, stored.ID)
	}

	if repeat.Source != "push" {
		t.Errorf("Source = %q, want the first write's %q kept", repeat.Source, "push")
	}

	if repeat.Raw != second.Raw {
		t.Errorf("Raw = %q, want the second write's %q", repeat.Raw, second.Raw)
	}
}

// TestWriteWrapsTheStoreError covers the one failure this package adds
// context to: a write against a user that does not exist, which the
// foreign key on checkins.user_id refuses. The message names the check-in,
// which is only true if Write's own error wrapping ran rather than some
// unrelated failure.
func TestWriteWrapsTheStoreError(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	_, err := checkin.Write(t.Context(), st, model.Checkin{
		UserID:              404,
		FoursquareCheckinID: "no-such-user",
		CheckedInAt:         time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
		Source:              "push",
		Raw:                 `{"id":"no-such-user"}`,
	})
	if err == nil {
		t.Fatal("Write returned nil for a check-in belonging to no user")
	}

	if !strings.Contains(err.Error(), "no-such-user") {
		t.Errorf("Write failed with %v, want it to name the check-in", err)
	}
}

// pushBody is a Swarm User Push notification's "checkin" JSON, with a venue,
// for the account this package's tests link to foursquareUserID.
const (
	foursquareUserID = "1709193"
	pushBody         = `{
		"id": "5f2a1b3c4d5e6f708192a3b4",
		"createdAt": 1767225906,
		"timeZoneOffset": 540,
		"user": {"id": "` + foursquareUserID + `"},
		"venue": {
			"id": "4b4429abf964a520a80f25e3",
			"name": "Tokyo Tower",
			"location": {"lat": 35.6586, "lng": 139.7454, "cc": "JP", "city": "Minato", "state": "Tokyo", "country": "Japan"},
			"categories": [{"id": "4bf58dd8d48988d12d941735", "name": "Monument", "primary": true}]
		}
	}`
)

// linkedUser returns a user with a Foursquare account linked to
// foursquareUserID, which is what lets a pushed check-in for that id resolve
// onto it.
func linkedUser(t *testing.T, st store.Store) model.User {
	t.Helper()

	user := testUser()

	if _, err := st.FoursquareAccounts().Create(t.Context(), model.FoursquareAccount{
		UserID:           user.ID,
		FoursquareUserID: foursquareUserID,
		AccessToken:      "the-access-token",
	}); err != nil {
		t.Fatalf("linking the Foursquare account: %v", err)
	}

	return user
}

// TestWritePushStoresAndUpserts pins the whole path a webhook push takes:
// parsed, resolved onto the linked account, and upserted like any other
// write, so a repeat delivery still lands on one row.
func TestWritePushStoresAndUpserts(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	linkedUser(t, st)

	first, err := checkin.WritePush(t.Context(), st, pushBody)
	if err != nil {
		t.Fatalf("WritePush returned %v", err)
	}

	if first.UserID != 1 {
		t.Errorf("UserID = %d, want the linked user's id 1", first.UserID)
	}

	if first.FoursquareCheckinID != "5f2a1b3c4d5e6f708192a3b4" {
		t.Errorf("FoursquareCheckinID = %q, want the payload's checkin.id", first.FoursquareCheckinID)
	}

	if !first.CheckedInAt.Equal(time.Unix(1767225906, 0).UTC()) {
		t.Errorf("CheckedInAt = %v, want the payload's createdAt", first.CheckedInAt)
	}

	if first.VenueName == nil || *first.VenueName != "Tokyo Tower" {
		t.Errorf("VenueName = %v, want %q", first.VenueName, "Tokyo Tower")
	}

	if first.CategoryID == nil || *first.CategoryID != "4bf58dd8d48988d12d941735" {
		t.Errorf("CategoryID = %v, want the payload's primary category", first.CategoryID)
	}

	if first.Source != "push" {
		t.Errorf("Source = %q, want %q", first.Source, "push")
	}

	second, err := checkin.WritePush(t.Context(), st, pushBody)
	if err != nil {
		t.Fatalf("the second WritePush returned %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("the second WritePush returned id %d, want the first row's %d", second.ID, first.ID)
	}
}

// TestWritePushWithoutAVenue pins that the venue-derived columns come back
// nil as a group for a check-in made without one, rather than partially
// filled in.
func TestWritePushWithoutAVenue(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())
	linkedUser(t, st)

	got, err := checkin.WritePush(t.Context(), st, `{"id":"no-venue","createdAt":1767225906,"user":{"id":"`+foursquareUserID+`"}}`)
	if err != nil {
		t.Fatalf("WritePush returned %v", err)
	}

	if got.VenueID != nil || got.VenueName != nil || got.Latitude != nil || got.CategoryID != nil {
		t.Errorf("a venue-derived field is set on %+v, want them all nil", got)
	}
}

// TestWritePushReportsAnUnknownAccount pins the one outcome the httpapi
// handler answers 200 for rather than an error: a push naming a Foursquare
// user id nothing here has linked.
func TestWritePushReportsAnUnknownAccount(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	_, err := checkin.WritePush(t.Context(), st, pushBody)
	if !errors.Is(err, checkin.ErrUnknownAccount) {
		t.Errorf("WritePush returned %v, want ErrUnknownAccount", err)
	}
}

// TestWritePushRejectsUnparseableJSON covers a "checkin" value that is not
// JSON at all, which is not a shape a User Push notification is documented
// to send.
func TestWritePushRejectsUnparseableJSON(t *testing.T) {
	t.Parallel()

	st := storetest.New(t, testUser())

	_, err := checkin.WritePush(t.Context(), st, `not json`)
	if !errors.Is(err, checkin.ErrMalformedPush) {
		t.Errorf("WritePush returned %v, want ErrMalformedPush", err)
	}
}
