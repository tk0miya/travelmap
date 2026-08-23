# travelmap — Development Plan for a Dawarich-Compatible API Server

## Goal

Implement a [Dawarich](https://github.com/Freika/dawarich)-compatible web API server in Go.

Upstream Dawarich is a multi-container stack of Rails + PostgreSQL/PostGIS + Sidekiq + Redis,
which is a large runtime footprint for personal use. This project aims for a lightweight
compatible server that runs as **a single statically linked binary plus one SQLite file**.

**Final goal**: point the Dawarich iPhone app at this server and be able to record and
browse location history.

After that, we plan to **build our own web UI** (Milestone H). It will not be a port of the
upstream browser screens; it will be built on top of this server's API. The API comes first,
and the policy is to **reuse the existing `/api/v1` rather than adding UI-only data
endpoints** (browser-specific routes such as login and sessions are added in Milestone H).

### Non-goals

The following are out of scope for this project.

- Immich / Photoprism integration and photo-related APIs
- Billing and subscription APIs
- Family sharing (Families)
- H3 hex maps / fog of war
- Areas, Places, Notes, Tags, Digests, Insights

## Reference Specification

The Dawarich OpenAPI document is the single source of truth for compatibility.

- Source: `https://raw.githubusercontent.com/Freika/dawarich/master/swagger/v1/swagger.yaml`
- Fetched: 2026-08-17
- Fingerprint: 5680 lines / `sha256:a16411a389e0130d9e0b04b54cfc80726c234b8a017cc76d9d921bfc91adc89a`

Upstream changes continuously, so update the fingerprint above whenever the spec is
re-fetched. A running Dawarich instance also serves the same document at `/api-docs`.

## Technical Decisions

| Item | Decision | Rationale |
| --- | --- | --- |
| Data store | SQLite (`modernc.org/sqlite`, no CGO) | Self-contained in a single static binary. No DB process, so the footprint is minimal. Chosen after benchmarking (commit 59d0e04) |
| Store abstraction | Repository layer behind an interface, reached through a `store.Store` that also runs transactions | Leaves room to add a PostgreSQL implementation later. Handing repositories out through one object is what lets a point insert and its `daily_stats` rebuild share a transaction (Step 4) |
| Migrations | `github.com/pressly/goose/v3` as a library, over numbered `.sql` files in an `embed.FS` | Measured in Step 4 by writing both against the same API: 86 lines of migrator and 107 of test with goose, against 213 and 137 hand-rolled, for one direct dependency and four indirect ones. Down migrations, Go-code migrations and an applied-at history come with it. The cost is having to read goose's bookkeeping rather than own it: **it creates and commits its version table, with a row for version 0, before the first migration runs**, so "the version table exists" is not "the schema exists" — a hand-rolled migrator bumping `user_version` inside the DDL's own transaction had no such state |
| Connections | One connection (`SetMaxOpenConns(1)`), WAL, `synchronous=NORMAL`, `BEGIN IMMEDIATE`, `busy_timeout=5s` | SQLite has a single writer, so a larger pool converts concurrent writes into `SQLITE_BUSY` for every caller to retry; with one they queue in `database/sql`. The timeout is for the second process — a CLI command run while the server is serving (Step 4). Two races are left unhandled deliberately, both needing something the workflow does not do: several processes opening a **brand-new** database at the same moment, where converting the file to WAL answers `SQLITE_BUSY` and no timeout can wait it out; and two `travelmap migrate` processes at once, where goose has no lock for SQLite and the loser fails on the DDL instead of reporting nothing to do. The first run is one `travelmap migrate`, and retry logic for a race nobody reaches would cost more than it returns |
| SQL | Hand-written in `internal/store/sqlite`, no generator | The queries are few and shaped by the API's own filters, and a generator would add a code-generation step to a checkout that today needs nothing installed but Go (Step 4) |
| Schema | `STRICT` tables, times as integer Unix seconds | Without `STRICT`, type affinity stores a string in an `INTEGER` column and the mistake surfaces as a query that quietly stops matching. Times are integers because `points.timestamp` arrives as a Unix timestamp and is range-compared on every request (Step 4) |
| Indexes | A single `points(user_id, timestamp)` | App queries always narrow by user and time range first. See "Data Model" |
| Distance | Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations | Pulling a whole-history aggregation into Go spends all its time transferring rows. See "Data Model" |
| Statistics | `daily_stats` precomputed table, updated during ingest | Aggregating `points` on demand is too slow to serve a request. See "Data Model" |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Chosen with the future web UI in mind (see "Library Choices for the Web UI") |
| Compatibility scope | Mobile-app subset | Everything except the non-goals above |
| User management | Issued via CLI. `auth/login` implemented, `auth/register` optional behind an env var, no 2FA | Self-hosted assumption |
| Background work | Goroutines + a job table in SQLite | Keeps a single process, with no Sidekiq/Redis equivalent |
| Reverse geocoding | Off by default; optionally point at a Nominatim/Photon URL | Does not make an external service mandatory |

These are the defaults as of planning. If any turns out to be wrong during implementation,
change it — after updating this file.

## Data Model

### `users`

Columns: `id`, `email`, `password_hash`, `api_key`, `created_at`, `updated_at`. Unique indexes on
`email` and on `api_key`; `email` is declared `COLLATE NOCASE`, so an address is one identity
however it was typed and the index refuses a second account differing only in case.

`api_key` is stored **as issued, not hashed**: `POST /api/v1/auth/login` has to hand the key
itself back to the client, and a digest could not be turned back into one.

The login response's `status`, `plan`, `subscription_source` and `active_until` get **no columns**
— they are upstream Cloud's billing fields, billing is a non-goal, and Step 5 answers them with
the constants a self-hosted upstream instance reports; the values are on the `auth/login` bullet
under "Per-endpoint".

### `points`

Cover every field of the upstream point object. One index: `(user_id, timestamp)`.
Without it the `GET /points` time filter degrades to a full scan.

No latitude/longitude index and no R\*Tree: no in-scope endpoint takes a bounding box
(`GET /points` accepts only `start_at` / `end_at` / `page` / `per_page` / `order`), and an index
with no query to serve costs insert time and storage for nothing. Decide which to add if and
when rectangular search is actually needed.

### `daily_stats`

A precomputed per-day aggregate. `/stats` and `/points/tracked_months` read only from here and
must never aggregate `points` directly.

Columns: `user_id`, `day`, `points`, `reverse_geocoded_points`, `km`, `countries`, `cities`.
Primary key: the composite `(user_id, day)`.
`countries` / `cities` are JSON arrays of the country and city names visited that day; take the
union when aggregating over a longer range. All five `/stats` totals (`totalDistanceKm`,
`totalPointsTracked`, `totalReverseGeocodedPoints`, `totalCountriesVisited`,
`totalCitiesVisited`) must be derivable from this table alone.

Reverse geocoding is off by default, so until it is enabled `countries` / `cities` stay empty
and `reverse_geocoded_points` stays 0; `/stats` reports 0 for the corresponding totals. Matching
upstream requires the user to configure a reverse geocoder.

**Delete the row entirely once a day has zero points.** Leaving it at zero would make
`tracked_months` keep returning months with no points, and would no longer match a rebuild.

#### Segment attribution

The distance between two consecutive points is **attributed to the day of the later point**.

Segments whose time gap exceeds `TRAVELMAP_TRACK_BREAK_MINUTES` are **not counted at all**,
so `km` means "distance travelled within tracks". Without this, the straight-line distance
across a tracking gap or a flight lands in the total. Track splitting (Step 13) uses the same
value.

#### Which days to update

Inserting, updating, or deleting a point changes `km` for **that point's day and the day of the
next point in time order**. The previous point's day does not change, because the `prev → P`
distance is already counted on P's day.

The next point can be days away if tracking was stopped, so never hardcode "the following day".
If an update moves a point to a different day, take those same two days for both the before and
after states. Out-of-order and late-arriving batches are handled the same way.

#### How to update

**Rebuild the affected day's row from scratch. Never adjust values arithmetically.**
`countries` / `cities` are sets: when one point is deleted there is no way to tell whether that
country or city still applies to another point that day without scanning the day, so an
arithmetic approach would never shrink them and `/stats` would keep reporting inflated values.

**The rebuild input is that day's points plus the immediately preceding point**, which may be
from an earlier day. The first point of a day measures its segment against the last point of a
previous day, so it cannot be computed from that day's points alone. Missing this drops every
cross-midnight movement from `km`.

The update runs in the same transaction as the mutation that triggered it.

### Configuration affecting stored aggregates

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

### Distance calculation

Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations. The formula
therefore exists twice: share the Earth-radius constant and pin agreement with a test.

### Switching to PostgreSQL / PostGIS

SQLite was chosen after benchmarking; see commit 59d0e04 for the measurements. None of the
following apply today, but if one becomes real, add a PostgreSQL implementation behind the
`internal/store` interface — which is why the store is abstracted.

- Multiple users writing concurrently and continuously (SQLite always has a single writer)
- kNN or complex spatial joins against our own boundary data rather than an external geocoder
- H3 hex aggregation / fog of war (both non-goals)
- The database reaching tens of GB (roughly 100 bytes per point, so 10M points is about 1 GB)

## Dawarich API Compatibility Notes

Upstream quirks that must be checked before implementing.

### Authentication

- Either an `api_key` query parameter **or** an `Authorization: Bearer {api_key}` header.
- The spec lists only one of the two per endpoint (query for points / stats / tracks /
  settings; header for users/me and visits), but **the implementation should accept both on
  every endpoint**. The community Android client sends `Authorization: Bearer` on everything
  including `/api/v1/points`, which the spec documents as query-only — so the spec's per-endpoint
  split does not reflect what clients actually do.

### Per-endpoint

- **`GET /api/v1/health`** — No authentication. The response headers `X-Dawarich-Response`
  (`Hey, I'm alive!` when unauthenticated, `Hey, I'm alive and authenticated!` when
  authenticated) and `X-Dawarich-Version` are **required**. Body is `{"status":"ok"}`.
  Implemented in Step 3, on the **assumption** that the app's server-URL validation goes
  through here (needs confirmation against a real device — but health is needed regardless, so
  being wrong costs no rework).
  **Both headers belong on every `/api/v1` response, not just this one**: upstream sets them in
  a `before_action` on `ApiController`, so a client is free to read the version off any
  response. They are therefore middleware on the whole `/api/v1` group here (Step 3), and Step
  5 made the `X-Dawarich-Response` value authentication-aware. That middleware runs **inside**
  the authentication one, because the header reports the outcome of the key lookup; the one
  response it cannot reach — the 500 the authentication answers itself when the database is
  unreadable — sets both headers directly.
  Note what that means for health: a request **carrying a key** reaches the database, so it is
  answered 500 when the database cannot be read. That is upstream's behaviour too — its
  `set_version_header` resolves `current_api_user` before the controller runs — and it is the
  honest answer, since a server that cannot read its database is not one a client should be
  told is fine. One deliberate difference: a request carrying **no** key is not looked up at
  all here, where upstream queries for a user whose `api_key` is NULL.
- **`POST /api/v1/auth/login`** — Body `{email, password}` → 200 with
  `{user_id, email, api_key, status, plan, subscription_source, active_until}`. With 2FA
  enabled it returns 202 plus a `challenge_token` (this project always returns 200).
  The last four are upstream Cloud's billing fields and get no columns here (see "`users`"
  above). Step 5 answers them with what a **self-hosted upstream instance** ends up reporting,
  so that a client gating a feature on them sees what it would see there: `status` `active`,
  `plan` `pro`, `subscription_source` `none`, and an `active_until` far enough out never to
  have passed — upstream activates a self-hosted user with `active_until: 1000.years.from_now`,
  and this server sends the constant `9999-12-31T23:59:59Z` because it has no subscription to
  expire. Note that it carries **no milliseconds**, unlike the timestamps of `users/me` below:
  upstream renders this one with an explicit `active_until&.iso8601` rather than handing a time
  to the JSON encoder, and the two really do differ.
  **A refused login is the one 401 with a body**: upstream's auth controllers render
  `{"error": "auth_failed", "message": "Invalid email or password"}`, which is also the 401
  the spec documents for this endpoint. Every way of failing gets that same answer — wrong
  password, unknown address, an address that is not one, no password field at all — and an
  unknown address is answered only after a bcrypt comparison against a throwaway digest
  (`auth.CheckAbsentPassword`), so that how long the refusal takes does not say which addresses
  have accounts.
