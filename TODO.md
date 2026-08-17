# travelmap — Development Plan for a Dawarich-Compatible API Server

## Goal

Implement a [Dawarich](https://github.com/Freika/dawarich)-compatible web API server in Go.

Upstream Dawarich is a multi-container stack of Rails + PostgreSQL/PostGIS + Sidekiq + Redis,
which is a large runtime footprint for personal use. This project aims for a lightweight
compatible server that runs as **a single statically linked binary plus one SQLite file**.

**Final goal**: point the Dawarich iPhone app at this server and be able to record and
browse location history.

After that, we plan to **build our own web UI** (Stage 7). It will not be a port of the
upstream browser screens; it will be built on top of this server's API. The API comes first,
and the policy is to **reuse the existing `/api/v1` rather than adding UI-only data
endpoints** (browser-specific routes such as login and sessions are added in Stage 7).

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
| Data store | SQLite (`modernc.org/sqlite`, no CGO) | Self-contained in a single static binary. No DB process, so the footprint is minimal. Performance measured as sufficient (see "Data Store Verification") |
| Store abstraction | Repository layer behind an interface | Leaves room to add a PostgreSQL implementation later |
| Indexes | A single `points(user_id, timestamp)` | App queries always narrow by user and time range first. No lat/lon index for now, since no in-scope endpoint takes a bounding box (same section) |
| Distance | Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations | Pulling a whole-history aggregation into Go spends all its time transferring rows. SQL math functions confirmed available by measurement. **Since the formula then exists twice, share the Earth-radius constant and pin agreement between the two with a test** |
| Statistics | `daily_stats` precomputed table, updated during ingest | On-demand computation is not viable (same section) |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Chosen with the future web UI in mind (see "Library Choices for the Web UI") |
| Compatibility scope | Mobile-app subset | Everything except the non-goals above |
| User management | Issued via CLI. `auth/login` implemented, `auth/register` optional behind an env var, no 2FA | Self-hosted assumption |
| Background work | Goroutines + a job table in SQLite | Keeps a single process, with no Sidekiq/Redis equivalent |
| Reverse geocoding | Off by default; optionally point at a Nominatim/Photon URL | Does not make an external service mandatory |

These are the defaults as of planning. If any turns out to be wrong during implementation,
change it — after updating this file.

## Data Store Verification (Is SQLite Enough?)

Measured (2026-08-17) to answer whether SQLite can carry location data or whether PostGIS is
required. Environment: `modernc.org/sqlite` v1.56.0 / SQLite 3.53.3, **2,000,000 points**
(one user, roughly five years), WAL.

These numbers are a snapshot taken at planning time; no reproduction script was kept
(if a major driver or Go update makes it worth revisiting the decision, re-measure under the
conditions described in this section).

### Available features

| Feature | Available | Notes |
| --- | --- | --- |
| R\*Tree | Yes | Virtual table for 2D bbox search |
| geopoly | Yes | Polygon containment; usable for country attribution from border data |
| `sin` / `cos` / `asin` / `radians` / `pow` | Yes | **Haversine can be written in SQL** (verified: Tokyo–Osaka = 402.8 km) |
| Window function `lag()` | Yes | Required for distance between consecutive points |
| WAL / `busy_timeout` | Yes | Read concurrency |

R\*Tree and geopoly ship with the pure-Go driver, so neither CGO nor SpatiaLite is needed.

### Measurements (2M points / 209 MB DB = 104 bytes per point)

All measured with both `(user_id, timestamp)` and `(user_id, latitude, longitude)` present
(the latter is not created in production — see conclusion 3).

| Query | Naive | After |
| --- | --- | --- |
| `GET /points`, first page of one day (`per_page=100`) | **0.3 ms** | — |
| `GET /points`, one month (32,401 rows) | **3.6 ms** | — |
| bbox search (186,272 rows matched) | **173 ms** | 51 ms with R\*Tree (not created in production, per conclusion 3) |
| `GET /points/tracked_months` | 1,448 ms | **0.64 ms** |
| `GET /stats`, whole-history distance | 11,183 ms | **2.29 ms** |
| `GET /stats`, per-year aggregation | 2,745 ms | **2.29 ms** (included above) |
| 100-point batch insert + stats update | — | **5.74 ms** |
| Full `daily_stats` rebuild (`recalculate`) | — | **15.6 s** |

### Conclusions

1. **The queries the app issues most (points over a time range) run in 0.3–4 ms; SQLite is
   entirely fine.** Location history is fundamentally a *time-series* workload: it narrows by
   `user_id` and time range first. What remains after that is about 1,100 rows for a day and
   about 30,000 even for a month, so a spatial index has nothing to contribute anyway.

2. **What was slow was not the spatial queries but the whole-history aggregation.** That is a
   design problem, not a database-engine problem, and it would be just as slow on PostgreSQL.
   This is presumably why upstream Dawarich keeps a `stats` table and exposes
   `POST /api/v1/recalculations`.

   → **Keep a `daily_stats` table and update the affected days in the same transaction as
   ingest.** This brings `/stats` to 2.3 ms, and insert plus stats update together stay under
   6 ms per 100-point batch.

   **Update method: rebuild the affected day's row from scratch.** Do not adjust values
   arithmetically. `countries` / `cities` are sets, so when a single point is deleted there is
   no way to tell whether that country or city still applies to another point that day without
   scanning the day; an arithmetic approach would never shrink them and `/stats` would keep
   reporting inflated values. A day holds about 1,100 points, so even a full rebuild stays
   under 6 ms per batch.

   **The rebuild input is "that day's points plus the immediately preceding point"** (which may
   be from an earlier day). The first point of a day measures its segment against the last
   point of a previous day, so it cannot be computed from that day's points alone. Missing this
   drops every cross-midnight movement from `km`.

   The set of affected days is not just "today". A segment between two consecutive points is
   **attributed to the day of the later point**, so inserting, updating, or deleting a point
   changes `km` both for that point's day and for **the day of the next point in time order**.
   Implement it as "the day of the target point, and the day of the next point in time order"
   (the previous point's day does not change, because the `prev → P` distance is already
   counted on P's day). If an update moves a point to a different day, take those same two days
   for both the before and after states. The next point can be days away if tracking was
   stopped, so never hardcode "the following day". Out-of-order and late-arriving batches are
   handled the same way.

   **Segments whose time gap exceeds `TRAVELMAP_TRACK_BREAK_MINUTES` (default 30) are not
   counted.** Adding the straight-line distance between two points that span a tracking gap or
   a flight would make the `/stats` total wildly unrealistic. Stage 4 track splitting uses the
   same value, giving a consistent meaning: "the sum of distance travelled within tracks".

   This is a **server-side setting**, distinct from `track_break` in `settings/mobile` (the app
   setting for how the device splits tracks). Using the latter for aggregation would change the
   meaning of past aggregates every time the user changes a setting in the app. As with
   `TRAVELMAP_TIMEZONE`, changing it requires `travelmap recalculate`. The spec does not say how
   upstream computes this, so compare against the app's display in Stage 3 and revisit if the
   numbers diverge.

   Columns: `user_id`, `day`, `points`, `reverse_geocoded_points`, `km`, `countries`, `cities`.
   The primary key is the composite `(user_id, day)`.
   **Delete the row entirely once a day has zero points** (leaving it at zero would make
   `tracked_months` keep returning months with no points, and would no longer match a
   `recalculate` rebuild).

   **The timezone used to cut `day` is set by `TRAVELMAP_TIMEZONE` (default `UTC`).**
   `day` is part of the primary key, so changing it later requires rebuilding `daily_stats` for
   every user (about 16 seconds for 2M points, per the table above). Using this in Japan without
   setting `Asia/Tokyo` would attribute movement between 00:00 and 09:00 to the previous day,
   making the monthly and yearly figures in `/stats` and `tracked_months` disagree with the app.

   All five `/stats` totals (`totalDistanceKm`, `totalPointsTracked`,
   `totalReverseGeocodedPoints`, `totalCountriesVisited`, `totalCitiesVisited`) must be
   derivable from this table alone (`countries` / `cities` are JSON arrays of the country and
   city names visited that day; take the union when aggregating over the whole history).
   Reverse geocoding is off by default, so until it is enabled both stay empty arrays and
   `/stats` returns 0 for them. Matching upstream requires the user to configure a reverse
   geocoder.

3. **Do not add a bbox index for now — neither R\*Tree nor a lat/lon composite index.**
   R\*Tree takes bbox search from 173 ms to 51 ms, but it costs 58 seconds to build and adds
   storage. More to the point, **no in-scope endpoint takes a bounding box** (`GET /points`
   accepts only `start_at` / `end_at` / `page` / `per_page` / `order`). An index with no query
   to serve costs insert time and storage for nothing. Decide which one to add if and when
   rectangular search is actually needed.

### When to Switch to PostgreSQL / PostGIS

None of these apply today. If any becomes real, add a PostgreSQL implementation behind the
`internal/store` interface and switch (which is why the store is abstracted).

- Multiple users writing concurrently and continuously (SQLite always has a single writer)
- Doing kNN or complex spatial joins against our own boundary data instead of an external
  reverse geocoder
- Implementing H3 hex aggregation / fog of war (both non-goals)
- The database reaching tens of GB (from the measurements above, even 10M points is about 1 GB)

## Dawarich API Compatibility Notes

Upstream quirks that must be checked before implementing.

### Authentication

- Either an `api_key` query parameter **or** an `Authorization: Bearer {api_key}` header.
- The spec lists only one of the two per endpoint (query for points / stats / tracks /
  settings; header for users/me and visits), but **the implementation should accept both on
  every endpoint**. Which one the app uses is unknown.

### Per-endpoint

- **`GET /api/v1/health`** — No authentication. The response headers `X-Dawarich-Response`
  (`Hey, I'm alive!` when unauthenticated, `Hey, I'm alive and authenticated!` when
  authenticated) and `X-Dawarich-Version` are **required**. Body is `{"status":"ok"}`.
  Implement first, on the **assumption** that the app's server-URL validation goes through here
  (needs confirmation against a real device — but health is needed regardless, so being wrong
  costs no rework).
- **`POST /api/v1/auth/login`** — Body `{email, password}` → 200 with
  `{user_id, email, api_key, status, plan, subscription_source, active_until}`. With 2FA
  enabled it returns 202 plus a `challenge_token` (this project always returns 200).
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

### About the iOS app

`dawarich-app/dawarich-ios` is an **empty repository**; the app is closed source. The endpoints
it actually calls, the required fields, and the call order therefore cannot be determined from
the spec. The approach is to add request-logging middleware in Stage 1 and fill in the gaps by
observing traffic from a real device.

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
  served even by the Stage 7 web UI. Revisit once the web UI's rendering approach is settled.
  Only `countries/visited_cities` is covered, in Stage 6.
- `GET /api/v1/locations`, `GET /api/v1/locations/suggestions`, `GET /api/v1/residency` — Place
  search and stay analysis. Out of scope as part of the Places family (non-goal).
- `POST /api/v1/auth/otp_challenge`, `GET/POST/DELETE /api/v1/users/me/two_factor`,
  `POST /api/v1/users/me/two_factor/setup`, `POST /api/v1/users/me/two_factor/confirm`,
  `GET /api/v1/users/me/two_factor/backup_codes` — 2FA. Not supported, given the self-hosted
  assumption (`POST /api/v1/auth/login` never returns 202; it always returns 200 with an
  `api_key`).
- `POST /api/v1/auth/apple`, `POST /api/v1/auth/google` — Social login. **A risk that could
  block Stage 1 from completing**; see "Risks and Open Questions".

## Development Environment

### Language and toolchain

**The floor is Go 1.25** (`modernc.org/sqlite` v1.56.0 declares `go 1.25.0` in its `go.mod`).
However, **use the latest stable release available at the time of starting**, not the floor.
As of 2026-08-17 that is **go1.26.6** (1.27 is at rc3). Go supports only the two most recent
releases, so 1.25 drops out of support the moment 1.27 goes GA. Installing the floor as-is means
that once `govulncheck` in CI reports a toolchain vulnerability, there is no remedy other than
upgrading.

Check <https://go.dev/dl/> for the latest stable release when starting, and align the `go`
directive in `go.mod` and `actions/setup-go` in CI with it.

### Libraries

Keep dependencies minimal.

| Purpose | Package | Notes |
| --- | --- | --- |
| Routing | `github.com/go-chi/chi/v5` | Rationale in "Library Choices for the Web UI" |
| SQLite driver | `modernc.org/sqlite` | Pure Go. No CGO. Requires Go 1.25+ |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Migrations | `github.com/pressly/goose/v3` (or `embed` + a hand-rolled migrator) | |
| Test diffs | `github.com/google/go-cmp` | |
| Configuration | No extra dependency (a thin hand-written env-var loader) | |
| Logging | Standard `log/slog` | |

### Library Choices for the Web UI

Since a web UI is planned, make choices now that will not need to be redone then.
**The only thing that must be decided now is the router**; UI libraries are not added until
Stage 7.

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

**Candidates for Stage 7 (not added now)**

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
session cookie on the `/*` side. To be decided when Stage 7 starts. The current front-runner is
**(a)**.

- **(a) The `/api/v1` middleware also accepts the session cookie** — lets the UI simply fetch.
  Accepting cookies means `/api/v1` needs CSRF protection too, but Go 1.25's
  `CrossOriginProtection` can be applied server-wide, so the added cost is small.
- (b) The UI calls handlers/store directly in-process — keeps the authentication split, but
  changes "reuse the API" from reusing the HTTP API to reusing the implementation.
- (c) Hand the api_key to the UI at login and call with Bearer — not recommended, since XSS
  would leak the API key.

### Development tools

- `golangci-lint` — enable errcheck, govet, staticcheck, revive, gosec, bodyclose, sqlclosecheck
  in `.golangci.yml`
- `gofumpt` — formatting
- `govulncheck` — vulnerability scanning
- `go test ./... -race -cover`

### Makefile

Provide `build` / `test` / `lint` / `fmt` / `run` / `migrate` / `docker` targets.

### CI

Add `.github/workflows/go.yml` with `build` / `test` (`-race`) / `lint` / `govulncheck` jobs.

**Follow the conventions of the existing workflows** (see
`.github/workflows/workflow-lint.yml`).

- Every workflow has a `permissions:` block
- Third-party actions are pinned to a full commit SHA with a `# vX.Y.Z` comment
- `actions/checkout` is given `persist-credentials: false`

Also add the `gomod` ecosystem to `.github/dependabot.yml` (weekly / `Asia/Tokyo` /
7-day cooldown).

### Containers

- Multi-stage `Dockerfile`: build with `CGO_ENABLED=0 go build -ldflags="-s -w"` and place the
  binary on `gcr.io/distroless/static`
- `docker-compose.yml` with just the server container and a volume for SQLite

### Directory layout

```
cmd/travelmap/            entry point (serve / user create / migrate / recalculate)
internal/config/          env-var loader
internal/httpapi/         routing, middleware, handlers
internal/httpapi/dto/     Dawarich-compatible JSON structs (compatibility lives here)
internal/auth/            API key issuing/validation, bcrypt
internal/ingest/          point insert/update/delete + daily_stats update (every path goes here)
internal/store/           repository interfaces
internal/store/sqlite/    SQLite implementation + migrations (embed)
internal/model/           domain models (User, Point, Track, Visit, Stat)
internal/geo/             Haversine (one-off), track-splitting decision logic
api/openapi.yaml          OpenAPI covering only the implemented subset (for reference)
testdata/golden/          golden JSON for upstream response shapes
```

### Testing approach

- Table-driven tests for handlers using `net/http/httptest` and a temporary SQLite database
- **Pin JSON key names, types, and naming convention (camelCase / snake_case) with golden
  files.** This is where compatibility lives
- Run `go test -race` in CI

## Development Steps

One stage is intended to be one PR. Each stage has a completion condition that can be
verified by running something.

### Stage 0: Project foundation

No application functionality yet.

- [ ] (Prerequisite; will not appear in the diff) Update the development environment's Go to
      the latest stable release. This environment has 1.24.7, below even the 1.25 floor.
      `GOTOOLCHAIN=auto` would fetch it automatically, but upgrade explicitly
      (see "Language and toolchain")
- [ ] Create `go.mod` and the directory skeleton
- [ ] `cmd/travelmap` responds to `--version`
- [ ] `Makefile`, `.gitignore` (Go plus `*.db`, `bin/`), `.golangci.yml`
- [ ] `CLAUDE.md` (project conventions: **English as the project language**, layering, testing
      approach, where documents live)
- [ ] Expand `README.md` (currently one line; describe the project's purpose and how to build
      and run it)
- [ ] `.github/workflows/go.yml` (build / test / lint / govulncheck)
- [ ] Add `gomod` to `.github/dependabot.yml`
- [ ] `Dockerfile`, `docker-compose.yml`

**Done when**: CI is green, `docker build` succeeds, and the image is 30 MB or smaller.

### Stage 1: Connectivity — the app recognises the server

- [ ] SQLite store and migration foundation (`users`, `points` tables)
- [ ] `travelmap user create --email --password` issues a user and an API key
- [ ] Authentication middleware (accepting both the `api_key` query parameter and Bearer)
- [ ] `GET /api/v1/health` (including the `X-Dawarich-Response` / `X-Dawarich-Version` headers)
- [ ] `POST /api/v1/auth/login`
- [ ] `GET /api/v1/users/me`
- [ ] Request-logging middleware (enabled by `TRAVELMAP_DEBUG_LOG_REQUESTS=1`).
      **Identify the unimplemented endpoints hit by a real device and record them in this file**

**Done when**: entering the server URL and API key in the iPhone app reports a successful
connection.

### Stage 2: Recording — locations get stored

- [ ] Finalise the `points` schema (covering every upstream point field). One index,
      `(user_id, timestamp)` (**without it the `GET /points` time filter becomes a full scan**;
      no lat/lon index, per "Data Store Verification")
- [ ] GeoJSON Feature parser (`internal/httpapi/dto`)
- [ ] `POST /api/v1/points`
- [ ] `POST /api/v1/overland/batches`
- [ ] Deduplication (same user × timestamp)
- [ ] Batch inserts wrapped in a transaction
- [ ] `daily_stats` table, updated in the same transaction as ingest (**only** the affected
      days, rebuilding each day's row from points rather than adjusting values arithmetically).
      **This has to be built on the write side so that Stage 3's `/stats` is fast enough**
      (column definitions, which days are affected, and segment attribution are in
      "Data Store Verification")
- [ ] Cut `day` by `TRAVELMAP_TIMEZONE` and decide which segments count towards distance by
      `TRAVELMAP_TRACK_BREAK_MINUTES` (defaults and consequences in "Data Store Verification").
      Document in the README that **changing either requires `travelmap recalculate`**
- [ ] `travelmap recalculate` subcommand (rebuilds `daily_stats` from points; for recovery after
      imports or inconsistency)
- [ ] **Route every path that changes points through a single ingest/mutation layer.**
      Scattered `daily_stats` updates are guaranteed to miss cases (Stage 3 updates and deletes,
      and Stage 6 imports, owntracks/traccar, and the reverse-geocoding worker all go through
      the same layer)
- [ ] A test that the Haversine in `internal/geo` and the Haversine in SQL agree on the same
      input (pass the Earth-radius constant from Go into SQL; do not put the literal in two
      places)
- [ ] **A test with `TRAVELMAP_TIMEZONE=Asia/Tokyo`.** All the other listed cases pass under the
      default `UTC`, so forgetting the timezone conversion would go undetected. Verify that a
      point at a time which falls on the previous day in UTC (e.g. 00:30 JST) is counted on the
      current day's row
- [ ] A boundary test for `TRAVELMAP_TRACK_BREAK_MINUTES`: a segment of exactly 30 minutes
      **is counted** (catches a `>` versus `>=` mix-up)
- [ ] **A test pinning the expected value of a cross-midnight segment distance.** Comparing for
      agreement is not enough: if the incremental update and `recalculate` both drop it, they
      agree and the test passes anyway (verify that the distance between the previous day's last
      point and the current day's first point appears in `km`)
- [ ] In addition to the above, a test that the incremental update and `recalculate` agree. For
      the same set of points, the `daily_stats` built up by per-ingest updates must equal the
      one produced by a full `recalculate` rebuild. Cover: the day boundary (distance between
      the previous day's last point and the current day's first point); out-of-order and
      late-arriving batches; **points separated by several days** (either side of a period with
      tracking stopped — also confirming that segments over
      `TRAVELMAP_TRACK_BREAK_MINUTES` are not counted); **a day whose points are all deleted so
      the row is removed**; and **deleting or updating only some of a day's points so that
      `countries` / `cities` shrink**

**Done when**: starting tracking in the app puts real device locations into the database and the
point count grows. Also, inserting points updates the corresponding `daily_stats` day, and
running `travelmap recalculate` afterwards produces the same values (incremental and rebuild
agree).

### Stage 3: Browsing — the user's history is visible in the app

- [ ] `GET /api/v1/points` (time filter, pagination, `X-Current-Page` / `X-Total-Pages`)
- [ ] `PATCH /api/v1/points/{id}` (body wrapped as `{"point": {...}}`),
      `DELETE /api/v1/points/{id}`
- [ ] `DELETE /api/v1/points/bulk_destroy` (body `{"point_ids": [...]}`)
- [ ] For those updates and deletes, **recalculate the affected days' `daily_stats` in the same
      transaction**. Never leave a state where points were deleted but `/stats` keeps reporting
      the old distance
- [ ] `GET /api/v1/points/tracked_months` (read from `daily_stats`)
- [ ] `GET /api/v1/stats` (aggregate `daily_stats`; keep camelCase exactly).
      **Do not aggregate points directly** (rationale and measurements in
      "Data Store Verification")

**Done when**: past points appear on the app's map and the stats screen shows distance and point
counts.

> **Reaching Stage 3 satisfies the requirement of recording and browsing location history from
> the iPhone app.** Stage 4 onwards is additional work to make the app's other screens function.

### Stage 4: Tracks / visits / timeline

- [ ] Track-splitting logic (split on `TRAVELMAP_TRACK_BREAK_MINUTES` of inactivity) as a
      background job. **Not `track_break` from `settings/mobile`** (see
      "Data Store Verification")
- [ ] `GET /api/v1/tracks` (GeoJSON FeatureCollection)
- [ ] `GET /api/v1/tracks/{id}`, `GET /api/v1/tracks/{track_id}/points`
- [ ] Stay detection → `visits` table
- [ ] `GET /api/v1/visits`
- [ ] `GET /api/v1/timeline` (including validation of the 31-day cap)

**Done when**: the app's timeline and track screens render without breaking.

### Stage 5: Settings sync

- [ ] `GET/PATCH /api/v1/settings/mobile` (12 fields with range validation; PATCH accepts both
      the top-level and `settings`-wrapped forms)
  - `tracking_mode` (precise|significant), `tracking_visits`, `track_visits_independently`,
    `auto_start`, `distance_filter` (1–10000 m), `time_filter` (1–3600 s),
    `track_break` (1–1440 min), `accuracy` (1–6), `show_background_location_indicator`,
    `upload_automatically`, `upload_all_on_tracking_stop`, `batch_size` (1–1000)
- [ ] `GET/PATCH /api/v1/settings`

**Done when**: settings changed in the app are stored on the server and restored after
reinstalling the app.

### Stage 6: Operations and extensions (optional)

- [ ] `POST /api/v1/imports`, `GET /api/v1/imports`, `GET /api/v1/imports/{id}`
      (GPX / GeoJSON / Google Takeout / upstream Dawarich export).
      **Imports go through the same ingest layer as Stage 2 and update `daily_stats`**
- [ ] Reverse geocoding (Nominatim / Photon, rate-limited worker). It runs asynchronously after
      points are inserted, so **on completion update `countries` / `cities` /
      `reverse_geocoded_points` in `daily_stats` for the affected days** (without this the
      corresponding `/stats` values stay 0 — see "Data Store Verification")
- [ ] `POST /api/v1/auth/register` (enabled by an env var)
- [ ] `POST /api/v1/owntracks/points`, `POST /api/v1/traccar/points`
- [ ] `GET /api/v1/countries/visited_cities`
- [ ] Backups (`VACUUM INTO`)
- [ ] `GET /metrics`, structured access logs

### Stage 7: Web UI

Start once the API has settled. Library candidates and rationale are in
"Library Choices for the Web UI".

- [ ] Cookie sessions + CSRF. How the browser authenticates against `/api/v1` follows the
      conclusion of "Open question: how the browser authenticates against `/api/v1`"
      (front-runner is (a))
- [ ] A login screen that accepts an account created with `travelmap user create`
- [ ] Map screen (render points / tracks for a selected time range), reusing the existing
      `GET /api/v1/points` and `/tracks` without adding UI-only APIs
- [ ] Statistics screen (using `daily_stats`)
- [ ] Settings screen. An import screen only if Stage 6's `/api/v1/imports` was implemented
- [ ] Embed static assets in `embed.FS` to preserve the single binary

**Done when**: logging in from a browser shows the user's history on a map, and deployment is
still one binary plus one SQLite file.

## Risks and Open Questions

- **Because the iOS app is closed source, the endpoints it actually calls and the required
  fields are unknown.** Use the Stage 1 request-logging middleware to observe traffic from a
  real device and identify unimplemented endpoints.
- **If social login is mandatory, Stage 1 may not be completable.** The spec has
  `POST /api/v1/auth/apple` (body `{id_token, nonce}`) and `POST /api/v1/auth/google`. If the
  iOS app forces Sign in with Apple, `POST /api/v1/auth/login` alone will not get to a
  successful connection. This is the first thing to check in Stage 1. If it turns out to be
  required, add verification of `id_token` against Apple's public keys and association with an
  existing user to Stage 1 (whether to auto-create users on a self-hosted instance is a separate
  decision).
- The app may check a minimum server version. Return a plausible recent version string in
  `X-Dawarich-Version` (e.g. `1.10.0`). Needs confirmation against a real device.
- For unimplemented endpoints, returning **an empty array / empty object with 200** may be less
  likely to crash the app than a 404. Decide based on observed behaviour on a real device.
