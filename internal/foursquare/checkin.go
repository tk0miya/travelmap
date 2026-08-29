package foursquare

import "encoding/json"

// Checkin is the wire shape of a Foursquare check-in: the object a Swarm User
// Push notification sends in its "checkin" form parameter, and the one
// GET /v2/users/self/checkins repeats in response.checkins.items. One struct
// serves both because it is one object — the two transports differ in how it
// is wrapped, not in what it carries.
//
// Unknown fields are ignored, and almost every field is optional: the
// documented sample payload is narrower than what has been observed to
// arrive, so a struct that required any of them would break on the next
// payload Foursquare widens.
type Checkin struct {
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

	User  User   `json:"user"`
	Venue *Venue `json:"venue"`

	// Raw is the check-in's own JSON, exactly as it arrived, which is what
	// the checkins table stores alongside the columns read out of it. It is
	// filled in by whatever decoded the check-in rather than by the decoder,
	// hence the "-" tag: encoding/json cannot hand a value the bytes it was
	// decoded from.
	Raw json.RawMessage `json:"-"`
}

// User is checkin.user. Only ID is read: it is the join key onto
// foursquare_accounts on the push path. A fetched check-in is already scoped
// to the token's own account, so nothing there needs it.
//
// The push's separate top-level "user" form parameter repeats the same
// identity with extra profile fields nothing here needs.
type User struct {
	ID string `json:"id"`
}

// Venue is checkin.venue, nil for a check-in made without one — a shape that
// has not itself been observed.
type Venue struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Location   Location   `json:"location"`
	Categories []Category `json:"categories"`
}

// Location is checkin.venue.location.
type Location struct {
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

// Category is one entry of checkin.venue.categories. Primary marks the one
// [PrimaryCategory] picks out.
type Category struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
}

// PrimaryCategory returns the category marked primary, or — if none is — the
// first one, and reports false for an empty list. Neither the reference nor
// the observed push says what more than one primary category, or none at
// all, means, so this is a choice rather than a documented rule.
func PrimaryCategory(categories []Category) (Category, bool) {
	for _, category := range categories {
		if category.Primary {
			return category, true
		}
	}

	if len(categories) > 0 {
		return categories[0], true
	}

	return Category{}, false
}