- **`GET /api/v1/users/me`** — **The spec documents no response body for it** ("user found",
  no schema), so the shape was read off upstream's `Api::UserSerializer` instead:
  `{"user": {email, theme, created_at, updated_at, settings: {...}}}`. Three things follow.
  The user object **carries no id** — a client that needs one has the `user_id` of
  `auth/login`. The `subscription` key beside `user` is Cloud-only (`unless self_hosted?`), so
  it is not sent. And `settings` is a **smaller set than `GET /api/v1/settings` answers with**:
  the 18 keys the serializer picks (`maps` among them), in its order. Nothing stores them yet,
  so Step 5 answers upstream's own defaults (`Users::SafeSettings::DEFAULT_VALUES`); `immich_url`,
  `photoprism_url` and `speed_color_scale` are `null`, and the first two stay that way, being a
  non-goal.
  Timestamps are written **RFC 3339 with milliseconds** (`2026-02-03T04:05:06.000Z`), which is
  what upstream's JSON encoder produces (`ActiveSupport::JSON::Encoding.time_precision = 3`) —
  a client parsing with a fixed format string would fail on a value without them.
- **`POST /api/v1/points`** / **`POST /api/v1/overland/batches`** — Both take
  `{"locations": [GeoJSON Feature, ...]}`. Feature `properties` include `timestamp` (ISO 8601),
  `horizontal_accuracy`, `vertical_accuracy`, `altitude`, `speed`, `speed_accuracy`, `course`,
  `course_accuracy`, `battery_state`, `battery_level`, `wifi`, `track_id`, `device_id`.
  **The success status codes differ**: 200 for points, 201 for overland.
