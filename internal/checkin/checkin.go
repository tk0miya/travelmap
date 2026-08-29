package checkin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// Write stores c, upserting on FoursquareCheckinID: push and the periodic
// fetch will both carry the same check-in, by design, so every write goes
// through here rather than a look-before-you-write in the caller. It returns
// the row as stored, with ID, CreatedAt and UpdatedAt filled in.
func Write(ctx context.Context, st store.Store, c model.Checkin) (model.Checkin, error) {
	stored, err := st.Checkins().Upsert(ctx, c)
	if err != nil {
		return model.Checkin{}, fmt.Errorf("checkin: writing %s: %w", c.FoursquareCheckinID, err)
	}

	return stored, nil
}

// ErrUnknownAccount reports that a pushed check-in named a Foursquare user id
// with no linked travelmap account. The caller's own account is not this
// server's to store, so this is not a failure to log as one: a caller that
// gets it back answers the request as having been handled correctly.
var ErrUnknownAccount = errors.New("checkin: no travelmap account is linked to this Foursquare user")

// ErrMalformedPush reports that a User Push notification's "checkin"
// parameter was not JSON at all — a shape nothing documents Foursquare
// sending. A caller answers it the same way as [ErrUnknownAccount]: logged
// and dropped, not refused, since nothing is known about what Foursquare
// makes of a non-2xx reply to a push.
var ErrMalformedPush = errors.New("checkin: the pushed checkin value is not JSON")

// WritePush parses raw — the value of a Swarm User Push notification's
// "checkin" form parameter — resolves it onto a travelmap account by
// checkin.user.id, and stores it through [Write].
//
// It returns [ErrMalformedPush] if raw is not JSON, [ErrUnknownAccount] for a
// Foursquare user id nothing here has linked, and otherwise whatever [Write]
// reports.
func WritePush(ctx context.Context, st store.Store, raw string) (model.Checkin, error) {
	parsed, err := foursquare.ParsePushCheckin(raw)
	if err != nil {
		return model.Checkin{}, fmt.Errorf("%w: %w", ErrMalformedPush, err)
	}

	account, err := st.FoursquareAccounts().ByFoursquareUserID(ctx, parsed.User.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.Checkin{}, fmt.Errorf("%w: foursquare user %s", ErrUnknownAccount, parsed.User.ID)
		}

		return model.Checkin{}, fmt.Errorf("checkin: resolving the Foursquare account for %s: %w", parsed.User.ID, err)
	}

	return Write(ctx, st, checkinFromPush(account.UserID, raw, parsed))
}

// checkinFromPush converts a parsed push checkin into the model.Checkin
// [Write] stores, for the account it resolved to. This is the one place a
// [foursquare.PushCheckin] becomes a [model.Checkin], per CLAUDE.md's
// layering rules.
func checkinFromPush(userID int64, raw string, c foursquare.PushCheckin) model.Checkin {
	checkin := model.Checkin{
		UserID:              userID,
		FoursquareCheckinID: c.ID,
		CheckedInAt:         time.Unix(c.CreatedAt, 0).UTC(),
		TimezoneOffset:      c.TimeZoneOffset,
		Shout:               c.Shout,
		Source:              "push",
		Raw:                 raw,
	}

	// The venue-derived columns are nullable as a group, for a check-in made
	// without one — not nullable field by field, since none of the push's
	// venue sub-fields are themselves documented as optional.
	if c.Venue != nil {
		checkin.VenueID = &c.Venue.ID
		checkin.VenueName = &c.Venue.Name
		checkin.Latitude = &c.Venue.Location.Lat
		checkin.Longitude = &c.Venue.Location.Lng
		checkin.CountryCode = &c.Venue.Location.CC
		checkin.City = &c.Venue.Location.City
		checkin.State = &c.Venue.Location.State
		checkin.Country = &c.Venue.Location.Country

		if category, ok := foursquare.PrimaryCategory(c.Venue.Categories); ok {
			checkin.CategoryID = &category.ID
			checkin.CategoryName = &category.Name
		}
	}

	return checkin
}
