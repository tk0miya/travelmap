# travelmap — Development Plan

What travelmap is, its purpose, and the parts of its design already settled are in
[docs/README.md](docs/README.md). How to build, run and configure it is in
[README.md](README.md). This file is the plan for what is still ahead: what gets built, in what
order, and why.

## Technical Decisions

Decisions for work not built yet. A decision already implemented is not here — it is in
[`docs/architecture.md`](docs/architecture.md) or [`docs/database.md`](docs/database.md),
whichever already owns that ground — and a row below moves there once the step that implements
it lands.

| Item | Decision | Rationale |
| --- | --- | --- |
| Background work | Goroutines + a job table in SQLite | Keeps a single process, with no Sidekiq/Redis equivalent |
| Reverse geocoding | Off by default; optionally point at a Nominatim/Photon URL | Does not make an external service mandatory |
| Templating | Replace `html/template` with templ (or gomponents) before the remaining screens are written | `html/template` has no component model and no type checking. `internal/httpapi/web.go` already works around the first: its `pageTemplate` builds a separate `*template.Template` per page, because every page defines a `"content"` block and one set would collide on the name. Four pages exist today, and the map and statistics screens Milestone H still has to plan add one each, so the workaround grows with the UI. templ's generator is a Go binary and fits `go.mod`'s `tool` directives; gomponents needs no generator at all. Either way a fresh checkout still needs nothing but Go |
| Trip boundaries | **The MVP creates a trip only from what the user enters** — a title and a time range. Detection is left out, not ruled out | Detecting a trip needs a "home" cluster, which needs stay detection, and the MVP builds neither. Nothing about detection is wrong in principle: the bar is proposal accuracy and what the detector costs, and stay detection landing is when there is something to judge against. Upstream agrees on the shape — its own `trips.name` is `NOT NULL`, so a Dawarich trip is user-named too (Milestone J) |
| Trip proposals | Open: **whether a candidate is stored is decided when proposals are built**, not now | Computing candidates on read keeps a mis-tuned threshold from putting rows in the trip list — a bad threshold misorders a list instead of corrupting data. Storing them is what lets a dismissal be remembered, which matters once the list is long enough that the same commute at the top becomes a nuisance. Upstream stores its equivalent for exactly that reason (`visits.status`), so neither side is obviously right until the list exists |
| The timeline | Assembled on read from `checkins` and `points`. **No derived table** | With stay and gap detection deferred there is nothing to derive, so no re-derivation, no `travelmap recalculate` pass, and no coupling to `internal/ingest` |
| Annotations (`notes`, later `photos`) | Always carry **their own timestamp**, and may *also* point at a trip. Never at the id of a derived row | The timestamp is what stops user-written text from being orphaned: a derived table such as `visits` is rebuilt id and all, so a note pointing into one loses its anchor. A trip is not derived, so pointing at one is safe, and it is what expresses "this note is about that trip" rather than "about that instant". Upstream's `notes` does both — `noted_at` (nullable as a column but required by the model), a polymorphic `attachable`, and a `lonlat` — but it allows that attachable to be a `Visit`, i.e. a derived row, which is the half not to copy. **travelmap narrows the target to a trip deliberately.** Worth settling before the table exists, since it cannot be changed cheaply afterwards |
| Client-side code | TypeScript, built by esbuild **as a Go library** (`github.com/evanw/esbuild/pkg/api`) | Puts types where client state lives without an npm/Node toolchain, so "a fresh checkout needs nothing but Go" survives. Rules out neither an SPA later nor htmx now; it removes the build chain from the argument |

These are the defaults as of planning. If any turns out to be wrong during implementation,
change it — after updating this file.

## Data Model

Columns and indexes for the tables already migrated are in `internal/store/sqlite/schema.sql`,
kept current by `TestSchema`; the rationale for how a column or index is shaped is a comment in
the migration that adds it. Behaviour that spans tables or does not attach to a single column —
invariants, algorithms, config effects — is in `docs/database.md`.

A table not yet migrated has its data model here instead, until the step that adds it.

### `trips`

Columns: `id`, `user_id`, `title`, `started_at`, `ended_at`, `description`, `created_at`,
`updated_at`. `description` is free text about the trip as a whole, and is deliberately not
called `note`: the deferred `notes` below are a different thing, carrying their own timestamp,
and one word for both would blur which of the two a reader is looking at.
One index: `(user_id, started_at)`. `started_at` and `ended_at` are Unix seconds UTC like every
other time in the schema, and `ended_at` is an exclusive upper bound.

**Ranges may overlap, and nothing rejects one that does.** "Europe" containing "Paris" is the
case a traveller actually has, and since the timeline is assembled from a time range rather
than from rows that point at a trip, two trips covering the same hour is not an ambiguity
anything has to resolve.

**In the MVP nothing derives a trip and nothing rebuilds one.** Every row comes from what the
user typed, so no config change invalidates one and `travelmap recalculate` does not touch the
table — unlike `daily_stats`, which is the other table a time range means something to. This is
the MVP's property, not a law: the "Trip proposals" row above leaves open whether an accepted
candidate becomes a row, and such a row would have been derived from points. What holds either
way is that a trip is not *re*-derived — once it exists, only the user changes it.
Added by Milestone J's Step 37.

**No column holds what a trip contains**, and none is planned: the contents are found by range.
Upstream's own `trips` is the same shape — a `name`, a range, and no join table to its visits or
tracks — which is worth knowing because it also shows where this goes if reading the range ever
gets slow: it caches `distance`, `path` and `visited_countries` on the row beside a
`last_recalculated_at`. That is the answer to a performance problem, not the starting point,
and it is the one thing that would make a trip row hold derived state.

## Dawarich API Compatibility Notes

Upstream quirks for endpoints not yet implemented. For an endpoint already implemented,
`docs/api-notes.md` covers it — this section holds only what is still ahead.

