package sqlite

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// testSettings builds a settings row with every field set to something other
// than [model.DefaultSettings], so a round trip through Create/Update/Get
// cannot pass by accident on an untouched default.
func testSettings(userID int64) model.Settings {
	scale := "viridis"

	return model.Settings{
		UserID: userID,

		TrackingMode:                    "significant",
		TrackingVisits:                  false,
		TrackVisitsIndependently:        true,
		AutoStart:                       false,
		DistanceFilter:                  50,
		TimeFilter:                      20,
		TrackBreak:                      45,
		Accuracy:                        5,
		ShowBackgroundLocationIndicator: false,
		UploadAutomatically:             false,
		UploadAllOnTrackingStop:         true,
		BatchSize:                       200,

		RouteOpacity:          0.8,
		MetersBetweenRoutes:   600,
		MinutesBetweenRoutes:  45,
		FogOfWarMeters:        75,
		TimeThresholdMinutes:  60,
		MergeThresholdMinutes: 20,
		PreferredMapLayer:     "Satellite",
		SpeedColoredRoutes:    true,
		PointsRenderingMode:   "heatmap",
		LiveMapEnabled:        false,
		SpeedColorScale:       &scale,
		FogOfWarThreshold:     80,
		DistanceUnit:          "mi",
	}
}

// TestSettingsGetReportsMissing pins ErrNotFound rather than a zero row for
// a user with no settings row at all — the repository's own contract,
// exercised here by creating the user through db.Users().Create directly
// rather than internal/auth.Register, which is what actually guarantees
// every real account one.
func TestSettingsGetReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("alice@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	if _, err := db.Settings().Get(t.Context(), user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get returned %v, want ErrNotFound", err)
	}
}

// TestSettingsCreate covers the first write: every field round-trips, and
// CreatedAt/UpdatedAt are stamped with the same instant.
func TestSettingsCreate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("alice@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	want := testSettings(user.ID)

	created, err := db.Settings().Create(t.Context(), want)
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if !created.UpdatedAt.Equal(created.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want CreatedAt %v on a freshly stored row", created.UpdatedAt, created.CreatedAt)
	}

	want.CreatedAt = created.CreatedAt
	want.UpdatedAt = created.UpdatedAt

	if diff := cmp.Diff(want, created); diff != "" {
		t.Errorf("the settings differ (-want +got):\n%s", diff)
	}

	got, err := db.Settings().Get(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}

	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("Get differs from what Create returned (-want +got):\n%s", diff)
	}
}

// TestSettingsCreateNullSpeedColorScale covers that a nil SpeedColorScale
// round-trips as nil rather than as an empty string.
func TestSettingsCreateNullSpeedColorScale(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("alice@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	settings := testSettings(user.ID)
	settings.SpeedColorScale = nil

	stored, err := db.Settings().Create(t.Context(), settings)
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if stored.SpeedColorScale != nil {
		t.Errorf("SpeedColorScale = %v, want nil", *stored.SpeedColorScale)
	}
}

// TestSettingsUpdate covers a repeat write: it replaces every field but
// leaves CreatedAt alone, since Update's own SET clause never touches it.
func TestSettingsUpdate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("alice@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	created, err := db.Settings().Create(t.Context(), testSettings(user.ID))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	want := model.DefaultSettings(user.ID)

	updated, err := db.Settings().Update(t.Context(), want)
	if err != nil {
		t.Fatalf("Update returned %v", err)
	}

	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want the row's original %v kept", updated.CreatedAt, created.CreatedAt)
	}

	want.CreatedAt = updated.CreatedAt
	want.UpdatedAt = updated.UpdatedAt

	if diff := cmp.Diff(want, updated); diff != "" {
		t.Errorf("the settings differ (-want +got):\n%s", diff)
	}
}

// TestSettingsUpdateReportsMissing pins ErrNotFound rather than a silent
// no-op for a user with no settings row — the same case [TestSettingsGetReportsMissing]
// covers for Get, which does not happen for a real account either.
func TestSettingsUpdateReportsMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("alice@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	if _, err := db.Settings().Update(t.Context(), model.DefaultSettings(user.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Update returned %v, want ErrNotFound", err)
	}
}