- **`GET /api/v1/points`** — `start_at` / `end_at` / `page` / `per_page` / `order`. Must return
  the `X-Current-Page` and `X-Total-Pages` response headers. Body is an array of point objects
  with roughly 30 fields.
- **`GET /api/v1/stats`** — The only endpoint using **camelCase** (`totalDistanceKm`,
  `totalPointsTracked`, `totalReverseGeocodedPoints`, `totalCountriesVisited`,
  `totalCitiesVisited`, `yearlyStats[].monthlyDistanceKm.january`, …). Everything else is
  snake_case, so do not mix them up.
- **`GET /api/v1/points/tracked_months`** — `[{"year": 2024, "months": ["Jan", "Feb", ...]}]`.
  Months are three-letter English abbreviations.
- **`GET/PATCH /api/v1/settings/mobile`** — GET wraps as
  `{settings: {...}, updated_at, status}`. For PATCH the **spec contradicts itself**: the schema
  puts fields at the top level while the example wraps them in `{"settings": {...}}`.
  **Accept both forms** so neither breaks. Assuming only one means that when the other arrives,
  every field is silently ignored — no error — and settings sync quietly breaks.
- **`GET /api/v1/tracks`** — GeoJSON `FeatureCollection` of LineStrings. Properties are `id`,
  `color`, `start_at`, `end_at`, `distance` (metres), `avg_speed` (km/h), `duration` (seconds),
  `dominant_mode`, `dominant_mode_emoji`.
- **`GET /api/v1/timeline`** — `start_at` / `end_at` required, **range capped at 31 days**.
  Response is `{days: [...]}`.
- **Request body wrapping is inconsistent** — `PATCH /api/v1/points/{id}` wraps as
  `{"point": {"latitude": ..., "longitude": ...}}`, while
  `DELETE /api/v1/points/bulk_destroy` takes `{"point_ids": [...]}` at the top level. Check the
  spec per endpoint.

### Responses carry `Content-Type: application/json; charset=utf-8`

Upstream's `render json:` sends the charset, and it is worth copying rather than sending a bare
`application/json`: Dart's `http` package — what the community Android client is built on —
decodes a body whose `Content-Type` names no charset as latin1, so every non-ASCII city or
country name would arrive as mojibake. Fixed in Step 3 for every response, error ones included.

### Error responses are a bare `{"error": "..."}`

Upstream renders `{"error": "<message>"}` and nothing else — no code, no details, no field a
client could match on other than the message. Step 3 fixes that as the error body of every
failing request, `internal/httpapi/dto.Error`.

One exception is already visible in upstream's `ApiController`: a request that fails
authentication is answered with `head :unauthorized`, so a **401 has an empty body**. Step 5
reproduces that rather than sending the error body, since a client parsing the body of a 401 is
parsing nothing.

The other exception is `POST /api/v1/auth/login`, which renders `error` **and** `message`; it
is on that endpoint's bullet above. So a 401 has an empty body everywhere except the one
endpoint whose whole purpose is to tell a client whether its credentials work.

### Unimplemented endpoints must return 404

**Never return an empty array or empty object with 200 for an endpoint we do not implement.**