### Per-endpoint

- **`GET /api/v1/tracks`** — GeoJSON `FeatureCollection` of LineStrings. Properties are `id`,
  `color`, `start_at`, `end_at`, `distance` (metres), `avg_speed` (km/h), `duration` (seconds),
  `dominant_mode`, `dominant_mode_emoji`.
- **`GET /api/v1/timeline`** — `start_at` / `end_at` required, **range capped at 31 days**.
  Response is `{days: [...]}`.

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
   evidence about *a* client, not *the* one. The HEAD requirement above, and the
   Bearer-everywhere behaviour in `docs/api-notes.md`, both came from it.
   Endpoints it uses: `/api/v1/health`, `/api/v1/users/me`, `/api/v1/points`,
   `/api/v1/points/{id}`, `/api/v1/stats`, `/api/v1/countries/visited_cities`.
2. **Request logging against a real device** (Step 6), for anything the above does not settle —
   in particular whatever the official app does that this client does not.

## Endpoints Deliberately Excluded

**The checklists under "Development Steps" are the single source of truth for what gets
implemented.** Keeping a separate list would mean double bookkeeping that drifts out of date, so
this section records only the spec endpoints whose absence needs explaining — not the ones
[docs/README.md](docs/README.md)'s Non-goals already rule out; no future step reconsiders those.

- `GET /api/v1/points/{id}` and `GET /api/v1/visits/{id}` **do not exist in the spec** (both
  have only `patch` and `delete`). If device logs show them being called, add them on the basis
  of that observation.
- `POST /api/v1/visits`, `PATCH/DELETE /api/v1/visits/{id}`, `POST /api/v1/visits/merge`,
  `POST /api/v1/visits/bulk_update` — Visit editing. Out of scope for now, since the goal is
  browsing only.
- `POST /api/v1/points/reapply_anomaly_filter` and
  `GET /api/v1/settings/transportation_recalculation_status` — Anomaly filtering and transport
  mode inference. **Neither feature is implemented at all**, so there is nothing to trigger or
  report progress on (tracks return `null` for `dominant_mode`).
- `GET /api/v1/demo_data`, `POST /api/v1/demo_data`, `DELETE /api/v1/demo_data` — Upstream
  Cloud demo data, irrelevant to a self-hosted instance. Not one of `docs/README.md`'s declared
  Non-goals, so recorded here rather than assumed obvious.
- `GET /api/v1/countries/borders` — GeoJSON country border polygons (serving several MB of
  static data). Border rendering is expected to be handled by the map tiles, so it will not be
  served even by the Milestone H web UI. Revisit once the web UI's rendering approach is settled.
  Only `countries/visited_cities` is covered, in Milestone G.
- `POST /api/v1/auth/apple`, `POST /api/v1/auth/google` — Social login. **A risk that could
  block Milestone B from completing**; see "Risks and Open Questions".

## Foursquare / Swarm Integration Notes

External behaviour that must be known before finishing Milestone I, in the same spirit as
"Dawarich API Compatibility Notes".

Two sources feed what is left here, and they are kept apart on purpose: **a real push captured
with a webhook recorder**, and **Foursquare's own reference** at
`https://docs.foursquare.com/developer/reference/`, which documents v2 today under the name
**Personalization APIs**, labelled a public beta. Where an observation and the reference disagree
the observation wins, and the disagreement is recorded rather than smoothed over.

The reference is **an incomplete subset of the API it describes**: it omits fields the captured
push demonstrably carries, and parameters working clients still pass. So "not in the reference" is
not "not there" — but it is not "still there" either, only "not settled here", and each note below
says which of those it is.

Pages read on 2026-08-24, all under `https://docs.foursquare.com/developer/`:
`reference/get-user-checkins`, `reference/get-checkin-details`, `reference/create-a-checkin`,
`reference/get-user-details`, `reference/v2-authentication`, `reference/real-time-view`,
`reference/personalization-api-overview`, `reference/personalization-apis-errors`,
`reference/personalization-apis-rate-limits`, `reference/personalization-apis-localization`,
`reference/upcoming-changes`, `docs/configure-server-webhooks`.

### Whether the two paths agree on a check-in's language

Neither collection path asserts a locale, and what that means for the display columns is under
"Localisation" in [`docs/database.md`](docs/database.md). What is not settled is whether the two
paths then render the same check-in the same way.

v2 takes a locale from an `Accept-Language` header, which the documentation prefers, or from a
`locale=` query parameter, over `en`, `es`, `fr`, `de`, `it`, `ja`, `th`, `tr`, `ko`, `ru`, `pt`
and `id`. `en` is the documented default for the request — but **geographical names are handled
separately**: with no locale specified they come back in "the language that's most popular in the
country for that venue". So an unlocalised request is not an English one.

**What renders the observed push in Japanese is not decided by this.** The venue was in Japan, so
the app's own locale and the venue-country fallback predict the same `country` text, and that half
of the observation separates nothing. The category name also came back in Japanese, and there the
two readings do differ: the reference describes the fallback for geographical names and says
nothing about categories, so the fallback does not account for it while an app locale of `ja`
would. That leans towards the locale reading — **one observation leaning is not a conclusion**,
and neither reading is settled.

What that leaves is a disagreement nobody has looked for: a repeat write refreshes `venue_name`,
`country`, `city`, `state` and `category_name`, so if the two paths render them differently, those
columns flip language on every write.

**Step 22 measures it rather than trusting it.** Fetch a check-in that also arrived by push and
compare those five columns. If they agree, this section is settled and says so. **If they differ,
the fix is not a locale setting** — no setting can track whatever the push does — but removing
those columns from what a repeat write refreshes, leaving the first writer's rendering in place.
`cc` is stable under either outcome, per "checkins" in docs/database.md.

## Library Choices for the Web UI

