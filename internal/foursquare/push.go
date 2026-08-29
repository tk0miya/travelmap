package foursquare

import (
	"encoding/json"
	"fmt"
)

// ParsePushCheckin decodes raw — the value of a Swarm User Push
// notification's "checkin" form parameter — into the wire shape [Checkin],
// carrying raw itself along in [Checkin.Raw].
func ParsePushCheckin(raw string) (Checkin, error) {
	var checkin Checkin

	if err := json.Unmarshal([]byte(raw), &checkin); err != nil {
		return Checkin{}, fmt.Errorf("foursquare: parsing a pushed check-in: %w", err)
	}

	checkin.Raw = json.RawMessage(raw)

	return checkin, nil
}