Dawarich has no version negotiation. Feature detection is done by calling the endpoint and
treating **404 as "this server does not support the feature", at which point the client hides it
entirely** (upstream PR #3067 introduces `/api/v1/demo_data` on exactly this basis). A 200 with
an empty body therefore tells the app the feature *exists*, and it will surface UI that then
misbehaves.

This makes the exclusions in "Endpoints Deliberately Excluded" safe by construction: not
implementing them is the supported way to say we do not have them.

### `GET /api/v1/points` must answer HTTP HEAD

Clients issue a `HEAD /api/v1/points` with the same query parameters first, read
`X-Total-Pages` from the response, and only then fetch the pages. **If `X-Total-Pages` is absent
or 0 the client concludes there is nothing to fetch and stops** — the map silently shows no
points.

chi registers methods explicitly, so a route declared with `r.Get` returns 405 to a HEAD
request. Register HEAD alongside GET and make sure the pagination headers are computed for it.

**This is not specific to `/points`.** Rails answers HEAD on every route it routes GET to, so
every upstream GET endpoint does. Step 3 therefore registers both methods for every GET route
through one helper, and a later handler gets the behaviour without having to remember it.

Verified in the community Android client
(`lib/core/network/repositories/api_point_repository.dart`).

### About the iOS app

`dawarich-app/dawarich-ios` is an **empty repository**; the app is closed source. The endpoints
it actually calls, the required fields, and the call order therefore cannot be determined from
the spec.

Two sources close the gap, in this order:

1. **`sunstep/dawarich-community`** — a community-built Flutter/Dart **Android** client, open
   source. Reading its HTTP layer (`lib/core/network/`) shows which endpoints are called in what
   order and which response fields are actually consumed. Cheaper and more precise than
   observing traffic. Caveats: it is a different client from the official iOS app, and its
   maintainer states updates have stalled and recommends the official app — so treat it as
   evidence about *a* client, not *the* one. The HEAD requirement and the Bearer-everywhere
   behaviour above both came from it.
   Endpoints it uses: `/api/v1/health`, `/api/v1/users/me`, `/api/v1/points`,
   `/api/v1/points/{id}`, `/api/v1/stats`, `/api/v1/countries/visited_cities`.
2. **Request logging against a real device** (Step 6), for anything the above does not settle —
   in particular whatever the official app does that this client does not.

## Endpoints Deliberately Excluded

**The checklists under "Development Steps" are the single source of truth for what gets
implemented.** Keeping a separate list would mean double bookkeeping that drifts out of date,
so this section records only the exclusions and why.

Apart from anything falling under the "Non-goals" categories (Areas, Places, Notes, Tags,
Digests, Insights, Families, Immich, Photos, Maps/hexagons, …), any endpoint in the spec that
is not listed below should appear in the development steps.

- `GET /api/v1/points/{id}` and `GET /api/v1/visits/{id}` **do not exist in the spec** (both
  have only `patch` and `delete`). If device logs show them being called, add them on the basis
  of that observation.
- `POST /api/v1/visits`, `PATCH/DELETE /api/v1/visits/{id}`, `POST /api/v1/visits/merge`,
  `POST /api/v1/visits/bulk_update` — Visit editing. Out of scope for now, since the goal is
  browsing only.
- `GET /api/v1/plan`, `POST /api/v1/subscriptions/callback` — Billing/subscription (non-goal).
- `POST /api/v1/users/exist` — Internal endpoint for upstream Cloud's Subscription Manager
  (non-goal).
- `GET /api/v1/demo_data`, `POST /api/v1/demo_data`, `DELETE /api/v1/demo_data` — Upstream
  Cloud demo data (non-goal).
- `POST /api/v1/points/reapply_anomaly_filter` and
  `GET /api/v1/settings/transportation_recalculation_status` — Anomaly filtering and transport
  mode inference. **Neither feature is implemented at all**, so there is nothing to trigger or
  report progress on (tracks return `null` for `dominant_mode`).
- `POST /api/v1/recalculations` — Rebuilding `daily_stats` is needed, but it is **done via the
  CLI (`travelmap recalculate`) and not exposed as an API**. On a self-hosted instance a rebuild
  is only needed after an import, on inconsistency, or when `TRAVELMAP_TIMEZONE` or
  `TRAVELMAP_TRACK_BREAK_MINUTES` changes — all of which the operator can run locally. Revisit
  if triggering it from the app turns out to be necessary.
- `GET /api/v1/countries/borders` — GeoJSON country border polygons (serving several MB of
  static data). Border rendering is expected to be handled by the map tiles, so it will not be
  served even by the Milestone H web UI. Revisit once the web UI's rendering approach is settled.
  Only `countries/visited_cities` is covered, in Milestone G.
- `GET /api/v1/locations`, `GET /api/v1/locations/suggestions`, `GET /api/v1/residency` — Place
  search and stay analysis. Out of scope as part of the Places family (non-goal).
- `POST /api/v1/auth/otp_challenge`, `GET/POST/DELETE /api/v1/users/me/two_factor`,
  `POST /api/v1/users/me/two_factor/setup`, `POST /api/v1/users/me/two_factor/confirm`,
  `GET /api/v1/users/me/two_factor/backup_codes` — 2FA. Not supported, given the self-hosted
  assumption (`POST /api/v1/auth/login` never returns 202; it always returns 200 with an
  `api_key`).
- `POST /api/v1/auth/apple`, `POST /api/v1/auth/google` — Social login. **A risk that could
  block Milestone B from completing**; see "Risks and Open Questions".

## Development Environment

### Language and toolchain

**Use `go 1.26.6` in `go.mod`** (the current stable release; the floor is 1.25, required by
`modernc.org/sqlite` v1.56.0).

**The directive names a patch version, not just `1.26`.** `actions/setup-go` takes the version
from `go-version-file: go.mod` and runs with `GOTOOLCHAIN=local`, so this line alone decides
which toolchain CI builds with, and `GOTOOLCHAIN=auto` fetches the same one locally. Since
`govulncheck` reports a standard-library advisory against the toolchain the code would be built
with, a stdlib advisory is fixed by **raising this line** — and nothing does it automatically,
because Dependabot's `gomod` ecosystem updates requirements and not the `go` directive. The
`vulncheck` job going red with `Standard library` in the output is that signal.

**Declare the development tools with `tool` directives in `go.mod`**, not as separately
installed binaries:

```
tool (
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	golang.org/x/vuln/cmd/govulncheck
	mvdan.cc/gofumpt
)
```

added with `go get -tool <path>@<version>` (one `-tool` per invocation) and run as
`go tool golangci-lint run`.

This matters for more than convenience. `golangci-lint` refuses to analyse a module whose `go`
directive is newer than the Go version the linter binary was built with:

```
can't load config: the Go language version (go1.25) used to build golangci-lint
is lower than the targeted Go version (1.26.0)
```

A pre-installed binary therefore caps the `go` directive at whatever it happens to be built
with — in the development container, an old 2.5.0 that caps it at 1.25. **`go tool` removes the
ceiling by construction**: the linter is rebuilt from source with the module's own toolchain, so
it always matches. Versions are pinned in `go.mod`, and CI and local runs use the same build.

Measured in the container: `go tool golangci-lint run` takes about 43 s the first time (it
compiles the linter) and about 0.5 s afterwards. `gofumpt` runs in about 1 s. The installed Go
does not have to match the directive — 1.24.7 with `GOTOOLCHAIN=auto` builds, tests with
`-race` and vets a `go 1.26` module fine, fetching the toolchain once. So there is no
environment prerequisite before starting, and no setup script to maintain.

Three caveats:

- **`go get -tool` pulls the tools' entire dependency trees into `go.mod` as `// indirect`** —
  214 entries for the three above, measured in Step 1. If `go.mod` becomes unreadable because of
  them, move the tools into a separate `tools/go.mod`.
- **The tool modules themselves are recorded as `// indirect` too**, because no package in this
  module imports them — `go mod tidy` restores the marker if it is removed by hand. Dependabot's
  scheduled `gomod` updates cover direct dependencies only, so it never offers the tools, and the
  one setting that would reach them, `allow: dependency-type: "all"`, reaches the 211 modules they
  are built from as well. **`.github/workflows/go-tools.yml` does it instead**: a weekly
  `go get -u tool` and `go mod tidy`, opened as a pull request carrying the `auto-merge` label, so
  a golangci-lint release that finds something new turns `lint` red and waits for a human rather
  than merging. The `tool` meta-pattern expands to the tools declared in `go.mod`, so no module
  path is written in the workflow; `-u` stops at the latest minor or patch, and a new major, being
  a different module path, is adopted by hand. `.github/dependabot.yml` therefore keeps the default
  scope, which is also what makes a chi or SQLite-driver bump arrive as its own pull request.
- **`govulncheck` cannot run in the development container**: the egress proxy blocks
  `vuln.go.dev` (`Forbidden`). Treat it as a CI-only check.

### Libraries

Keep dependencies minimal.

| Purpose | Package | Notes |
| --- | --- | --- |
| Routing | `github.com/go-chi/chi/v5` | Rationale in "Library Choices for the Web UI" |
| SQLite driver | `modernc.org/sqlite` | Pure Go. No CGO. Requires Go 1.25+ |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Migrations | `github.com/pressly/goose/v3` | Used as a library, not a CLI: the provider API takes the `embed.FS` and the `*sql.DB` this server already has, so the migrations stay inside the binary. Chosen in Step 4 after writing both — see the Migrations row under "Technical Decisions" for the measurement. `.sql` files carry goose's `-- +goose Up` annotation, and **a comment inside one cannot name an annotation**, because goose reads any comment that does as the annotation itself |
| Test diffs | `github.com/google/go-cmp` | |
| Configuration | No extra dependency (a thin hand-written env-var loader) | |
| Logging | Standard `log/slog` | |

### Library Choices for the Web UI

Since a web UI is planned, make choices now that will not need to be redone then.
**The only thing that must be decided now is the router**; UI libraries are not added until
Milestone H.

**Why chi rather than the standard `net/http.ServeMux`**

Since Go 1.22 `ServeMux` supports methods and wildcards, so the standard library is enough for
an API server on its own. But once a web UI is added there are **two authentication schemes**:

- `/api/v1/*` — `api_key` query / Bearer token (mobile app)
- `/*` — Cookie session + CSRF (browser)

`ServeMux` has **no mechanism for attaching a different middleware chain per prefix**, so that
would have to be hand-written. chi's `Route` / `Group` exist for exactly this, and since
everything is a plain `http.Handler`, nothing breaks when more is added later. chi v5.3.1 has no
`require` entries at all in its `go.mod`, so **it adds no dependencies** (verified).

```go
r := chi.NewRouter()
r.Route("/api/v1", func(r chi.Router) {
    r.Use(auth.APIKey)      // Bearer / api_key
    ...
})
r.Group(func(r chi.Router) {
    r.Use(session.Load, csrf.Protect)  // browser
    r.Handle("/*", webui.Handler())
})
```

**Candidates for Milestone H (not added now)**

| Purpose | Candidate | Notes |
| --- | --- | --- |
| Sessions | `github.com/alexedwards/scs/v2` | Has a SQLite store; better maintained than `gorilla/sessions` |
| CSRF | Standard `net/http.CrossOriginProtection` | **Landed in the standard library in Go 1.25** (`Sec-Fetch-Site` based). Confirm on starting whether an external dependency can be avoided |
| Templates | `github.com/a-h/templ` or standard `html/template` | templ is type-safe but adds a code-generation step |
| Page updates | htmx | Avoids pulling in a Node build chain |
| Map rendering | MapLibre GL JS or Leaflet | The one place JS is unavoidable. Vendor it into `embed.FS` rather than using a CDN, to keep a single binary |

An SPA (React, etc.) can also keep the single-binary property by embedding the build output in
`embed.FS`. The only trade-off there is needing a Node build chain, and **the router choice is
the same either way**.

**Open question: how the browser authenticates against `/api/v1`**

Having decided not to add UI-only data endpoints, the browser will also call `/api/v1/points`
and friends — but in the layout above `/api/v1` accepts only Bearer / `api_key`, with the
session cookie on the `/*` side. To be decided when Milestone H starts. The current front-runner is
**(a)**.

- **(a) The `/api/v1` middleware also accepts the session cookie** — lets the UI simply fetch.
  Accepting cookies means `/api/v1` needs CSRF protection too, but Go 1.25's
  `CrossOriginProtection` can be applied server-wide, so the added cost is small.
- (b) The UI calls handlers/store directly in-process — keeps the authentication split, but
  changes "reuse the API" from reusing the HTTP API to reusing the implementation.
- (c) Hand the api_key to the UI at login and call with Bearer — not recommended, since XSS
  would leak the API key.

### Development tools

- `golangci-lint` — keep its standard set (errcheck, govet, ineffassign, staticcheck, unused) and
  add revive, gosec, bodyclose, sqlclosecheck in `.golangci.yml`. Starting from the standard set
  rather than listing every linter means new entries golangci-lint promotes into it arrive on
  their own.
- `gofumpt` — formatting
- `govulncheck` — vulnerability scanning
- `go test ./... -race -cover -shuffle=on` (`-shuffle=on` catches tests that only pass in the
  order they are written)
- `go mod tidy -diff` in `make lint`, so a `go.mod` / `go.sum` left untidy fails CI instead of
  turning up inside someone else's diff

### Makefile

Provide `build` / `test` / `lint` / `fmt` / `check` / `run` / `migrate` targets, where `check` is
`lint` plus `test` — the set that can run locally, which the pre-commit hook runs.
A `docker` target comes with the packaging work in Milestone G.

### CI

Add `.github/workflows/go.yml` with `build` / `test` / `lint` / `govulncheck` jobs.

**Follow the conventions of the existing workflows** (see
`.github/workflows/workflow-lint.yml`); they are listed in [CLAUDE.md](CLAUDE.md).

Also add the `gomod` ecosystem to `.github/dependabot.yml`, on the same weekly / `Asia/Tokyo` /
7-day-cooldown schedule as the existing `github-actions` entry, and
`.github/workflows/go-tools.yml` for the development tools Dependabot's scope does not reach (both
are explained in "Language and toolchain").