Sessions and CSRF are settled and already implemented; their rows moved to
`docs/architecture.md`, which also covers the router and HTML rendering, for the reasoning that
applies here as well. HTML rendering is implemented but not closed: the engine itself is being
replaced, for the reason in "Technical Decisions" above. What is left undecided is what the
remaining screens need.

**Still open, for Milestone H's remaining screens**

| Purpose | Candidate | Notes |
| --- | --- | --- |
| Map rendering | MapLibre GL JS or Leaflet | The one place a library has to be fetched rather than written. Vendor it into `embed.FS` rather than using a CDN, to keep a single binary. Milestone J's Step 41 picks one |

Templating and client-side code are settled in "Technical Decisions" above: `html/template` is
replaced with a type-safe engine before the screens multiply, and what runs in the browser is
TypeScript built by esbuild as a Go library. htmx is worth adding only if partial page updates
turn out to want it — not to avoid a build chain, which esbuild-as-a-Go-library already avoids.

**What decides whether the UI is ever split from the server is not the single-binary
property.** An SPA keeps that just as well, by embedding its build output in `embed.FS`; the
binary that runs is still one file. What a Node build chain actually costs is the other promise
— "a fresh checkout needs nothing but Go" (see "Working on a change" in CLAUDE.md) — which is
why the front end stays in Go's own toolchain for as long as that promise is worth keeping.

The question that settles it is what the screens turn out to be: documents to look at, or an
application to operate. Milestone J's Step 41 is the first screen with client-side state of its
own, so it is the honest place to judge, and it says so.

**Open question: how the browser authenticates against `/api/v1`**

**The policy is to reuse the existing `/api/v1` rather than add UI-only data endpoints**
(browser-specific routes such as login and sessions are added in this milestone instead), so the
browser will also call `/api/v1/points` and friends — but `/api/v1` accepts only Bearer /
`api_key`, with the session cookie planned for everything else. The current front-runner is
**(a)**.

- **(a) The `/api/v1` middleware also accepts the session cookie** — lets the UI simply fetch.
  Accepting cookies means `/api/v1` needs CSRF protection too, but Go 1.25's
  `CrossOriginProtection` can be applied server-wide, so the added cost is small.
- (b) The UI calls handlers/store directly in-process — keeps the authentication split, but
  changes "reuse the API" from reusing the HTTP API to reusing the implementation.
- (c) Hand the api_key to the UI at login and call with Bearer — not recommended, since XSS
  would leak the API key.

**Steps 27 to 34 do not settle this and do not need to**: none of them adds a data endpoint, so
`/api/v1` is left exactly as it is and keeps needing no CSRF protection. It is the first screen
that reads a point *through the HTTP API* — Milestone H's own map screen, listed under "Still to
plan" — that has to answer it. Milestone J's Step 41 draws a map without answering it and
without deferring it either: it reads through `internal/timeline` in process and hands the
browser its GeoJSON inside the page, so it adds no data endpoint and `/api/v1` stays
untouched. The session middleware already in place is what makes (a) cheap when that day
comes: accepting the cookie there is one more branch in `authenticate`, and
`CrossOriginProtection` can then be moved from the browser group up to the whole server.

## Distribution

Not built yet — packaging belongs to Milestone G, not the foundation.

A `CGO_ENABLED=0` binary has no runtime dependencies, so there is nothing for a container to
isolate — a `distroless/static` image is the binary plus a CA bundle. The reason to ship one is
the audience, not the technology: upstream Dawarich is distributed as docker-compose, the server
has to run continuously somewhere (NAS, VPS, home server), and NAS platforms such as Synology,
unRAID and TrueNAS are container-first. A systemd unit serves the same purpose on a plain host,
so document both and treat neither as the foundation.

- Multi-stage `Dockerfile`: build with `CGO_ENABLED=0 go build -ldflags="-s -w"` and place the
  binary on `gcr.io/distroless/static`. **Add `-X main.version=<release>` to those ldflags**:
  the build stage copies sources without `.git`, so Go stamps no VCS information and
  `travelmap --version` would report `unknown` on exactly the builds people run
- `docker-compose.yml` with just the server container and a volume for SQLite. Note the SQLite
  file's ownership: the container runs as a non-root user, so a bind-mounted directory has to be
  writable by that UID
- An example systemd unit for hosts not running containers
- A `docker` Makefile target for building the image, alongside `build` / `test` / `lint` / `fmt` /
  `check` / `run` / `migrate`

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

**A step's number says when it was planned, not when to take it.** Milestone H's Steps 23 to 34
were planned after Milestone I's 18 to 22 and so carry higher numbers while appearing earlier in
this file, and some of them depended on work from the other milestone that has since landed. What
order to take them in is what each milestone's own ordering note says, never the numbers.

---

## Milestone B — The app connects

### Step 6: Request logging for endpoint discovery

Small, but its output is a planning input for everything after: the iOS app is closed source,
so this is how the remaining endpoint list gets confirmed.

- [ ] Record the endpoints a real device actually hits in this file, and diff them against the
      list the community Android client uses (see "About the iOS app")

**Done when**: connecting the app produces a log of every route it calls, with no credentials in
it. **Not yet done**: no device has been pointed at this server yet, so the capture below is
empty. The rest of the plan runs on the community Android client's endpoint list until it is
filled in.

#### Endpoints a real device hits

Nothing recorded yet. Fill this in from one capture session — start the server with
`server.debug_log_requests = true`, add it in the app, let it record and browse — and list the
method, the path and the query parameters of every line, including the 404s. Diff that against
the six endpoints under "About the iOS app": what is here and not there is what the official
app needs and the community client does not, and it is the input to Milestone F's ordering.

---

## Milestone F — The app's remaining screens

Steps 13 and 14 are independent and can run in parallel. Step 15 needs 13 and 14.

**But all three now depend on Milestone J**, which owns the assembly they project: they need its
Step 37 (the `internal/timeline` package) and Step 39 (the assembly itself), and Step 13
additionally needs the gap detection deferred there.

