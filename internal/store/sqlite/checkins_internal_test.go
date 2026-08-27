package sqlite

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
)

// testCheckin builds a check-in with every optional field filled in, so a
// round trip through the columns has something in each to lose.
func testCheckin(userID int64, foursquareCheckinID string) model.Checkin {
	offset := 540
	venueID, venueName := "venue-1", "Tokyo Tower"
	latitude, longitude := 35.6586, 139.7454
	countryCode, city, state, country := "JP", "Minato", "Tokyo", "日本"
	categoryID, categoryName := "cat-1", "Monument"
	shout := "Great view!"

	return model.Checkin{
		UserID:              userID,
		FoursquareCheckinID: foursquareCheckinID,
		CheckedInAt:         time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
		TimezoneOffset:      &offset,
		VenueID:             &venueID,
		VenueName:           &venueName,
		Latitude:            &latitude,
		Longitude:           &longitude,
		CountryCode:         &countryCode,
		City:                &city,
		State:               &state,
		Country:             &country,
		CategoryID:          &categoryID,
		CategoryName:        &categoryName,
		Shout:               &shout,
		Source:              "push",
		Raw:                 `{"id":"` + foursquareCheckinID + `"}`,
	}
}

// TestCheckinUpsertCreates covers the plain insert: what Upsert fills in, and
// that every field survives the round trip.
func TestCheckinUpsertCreates(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("checkin@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	before := time.Now().UTC().Truncate(time.Second)

	checkin := testCheckin(user.ID, "checkin-1")

	got, err := db.Checkins().Upsert(t.Context(), checkin)
	if err != nil {
		t.Fatalf("Upsert returned %v", err)
	}

	if got.ID == 0 {
		t.Error("Upsert returned a check-in with no id")
	}

	if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now().UTC()) {
		t.Errorf("CreatedAt = %v, want a time from this test's run", got.CreatedAt)
	}

	if !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want CreatedAt %v on a first write", got.UpdatedAt, got.CreatedAt)
	}

	checkin.ID = got.ID
	checkin.CreatedAt = got.CreatedAt
	checkin.UpdatedAt = got.UpdatedAt

	if diff := cmp.Diff(checkin, got); diff != "" {
		t.Errorf("the check-in differs (-want +got):\n%s", diff)
	}
}

// TestCheckinUpsertStoresAbsentPropertiesAsNull pins that a check-in whose
// optional fields are nil comes back with nil pointers, not the zero value of
// their type — the venue-derived columns are nullable for a check-in made
// without one, per the migration's own comment.
func TestCheckinUpsertStoresAbsentPropertiesAsNull(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("bare-checkin@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	bare := model.Checkin{
		UserID:              user.ID,
		FoursquareCheckinID: "checkin-bare",
		CheckedInAt:         time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC),
		Source:              "push",
		Raw:                 `{"id":"checkin-bare"}`,
	}

	got, err := db.Checkins().Upsert(t.Context(), bare)
	if err != nil {
		t.Fatalf("Upsert returned %v", err)
	}

	for name, isNil := range map[string]bool{
		"TimezoneOffset": got.TimezoneOffset == nil, "VenueID": got.VenueID == nil, "VenueName": got.VenueName == nil,
		"Latitude": got.Latitude == nil, "Longitude": got.Longitude == nil, "CountryCode": got.CountryCode == nil,
		"City": got.City == nil, "State": got.State == nil, "Country": got.Country == nil,
		"CategoryID": got.CategoryID == nil, "CategoryName": got.CategoryName == nil, "Shout": got.Shout == nil,
	} {
		if !isNil {
			t.Errorf("%s is not nil, want it to be", name)
		}
	}
}

// TestCheckinUpsertOnConflictKeepsSourceAndCreatedAt is Step 17's own
// completion condition: writing the same check-in twice must find one row,
// carrying the second write's values everywhere except source and
// created_at, which still name the first write.
func TestCheckinUpsertOnConflictKeepsSourceAndCreatedAt(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("repeat-checkin@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	first := testCheckin(user.ID, "checkin-repeat")

	stored, err := db.Checkins().Upsert(t.Context(), first)
	if err != nil {
		t.Fatalf("the first Upsert returned %v", err)
	}

	// Backdated by hand, so that a created_at surviving the second write is
	// unmistakable rather than coincidentally close to "now" — created_at on
	// a fresh row and on the repeat write would otherwise land in the same
	// second and prove nothing.
	backdated := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.q.ExecContext(t.Context(),
		`UPDATE checkins SET created_at = ? WHERE id = ?`, backdated.Unix(), stored.ID,
	); err != nil {
		t.Fatalf("backdating created_at: %v", err)
	}

	second := testCheckin(user.ID, "checkin-repeat")
	second.Source = "sync"
	newVenueName, newShout := "Shibuya Crossing", "Back again"
	second.VenueName = &newVenueName
	second.Shout = &newShout

	got, err := db.Checkins().Upsert(t.Context(), second)
	if err != nil {
		t.Fatalf("the second Upsert returned %v", err)
	}

	if got.ID != stored.ID {
		t.Errorf("the second Upsert returned id %d, want the first row's %d", got.ID, stored.ID)
	}

	if got.Source != "push" {
		t.Errorf("Source = %q, want the first write's %q kept", got.Source, "push")
	}

	if !got.CreatedAt.Equal(backdated) {
		t.Errorf("CreatedAt = %v, want the backdated %v kept", got.CreatedAt, backdated)
	}

	if got.VenueName == nil || *got.VenueName != newVenueName {
		t.Errorf("VenueName = %v, want the second write's %q", got.VenueName, newVenueName)
	}

	if got.Shout == nil || *got.Shout != newShout {
		t.Errorf("Shout = %v, want the second write's %q", got.Shout, newShout)
	}

	var count int
	if err := db.q.QueryRowContext(t.Context(),
		`SELECT count(*) FROM checkins WHERE foursquare_checkin_id = ?`, "checkin-repeat",
	).Scan(&count); err != nil {
		t.Fatalf("counting rows: %v", err)
	}

	if count != 1 {
		t.Errorf("checkins holds %d rows for the repeated check-in, want 1", count)
	}
}
