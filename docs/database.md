# Database

This is the behaviour of the data model that does not attach to a single column or table:
cross-table invariants and algorithms. Structural rationale — why a column has the type or
constraint it does, why an index exists — is a comment in the migration that adds it
(`internal/store/sqlite/migrations/`), not repeated here; where that comment sits inside the
`CREATE TABLE` or `CREATE INDEX` it explains, it also shows up in the generated
`internal/store/sqlite/schema.sql` (kept current by `TestSchema`), so that file is the quick way
to check a column or index's own rationale before coming here.

`checkins` and `foursquare_accounts` (Milestone I) have no migration yet, so this file says
nothing about them. Their design lives in `TODO.md`'s "Data Model" section until the step that
adds them, which is also when their column-level rationale moves into the migration itself and
whatever does not fit a single column moves here.

## `points`

### Deduplication

A second point at the same `(user_id, timestamp)` is silently dropped on insert
(`ON CONFLICT (user_id, timestamp) DO NOTHING`) rather than duplicated or refused. This is
narrower than upstream's own dedup key, which also includes the coordinates: two points from one
user at the same instant are not a case a device sends deliberately, and the narrower key is what
the `points_user_id_timestamp_key` unique index can serve for both deduplication and the
`GET /points` time-range query, rather than needing an index of its own.

## `daily_stats`

`daily_stats` is a precomputed per-day aggregate and the sole source for aggregate statistics:
nothing may recompute them by aggregating `points` directly. Today's readers are `/stats` and
`/points/tracked_months`.

`countries` / `cities` are unioned when aggregating over a range longer than one day. All five
`/stats` totals (`totalDistanceKm`, `totalPointsTracked`, `totalReverseGeocodedPoints`,
`totalCountriesVisited`, `totalCitiesVisited`) must be derivable from this table alone.

Reverse geocoding is off by default, so until it is enabled `countries` / `cities` stay empty and
`reverse_geocoded_points` stays 0, and `/stats` reports 0 for the corresponding totals. Matching
upstream requires the user to configure a reverse geocoder.

**Delete the row entirely once a day has zero points.** Leaving it at zero would make
`tracked_months` keep returning months with no points, and would no longer match a rebuild.

### Segment attribution

The distance between two consecutive points is **attributed to the day of the later point**.

Segments whose time gap exceeds `TRAVELMAP_TRACK_BREAK_MINUTES` are **not counted at all**,
so `km` means "distance travelled within tracks". Without this, the straight-line distance
across a tracking gap or a flight lands in the total. Track splitting (Step 13) uses the same
value.

### Which days to update

Inserting, updating, or deleting a point changes `km` for **that point's day and the day of the
next point in time order**. The previous point's day does not change, because the `prev → P`
distance is already counted on P's day.

The next point can be days away if tracking was stopped, so never hardcode "the following day".
If an update moves a point to a different day, take those same two days for both the before and
after states. Out-of-order and late-arriving batches are handled the same way.

### How to update

**Rebuild the affected day's row from scratch. Never adjust values arithmetically.**
`countries` / `cities` are sets: when one point is deleted there is no way to tell whether that
country or city still applies to another point that day without scanning the day, so an
arithmetic approach would never shrink them and `/stats` would keep reporting inflated values.

**The rebuild input is that day's points plus the immediately preceding point**, which may be
from an earlier day. The first point of a day measures its segment against the last point of a
previous day, so it cannot be computed from that day's points alone. Missing this drops every
cross-midnight movement from `km`.

The update runs in the same transaction as the mutation that triggered it.

## Configuration affecting stored aggregates

Both are **server-side** settings. Changing either invalidates every existing `daily_stats` row
and requires `travelmap recalculate`.

| Variable | Default | Effect |
| --- | --- | --- |
| `TRAVELMAP_TIMEZONE` | `UTC` | The timezone used to cut `day`. Part of the primary key, so a change means rebuilding for every user. Running in Japan without `Asia/Tokyo` attributes movement between 00:00 and 09:00 to the previous day, making `/stats` and `tracked_months` disagree with the app |
| `TRAVELMAP_TRACK_BREAK_MINUTES` | `30` | The gap above which a segment is excluded from `km` |

`TRAVELMAP_TRACK_BREAK_MINUTES` is **distinct from `track_break` in `settings/mobile`**, which
is the app's own setting for how the device splits tracks. Using the app setting for aggregation
would change the meaning of past aggregates whenever the user changes it in the app.

The spec does not say how upstream computes distance, so compare against the app's display in
Step 11 and revisit if the numbers diverge.

## Distance calculation

Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations. The formula
therefore exists twice: share the Earth-radius constant and pin agreement with a test.

## Switching to PostgreSQL / PostGIS

SQLite was chosen after benchmarking; see commit 59d0e04 for the measurements. None of the
following apply today, but if one becomes real, add a PostgreSQL implementation behind the
`internal/store` interface — which is why the store is abstracted.

- Multiple users writing concurrently and continuously (SQLite always has a single writer)
- kNN or complex spatial joins against our own boundary data rather than an external geocoder
- H3 hex aggregation / fog of war (both non-goals)
- The database reaching tens of GB (roughly 100 bytes per point, so 10M points is about 1 GB)