### Step 13: Tracks

- [ ] Track-splitting logic (split on `tracking.track_break_minutes` of inactivity).
      **Not `track_break` from `settings/mobile`** (see "Data Model")
- [ ] `GET /api/v1/tracks` (GeoJSON FeatureCollection)
- [ ] `GET /api/v1/tracks/{id}`, `GET /api/v1/tracks/{track_id}/points`

**These endpoints are a projection, not a second implementation.** The splitting belongs to the
one assembly in `internal/timeline` (see Milestone J), and these routes render its move entries
in upstream's shape at the DTO boundary. Upstream materialises its own equivalent — a `tracks`
table holding `original_path` as a LineString plus `distance`, `duration`, `avg_speed` and
elevation — which is where this goes if reading the points per request turns out to be too slow,
not where it starts.

### Step 14: Visits

- [ ] Stay detection → `visits` table. **This step owns the detector**, so it also owns what
      one costs: `tracking.stay_radius_meters` and `tracking.stay_minimum_minutes`,
      re-derivation driven from `internal/ingest` and `internal/checkin` the way `daily_stats`
      already is, and a pass in `travelmap recalculate` — changing either threshold invalidates
      every stored row
- [ ] `GET /api/v1/visits`, a projection of the same assembly's stay entries into upstream's
      shape at the DTO boundary — not a second reader of the table. **It reports every stay,
      `declined` included**, because upstream's response requires `status` and a client filters
      on it itself; hiding one is a screen's policy and belongs above the assembly, never inside
      it (see "Deferred: gap and stay" in Milestone J)

`visits` is upstream's own concept and keeps upstream's shape, `status` included. travelmap gets
no stay table beside it: when Milestone J takes up stay detection it reads these rows rather
than deriving its own — see "Deferred: gap and stay" there.

**Upstream's `visits` carries more than `status`, and this step needs the same two mechanisms.**
`confidence`, with a `confidence_breakdown` beside it, records how sure the detector was;
`detection_version` is what lets the detector be re-run over old rows without touching the ones
a user has already ruled on. Upstream also soft-deletes (`deleted_at`) rather than removing a
row, and joins candidate places through a `place_visits` table with one chosen `place_id` —
neither is needed
until reverse geocoding is, but both explain why `visits` is not shaped like a plain detection
output.

### Step 15: Timeline

- [ ] `GET /api/v1/timeline` (including validation of the 31-day cap), grouping the same
      assembly's entries by day rather than assembling anything of its own

**Blocked on knowing the shape, not on the code.** Upstream's spec defines the response as
`{days: [...]}` where each item is a bare `type: object` with no properties at all, so there is
nothing to implement faithfully against: the real contract lives in the client, which is what
Step 6 exists to observe. The description also says the day covers "visits, tracks, and
**photos**" — and photo APIs are a non-goal in `docs/README.md`, so what this endpoint reports
for photos has to be answered before it can be written.

Note that upstream has no `timelines` table: its timeline is a range query, and the named,
persisted thing is its `trips` — the same split travelmap makes.

**Milestone done when**: the app's timeline and track screens render without breaking, and
settings changed in the app survive a reinstall.

---

## Milestone G — Operations and extensions (optional)

All independent of each other; take them in whatever order the need arises.

- [ ] `POST /api/v1/imports`, `GET /api/v1/imports`, `GET /api/v1/imports/{id}`
      (GPX / GeoJSON / Google Takeout / upstream Dawarich export).
      **Imports go through the `internal/ingest` layer**
- [ ] Reverse geocoding (Nominatim / Photon, rate-limited worker). It runs asynchronously after
      points are inserted, so **on completion update `countries` / `cities` /
      `reverse_geocoded_points` in `daily_stats` for the affected days** (without this the
      corresponding `/stats` values stay 0 — see "Data Model")
- [ ] `POST /api/v1/auth/register`, **open like the browser sign-up screen**, not behind a
      config setting. Two registration routes whose defaults disagree is exactly the
      drift this file's rules exist to stop, and there is no reading under which the API is the
      cautious one: a bot reaches a JSON endpoint more easily than a form
- [ ] `POST /api/v1/owntracks/points`, `POST /api/v1/traccar/points`
- [ ] `GET /api/v1/countries/visited_cities`
- [ ] `Dockerfile`, `docker-compose.yml`, and an example systemd unit (see "Distribution")
- [ ] Backups (`VACUUM INTO`)
- [ ] `GET /metrics`, structured access logs

---

## Milestone H — Web UI

Start once the API has settled. What the screens still need a library for is in "Library Choices
for the Web UI".

The Swarm link itself is done: its own section on the settings page, built directly against the
browser session rather than `api_key` — see "Swarm OAuth linking" in `docs/architecture.md`. It
added no data endpoint, which is what leaves "Open question: how the browser authenticates
against `/api/v1`" for the map screen to answer.

The map, statistics and settings screens keep their bullet form below. How they are rendered is
settled — see Templating and Client-side code under "Technical Decisions" — but what each of
them shows is not, and neither is how the browser authenticates against `/api/v1`, which the map
screen has to answer. Writing a checklist before those are decided would be inventing the plan
rather than recording it.

Steps 35 and 36 are not screens. What is decided is that `html/template` goes, and that is
plannable now precisely because it does not depend on how any screen renders; **which engine
replaces it is Step 36's own decision**, which is the one thing that step is worth reviewing
for. They also do not wait on "start once the API has settled" above: neither touches the API,
and the argument for taking them is that the cost grows with every screen added, so waiting is
what makes them expensive. Take them in order — the pin has to exist before the conversion it
judges.

### Step 35: Pin the current HTML

