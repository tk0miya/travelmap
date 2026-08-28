package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// checkinColumns is the select list [scanCheckin] reads, in that order.
const checkinColumns = `id, user_id, foursquare_checkin_id, checked_in_at, timezone_offset,
	venue_id, venue_name, latitude, longitude, country_code, city, state, country,
	category_id, category_name, shout, source, raw, created_at, updated_at`

// checkinRepository implements [store.CheckinRepository].
type checkinRepository struct {
	q querier
}

// Upsert implements [store.CheckinRepository].
//
// source and created_at are left out of the SET clause on purpose — they are
// what a repeat write must not touch, per "checkins" in docs/database.md.
// Every other column, raw included, becomes the newest rendering of the same
// check-in. RETURNING hands back the row as it now stands, first write or
// repeat, without a second round trip to read it back.
func (r checkinRepository) Upsert(ctx context.Context, checkin model.Checkin) (model.Checkin, error) {
	now := time.Now().UTC().Truncate(time.Second)

	row := r.q.QueryRowContext(ctx,
		`INSERT INTO checkins (
			user_id, foursquare_checkin_id, checked_in_at, timezone_offset,
			venue_id, venue_name, latitude, longitude, country_code, city, state,
			country, category_id, category_name, shout, source, raw, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (foursquare_checkin_id) DO UPDATE SET
			 user_id = excluded.user_id,
			 checked_in_at = excluded.checked_in_at,
			 timezone_offset = excluded.timezone_offset,
			 venue_id = excluded.venue_id,
			 venue_name = excluded.venue_name,
			 latitude = excluded.latitude,
			 longitude = excluded.longitude,
			 country_code = excluded.country_code,
			 city = excluded.city,
			 state = excluded.state,
			 country = excluded.country,
			 category_id = excluded.category_id,
			 category_name = excluded.category_name,
			 shout = excluded.shout,
			 raw = excluded.raw,
			 updated_at = excluded.updated_at
		 RETURNING `+checkinColumns,
		checkin.UserID, checkin.FoursquareCheckinID, unixTime(checkin.CheckedInAt), checkin.TimezoneOffset,
		checkin.VenueID, checkin.VenueName, checkin.Latitude, checkin.Longitude, checkin.CountryCode,
		checkin.City, checkin.State, checkin.Country, checkin.CategoryID, checkin.CategoryName,
		checkin.Shout, checkin.Source, checkin.Raw, unixTime(now), unixTime(now),
	)

	stored, err := scanCheckin(row)
	if err != nil {
		return model.Checkin{}, fmt.Errorf("sqlite: upserting the check-in %s: %w", checkin.FoursquareCheckinID, err)
	}

	return stored, nil
}

// scanCheckin reads one row of [checkinColumns].
func scanCheckin(row *sql.Row) (model.Checkin, error) {
	var (
		c                                 model.Checkin
		checkedInAt, createdAt, updatedAt unixTime
		timezoneOffset                    sql.NullInt64
		venueID, venueName                sql.NullString
		latitude, longitude               sql.NullFloat64
		countryCode, city, state, country sql.NullString
		categoryID, categoryName, shout   sql.NullString
	)

	err := row.Scan(
		&c.ID, &c.UserID, &c.FoursquareCheckinID, &checkedInAt, &timezoneOffset,
		&venueID, &venueName, &latitude, &longitude, &countryCode, &city, &state, &country,
		&categoryID, &categoryName, &shout, &c.Source, &c.Raw, &createdAt, &updatedAt,
	)
	if err != nil {
		return model.Checkin{}, translate(err)
	}

	c.CheckedInAt = time.Time(checkedInAt)
	c.CreatedAt = time.Time(createdAt)
	c.UpdatedAt = time.Time(updatedAt)
	c.TimezoneOffset = nullInt(timezoneOffset)
	c.VenueID = nullString(venueID)
	c.VenueName = nullString(venueName)
	c.Latitude = nullFloat64(latitude)
	c.Longitude = nullFloat64(longitude)
	c.CountryCode = nullString(countryCode)
	c.City = nullString(city)
	c.State = nullString(state)
	c.Country = nullString(country)
	c.CategoryID = nullString(categoryID)
	c.CategoryName = nullString(categoryName)
	c.Shout = nullString(shout)

	return c, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.CheckinRepository = checkinRepository{}
