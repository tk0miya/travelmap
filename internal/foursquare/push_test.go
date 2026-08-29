package foursquare_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/foursquare"
)

// pushBody reads the fixture push body: a synthetic Swarm User Push
// notification built to match every documented field and quirk, since no
// live capture was available to record one from. It is what pins the parse —
// the role a golden file plays for a response — for both this package's own
// struct and, end to end, the webhook route that hands its "checkin" value
// here.
func pushBody(t *testing.T) url.Values {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "push_body.txt"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	// The file ends in the trailing newline every text file does; the body
	// it represents does not.
	form, err := url.ParseQuery(strings.TrimRight(string(raw), "\n"))
	if err != nil {
		t.Fatalf("parsing the fixture as a form body: %v", err)
	}

	return form
}

// TestParsePushCheckin pins the wire shape the fixture parses to.
func TestParsePushCheckin(t *testing.T) {
	t.Parallel()

	form := pushBody(t)

	got, err := foursquare.ParsePushCheckin(form.Get("checkin"))
	if err != nil {
		t.Fatalf("ParsePushCheckin returned %v", err)
	}

	offset := 540

	want := foursquare.PushCheckin{
		ID:             "5f2a1b3c4d5e6f708192a3b4",
		CreatedAt:      1767225906,
		TimeZoneOffset: &offset,
		Shout:          nil,
		User:           foursquare.PushUser{ID: "1709193"},
		Venue: &foursquare.PushVenue{
			ID:   "4b4429abf964a520a80f25e3",
			Name: "東京タワー",
			Location: foursquare.PushLocation{
				Lat:     35.6586,
				Lng:     139.7454,
				CC:      "JP",
				City:    "港区",
				State:   "東京都",
				Country: "日本",
			},
			Categories: []foursquare.PushCategory{
				{ID: "4bf58dd8d48988d12d941735", Name: "モニュメント / ランドマーク", Primary: true},
			},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParsePushCheckin differs (-want +got):\n%s", diff)
	}
}

// TestParsePushCheckinIgnoresUnknownFields pins the tolerance the whole
// design leans on: a field this struct does not name must not make the parse
// fail, since the documented sample and the observed push already disagree
// about which fields exist at all.
func TestParsePushCheckinIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	got, err := foursquare.ParsePushCheckin(`{"id":"abc","somethingNew":{"nested":true}}`)
	if err != nil {
		t.Fatalf("ParsePushCheckin returned %v", err)
	}

	if got.ID != "abc" {
		t.Errorf("ID = %q, want %q", got.ID, "abc")
	}
}

// TestParsePushCheckinRejectsUnparseableJSON covers the one input this can
// refuse: a value that is not JSON at all.
func TestParsePushCheckinRejectsUnparseableJSON(t *testing.T) {
	t.Parallel()

	if _, err := foursquare.ParsePushCheckin(`not json`); err == nil {
		t.Fatal("ParsePushCheckin returned nil for a value that is not JSON")
	}
}

// TestPrimaryCategory covers the three shapes a categories list arrives in.
func TestPrimaryCategory(t *testing.T) {
	t.Parallel()

	primary := foursquare.PushCategory{ID: "primary", Primary: true}
	other := foursquare.PushCategory{ID: "other"}

	tests := map[string]struct {
		categories []foursquare.PushCategory
		want       foursquare.PushCategory
		wantOK     bool
	}{
		"one marked primary among several": {
			categories: []foursquare.PushCategory{other, primary},
			want:       primary,
			wantOK:     true,
		},
		"none marked primary falls back to the first": {
			categories: []foursquare.PushCategory{other, {ID: "second"}},
			want:       other,
			wantOK:     true,
		},
		"no categories at all": {
			categories: nil,
			want:       foursquare.PushCategory{},
			wantOK:     false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := foursquare.PrimaryCategory(tt.categories)
			if ok != tt.wantOK {
				t.Fatalf("PrimaryCategory ok = %v, want %v", ok, tt.wantOK)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PrimaryCategory differs (-want +got):\n%s", diff)
			}
		})
	}
}
