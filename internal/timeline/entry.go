package timeline

import (
	"context"
	"fmt"
	"time"

	"github.com/tk0miya/travelmap/internal/geo"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// EntryKind distinguishes what produced an [Entry]. assemble today only ever
// emits EntryKindMove; a second kind arrives once stay detection folds
// visits into the same assembly.
type EntryKind string

// EntryKindMove is a check-in and the movement to the next entry.
const EntryKindMove EntryKind = "move"

// Entry is one item in an assembled timeline: a moment, in its own local
// time, and what it cost to reach the next one.
type Entry struct {
	Kind EntryKind

	// At is when the entry happened, rendered in the check-in's own
	// TimezoneOffset where it has one, and in the caller's fallback
	// location otherwise.
	At time.Time

	// Elapsed is the time to the next entry in the same assembly, zero for
	// the last one.
	Elapsed time.Duration

	// DistanceMeters is summed from the points recorded between this entry
	// and the next, zero for the last one or when none were recorded.
	DistanceMeters float64

	// Checkin is the check-in this entry was built from.
	Checkin model.Checkin
}

// sources bundles the raw rows assemble folds into a timeline. A struct
// rather than positional parameters is what lets stay detection add a third
// field, for visits, without changing the shape of every existing call.
type sources struct {
	// Checkins and Points must each already be ordered by their own time
	// field ascending — the precondition [store.CheckinRepository.List] and
	// [store.PointRepository.InRange] already satisfy.
	Checkins []model.Checkin
	Points   []model.Point
}

// Entries assembles userID's timeline for [from, to): one [Entry] per
// check-in in the range, in CheckedInAt order, each carrying the elapsed
// time and the distance travelled to the next one.
//
// fallback is the [time.Location] an entry's At renders in when its own
// check-in carries no TimezoneOffset.
func Entries(
	ctx context.Context, st store.Store, userID int64, from, to time.Time, fallback *time.Location,
) ([]Entry, error) {
	checkins, err := st.Checkins().List(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("timeline: listing check-ins for user %d: %w", userID, err)
	}

	points, err := st.Points().InRange(ctx, userID, from, to)
	if err != nil {
		return nil, fmt.Errorf("timeline: listing points for user %d: %w", userID, err)
	}

	return assemble(sources{Checkins: checkins, Points: points}, fallback), nil
}

// assemble builds one Entry per check-in in src.Checkins, each carrying the
// elapsed time and the distance to the next one, summed from src.Points
// between the two. A point outside every interval — before the first
// check-in, or after the last one, which has no next check-in to bound an
// interval with — contributes to no entry.
func assemble(src sources, fallback *time.Location) []Entry {
	entries := make([]Entry, len(src.Checkins))
	next := 0 // src.Points not yet consumed by an earlier interval

	for i, c := range src.Checkins {
		entries[i] = Entry{
			Kind:    EntryKindMove,
			At:      localTime(c.CheckedInAt, c.TimezoneOffset, fallback),
			Checkin: c,
		}

		if i+1 == len(src.Checkins) {
			continue
		}

		upTo := src.Checkins[i+1].CheckedInAt
		entries[i].Elapsed = upTo.Sub(c.CheckedInAt)

		var (
			distanceKm float64
			prev       *model.Point
		)

		for next < len(src.Points) && src.Points[next].Timestamp.Before(upTo) {
			p := src.Points[next]
			next++

			if p.Timestamp.Before(c.CheckedInAt) {
				continue
			}

			if prev != nil {
				distanceKm += geo.Haversine(prev.Latitude, prev.Longitude, p.Latitude, p.Longitude)
			}

			prev = &p
		}

		entries[i].DistanceMeters = distanceKm * 1000
	}

	return entries
}

// localTime renders at in offsetMinutes' own zone when it is not nil, and in
// fallback otherwise — a check-in's own TimezoneOffset wins because it is
// what the device measured at the place itself, tracking.timezone being only
// a server-wide default.
func localTime(at time.Time, offsetMinutes *int, fallback *time.Location) time.Time {
	if offsetMinutes == nil {
		return at.In(fallback)
	}

	return at.In(time.FixedZone("", *offsetMinutes*60))
}