- [ ] Pin what the browser pages render today, before any conversion touches them.
      `assertGolden` compares JSON bodies and every file in `internal/httpapi/testdata/golden/`
      is a `.json`, so nothing pins this HTML — only scattered `bytes.Contains` assertions in
      `session_internal_test.go`, `login_page_test.go`, `signup_page_test.go` and
      `settings_page_test.go`, none of which is a picture of a whole page. Find every one of
      them rather than trusting this list
- [ ] **Pin states, not URLs.** Four pages render more than four things: `base.html` branches on
      `.Header.SignedIn`, `login.html` on `.Error`, `signup.html` on `.Done` and then on each of
      `.EmailError` / `.PasswordError` / `.ConfirmError`, and `settings.html` on
      `.FoursquareLinked` and `.FoursquareSyncedThrough` — `signup_page.go` alone renders from
      seven places. A branch nobody pinned is what the conversion can drop while Step 36's own
      completion condition still passes
- [ ] **Compare elements, attributes and text, not bytes.** `base.html`'s own indentation and
      newlines reach the response verbatim, and no engine that builds the page from Go
      reproduces them, so a byte comparison would fail on whitespace nobody cares about

**Settles**: what "the same page" means, while `html/template` is still the thing producing it.
Written in the same pull request as the conversion, this pin would be shaped by its output
rather than judging it — which is the whole reason it is a step of its own rather than the first
half of the next one.

**Done when**: removing an element from a template fails a test that named neither the element
nor the template.

### Step 36: Type-safe templates

- [ ] Replace `html/template` with templ (or gomponents). If the engine has a generator it goes
      in `go.mod`'s `tool` directives and is invoked from the `Makefile`; gomponents has none,
      being ordinary Go functions. Either way no step may require installing a binary, so an
      engine whose generator is not a Go tool is not a candidate
- [ ] Convert every template in `internal/httpapi/templates/` and drop `pageTemplate`'s per-page
      `*template.Template` in `internal/httpapi/web.go`
- [ ] Rewrite `docs/architecture.md`'s "HTML rendering" section, sentence by sentence. "Keeps
      deployment a single binary" and "nor a Node build chain" both survive, and the second is
      the promise this whole decision exists to protect. What does not: templates become Go
      code, so "parsed once at startup" describes nothing and the `embed.FS` holds the CSS and
      static files rather than the templates, and "adds neither a code-generation step" is
      exactly what stops being true if templ is the choice. Type checking is what replaces it in
      the rationale

**Settles**: how every screen after this is written.

**Done when**: Step 35's pin passes unchanged, and a template referencing a field the data does
not have fails to compile.

### Still to plan

- [ ] Map screen (render points / tracks for a selected time range), reusing the existing
      `GET /api/v1/points` and `/tracks` without adding UI-only APIs. **This is the step that has
      to answer "Open question: how the browser authenticates against `/api/v1`"**, being the
      first to read data from it. The map library itself is picked and vendored into `embed.FS`
      by Milestone J's Step 41; if this screen is taken first, it does that instead
- [ ] Statistics screen (using `daily_stats`)
- [ ] Settings screen: more sections beyond the Swarm connection already there — timezone, track
      break, etc. — on the page and header link this milestone already built. An import screen
      only if Milestone G's `/api/v1/imports` was implemented

**Done when**: logging in from a browser shows the user's history on a map, and deployment is
still one binary plus one SQLite file.

---

## Milestone I — Swarm check-in collection

**Milestone done when**: with both paths configured, a check-in made in Swarm appears in `checkins`
within seconds, and a check-in added after the fact appears no later than the next periodic fetch,
or a hand-run `foursquare sync` before it.

**Neither of those exercises the fetch path on its own, and this milestone does not ask them to.**
With push configured, either check-in may well arrive by push within seconds — whether it does for
a retroactive one is the open question — so `source` here records which path won a race, not which
paths work. **Isolating the fetch path needs the push secret unset**, so nothing else can have
collected the row — a stricter check than the milestone condition above, which reads as "both
paths are configured and nothing is lost" rather than "the fetch path collects" on its own.

### Step 22: Ambiguous errors and locale drift

Split out of the API client's own step because none of this is decided by the calling convention
or the paging design — it is confirmed empirically, once a client exists to run and a check-in has
arrived by both paths to compare. It extends the client already in `internal/foursquare`, and
depends on a check-in already collected by the push webhook for the locale bullet to compare
against.

- [x] **Branch on `meta.errorType`, never on the status alone**: 403 is both
      `rate_limit_exceeded` and `not_authorized`. A revoked authorisation, which is permanent,
      must not be reported as the transient one
- [ ] **Measure whether the fetch's rendering matches the push's**: fetch a check-in that also
      arrived by push and compare `venue_name`, `country`, `city`, `state` and `category_name`.
      If they differ, take those columns out of what a repeat write refreshes, and record the
      outcome under "Whether the two paths agree on a check-in's language"
- [x] A test that a 403 is distinguished by `errorType` between the two meanings it carries — the
      case the status code alone hides, and the one the client's existing `meta` handling stops
      short of

**Settles**: how this client behaves when Foursquare refuses a request with a status that means
two different things, and what the two collection paths do when they disagree about a check-in's
language.

**Nothing else in the milestone waits on this.** An account that hits either case before this
step lands sees the run fail with an error that already names the `errorType` and the
`requestId`; what is missing is a client that tells a permanent refusal from a transient one.

**Done when**: a fake server answering 403 with `errorType: not_authorized` is reported as a
revoked authorisation rather than as a rate limit, and a check-in observed by both paths either
matches on the five columns or has them recorded as excluded from refresh.

---

## Milestone J — Trips and the timeline

travelmap's own feature, not upstream's: the trip a traveller actually looks at, assembled from
the GPS trace and the Swarm check-ins already being collected. It gets its own table and its own
routes at the top level, never a path under `/api/v1` — see "Keeping the two parts apart" in
`docs/api-notes.md`.