### Distribution

A `CGO_ENABLED=0` binary has no runtime dependencies, so there is nothing for a container to
isolate — a `distroless/static` image is the binary plus a CA bundle. The reason to ship one is
the audience, not the technology: upstream Dawarich is distributed as docker-compose, the server
has to run continuously somewhere (NAS, VPS, home server), and NAS platforms such as Synology,
unRAID and TrueNAS are container-first. A systemd unit serves the same purpose on a plain host,
so document both and treat neither as the foundation.

This is packaging, not infrastructure. It belongs in Milestone G, not the foundation.

- Multi-stage `Dockerfile`: build with `CGO_ENABLED=0 go build -ldflags="-s -w"` and place the
  binary on `gcr.io/distroless/static`. **Add `-X main.version=<release>` to those ldflags**:
  the build stage copies sources without `.git`, so Go stamps no VCS information and
  `travelmap --version` would report `unknown` on exactly the builds people run
- `docker-compose.yml` with just the server container and a volume for SQLite. Note the SQLite
  file's ownership: the container runs as a non-root user, so a bind-mounted directory has to be
  writable by that UID
- An example systemd unit for hosts not running containers

### Layering and testing approach

Both are conventions rather than plan, so they live in [CLAUDE.md](CLAUDE.md): which package
may import which, and the testing approach down to golden files being the compatibility
contract. What belongs in a package is its own `doc.go`.

## Development Steps

**One step = one PR.** Steps are deliberately uneven in size.

Early steps are small because each one settles a convention that everything after it inherits:
the first handler fixes the error response shape, the first store fixes how migrations and
transactions work. Bundling those means arguing five conventions in one diff, and the review
stops being about the code. So the early steps carry little code and are listed with **what
they settle** alongside what they build — that is the part worth the reviewer's attention.

Later steps are mostly applying patterns that already exist, so they get bigger, and several
can run in parallel. Parallelism is noted where it applies.

Milestones group the steps and state a user-visible outcome. Individual steps have a completion
condition that can be verified by running something.

---

## Milestone A — Foundation

