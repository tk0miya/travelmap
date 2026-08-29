package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/geo"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// dayFormat is how a calendar day is stored: a plain "YYYY-MM-DD", read in
// whatever timezone it was written under. The column carries no timezone of
// its own — that is TRAVELMAP_TIMEZONE, a server-side setting, not something
// that varies row to row.
const dayFormat = "2006-01-02"

// rebuildQuery computes, for one user and one candidate window, the point
// count and the total travelled distance for the day at [:day_start,
// :day_end).
//
// The candidate set is that day's points plus the single point immediately
// preceding :day_start, which may belong to an earlier day — the day's first
// point measures its segment against it. LAG over that set (ordered by
// timestamp) pairs each row with its predecessor; the outer WHERE then keeps
// only the rows that actually fall in the day, so the preceding point
// contributes its coordinates to that pairing but never a distance of its
// own.
//
// Named parameters are used, rather than positional ones, because
// :earth_radius_km, :user_id, :day_start and :day_end each appear more than
// once: repeating each value positionally would be easy to get out of step
// with the query text on an edit.
const rebuildQuery = `
WITH candidates AS (
	SELECT timestamp, latitude, longitude FROM (
		SELECT timestamp, latitude, longitude FROM points
		WHERE user_id = :user_id AND timestamp < :day_start
		ORDER BY timestamp DESC
		LIMIT 1
	)
	UNION ALL
	SELECT timestamp, latitude, longitude FROM points
	WHERE user_id = :user_id AND timestamp >= :day_start AND timestamp < :day_end
),
lagged AS (
	SELECT
		timestamp,
		latitude,
		longitude,
		LAG(timestamp) OVER w AS prev_timestamp,
		LAG(latitude) OVER w AS prev_latitude,
		LAG(longitude) OVER w AS prev_longitude
	FROM candidates
	WINDOW w AS (ORDER BY timestamp)
)
SELECT
	COUNT(*),
	COALESCE(SUM(
		CASE
			WHEN prev_timestamp IS NOT NULL AND timestamp - prev_timestamp <= :track_break
			THEN :earth_radius_km * 2 * atan2(
				sqrt(
					power(sin((radians(latitude) - radians(prev_latitude)) / 2.0), 2)
					+ cos(radians(prev_latitude)) * cos(radians(latitude))
						* power(sin((radians(longitude) - radians(prev_longitude)) / 2.0), 2)
				),
				sqrt(
					1 - (
						power(sin((radians(latitude) - radians(prev_latitude)) / 2.0), 2)
						+ cos(radians(prev_latitude)) * cos(radians(latitude))
							* power(sin((radians(longitude) - radians(prev_longitude)) / 2.0), 2)
					)
				)
			)
			ELSE 0
		END
	), 0)
FROM lagged
WHERE timestamp >= :day_start AND timestamp < :day_end
`

// dailyStatsRepository implements [store.DailyStatsRepository].
type dailyStatsRepository struct {
	q querier
}

// Rebuild implements [store.DailyStatsRepository].
func (r dailyStatsRepository) Rebuild(ctx context.Context, userID int64, day time.Time, trackBreak time.Duration) error {
	label := day.Format(dayFormat)

	var (
		points int
		km     float64
	)

	err := r.q.QueryRowContext(ctx, rebuildQuery,
		sql.Named("user_id", userID),
		sql.Named("day_start", unixTime(day)),
		sql.Named("day_end", unixTime(day.AddDate(0, 0, 1))),
		sql.Named("track_break", int64(trackBreak/time.Second)),
		sql.Named("earth_radius_km", geo.EarthRadiusKm),
	).Scan(&points, &km)
	if err != nil {
		return fmt.Errorf("sqlite: rebuilding daily_stats for user %d, day %s: %w", userID, label, err)
	}

	if points == 0 {
		if _, err := r.q.ExecContext(ctx,
			`DELETE FROM daily_stats WHERE user_id = ? AND day = ?`, userID, label,
		); err != nil {
			return fmt.Errorf("sqlite: deleting the emptied day %s for user %d: %w", label, userID, err)
		}

		return nil
	}

	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO daily_stats (user_id, day, points, reverse_geocoded_points, km, countries, cities)
		 VALUES (?, ?, ?, 0, ?, ?, ?)
		 ON CONFLICT (user_id, day) DO UPDATE SET
		     points = excluded.points,
		     reverse_geocoded_points = excluded.reverse_geocoded_points,
		     km = excluded.km,
		     countries = excluded.countries,
		     cities = excluded.cities`,
		// Written empty until reverse geocoding is enabled.
		userID, label, points, km, jsonStrings(nil), jsonStrings(nil),
	); err != nil {
		return fmt.Errorf("sqlite: writing daily_stats for user %d, day %s: %w", userID, label, err)
	}

	return nil
}

// DeleteAll implements [store.DailyStatsRepository].
func (r dailyStatsRepository) DeleteAll(ctx context.Context) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM daily_stats`); err != nil {
		return fmt.Errorf("sqlite: deleting daily_stats: %w", err)
	}

	return nil
}

// Get implements [store.DailyStatsRepository].
func (r dailyStatsRepository) Get(ctx context.Context, userID int64, day time.Time) (model.DailyStat, error) {
	label := day.Format(dayFormat)

	var (
		stat              model.DailyStat
		countries, cities jsonStrings
	)

	err := r.q.QueryRowContext(ctx,
		`SELECT points, reverse_geocoded_points, km, countries, cities
		 FROM daily_stats WHERE user_id = ? AND day = ?`,
		userID, label,
	).Scan(&stat.Points, &stat.ReverseGeocodedPoints, &stat.KM, &countries, &cities)
	if err != nil {
		return model.DailyStat{}, fmt.Errorf("sqlite: looking up daily_stats for user %d, day %s: %w",
			userID, label, translate(err))
	}

	stat.Countries = countries
	stat.Cities = cities
	stat.UserID = userID
	stat.Day = day

	return stat, nil
}

// All implements [store.DailyStatsRepository].
func (r dailyStatsRepository) All(ctx context.Context, userID int64) ([]model.DailyStat, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT day, points, reverse_geocoded_points, km, countries, cities
		 FROM daily_stats WHERE user_id = ? ORDER BY day ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing daily_stats for user %d: %w", userID, err)
	}

	stats, err := collect(rows, func(rows *sql.Rows) (model.DailyStat, error) {
		var (
			stat              model.DailyStat
			label             string
			countries, cities jsonStrings
		)

		if err := rows.Scan(&label, &stat.Points, &stat.ReverseGeocodedPoints, &stat.KM, &countries, &cities); err != nil {
			return model.DailyStat{}, err
		}

		day, err := time.Parse(dayFormat, label)
		if err != nil {
			return model.DailyStat{}, fmt.Errorf("parsing day %q: %w", label, err)
		}

		stat.UserID = userID
		stat.Day = day
		stat.Countries = countries
		stat.Cities = cities

		return stat, nil
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing daily_stats for user %d: %w", userID, err)
	}

	return stats, nil
}

// The interface this type exists to satisfy. See the equivalent assertion on
// [DB] for why this is worth spelling out.
var _ store.DailyStatsRepository = dailyStatsRepository{}