**The MVP deliberately derives nothing.** A trip is a range the user declares; the timeline is
assembled on read from the two tables that already exist. That is what keeps this milestone to
one new table, with no change to `internal/ingest`, `internal/checkin` or `daily_stats`, and no
re-derivation to trigger, invalidate or rebuild.

**There is one timeline model, and the compatibility endpoints are projections of it.**
`internal/timeline` owns the entry type and one assembly over a set of sources; what a surface
returns is decided at the DTO boundary, where every response shape in this project already
stops. So the browser reads entries directly, `GET /api/v1/timeline` groups the same entries by
day, and `/api/v1/visits` and `/api/v1/tracks` render the stay and move entries in upstream's
shape. Their fixed response formats constrain the DTOs, never the model — writing two nearly
identical models because one surface cannot change its JSON is the mistake this paragraph
exists to prevent.

It also accepts a known inaccuracy, and the accepting is the point: with gap detection deferred
(see below), an untracked stretch is drawn on the map as a straight line and its straight-line
distance lands in the timeline's own totals. So the timeline's distances read higher than
`daily_stats`, which excludes exactly those intervals. Nothing else reads these numbers, and
the fix is a step rather than a redesign.

Take them in order. Nothing here depends on Milestone H's Steps 35 and 36, but the screens
this milestone adds are three more pages to convert — the trip list, the trip form and the
timeline screen — so taking the conversion first is what stops them being written twice.

The steps are small on purpose. Two of them settle a convention everything after inherits —
where the trip packages sit, and how client-side code is written and built — and a convention
argued inside a feature diff is a convention nobody reviews.

### Step 37: The trips table

- [ ] `trips` migration, per "Data Model"
- [ ] `store.TripRepository` and its sqlite implementation, plus `storetest.UnavailableTrips`
- [ ] `internal/timeline`, holding trip CRUD now and the assembly from Step 39. **It writes no
      derived state**, so it sits beside `ingest` / `checkin` rather than below them, and only
      `internal/httpapi` imports it. Add it to CLAUDE.md's layering diagram

**Settles**: that a trip is user-owned data and never derived, so nothing rebuilds it and no
config change invalidates it — and where the package that owns trips and the timeline sits.

**Done when**: a trip round-trips through the repository, and dropping the table is what makes
the failure path return 500.

### Step 38: The trip screens

- [ ] `GET /trips`, `GET /trips/new`, `POST /trips`, `GET /trips/{id}`,
      `GET /trips/{id}/edit`, `POST /trips/{id}` and `POST /trips/{id}/delete`, in the browser
      group so they carry the session and CSRF middleware. The form gets its own two GETs
      rather than being embedded: `GET /trips/{id}` is the timeline screen from Step 40, and
      putting an edit form on it would make that screen mean two things
- [ ] The pages those routes render: the trip list, and the form that creates and edits one.
      `GET /trips/{id}` shows the trip itself and carries no timeline until Step 40, which
      renders it by handing the row's range to the one timeline screen
- [ ] Handlers read through `internal/timeline` and receive `internal/model` types, never
      reaching the store directly. **This is a new rule, not what the existing handlers do** —
      `points.go` and `stats.go` read the store themselves — and it applies to the trip and
      timeline routes only. It is the whole cost of keeping a future JSON API, or a separate
      front end, an addition rather than a refactor

**Done when**: a trip can be declared from a browser, edited, deleted, and survives a restart.

### Step 39: Assembling the timeline

- [ ] `CheckinRepository.List(ctx, userID, from, to)`. The repository is write-only today; the
      `checkins_user_id_checked_in_at_idx` index the query needs already exists. Its doc comment
      says `internal/checkin` is the only caller, which stops being true here — reword it to say
      that every *write* goes through `internal/checkin`, which is what the layering rule in
      CLAUDE.md actually requires, and that `internal/timeline` reads
- [ ] `PointRepository.InRange(ctx, userID, from, to)`, for the points between one check-in and
      the next. The existing `List` is paginated and returns a total count for `X-Total-Pages`,
      which does not fit reading an interval whole. **No new index**: this is a time-range scan
      on `points_user_id_timestamp_key`, not a bounding-box query, so the note in
      `0002_points.sql` about adding neither a lat/lon index nor an `R*Tree` still holds
- [ ] The assembly itself: one entry per check-in, with the elapsed time and the distance to the
      next one summed from that interval's points
- [ ] **An entry carries its kind**, even though the MVP produces only one of them. Stay and
      move are what Steps 13 and 14 project into upstream's shapes, and a type that cannot say
      which it is would force those steps to invent the distinction separately — the exact
      duplication this milestone's one-model rule exists to prevent
- [ ] **Take the sources as a set, not as two fixed reads.** The MVP has two — check-ins and
      points — and stay detection later adds `visits`. Written so that a source is added rather
      than the assembly rewritten, "one engine" is a property of the signature instead of a
      promise a later step has to keep
- [ ] Local time per entry from the check-in's own `timezone_offset`, falling back to
      `tracking.timezone` where it is absent. Without this a trip abroad renders entirely in
      the home zone, which is what makes a travel log unreadable

**Settles**: what a timeline entry is — its fields and its kinds — and that everything reading
a time range reads it through here, before a template or a compatibility DTO has an opinion
about it.

**Done when**: table-driven tests cover a check-in with no points around it, points with no
check-in, an entry crossing midnight, and a check-in carrying no `timezone_offset`.

### Step 40: The timeline screen

- [ ] **One screen, taking a range.** `GET /timeline` renders the assembly for whatever range it
      is given, and `GET /trips/{id}` fills that range and the heading from the `trips` row and
      renders the same screen. Two screens over one assembly would only raise the question of
      which of them an annotation belongs on
- [ ] A default range for `GET /timeline` with no parameters — one day — since that is how a
      user finds what is worth declaring as a trip in the first place