No application behaviour yet. Three small PRs, each settling conventions.

### Step 1: Toolchain and CI

- [x] `go.mod` with `go 1.26.6` and `tool` directives for golangci-lint, govulncheck and gofumpt
      (see "Language and toolchain"), and the package skeleton (empty packages with a doc
      comment each)
- [x] `Makefile` (targets invoke `go tool <name>`, so a checkout needs no tool installation),
      `.gitignore` (Go plus `*.db`, `bin/`), `.golangci.yml`
- [x] `.github/workflows/go.yml` (build / test / lint / govulncheck, all via `go tool`).
      `govulncheck` runs only here — it cannot reach `vuln.go.dev` from the development
      container
- [x] Add `gomod` to `.github/dependabot.yml`, and `.github/workflows/go-tools.yml` for the
      development tools its scope does not reach

**Settles**: directory layout, the enabled linter set, CI conventions.

**Done when**: CI is green.

### Step 2: Project conventions

No code. Separated so that the convention discussion does not ride along with a code diff.

- [x] `CLAUDE.md`: **English as the project language**, layering rules (what may import what),
      testing approach, where documents live, commit conventions
- [x] Expand `README.md` (currently one line): what the project is, how to build and run it

**Settles**: everything a reviewer would otherwise re-litigate in each later PR.

**Done when**: `CLAUDE.md` states the layering rules and the conventions a later pull request
would otherwise re-argue, without restating what each package's `doc.go` already says — adding
a package must not mean editing `CLAUDE.md` too.

### Step 3: Server skeleton and `GET /api/v1/health`

The smallest possible full-stack slice: no database, no authentication. Chosen first precisely
because it carries almost no logic, so the review is entirely about the shapes that every later
handler will copy.

- [x] `internal/config` env-var loader (`TRAVELMAP_ADDR`, `TRAVELMAP_LOG_LEVEL`; read through
      an injected `getenv` so tests never touch the process environment), `log/slog` setup,
      chi router, graceful shutdown on SIGINT/SIGTERM
- [x] `cmd/travelmap serve` and `--version`
- [x] JSON response and error helpers
- [x] `GET /api/v1/health` with the `X-Dawarich-Response` and `X-Dawarich-Version` headers
      (the authenticated variant of the header comes in Step 5)
- [x] `httptest` + golden-file test helper

**Settles**: handler signature, JSON and error response shape, config loading, the
golden-file testing pattern.

**Done when**: `curl` returns both headers and `{"status":"ok"}`, pinned by a golden test.

---

## Milestone B — The app connects

### Step 4: Store foundation and users

No HTTP. Splitting the store from the handlers that use it keeps the migration and repository
discussion separate from the API discussion.

- [x] SQLite open with WAL and pragmas, embedded migrations run through goose. The file comes
      from `TRAVELMAP_DATABASE` (default `travelmap.db`), added to `internal/config` here
- [x] The store exposes no schema version. `Migrate` reports whether it applied anything and
      `Migrated` whether the schema is there at all — the version number had no caller but the
      CLI's own output, and what `travelmap migrate` owes the operator is whether the database is
      at the current schema, not which files went by
- [x] `users` table, repository interface and its SQLite implementation
- [x] API key generation and bcrypt password hashing (`internal/auth`)
- [x] `travelmap user create --email --password`. `--password` may be left out, in which case the
      first line of standard input is read instead, for a script or a systemd unit that redirects
      a file. Neither is good: `argv` is readable by every user on the host through `ps`, and a
      bare standard-input read has no prompt, so at a terminal the command waits in silence. The
      documented procedure therefore uses `--password` for now, and the echo-off prompt that
      replaces both is its own step in Milestone G
- [x] `travelmap migrate`. Neither `serve` nor `user create` migrates implicitly: opening a
      SQLite database creates the file, so migrating on the way up would turn a typo in
      `TRAVELMAP_DATABASE` into a working server holding none of the user's history. `user create`
      reports an unmigrated database and names the command to run
- [x] Temp-database test helper (`newTestDB`, package-internal to `internal/store/sqlite`; promote
      it if another package ever needs a real database rather than a substituted store)
- [x] One file per command under `cmd/travelmap` (`serve.go`, `migrate.go`, `user.go`), leaving
      `main.go` with the argument handling and the dispatch alone

**Settles**: migration mechanism, repository interface style, hand-written SQL versus generated,
transaction handling, how store tests get a database.

**Done when**: `travelmap user create` issues a user and an API key, and running migrations
twice is a no-op.

### Step 5: Authentication

- [x] Auth middleware accepting both the `api_key` query parameter and `Authorization: Bearer`
- [x] `POST /api/v1/auth/login`
- [x] `GET /api/v1/users/me`
- [x] `GET /api/v1/health` becomes auth-aware (`Hey, I'm alive and authenticated!`)

**Settles**: how handlers reach the authenticated user, the 401 body shape.

The middleware resolves the credentials a request carries and **refuses nothing**: the user
goes on the request context, and a second middleware on the authenticated routes turns "no
user" into the empty-bodied 401. That split is what lets `/health` answer 200 either way and
report which it was, and what keeps a key that names no user from being a server error. A
route registered outside that group serves one account's data to whoever asks, so the group —
not the handler — is where a new endpoint is added.

`serve` now opens the database, and refuses to start against an unmigrated one with the same
message `user create` gives. Opening a SQLite file creates it, so without the check a typo in
`TRAVELMAP_DATABASE` would come up as a healthy server answering every request with an error
about a missing table.

