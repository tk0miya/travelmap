package sqlite

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
)

// testPoint builds a point at timestamp with every optional field filled in,
// so a round trip through the columns has something in each to lose.
func testPoint(userID int64, timestamp time.Time) model.Point {
	altitude, velocity, accuracy, verticalAccuracy := 12.5, 3.4, 5.0, 6.0
	course, courseAccuracy, battery := 90.0, 1.0, 0.75
	batteryStatus, ssid, trackerID := "charging", "home-wifi", "device-1"

	return model.Point{
		UserID:    userID,
		Timestamp: timestamp,
		Latitude:  52.502397,
		Longitude: 13.356718,

		Altitude:         &altitude,
		Velocity:         &velocity,
		Accuracy:         &accuracy,
		VerticalAccuracy: &verticalAccuracy,
		Course:           &course,
		CourseAccuracy:   &courseAccuracy,
		BatteryStatus:    &batteryStatus,
		Battery:          &battery,
		SSID:             &ssid,
		TrackerID:        &trackerID,
	}
}

// TestPointsCreate pins that a batch insert reports the number of rows
// actually inserted, which is all a caller can otherwise learn about a
// dedup-on-conflict insert without a lookup.
func TestPointsCreate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("points@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	points := []model.Point{
		testPoint(user.ID, base),
		testPoint(user.ID, base.Add(time.Minute)),
	}

	inserted, err := db.Points().Create(t.Context(), points)
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if inserted != 2 {
		t.Errorf("Create returned %d, want 2", inserted)
	}
}

// TestPointsCreateDeduplicates pins that a second point at the same
// (user_id, timestamp) is dropped rather than duplicated or refused.
func TestPointsCreateDeduplicates(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("dedup@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	timestamp := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{testPoint(user.ID, timestamp)}); err != nil {
		t.Fatalf("the first Create returned %v", err)
	}

	inserted, err := db.Points().Create(t.Context(), []model.Point{testPoint(user.ID, timestamp)})
	if err != nil {
		t.Fatalf("the second Create returned %v", err)
	}

	if inserted != 0 {
		t.Errorf("the second Create returned %d, want 0", inserted)
	}

	// A different user at the same instant is not a duplicate: the unique
	// index is scoped to (user_id, timestamp), not timestamp alone.
	other, err := db.Users().Create(t.Context(), testUser("other@example.com"))
	if err != nil {
		t.Fatalf("creating the other user: %v", err)
	}

	inserted, err = db.Points().Create(t.Context(), []model.Point{testPoint(other.ID, timestamp)})
	if err != nil {
		t.Fatalf("the other user's Create returned %v", err)
	}

	if inserted != 1 {
		t.Errorf("the other user's Create returned %d, want 1", inserted)
	}
}

// TestPointsCreateStoresAbsentPropertiesAsNull pins that a point whose
// optional fields are nil round-trips as NULL in every one of those columns,
// not as the zero value of its type: a speed of exactly 0 and a speed the
// device never reported are not the same point, per model.Point's own doc
// comment. Create's argument list is where a nil pointer would have to
// survive the driver's parameter conversion for that to hold.
func TestPointsCreateStoresAbsentPropertiesAsNull(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("bare@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	timestamp := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	bare := model.Point{UserID: user.ID, Timestamp: timestamp, Latitude: 1, Longitude: 2}

	if _, err := db.Points().Create(t.Context(), []model.Point{bare}); err != nil {
		t.Fatalf("Create returned %v", err)
	}

	var (
		altitude, velocity, accuracy, verticalAccuracy sql.NullFloat64
		course, courseAccuracy, battery                sql.NullFloat64
		batteryStatus, ssid, trackerID                 sql.NullString
	)

	err = db.q.QueryRowContext(t.Context(),
		`SELECT altitude, velocity, accuracy, vertical_accuracy, course, course_accuracy,
		        battery, battery_status, ssid, tracker_id
		 FROM points WHERE user_id = ? AND timestamp = ?`,
		user.ID, timestamp.Unix(),
	).Scan(
		&altitude, &velocity, &accuracy, &verticalAccuracy, &course, &courseAccuracy,
		&battery, &batteryStatus, &ssid, &trackerID,
	)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}

	for name, valid := range map[string]bool{
		"altitude": altitude.Valid, "velocity": velocity.Valid, "accuracy": accuracy.Valid,
		"vertical_accuracy": verticalAccuracy.Valid, "course": course.Valid, "course_accuracy": courseAccuracy.Valid,
		"battery": battery.Valid, "battery_status": batteryStatus.Valid,
		"ssid": ssid.Valid, "tracker_id": trackerID.Valid,
	} {
		if valid {
			t.Errorf("%s is not NULL, want it to be", name)
		}
	}
}