**Done when**: opening a trip lists when, where and what in time order, in local time; the same
range reached through `/timeline` shows the same entries, differing only in the heading the trip
row supplies; and a day with check-ins but no points still renders.

### Step 41: The trip map

- [ ] Build the route's GeoJSON from `PointRepository.InRange`, which Step 39 added, and hand it
      to the page in a `<script type="application/json">` block, so the screen adds no data
      endpoint and makes no `/api/v1` call
- [ ] Pick the map library from "Library Choices for the Web UI" and vendor it into `embed.FS`,
      for Milestone H's own map screen as well as this one
- [ ] Write the initialisation in TypeScript, built by esbuild as a Go library. The pipeline
      arrives here rather than as a step of its own because until there is a map to draw there
      is nothing for it to build, and a build step with no input is scaffolding — the same shape
      of argument the "Background work" row above is still owed, where a job table is planned
      and nothing yet queues anything into it

**Settles**: how client-side code is written and built, for every screen after this.

**Done when**: opening a trip draws its route, the initial view fits the whole trip, and
deployment is still one binary plus one SQLite file.

**This is the step that says whether the UI is a document or an application.** It is the first
screen with client-side state of its own, so once it exists there is something real to judge:
whether server-rendered Go plus TypeScript islands is enough, or whether the front end should be
split off. Record the answer in `docs/architecture.md` either way — and if it is a split, that
is when "how the browser authenticates against `/api/v1`" has to be answered for real, and when
travelmap's own JSON namespace has to be named.

### Deferred: gap and stay

Both are real and both are left out on purpose. Each has its own trigger, so neither is a
prerequisite for the other.

**gap — splitting on `tracking.track_break_minutes`.** Take it when the straight lines drawn
across untracked stretches start to mislead on the map, or when the timeline's distances
disagreeing with `daily_stats` becomes a problem rather than a footnote. It cuts the polyline at
the break, excludes those intervals from the distance the way `daily_stats` already does, and
lets a row say "not recorded" instead of a number that is not one. **Step 13's
`GET /api/v1/tracks` is the same computation**, so that endpoint arrives with it — and note that
the splitting is computed on read, so it needs no job to run it. That matters beyond Step 13:
the "Background work" row above still plans a job table, and
`docs/architecture.md` has since settled that periodic work is a ticker goroutine with no job
table. Track splitting was the one per-item consumer that would have justified one, and
computing it on read removes it, so whichever step lands first has to say whether that row
survives at all.

**stay — dwell detection.** Take it when a duration is wanted (a check-in is an instant, so the
MVP can say "arrived at 11:27" but never "stayed 43 minutes"), or when places nobody checked
into — the hotel slept in, the shop browsed — should appear on the timeline at all. This is the
expensive one, and the only part of the original design with thresholds to tune and failure
modes to live with — but **the detector itself is Step 14's**, and that step's checklist carries
what it costs. It fills `visits`, upstream's concept in upstream's shape, and travelmap gets no
stay table beside it: two detectors would disagree, and a second table would make "which stay" a
question nobody should have to answer.

What is left here is the fusing: the timeline reads those rows alongside `checkins`, exactly as
it already reads `points` alongside them, and `internal/timeline` moves below `internal/ingest`
and `internal/checkin` in the layering if reading them means being re-derived with them.
`docs/database.md` already records why a Swarm check-in is not one of these rows.

**These two cannot be split the way the rest of this file's steps can.** Step 14's first bullet
is the detector and its second is a projection of the assembly's stay entries, so finishing that
step needs the fusing described here — and the fusing needs the detector. Whichever is picked up
first carries the other with it, and the pair is one step's worth of work however it is
numbered.

**`status` is the one thing the fusing has to interpret**, and Step 14 can leave it alone because
upstream's own endpoint just reports the column. **The assembly emits every stay and carries
`status` on the entry; the browser timeline is what hides a `declined` one**, since a stay the
user has ruled out is not something to put in a travel log. Filtering inside the assembly would
break `GET /api/v1/visits`, which has to report those rows. `suggested` and `confirmed` both
show, kept apart so a screen can mark which of them a human has looked at.
The asymmetry to be honest about is that **`status` cannot be moved while visit editing is out
of scope** — nothing in travelmap can turn `suggested` into `confirmed` or `declined`, so the
column reads a constant and the timeline's quality is whatever the detector's is. That is a
consequence of the current scope, not a position: implementing `POST /api/v1/visits/bulk_update`
is the answer the moment a wrong stay in a travel log is worth removing, and nothing here argues
against doing it.

Neither is blocked by the MVP's shape. `trips` does not change, annotations carry their own
timestamp rather than pointing at anything derived, and the assembly moving from
computed-on-read to read-from-a-table stays inside `internal/timeline`.

**A trip proposal and a suggested visit are the same shape at different layers, and should not
be built as one thing.** Both are a machine guess a human rules on, and both have to survive the
detector being re-run. But a suggested visit proposes *content* — a stay that either happened or
did not — while a trip candidate proposes a *selection over* content, a range worth naming.
Upstream keeps its own equivalents apart for the same reason. So if trip candidates ever need to
remember a dismissal, that is a decision about trip candidates and gets its own state; reusing
`visits.status` for it would be the mistake `docs/database.md` already declined to make when it
kept Swarm check-ins out of `visits`.

### Deferred: notes and photos

The point of a trip is eventually to carry what the traveller wrote and photographed, not only
where they went. Neither is planned as a step, and photos may not be built at all — but the one
decision they need is already in "Technical Decisions" above: **both carry their own timestamp,
and may also point at a trip, never at the id of a derived row.** It is settled now because it
is the part that cannot be changed cheaply once rows exist.