**Done when**: **the iPhone app reports a successful connection** after entering the server URL
and API key. **Not yet confirmed**: no device has been pointed at this server. The endpoints
are covered by tests against the router, and Step 6's request log is how the remaining half of
this condition gets checked — including whether the app insists on `auth/apple` (see "Risks and
Open Questions").

### Step 6: Request logging for endpoint discovery

Small, but its output is a planning input for everything after: the iOS app is closed source,
so this is how the remaining endpoint list gets confirmed.

- [x] Request-logging middleware behind `TRAVELMAP_DEBUG_LOG_REQUESTS=1`, logging unmatched
      routes too. Everything not yet implemented keeps returning 404, which is both the correct
      answer (see "Unimplemented endpoints must return 404") and what makes the log a complete
      list of what the app wants
- [x] **Redact `api_key` and `Authorization` before logging.** The whole point is to capture
      real device traffic, which carries live credentials
- [ ] Record the endpoints a real device actually hits in this file, and diff them against the
      list the community Android client uses (see "About the iOS app")

**Settles**: what may and may not appear in logs.

The middleware is the router's first, and chi runs its middleware chain before it matches a
route — which is what puts the requests that matched no route in the log, and what makes a line
report the status the client was actually answered with: the 404 for an unknown route, the 500
the recovery writes for a panic. It logs at Info rather than Debug, because
`TRAVELMAP_DEBUG_LOG_REQUESTS` is meant to be the only switch — and for the same reason turning
it on holds `TRAVELMAP_LOG_LEVEL` down to Info, so that a level set higher cannot swallow the
capture without saying so.

Redaction is by name, not by value, and errs towards redacting: a name containing any of the
words in `sensitiveWords` in `internal/httpapi/requestlog.go` — which is where the list lives,
so that adding one is a single-file change — plus `Cookie`, whose name says nothing about what
it carries. Parameters and headers are judged by the same rule because the client is closed
source: it may carry a credential under a name nobody here predicted, and a name that cannot be
predicted cannot be enumerated. Only values are replaced — which parameters and headers the
client sends is the finding, so the names stay. Every other header is logged in full, because
a shortlist of the interesting ones could only be written by someone who already knew what the
app does. **Request bodies are never logged at any level**:
`POST /api/v1/auth/login` carries a password in its body, and a body cannot be redacted
without parsing it as whatever the endpoint takes.

**Done when**: connecting the app produces a log of every route it calls, with no credentials in
it. **Not yet done**: the middleware and its redaction are implemented and tested, but no device
has been pointed at this server, so the capture below is empty. The rest of the plan runs on the
community Android client's endpoint list until it is filled in.

#### Endpoints a real device hits

Nothing recorded yet. Fill this in from one capture session — start the server with
`TRAVELMAP_DEBUG_LOG_REQUESTS=1`, add it in the app, let it record and browse — and list the
method, the path and the query parameters of every line, including the 404s. Diff that against
the six endpoints under "About the iOS app": what is here and not there is what the official
app needs and the community client does not, and it is the input to Milestone F's ordering.

---

## Milestone C — Locations are recorded

### Step 7: Points ingest

Deliberately excludes `daily_stats`: `/stats` is not needed until Milestone E, and mixing
aggregation into this PR is what made the original plan's second stage unreviewable.

- [ ] `points` schema and its `(user_id, timestamp)` index, per "Data Model"
- [ ] GeoJSON Feature parser (`internal/httpapi/dto`)
- [ ] `POST /api/v1/points`
- [ ] `POST /api/v1/overland/batches` (note the different success status code)
- [ ] Deduplication (same user × timestamp)
- [ ] Batch inserts wrapped in a transaction
- [ ] Check `maxRequestBody` in `internal/httpapi` (1 MiB as of Step 5) against a full batch —
      the mobile settings allow a `batch_size` of up to 1000 points, and a body over the limit
      is answered `400 invalid request body`, which reads like malformed JSON rather than like
      a body that was too large

**Settles**: how the wide Dawarich JSON shapes are modelled and pinned with golden files.

**Done when**: starting tracking in the app puts real device locations into the database and the
point count grows.

---

## Milestone D — Aggregation

Two PRs. The dense-logic part of the project; splitting it is what makes the second PR's
consistency test meaningful, because the first PR provides the reference implementation to
compare against.

### Step 8: `daily_stats` and full rebuild

No HTTP. Write the *full* rebuild first, as the definition of correct.

- [ ] `daily_stats` table, per "Data Model"
- [ ] Rebuild-a-day function (that day's points plus the immediately preceding point)
- [ ] `GET /api/v1/users/me` reports the configured zone in `settings.timezone`, which Step 5
      answers with the constant `UTC`
- [ ] `TRAVELMAP_TIMEZONE` and `TRAVELMAP_TRACK_BREAK_MINUTES` in `internal/config`.
      The README already documents both and says that **changing either requires
      `travelmap recalculate`**; check that what it says still matches what was built
- [ ] `travelmap recalculate` (rebuilds `daily_stats` from points; for recovery after imports or
      inconsistency, and after either variable above changes)
- [ ] A test that the Haversine in `internal/geo` and the Haversine in SQL agree on the same
      input (pass the Earth-radius constant from Go into SQL; do not put the literal in two
      places)
- [ ] **A test with `TRAVELMAP_TIMEZONE=Asia/Tokyo`.** Every other case passes under the default
      `UTC`, so forgetting the timezone conversion would go undetected. Verify that a point at a
      time which falls on the previous day in UTC (e.g. 00:30 JST) is counted on the current
      day's row
- [ ] A boundary test for `TRAVELMAP_TRACK_BREAK_MINUTES`: a segment of exactly 30 minutes
      **is counted** (catches a `>` versus `>=` mix-up)
- [ ] **A test pinning the expected value of a cross-midnight segment distance.** Agreement
      testing in Step 9 cannot catch this: if both paths drop the segment they still agree.
      Verify that the distance between the previous day's last point and the current day's first
      point appears in `km`

**Done when**: `travelmap recalculate` produces correct `daily_stats` for a seeded database, with
the above pinned by tests.

### Step 9: Ingest layer and incremental update

- [ ] **Route every path that changes points through a single `internal/ingest` layer**, and
      move Step 7's handlers onto it. Scattered `daily_stats` updates are guaranteed to miss
      cases — Milestone E's updates and deletes, and Milestone G's imports, owntracks/traccar
      and reverse-geocoding worker all go through this layer
- [ ] Update the affected days in the same transaction as the mutation, per "Data Model"
- [ ] A test that the incremental update and `recalculate` agree. For the same set of points, the
      `daily_stats` built up by per-ingest updates must equal the one produced by a full rebuild.
      Cover: the day boundary; out-of-order and late-arriving batches; **points separated by
      several days** (either side of a period with tracking stopped — also confirming segments
      over `TRAVELMAP_TRACK_BREAK_MINUTES` are not counted); **a day whose points are all deleted
      so the row is removed**; and **deleting or updating only some of a day's points so that
      `countries` / `cities` shrink**

**Done when**: inserting points updates the corresponding `daily_stats` day, and running
`travelmap recalculate` afterwards produces identical values.

---

## Milestone E — Browsing

Steps 10 and 11 are independent of each other and can run in parallel. Step 12 needs Step 9.

### Step 10: `GET /api/v1/points`

- [ ] Time filter, pagination, `X-Current-Page` / `X-Total-Pages` headers
- [ ] **Answer `HEAD` on the same route with the same headers.** Clients probe with HEAD to get
      the page count before fetching, and treat a missing or zero `X-Total-Pages` as "no data"
      (see "`GET /api/v1/points` must answer HTTP HEAD")

**Done when**: past points appear on the app's map.

### Step 11: Statistics

- [ ] `GET /api/v1/points/tracked_months` (read from `daily_stats`)
- [ ] `GET /api/v1/stats` (aggregate `daily_stats`; keep camelCase exactly).
      **Do not aggregate points directly** (see "Data Model")
- [ ] Compare the distance against the app's own display and revisit the
      `TRAVELMAP_TRACK_BREAK_MINUTES` rule if they diverge (see "Data Model")

**Done when**: the stats screen shows distance and point counts.

### Step 12: Point mutation endpoints

- [ ] `PATCH /api/v1/points/{id}` (body wrapped as `{"point": {...}}`)
- [ ] `DELETE /api/v1/points/{id}`
- [ ] `DELETE /api/v1/points/bulk_destroy` (body `{"point_ids": [...]}`)
- [ ] All three go through `internal/ingest`, so the affected days' `daily_stats` are
      recalculated in the same transaction. Never leave a state where points were deleted but
      `/stats` keeps reporting the old distance

**Done when**: deleting a point is reflected in `/stats` immediately.

> **Completing Milestone E satisfies the project's requirement: recording and browsing location
> history from the iPhone app.** Everything after this makes the app's remaining screens work,
> or is operational.

---

## Milestone F — The app's remaining screens

Steps 13, 14 and 16 are independent and can run in parallel. Step 15 needs 13 and 14.

Step 16 in fact only needs authentication (Step 5), so it can be pulled forward at any time —
useful as filler work while Milestone D is under review.

### Step 13: Tracks

- [ ] Track-splitting logic (split on `TRAVELMAP_TRACK_BREAK_MINUTES` of inactivity) as a
      background job. **Not `track_break` from `settings/mobile`** (see "Data Model")
- [ ] `GET /api/v1/tracks` (GeoJSON FeatureCollection)
- [ ] `GET /api/v1/tracks/{id}`, `GET /api/v1/tracks/{track_id}/points`

### Step 14: Visits

- [ ] Stay detection → `visits` table
- [ ] `GET /api/v1/visits`

### Step 15: Timeline

- [ ] `GET /api/v1/timeline` (including validation of the 31-day cap)

### Step 16: Settings sync

- [ ] `GET/PATCH /api/v1/settings/mobile` (12 fields with range validation; PATCH accepts both
      the top-level and `settings`-wrapped forms)
  - `tracking_mode` (precise|significant), `tracking_visits`, `track_visits_independently`,
    `auto_start`, `distance_filter` (1–10000 m), `time_filter` (1–3600 s),
    `track_break` (1–1440 min), `accuracy` (1–6), `show_background_location_indicator`,
    `upload_automatically`, `upload_all_on_tracking_stop`, `batch_size` (1–1000)
- [ ] `GET/PATCH /api/v1/settings`. Note that the settings block inside
      `GET /api/v1/users/me` is a **different, smaller set** than this endpoint answers with
      (see its bullet under "Per-endpoint"); both are fed from whatever this step stores, and
      the Step 5 constants in `internal/httpapi` go away with it

**Milestone done when**: the app's timeline and track screens render without breaking, and
settings changed in the app survive a reinstall.

---

## Milestone G — Operations and extensions (optional)

All independent of each other; take them in whatever order the need arises.

- [ ] `POST /api/v1/imports`, `GET /api/v1/imports`, `GET /api/v1/imports/{id}`
      (GPX / GeoJSON / Google Takeout / upstream Dawarich export).
      **Imports go through the `internal/ingest` layer from Step 9**
- [ ] Reverse geocoding (Nominatim / Photon, rate-limited worker). It runs asynchronously after
      points are inserted, so **on completion update `countries` / `cities` /
      `reverse_geocoded_points` in `daily_stats` for the affected days** (without this the
      corresponding `/stats` values stay 0 — see "Data Model")
- [ ] `POST /api/v1/auth/register` (enabled by an env var)
- [ ] `POST /api/v1/owntracks/points`, `POST /api/v1/traccar/points`
- [ ] `GET /api/v1/countries/visited_cities`
- [ ] `Dockerfile`, `docker-compose.yml`, and an example systemd unit (see "Distribution")
- [ ] Backups (`VACUUM INTO`)
- [ ] `GET /metrics`, structured access logs
- [ ] **An echo-off password prompt for `travelmap user create`** (`golang.org/x/term`, whose only
      requirement `golang.org/x/sys` is already an indirect dependency — so it brings no dependency
      fan-out, adding its own two lines to `go.sum` and a `require` line and nothing else;
      measured). Ask twice, since nothing verifies the password afterwards: the API key the command
      prints works whatever the password is, so a typo surfaces later as a login that fails for an
      account that has to be created again. Prompts go to stderr, leaving stdout to the API key a
      setup script reads; standard input keeps its current meaning when there is no terminal.
      **Rewrite `user create`'s own `--help` text with it**, which recommends standard input today.
      This is what lets the README stop documenting `--password`, which puts the password in `ps`
      output

---

## Milestone H — Web UI

Start once the API has settled. Library candidates and rationale are in
"Library Choices for the Web UI".

- [ ] Cookie sessions + CSRF. How the browser authenticates against `/api/v1` follows the
      conclusion of "Open question: how the browser authenticates against `/api/v1`"
      (front-runner is (a))
- [ ] A login screen that accepts an account created with `travelmap user create`
- [ ] Map screen (render points / tracks for a selected time range), reusing the existing
      `GET /api/v1/points` and `/tracks` without adding UI-only APIs
- [ ] Statistics screen (using `daily_stats`)
- [ ] Settings screen. An import screen only if Milestone G's `/api/v1/imports` was implemented
- [ ] Embed static assets in `embed.FS` to preserve the single binary

**Done when**: logging in from a browser shows the user's history on a map, and deployment is
still one binary plus one SQLite file.

## Risks and Open Questions

- **Because the iOS app is closed source, the endpoints it actually calls and the required
  fields are unknown.** Use the Step 6 request-logging middleware to observe traffic from a
  real device and identify unimplemented endpoints.
- **If social login is mandatory, Milestone B may not be completable.** The spec has
  `POST /api/v1/auth/apple` (body `{id_token, nonce}`) and `POST /api/v1/auth/google`. If the
  iOS app forces Sign in with Apple, `POST /api/v1/auth/login` alone will not get to a
  successful connection. **Still open after Step 5**: that step built email-and-password login
  and the API-key middleware, but answering this needs a real device, so it is the first thing
  to read out of Step 6's request log. If it turns out to be required, add verification of
  `id_token` against Apple's public keys and association with an existing user (whether to
  auto-create users on a self-hosted instance is a separate decision).
- The app checks a minimum server version. The community Android client reads
  `x-dawarich-version` and refuses to run against a server below its floor; the official app is
  assumed to do something similar, so confirm the accepted value against a real device.
  Step 3 reports **`1.12.2`**, upstream's `.app_version` on `master` as of 2026-08-18. It is a
  compatibility claim, not this server's own version — `travelmap --version` reports the build
  — so it is raised when this server is verified against a newer upstream, not on every
  release of ours.
