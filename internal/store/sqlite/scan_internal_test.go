package sqlite

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestUnixTimeRoundTrip(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, time.March, 4, 5, 6, 7, 0, time.UTC)

	value, err := unixTime(want).Value()
	if err != nil {
		t.Fatalf("Value returned %v", err)
	}

	var got unixTime
	if err := got.Scan(value); err != nil {
		t.Fatalf("Scan returned %v", err)
	}

	if diff := cmp.Diff(want, time.Time(got)); diff != "" {
		t.Errorf("round trip (-want +got):\n%s", diff)
	}
}

// TestUnixTimeScanRejectsAnythingButInt64 pins the failure mode for a driver
// value this type does not expect: STRICT guarantees the column is always an
// integer, so this path exists for the same reason [translate] exists rather
// than because a real column can produce it.
func TestUnixTimeScanRejectsAnythingButInt64(t *testing.T) {
	t.Parallel()

	var got unixTime
	if err := got.Scan("2026-03-04"); err == nil {
		t.Error("Scan returned nil, want an error")
	}
}

func TestJSONStringsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		strings jsonStrings
		want    string // the stored representation
	}{
		"empty": {
			strings: jsonStrings{},
			want:    "[]",
		},
		// A nil slice is what a caller that has not populated countries or
		// cities holds, per Rebuild before reverse geocoding writes them.
		"nil": {
			strings: nil,
			want:    "[]",
		},
		"values": {
			strings: jsonStrings{"JP", "US"},
			want:    `["JP","US"]`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			value, err := tt.strings.Value()
			if err != nil {
				t.Fatalf("Value returned %v", err)
			}

			if value != tt.want {
				t.Errorf("Value() = %q, want %q", value, tt.want)
			}

			var got jsonStrings
			if err := got.Scan(value); err != nil {
				t.Fatalf("Scan returned %v", err)
			}

			want := tt.strings
			if want == nil {
				want = jsonStrings{}
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("round trip (-want +got):\n%s", diff)
			}
		})
	}
}

// TestJSONStringsScanRejectsAnythingButString pins the failure mode for a
// driver value this type does not expect, for the reason given on
// [TestUnixTimeScanRejectsAnythingButInt64].
func TestJSONStringsScanRejectsAnythingButString(t *testing.T) {
	t.Parallel()

	var got jsonStrings
	if err := got.Scan(int64(0)); err == nil {
		t.Error("Scan returned nil, want an error")
	}
}