Upstream's `notes` is a worked example of most of it, and a caution on the rest. It carries
`noted_at`, a polymorphic `attachable` and a `lonlat`, requires the timestamp in the model even
though the column is nullable, and validates that an attached note's date falls inside the
attachable's own range — all worth copying. But its `ALLOWED_ATTACHABLE_TYPES` is
`Trip Area Visit Place`, so it permits anchoring to a `Visit`, which is exactly the derived row
the rule above keeps out. Narrowing that set is the deliberate difference.

**Both run into `docs/README.md`'s non-goals first, and that is what has to be settled before
either can be a step** — not the schema, which is the easy part.

`Notes` is named there outright, among "Areas, Places, Notes, Tags, Digests, Insights —
upstream's own concepts". The carve-out beside it covers travelmap's own check-ins and nothing
else. A note anchored to a moment on a trip is arguably not upstream's Notes at all, but the
list does not say that, so either the carve-out is widened to say what travelmap's own
annotations are, or the feature is dropped.

Photos hit the neighbouring line, "Immich / Photoprism integration and photo-related APIs".
Storing travelmap's own images is arguably not what that excludes either, but again the wording
does not say so.

**Milestone done when**: a trip declared in a browser shows its check-ins in order and its route
on a map, and deployment is still one binary plus one SQLite file.

## Risks and Open Questions

- **Because the iOS app is closed source, the endpoints it actually calls and the required
  fields are unknown.** Use the Step 6 request-logging middleware to observe traffic from a
  real device and identify unimplemented endpoints.
- **If social login is mandatory, Milestone B may not be completable.** The spec has
  `POST /api/v1/auth/apple` (body `{id_token, nonce}`) and `POST /api/v1/auth/google`. If the
  iOS app forces Sign in with Apple, `POST /api/v1/auth/login` alone will not get to a
  successful connection. **Still open**: email-and-password login and the API-key middleware are
  built, but answering this needs a real device, so it is the first thing to read out of Step 6's
  request log. If it turns out to be required, add verification of
  `id_token` against Apple's public keys and association with an existing user (whether to
  auto-create users on a self-hosted instance is a separate decision).
- The app checks a minimum server version. The community Android client reads
  `x-dawarich-version` and refuses to run against a server below its floor; the official app is
  assumed to do something similar, so confirm the accepted value against a real device.
  The health endpoint reports **`1.12.2`**, upstream's `.app_version` on `master` as of
  2026-08-18. It is a compatibility claim, not this server's own version — `travelmap --version`
  reports the build — so it is raised when this server is verified against a newer upstream, not
  on every release of ours.
- **Milestone I depends on a beta API with one supplier.** Foursquare v2 is not on a published
  retirement schedule: what was retired on 15 May 2026 was legacy **v3**, and the pricing change
  of 1 June 2026 explicitly keeps the checkins, lists, tastes, tips and users endpoints free — an
  API being priced for the future is an API being kept. What remains is that the surface is
  published as a public beta, under a "Public Beta End User License Agreement", and that no second
  source of this data exists. Two things are the hedge and both are already in the design: the push
  path is a separate mechanism, which one would expect to outlive the fetch endpoint though nothing
  documents that either, and `checkins.raw` means a later column can be derived from stored payloads
  instead of re-fetching. The `errorType: deprecated` check the client already makes is the early
  warning.
- **What each collection path actually sees is only partly known.** Whether a push fires for a
  check-in added after the fact, and whether one fires for an edit to a check-in already stored,
  are both undocumented and unobserved. Nothing in the design rests on either answer — the fetch
  window is argued from what a cursor cannot see, under "Fetching Swarm check-ins" in
  `docs/architecture.md` — and nothing should start to. One retroactive check-in, made with both
  paths configured and `source` read afterwards, settles both questions at once.
- **The push URL has to be reachable over HTTPS from the internet**, which a self-hosted
  instance behind a reverse proxy can do but a laptop cannot. Until then the fetch path alone
  collects check-ins, just with a delay. The README already says so, in the check-in
  configuration section the push webhook opened.
- **The push secret belongs to the Foursquare application, not to a user.** Keep the split it
  implies: the secret is server configuration, and identifying whose check-in arrived is
  `foursquare_user_id`'s job. Deriving a user from the secret would break the moment a second
  person connects.
- **No rate limiting anywhere a request computes bcrypt: `POST /api/v1/auth/login`, the browser's
  own `/login`, and `/signup` all let a caller ask as many times as they like.** Each spends one
  bcrypt hash per request and refuses a wrong password (or an email already taken) at the same
  cost as a right one, which narrows the attack to throughput rather than timing, but nothing here
  bounds that throughput itself — a caller can simply keep asking. `auth.Register` hashes the
  password before checking whether the email is taken, so a `/signup` repeated against an existing
  address still pays the full cost every time. None of this bites on a LAN or behind a reverse
  proxy that authenticates first, which is how a personal instance is normally run; it bites on
  one published to the internet, which the Swarm push webhook — already shipped — is a reason to
  do. Unmitigated today; adding a per-address or per-account attempt limiter is new work with no
  home in the current plan.
- **No security-related HTTP headers on the browser routes** — no `Strict-Transport-Security`,
  `X-Content-Type-Options`, `X-Frame-Options` or `Content-Security-Policy`. `/api/v1` carries only
  the Dawarich compatibility headers, which are a different thing. Left for a later Milestone H
  step if a browser surface reachable from the internet turns out to need them.
- **Revisit whether `internal/ingest` should be named `internal/usecase` (or `service`) instead.**
  `usecase`/`service` are the more familiar names for this layer in most Go codebases; `ingest`
  was picked because this milestone's whole job is literally ingesting device locations, which
  does not generalise as an argument once `internal/checkin` (Milestone I) is the same shape of
  layer over a different kind of write. Revisit once `internal/checkin` exists alongside
  `internal/ingest`, so the comparison is between two real packages rather than a naming
  preference argued in the abstract. A rename this late only costs an import-path edit — nothing
  in the layering itself depends on the name — so there is no cost to waiting for the evidence.
