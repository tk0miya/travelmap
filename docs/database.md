# Database

This explains travelmap's database: what a table or column means or does, beyond what its own
definition shows.

`internal/store/sqlite/schema.sql` is the accompanying source for the structure itself — a
generated, always-current snapshot of every table and index, carrying a column or index's own
rationale as a comment where one exists. Read it first; what follows here is whatever does not
fit there — too long for a schema.sql comment, or not attached to any single column or index at
all.

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
across a tracking gap or a flight lands in the total. Track splitting uses the same value.

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

## Distance calculation

Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations. The formula
therefore exists twice: share the Earth-radius constant and pin agreement with a test.

The spec does not say how upstream computes distance, so compare against the app's own display
and revisit if the numbers diverge.

## `checkins`

Swarm (Foursquare) check-ins — travelmap's own extension, not a Dawarich concept; see
"travelmap's own extensions" in TODO.md. Collected by the push webhook and the periodic fetch
(Milestone I), both of which write through `internal/checkin`, never the table directly.

### Idempotency and repeat writes

`foursquare_checkin_id` is unique, and every write is an upsert against it: push and the periodic
fetch will both carry the same check-in, by design, so idempotency lives in that index rather
than in a look-before-you-write in the caller.

**A repeat write keeps `source` and `created_at`** — when the row first appeared and which path
brought it are facts about history that a later write does not change — **and overwrites every
other column**, `raw` included, with the newest rendering of the same check-in.

`source` is `push` or `sync`, naming the path that first observed the check-in. The prose in
TODO.md calls the second path the periodic fetch while the identifiers say `sync`
(`TRAVELMAP_FOURSQUARE_SYNC_INTERVAL`, `synced_through`, `travelmap foursquare sync`); that split
is deliberate, "fetch" being the clearer word for what it does and `sync` the shorter one to type.

### `checked_in_at` vs. `created_at`

`checked_in_at` is the check-in's own time — the payload's `createdAt` — not `created_at`, which
stays the row's own bookkeeping as in every other table. The payload calls both of them
`createdAt` (the check-in has one and so does the venue inside it), so reusing the name here
would make the interesting one unfindable.

### Localisation

`country_code` (the payload's `cc`) is kept separately from `country` because the display text
is localised and `cc` is not. `cc` is the only stable column, so `country`, `city`, `state`,
`venue_name` and `category_name` are for display only and nothing keys off them. What decides
the language is unknown — see "Localisation is left unset on both paths" in TODO.md.

### Venue data is denormalised

`venue_id`, `venue_name` and the rest of the location/category columns sit directly on the
check-in rather than in a table of their own. Split them out if and when something needs a list
of distinct venues, which nothing does yet.

### `raw`

Holds the check-in JSON as received. The payload carries much more than the columns above
(`visibility`, `canonicalUrl`, `editableUntil`, `labeledLatLngs`, `formattedAddress`, the
category icon set), and it is not this server's to re-request at will: the fetch path spends an
hourly per-account budget, and `editableUntil` says a check-in Foursquare once returned can be
edited afterwards. So when a later step wants one of those fields, deriving it from stored JSON
beats re-fetching a payload that may no longer be the same one.

### Does not touch `daily_stats`

A check-in is neither a point nor a segment, so it contributes to no `/stats` total and the
"Which days to update" invariant above is untouched by this table.

## `foursquare_accounts`

One row per user, linking a travelmap account to a Swarm account. Created by `travelmap
foursquare connect` (Step 17); nothing is collected for a user until this row exists.

`foursquare_user_id` is stored as **TEXT**: the payload sends it quoted (`"1709193"`). Its unique
index is what lets an incoming push resolve to exactly one travelmap user — the webhook has no
other way to know whose check-in it is.

`access_token` is stored as issued, for the reason in the "Third-party credentials" row under
"Technical Decisions" in TODO.md.

`synced_through` records the end of the last successful fetch window, and is `NULL` until the
first one succeeds. **It is never the lower bound of a normal run** — that is always the lookback
window, for the reason under "The periodic fetch takes a window, not a cursor" in TODO.md. Its
two uses are reporting how current an account is, and recognising that an account has been
offline longer than the window so a wider one-off fetch is needed to close the gap.
