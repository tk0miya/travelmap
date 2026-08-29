package foursquare

import (
	"encoding/json"
	"fmt"
)

// PushCheckin is the wire shape of a Swarm User Push notification's "checkin"
// form parameter.
//
// Unknown fields are ignored, and almost every field is optional: the
// documented sample payload is narrower than what has been observed to
// arrive, so a struct that required any of them would break on the next
// payload Foursquare widens.
type PushCheckin struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`

	// TimeZoneOffset is minutes. A pointer, like every field below, so that a
	// key genuinely absent from the payload comes back nil rather than the
	// zero value of its type.
	TimeZoneOffset *int `json:"timeZoneOffset"`

	// Shout was absent as a key, not present as an empty string, in the
	// observed push — so a nil pointer here is read as "no shout" rather
	// than "an empty one".
	Shout *string `json:"shout"`

	User  PushUser   `json:"user"`
	Venue *PushVenue `json:"venue"`
}

// PushUser is checkin.user. Only ID is read: it is the join key onto
// foursquare_accounts. The separate top-level "user" form parameter repeats
// the same identity with extra profile fields nothing here needs.
type PushUser struct {
	ID string `json:"id"`
}

// PushVenue is checkin.venue, nil for a check-in made without one — a shape
// that has not itself been observed.
type PushVenue struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Location   PushLocation   `json:"location"`
	Categories []PushCategory `json:"categories"`
}

// PushLocation is checkin.venue.location.
type PushLocation struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`

	// CC is the stable country code. Country is the localised display text,
	// which the reference documents as coming back in "the language that's
	// most popular in the country for that venue" absent a locale — so it is
	// kept apart from CC rather than treated as another rendering of it.
	CC      string `json:"cc"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

// PushCategory is one entry of checkin.venue.categories. Primary marks the
// one [PrimaryCategory] picks out.
type PushCategory struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
}

// PrimaryCategory returns the category marked primary, or — if none is — the
// first one, and reports false for an empty list. Neither the reference nor
// the observed push says what more than one primary category, or none at
// all, means, so this is a choice rather than a documented rule.
func PrimaryCategory(categories []PushCategory) (PushCategory, bool) {
	for _, category := range categories {
		if category.Primary {
			return category, true
		}
	}

	if len(categories) > 0 {
		return categories[0], true
	}

	return PushCategory{}, false
}

// ParsePushCheckin decodes raw — the value of a Swarm User Push
// notification's "checkin" form parameter — into the wire shape above.
func ParsePushCheckin(raw string) (PushCheckin, error) {
	var checkin PushCheckin

	if err := json.Unmarshal([]byte(raw), &checkin); err != nil {
		return PushCheckin{}, fmt.Errorf("foursquare: parsing a pushed check-in: %w", err)
	}

	return checkin, nil
}
