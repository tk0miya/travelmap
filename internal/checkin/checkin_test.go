package checkin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/model"
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