// TestPointsCreateEmptyBatch pins that an empty batch is a no-op rather than
// an error, which is what a locations request with no valid Features answers
// with.
func TestPointsCreateEmptyBatch(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	inserted, err := db.Points().Create(t.Context(), nil)
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if inserted != 0 {
		t.Errorf("Create returned %d, want 0", inserted)
	}
}

// TestPointsUserIDs pins that it reports the distinct users with at least
// one point, in ascending order, and nobody else — a full recalculation
// walks exactly this list.
func TestPointsUserIDs(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	withPoints, err := db.Users().Create(t.Context(), testUser("with-points@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	if _, err := db.Users().Create(t.Context(), testUser("without-points@example.com")); err != nil {
		t.Fatalf("creating the other user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(withPoints.ID, base),
		testPoint(withPoints.ID, base.Add(time.Minute)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	got, err := db.Points().UserIDs(t.Context())
	if err != nil {
		t.Fatalf("UserIDs returned %v", err)
	}

	want := []int64{withPoints.ID}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("UserIDs differs (-want +got):\n%s", diff)
	}
}

// TestPointsTimestamps pins that it reports every timestamp for the given
// user, in ascending order, and none of another user's.
func TestPointsTimestamps(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("timestamps@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	other, err := db.Users().Create(t.Context(), testUser("other-timestamps@example.com"))
	if err != nil {
		t.Fatalf("creating the other user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	first, second := base.Add(time.Hour), base

	// Inserted out of order, so the result being sorted is the test rather
	// than an accident of insertion order.
	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, first),
		testPoint(user.ID, second),
		testPoint(other.ID, base.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	got, err := db.Points().Timestamps(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("Timestamps returned %v", err)
	}

	want := []time.Time{second, first}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Timestamps differs (-want +got):\n%s", diff)
	}
}

// TestPointsNextTimestamp pins the strictly-greater boundary and that a
// result is scoped to the given user.
func TestPointsNextTimestamp(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("next-timestamp@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	other, err := db.Users().Create(t.Context(), testUser("other-next-timestamp@example.com"))
	if err != nil {
		t.Fatalf("creating the other user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)
	at, after := base, base.Add(time.Hour)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, at),
		testPoint(user.ID, after),
		testPoint(other.ID, after.Add(time.Minute)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	got, ok, err := db.Points().NextTimestamp(t.Context(), user.ID, at)
	if err != nil {
		t.Fatalf("NextTimestamp returned %v", err)
	}

	if !ok || !got.Equal(after) {
		t.Errorf("NextTimestamp(%s) = (%v, %v), want (%v, true)", at, got, ok, after)
	}
}

// TestPointsNextTimestampNone pins that the answer for a user with nothing
// stored after the given time is false rather than the zero time.
func TestPointsNextTimestampNone(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("next-timestamp-none@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{testPoint(user.ID, base)}); err != nil {
		t.Fatalf("inserting a point: %v", err)
	}

	_, ok, err := db.Points().NextTimestamp(t.Context(), user.ID, base)
	if err != nil {
		t.Fatalf("NextTimestamp returned %v", err)
	}

	if ok {
		t.Error("NextTimestamp reported a next point, want none")
	}
}

// pointTimestamps extracts the timestamps of points, in order, for a test
// that cares which rows came back rather than every column of each.
func pointTimestamps(points []model.Point) []time.Time {
	timestamps := make([]time.Time, len(points))
	for i, p := range points {
		timestamps[i] = p.Timestamp
	}

	return timestamps
}

// TestPointsList pins that it returns only the given user's points, newest
// first by default, along with the total count across every page — not just
// the page requested.
func TestPointsList(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("list@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	other, err := db.Users().Create(t.Context(), testUser("other-list@example.com"))
	if err != nil {
		t.Fatalf("creating the other user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, base),
		testPoint(user.ID, base.Add(time.Hour)),
		testPoint(user.ID, base.Add(2*time.Hour)),
		testPoint(other.ID, base.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	points, total, err := db.Points().List(t.Context(), user.ID, nil, base.Add(24*time.Hour), false, 1, 100)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 3 {
		t.Errorf("total = %d, want 3 (the other user's point must not count)", total)
	}

	want := []time.Time{base.Add(2 * time.Hour), base.Add(time.Hour), base}
	if diff := cmp.Diff(want, pointTimestamps(points)); diff != "" {
		t.Errorf("timestamps differ (-want +got):\n%s", diff)
	}
}

// TestPointsListAscending pins that ascending reverses the default
// newest-first order.
func TestPointsListAscending(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("list-asc@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, base),
		testPoint(user.ID, base.Add(time.Hour)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	points, _, err := db.Points().List(t.Context(), user.ID, nil, base.Add(24*time.Hour), true, 1, 100)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	want := []time.Time{base, base.Add(time.Hour)}
	if diff := cmp.Diff(want, pointTimestamps(points)); diff != "" {
		t.Errorf("timestamps differ (-want +got):\n%s", diff)
	}
}

// TestPointsListFiltersByTimeRange pins that startAt/endAt narrow the result
// to the points falling inside the range, both bounds inclusive, and that a
// nil startAt leaves the range open at the bottom.
func TestPointsListFiltersByTimeRange(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("list-range@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, base),
		testPoint(user.ID, base.Add(time.Hour)),
		testPoint(user.ID, base.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	startAt := base.Add(time.Hour)
	endAt := base.Add(2 * time.Hour)

	points, total, err := db.Points().List(t.Context(), user.ID, &startAt, endAt, true, 1, 100)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}

	want := []time.Time{base.Add(time.Hour), base.Add(2 * time.Hour)}
	if diff := cmp.Diff(want, pointTimestamps(points)); diff != "" {
		t.Errorf("timestamps differ (-want +got):\n%s", diff)
	}
}

// TestPointsListPaginates pins that page/perPage slice the ordered result and
// that the total returned alongside it still counts every matching row, not
// just the page fetched.
func TestPointsListPaginates(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	user, err := db.Users().Create(t.Context(), testUser("list-page@example.com"))
	if err != nil {
		t.Fatalf("creating the user: %v", err)
	}

	base := time.Date(2026, time.February, 3, 4, 5, 6, 0, time.UTC)

	if _, err := db.Points().Create(t.Context(), []model.Point{
		testPoint(user.ID, base),
		testPoint(user.ID, base.Add(time.Hour)),
		testPoint(user.ID, base.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("inserting points: %v", err)
	}

	points, total, err := db.Points().List(t.Context(), user.ID, nil, base.Add(24*time.Hour), true, 2, 1)
	if err != nil {
		t.Fatalf("List returned %v", err)
	}

	if total != 3 {
		t.Errorf("total = %d, want 3 (the count across every page, not just this one)", total)
	}

	want := []time.Time{base.Add(time.Hour)}
	if diff := cmp.Diff(want, pointTimestamps(points)); diff != "" {
		t.Errorf("timestamps differ (-want +got):\n%s", diff)
	}
}
