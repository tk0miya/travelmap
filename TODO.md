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
| Swarm check-ins | Collected by webhook push, with a periodic API fetch as the backstop | Push is immediate but nothing documented makes it reliable: Foursquare publishes no retry, records a timed-out push as a failure, and reaches only a public HTTPS endpoint. What it does after a failure is not documented at all. Fetching alone would lag every check-in by up to a poll interval. **What each path does and does not see is only partly known** — whether a push fires for a check-in added after the fact, or for an edit to one already stored, is undocumented — which is itself a reason to run both (Milestone I) |
| Foursquare API version | v2 (`/v2/users/self/checkins`), with `v=` pinned as a constant and `m=swarm` | v2 is what returns Swarm check-ins, and it is current rather than abandoned: it is documented today as the "Personalization APIs", and the pricing change of 1 June 2026 names the checkins and users endpoints as remaining free while putting the venues endpoints behind paid tiers. `v=` is a date Foursquare uses to freeze response shape, so it is a constant raised deliberately after checking behaviour, never "today". `m` asks for the Swarm perspective rather than the Foursquare one; what it changes on this endpoint is untested, and its documented "required" status is contradicted by working clients — see "Fetching check-ins" |
| Scheduling | A ticker goroutine plus a fixed lookback window, and **no job table** | The periodic fetch is one cron-like task with no queue of items, so the job table in the "Background work" row above would be scaffolding with nothing in it. Step 13's track splitting is the first genuine per-item consumer; the decision stands, its first use just is not here |
| Browser CSRF | Standard `net/http.CrossOriginProtection`, on the browser routes only | Present in the toolchain `go.mod` already names (`go 1.26.6`), and its `Handler` is a plain `func(http.Handler) http.Handler`, so this costs no dependency, which is what needed confirming before it could be chosen. `/api/v1` keeps Bearer / `api_key` only and needs none |
| Sign-up | `GET/POST /signup`, open to anyone: no environment variable, no invite code, no first-user-only rule | A gate is one more setting to get wrong before the first login works, on a server whose first account is the operator's own. What open sign-up means for an instance reachable from the internet is recorded under "Risks and Open Questions" rather than answered here with a default nobody asked for |

These are the defaults as of planning. If any turns out to be wrong during implementation,
change it — after updating this file.

## Data Model

Columns and indexes for the tables already migrated are in `internal/store/sqlite/schema.sql`,
kept current by `TestSchema`; the rationale for how a column or index is shaped is a comment in
the migration that adds it. Behaviour that spans tables or does not attach to a single column —
invariants, algorithms, config effects — is in `docs/database.md`.

## Dawarich API Compatibility Notes

Upstream quirks for endpoints not yet implemented. For an endpoint already implemented,
`docs/api-notes.md` covers it — this section holds only what is still ahead.

### Per-endpoint

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

External behaviour that must be known before implementing Milestone I, in the same spirit as
"Dawarich API Compatibility Notes".

Four sources feed this section and they are kept apart on purpose, because they do not carry equal
weight and this milestone has already been planned once on the assumption that they did.

1. **A real push captured with a webhook recorder** — what the push webhook's own design and
   `docs/api-notes.md` are built on.
2. **Foursquare's own reference**, at `https://docs.foursquare.com/developer/reference/`, which
   documents v2 today under the name **Personalization APIs**, labelled a public beta.
3. **Foursquare's 2014 announcement** of the Foursquare/Swarm split, which the reference does not
   link and which is the only source for the `m` parameter.
4. **Long-lived Swarm importers**, which demonstrate that a parameter still works without
   documenting what it does.

Where an observation and the reference disagree the observation wins, and the disagreement is
recorded rather than smoothed over — there are several, and each one is a thing a reader would
otherwise trip on. Where only (3) or (4) supports something, the note says so rather than
promoting it.

The reference is also **an incomplete subset of the API it describes**: it omits fields that the
captured push demonstrably carries, and it omits parameters that working clients still pass. So
"not in the reference" is not "not there" — but for a parameter it is not "still there" either,
only "not settled here", and the notes below say which of those three readings each fact is.

Pages read on 2026-08-24, all under `https://docs.foursquare.com/developer/`:
`reference/get-user-checkins`, `reference/get-checkin-details`, `reference/create-a-checkin`,
`reference/get-user-details`, `reference/v2-authentication`, `reference/real-time-view`,
`reference/personalization-api-overview`, `reference/personalization-apis-errors`,
`reference/personalization-apis-rate-limits`, `reference/personalization-apis-localization`,
`reference/upcoming-changes`, `docs/configure-server-webhooks`. The `m` parameter appears in none
of them. It is documented in Foursquare's 2014 announcement of the Foursquare/Swarm split, which
the reference does not link and which working clients contradict — see "Fetching check-ins".

### Fetching check-ins

Read off the reference rather than observed, so Step 19 confirms all of it against the live API.
Where the reference is silent this section says so rather than filling the gap.

```
GET /v2/users/self/checkins?v=<pinned date>&m=swarm&limit=250&sort=newestfirst
        &afterTimestamp=<window start>&beforeTimestamp=<paging cursor>
Host: api.foursquare.com
Authorization: Bearer <access token>
```

- **`v` is required**, a `YYYYMMDD` date, and the reference marks it so.
- **`m` takes `foursquare` or `swarm`** and is the "mode" parameter Foursquare added when it split
  the two apps, so that one endpoint can answer with the Foursquare perspective (tips in the
  response) or the Swarm one (check-ins). Its status needs stating carefully, because the two
  sources disagree: the 2014 announcement calls it "a new required parameter" for every
  `v >= 20140806`, while importers running `v` dates years past that omit `m` entirely and get
  check-ins back. So **send `m=swarm` for what it selects, not because the API refuses without
  it** — and do not treat its absence as the error the announcement implies. What it changes on
  this endpoint is untested: the importers that omit it parse the same fields this design reads,
  which is evidence that the check-in shape does not depend on it. Sending it is the cheap way to
  ask for the perspective this milestone wants, not a fix for anything observed.
- **The token goes in an `Authorization: Bearer` header** — a choice, and an untested one on this
  endpoint. What the endpoint's own parameter table names is the `oauth_token` query parameter; the
  `Bearer` header is shown on the authentication page, for the API in general. The header is
  preferred anyway, because a credential in a URL ends up in proxy logs, error messages and
  anything that echoes a request line. But **if the header is not accepted here every request is a
  401**, so Step 19 confirms it, and the documented fallback is the query parameter.
- `limit` **defaults to 20 and caps at 250**. `offset` defaults to 0 with no documented maximum.
- `sort` (`newestfirst` or `oldestfirst`), `afterTimestamp` and `beforeTimestamp` are **absent
  from the current reference page.** What is known is that the long-lived Swarm importers pass them
  and get check-ins back; nothing consulted here documents them, so Step 19 is confirming their
  existence, not their behaviour. If any of them is gone, the fetch pages by `offset` over the
  whole history and filters by `createdAt` here instead — slower and heavier, and it can step over
  a check-in that arrives mid-run, which the next run's window then picks up anyway. Degraded,
  not broken.
- The response is
  `{"meta": {…}, "notifications": [...], "response": {"checkins": {"count": N, "items": [...]}}}`.
  The reference calls `count` the "total number of user checkins". **Whether it narrows when
  `afterTimestamp` is set is untested**, so it is a progress hint for a backfill and nothing keys
  off it — least of all a decision to stop paging.

**Page with `beforeTimestamp`, not `offset`.** `offset` is the documented one, and it works, but it
walks a list that an incoming check-in pushes along underneath it, so a check-in arriving mid-run
can be stepped over. A timestamp cursor pins the page to the data instead of to a position.
`beforeTimestamp` is not in the reference, so Step 19 confirms it exists before any of this
matters. **The rule below is defined here and nowhere else**; Step 19's checklist is what
implements it, and any other mention of paging points back to this paragraph.

**The cursor is the page's oldest `createdAt` plus one second.** Not that timestamp itself, and
certainly not one second earlier. Whether `beforeTimestamp` is inclusive or exclusive is
undocumented, and the two disagree about the boundary second: if it is exclusive, a cursor of `T`
excludes every other check-in that also happened at `T`, so a page boundary landing on a busy
second silently loses the rest of it. `T + 1` is inside the page under either reading, so — unless
a single second holds `limit` check-ins or more — nothing is skipped either way. It re-reads the
boundary second, and that overlap costs nothing: this is an upsert on `foursquare_checkin_id`, and
the run's own id set filters the repeat before it is written.

**A short page ends the run. A full page that did not advance is an error, not an end.** Those
are two different conditions, and conflating them is how this loop goes wrong.

- A page **shorter than `limit`** is the ordinary end of the data. It is the normal terminator, and
  it is why the hourly fetch of a quiet fortnight costs one request rather than two.
- A **full page whose cursor does not come out strictly lower than the one that fetched it** has
  made no progress, and repeating the request would return the same rows forever. Two things cause
  it: `limit` or more check-ins sharing one second, and — the one worth building the check for —
  **`beforeTimestamp` being accepted and ignored**, which makes every page the same newest 250.
- So a run that cannot advance **fails loudly and leaves `synced_through` alone**. Ending it
  quietly would let an ignored `beforeTimestamp` look like a successful sync of one page, forever.
- The check compares a page against **the cursor that fetched it**, so it cannot say anything about
  the first request of a run, which has no cursor.

**`sort` being ignored is the failure this cannot catch, and it is not a runtime check.** If the
effective selection is oldest-first, the first request returns the window's *oldest* 250, the cursor
lands just above the window's lower bound, and the second request returns the few check-ins in that
boundary second — a short page, a clean stop, and a run that collected a fortnight-old page and
never reached this week. Nothing in the loop can tell that apart from a genuinely quiet window.

Within-page ordering is no help either: the cursor is the page's **minimum** `createdAt`, not its
last element, precisely so that a server returning the right rows in an unspecified order still
pages correctly. That robustness is what removes the signal.

So this one is settled once, by observation, in Step 19: **against a window known to hold more than
`limit` check-ins, check whether the first page contains the most recent one.** If it does not,
`sort` is not being honoured and the paging scheme needs rethinking before the fetch path is
trusted — which is why it is a `Confirms` item there and not a runtime branch here.

**Read `meta`, not only the HTTP status.** Foursquare's own wording is that it "attempts to use
appropriate HTTP status codes", repeated in `meta.code` — an intention, not a guarantee, and the
same page names a case that arrives on a **200**: `errorType: deprecated`, the notice that the
pinned `v` or a field this server reads is on its way out. So the status is usually enough and
`meta` is what makes it reliable, and `deprecated` is the one warning this design will ever get
before a pinned version stops answering. Log it loudly rather than dropping it. `meta` also
carries `requestId`, `errorDetail` and sometimes `errorMessage`; log `requestId` with every
failure, because it is what a support question has to quote.

**Branch on `errorType`, not on the status.** Two mappings make that necessary rather than tidy.
`rate_limit_exceeded` is **403**, where a client would guess 429, while 429 is `quota_exceeded` —
documented as the daily call quota. And 403 is shared: `not_authorized` arrives with the same
status, which is what a revoked authorisation looks like. A client that reads every 403 as the
rate limit backs off forever against an account that will never answer again, and a client that
reads only 429 as "slow down" reads an exhausted hourly limit as a permissions problem.

### What the fetch path costs

- **500 authenticated requests per hour per OAuth token**, counted per top-level resource group,
  so every check-in call for one linked account shares one budget of 500. Responses carry
  `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset` (a timestamp). Over the
  limit the API answers 403 with an empty response object.
- **Neither the interval nor a backfill comes near that.** The hourly fetch of a 14-day window is
  a single request per account per hour for anyone checking in fewer than 250 times a fortnight —
  one, not two, because a short page ends the run without a confirming request. Even a full
  backfill would need 125,000 stored check-ins in one account to spend 500 requests inside an hour.
  So the headers are read not because the limit is close but because a backfill is the only run
  that pages without pausing, and the numbers are already in every response — the cheapest
  possible guard against a limit that changes, or a paging loop that does not terminate.
- **The endpoints this milestone uses are free.** Foursquare's pricing change of 1 June 2026 puts
  `/v2/venues/*` behind premium and pro tiers and names checkins, lists, tastes, tips and users as
  remaining without charge. Nothing here calls a venue endpoint — the venue arrives inside the
  check-in — and keeping that true is a reason not to add one casually.

### Localisation is left unset on both paths

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

So the fetch is decided on what happens if the guess is wrong, not on which reading is right. A
repeat write refreshes `venue_name`, `country`, `city`, `state` and `category_name`, so any
disagreement between the two paths shows up as those columns flipping language on every write.
**The fetch therefore sends no locale**: it is the only setting that does not assert an answer, and
the push has no locale to match it against anyway.

**Step 22 measures it rather than trusting it.** Fetch a check-in that also arrived by push and
compare those five columns. If they agree, this section is settled and says so. **If they differ,
the fix is not a locale setting** — no setting can track whatever the push does — but removing
those columns from what a repeat write refreshes, leaving the first writer's rendering in place.
`cc` is stable under either outcome, per "checkins" in docs/database.md.

### The periodic fetch takes a window, not a cursor

Whether a push fires for a check-in added after the fact is **neither documented nor observed** —
"every time one of your users checks in" could well cover it. Nothing here may rest on the answer,
in either direction, until one is observed.

The window does not need it. Two things a high-water-mark cursor cannot see, both of which happen
whatever push does. A check-in whose own timestamp sorts *before* the stored cursor is skipped,
and then skipped forever — which is the shape Swarm's retro check-in flow produces. That flow is
**app behaviour, not an API contract**: nothing in the reference describes it, and
`/v2/checkins/add` has no time parameter at all, so how the app dates such a check-in is exactly
the open question at the end of this section. And an **edit** to a check-in already stored moves no
timestamp at all, so a cursor never revisits it; `editableUntil` says edits are expected long after
the fact.

So **re-fetch a fixed window on every run** and let the unique index on
`foursquare_checkin_id` absorb the overlap.

- The window is `TRAVELMAP_FOURSQUARE_SYNC_LOOKBACK_DAYS` (default 14) back from now, sent as
  `afterTimestamp`. A run's first request carries no `beforeTimestamp`; the rest carry the cursor
  defined under "Page with `beforeTimestamp`, not `offset`", which is where that rule lives
- `foursquare_accounts.synced_through` advances on success; what it is and is not used for is
  under "foursquare_accounts" in docs/database.md
- Which columns a repeat write keeps and which it refreshes is under "checkins" there too

**Open, and the documentation does not close it**: whether a retroactive check-in's `createdAt` is
the visit time or the time it was created. The reference describes `createdAt` only as a "UNIX
timestamp in seconds since Epoch", and the retro check-in has no API equivalent to compare against
— `/v2/checkins/add` takes `venueId` and `shout` and **no time parameter at all**, so a check-in
this API creates is always "now" and cannot demonstrate the other case. The window design above is
correct either way, so it blocks nothing, but it decides what `checked_in_at` means. Settle it by
observing one retroactive check-in and record the answer here; the same observation settles whether
a push fires for one.

## Library Choices for the Web UI

CSRF is **settled** — it has a row under "Technical Decisions" above, carrying the measurement
that settled it, and Step 26 implements it. Sessions and HTML rendering are settled too, and
already implemented; their rows moved to `docs/architecture.md`, which also covers the router,
for the reasoning that applies here as well. What is left undecided is what the remaining screens
need.

**Still open, for Milestone H's remaining screens**

| Purpose | Candidate | Notes |
| --- | --- | --- |
| Page updates | htmx | Avoids pulling in a Node build chain |
| Map rendering | MapLibre GL JS or Leaflet | The one place JS is unavoidable. Vendor it into `embed.FS` rather than using a CDN, to keep a single binary |

An SPA (React, etc.) can also keep the single-binary property by embedding the build output in
`embed.FS`. The only trade-off there is needing a Node build chain, and the router choice is the
same either way.

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

**Steps 26 to 31 do not settle this and do not need to**: none of them adds a data endpoint, so
`/api/v1` is left exactly as it is and keeps needing no CSRF protection. It is the first screen
that reads a point — the map — that has to answer it. The session middleware already in place is
what makes (a) cheap when that day comes: accepting the cookie there is one more branch in
`authenticate`, and `CrossOriginProtection` can then be moved from the browser group up to the
whole server.

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

**A step's number says when it was planned, not when to take it.** Milestone H's Steps 23 to 31
were planned after Milestone I's 18 to 22 and so carry higher numbers while appearing earlier in
this file, and one of them (Step 30) depends on a step in the other milestone. What order to take
them in is what each milestone's own ordering note says, never the numbers.

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
`TRAVELMAP_DEBUG_LOG_REQUESTS=1`, add it in the app, let it record and browse — and list the
method, the path and the query parameters of every line, including the 404s. Diff that against
the six endpoints under "About the iOS app": what is here and not there is what the official
app needs and the community client does not, and it is the input to Milestone F's ordering.

---

## Milestone E — Browsing

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
- [ ] **Extend the incremental-update/`recalculate` agreement test to mutations other than
      insert**: the incremental update and `recalculate` still agree — **a day whose points are all deleted
      so the row is removed**, and **deleting or updating only some of a day's points so that
      `countries` / `cities` shrink**

**Done when**: deleting a point is reflected in `/stats` immediately.

> **Completing Milestone E satisfies the project's requirement: recording and browsing location
> history from the iPhone app.** Everything after this makes the app's remaining screens work,
> or is operational.

---

## Milestone F — The app's remaining screens

Steps 13, 14 and 16 are independent and can run in parallel. Step 15 needs 13 and 14.

Step 16 in fact only needs authentication, so it can be pulled forward at any time.

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
      the hard-coded settings defaults in `internal/httpapi` go away with it

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
- [ ] `POST /api/v1/auth/register`, **open like the browser sign-up Step 29 adds**, not behind
      an environment variable. Two registration routes whose defaults disagree is exactly the
      drift this file's rules exist to stop, and there is no reading under which the API is the
      cautious one: a bot reaches a JSON endpoint more easily than a form
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

Start once the API has settled. What the screens still need a library for is in "Library Choices
for the Web UI"; CSRF is already settled under "Technical Decisions".

Steps 26 to 31 are the browser's way in: a login screen, a sign-up screen, and the Swarm link,
which a browser is the only sensible place to start from — the `sessions` table and its
repository, the HTML route group and its one page, and the session middleware are already in
place. **They add no data endpoint**, which is what leaves "Open question: how the browser
authenticates against `/api/v1`" for the map screen to answer rather than this half.

The map, statistics and settings screens keep their bullet form below. They are not planned yet,
and writing a checklist for a screen whose rendering approach is undecided would be inventing the
plan rather than recording it.

### Ordering

```
The session middleware ─→ Step 26 (login) ─┐
                                           ├─→ Step 29 (sign-up)
Step 28 (auth.Register) ───────────────────┘

Step 26 + Milestone I's Step 20 ─→ Step 30 (Swarm over a session) ─→ Step 31 (the Swarm page)
```

Step 27 (the session sweep) can be taken at any time, needing nothing but the sessions store
already in place. Step 28 is independent of everything above it and can be taken in parallel. The
shortest path to logging in from a browser is now Step 26 alone; everything else hangs off that.

### Step 26: The login screen

Follows the session middleware, already in place. The first step a person can act on.

- [ ] `GET /login` renders the form, `POST /login` submits it, `POST /logout` ends the session.
      **Logout is a POST**, so an `<img>` on another page cannot end someone's session
- [ ] `net/http.CrossOriginProtection` on the browser group, and **only** there: `/api/v1` is
      Bearer / `api_key` only, so nothing it serves can be driven by a cross-origin form. Whether
      CSRF costs a dependency is already settled under "Technical Decisions"; what this step
      settles is where the protection is attached
- [ ] `RenewToken` on a successful login, **before** the user id goes into the session. A session
      token minted before the browser authenticated must not still be the one it holds afterwards
- [ ] `POST /logout` calls `Destroy`, deleting the row. Clearing the cookie alone would leave a
      session anyone holding the old token could still present
- [ ] A refused login says what `POST /api/v1/auth/login` says and **takes as long**, through the
      existing `auth.CheckAbsentPassword`. It re-renders the form and sets no cookie
- [ ] README: `/login`, and that it accepts an account made with `travelmap user create`
- [ ] Tests: a good login sets a cookie that then names the account on `GET /`; a wrong password
      re-renders the form and sets none; after logout the old cookie is refused **and its row is
      gone**; a `POST /login` carrying `Sec-Fetch-Site: cross-site` is refused; and the session
      token differs before and after login, which is the only way to see `RenewToken` worked

**Settles**: how a form reports a field error, where a successful post redirects to, and that CSRF
protection is attached to the browser group rather than the whole server.

**Done when**: an account made with `travelmap user create` logs in at
`http://localhost:3000/login` and lands on a page naming it; the cookie still works after
`travelmap serve` is restarted; `POST /logout` returns the browser to the form.

### Step 27: The expired-session sweep

Can be taken any time, needing nothing but the sessions store already in place; it does not wait
for a login to exist. Small, but it answers the same question Step 21 answers for Milestone I —
how a background worker in this server starts and stops — so whichever of the two lands first is
the one that settles it, and the second follows it rather than deciding again.

- [ ] A ticker calling `store.Sessions().DeleteExpired`, started from `cmd/travelmap/serve.go` —
      the only place holding both the signal-cancelled context and the concrete store, which is
      why Step 21's worker starts there too
- [ ] It stops with the server, on the same cancelled context, so a sweep in flight is not left
      writing into a closing database
- [ ] The interval is a constant, **not a setting**, and the code says why: `ByToken` already
      filters on `expiry`, so a late sweep leaves no session usable — only rows on disk. There is
      nothing for an operator to tune that would change behaviour
- [ ] Tests: an expired row is deleted and an unexpired one is not; a cancelled context stops the
      ticker. **Not Step 21's "a restart loses no tick"** — that test exists because a fetch window
      has to cover the time the process was down, and a sweep has no window to miss

**Settles**: the lifecycle of a background worker in this server, **without a job table**, per the
"Scheduling" row under "Technical Decisions" — **if it lands before Step 21**, which says the same
of itself. Whichever is second follows the first rather than deciding again, and moves nothing.

**Done when**: with one expired row in `sessions`, starting the server removes it on the next tick.

### Step 28: `auth.Register`

Nothing here blocks it, so it can be taken at any point; Step 29 is the one thing that waits on
it. **Nothing observable changes** — it is a move between layers, and it is what stops Step 29
from being a second way of creating an account.

- [ ] `auth.Register(ctx, users, email, password)`: normalise, hash, issue an API key, create.
      `internal/auth` gains `store` and `model` imports, which its tier allows
- [ ] `travelmap user create` rewritten to call it. CLAUDE.md makes `cmd/travelmap` wiring only,
      and a subcommand that grows business logic hands it to the package that owns the behaviour
- [ ] `user create` keeps refusing a password bcrypt will not take **before it opens the
      database**, by checking `auth.MinPasswordLength` and `MaxPasswordLength` itself. Those
      constants are exported for exactly this, and their doc comment already says the caller
      asking for a password is the one that has to state the bounds
- [ ] `internal/auth/doc.go`: that this package now issues accounts as well as checking
      credentials
- [ ] Tests: `user create`'s existing tests pass unchanged, which is the whole claim of this step;
      plus table-driven tests for `Register` itself — a malformed address, a duplicate answering
      `store.ErrConflict`, and both password bounds

**Settles**: that one code path creates every account, and that `internal/auth` owns it.

**Done when**: `travelmap user create` still creates a user and prints its API key, and
`make check` passes.

### Step 29: The sign-up screen

Follows Steps 26 and 28.

- [ ] `GET /signup` and `POST /signup` on the browser group. **Open to anyone** — no environment
      variable, no invite code, no first-user-only rule, per the "Sign-up" row under "Technical
      Decisions"
- [ ] The form re-renders with the reason against the field it belongs to: an address that is not
      one, an address already taken (`store.ErrConflict`), a password outside the bounds.
      **State the bounds in bytes**, because bcrypt's 72 is a byte limit and a Japanese password
      reaches it at 24 characters — a form saying "72 characters" would be wrong for the users
      most likely to hit it
- [ ] A confirmation field, compared before anything is written
- [ ] `RenewToken`, then sign the new account in, so sign-up does not end at a login form
- [ ] The page it lands on shows the account's **API key**. Sign-up replaces
      `travelmap user create`, whose entire output is that key, and without it someone who signed
      up in a browser has no way to configure the phone app
- [ ] Links between `GET /`, the login form and the sign-up form, so none of the three is
      reachable only by typing its path
- [ ] Two comments go false with this step and both say the same thing, so neither can be left:
      **`0001_users.sql`'s leading comment on `users`** ("there is no sign-up flow and no columns
      for one") and **`model.User`'s doc comment** ("A self-hosted instance issues its users from
      the command line, so a User is created once and then only read"). Rewrite both: there is a
      sign-up flow now, it still needs no column of its own, and the CLI is one path rather than
      the path. The migration one sits outside every statement, which is what makes a merged
      migration editable there
- [ ] README: sign-up as how the first account is made, `travelmap user create` as the one for a
      script or a unit file
- [ ] `docs/architecture.md`: the "User management" row records that an account can now be made in
      the browser and keeps the CLI as the other path, Step 28 leaving `travelmap user create`
      working. It also drops "`auth/register` optional behind an env var", which describes
      something not implemented and which Milestone G's own bullet does not say either
- [ ] Tests: a sign-up against an empty database creates one user, leaves a working session, and
      the API key the page shows authenticates `GET /api/v1/users/me`; a second sign-up with the
      same address re-renders the form and writes nothing; a mismatched confirmation writes
      nothing; the session token differs before and after

**Settles**: that an account can be created without shell access, and that registration is open.

**Done when**: a freshly migrated database with no users gets its first account entirely from a
browser, and the API key that page shows authenticates `GET /api/v1/users/me`.

### Step 30: Starting the Swarm flow from a browser session

Follows Step 26, and Milestone I's **Step 20**, which builds the OAuth exchange itself. This step
adds no new exchange — it changes what names the travelmap user on the way in and on the way back,
and it is what Step 20's "A limitation to accept knowingly" promises.

**If Step 20 was taken after Step 26**, as Milestone I's own ordering note suggests, it wrote the
session version directly and there is no `api_key` leg to move or README line to delete. What is
left of this step is then the callback checking the session and `state` against each other, and the
tests for it — a much smaller change. Take that reading rather than looking for an `api_key` route
that was never built.

- [ ] Move `GET /foursquare/oauth/start` off `authenticate` / `requireUser` and onto the browser
      group's session. **The API key stops travelling in a query string**: Step 20 accepts that
      knowingly only because no session exists yet, and it puts the key in browser history and in
      the `Referer` of the redirect to Foursquare. This step is the one that removes it
- [ ] `GET /foursquare/oauth/callback` **also gets the session now**, which Step 20 could not
      assume. The browser returns from Foursquare by a top-level GET navigation, and a
      `SameSite=Lax` cookie is sent on exactly that — so the callback can require the session's
      user and the user `state` names to be **the same user**, instead of resting on `state`
      alone. Verify the cookie really does arrive before relying on it; if it does not, `state`
      alone is still Step 20's design and nothing is lost
- [ ] `state` keeps its single use and short expiry regardless. It is now a second factor rather
      than the only one, and CSRF on the callback is what it still buys
- [ ] Revisit the `dawarichHeaders` leak Step 20 has to solve: with `start` no longer reusing
      `authenticate`, that route stops leaking a compatibility header. Record whether any caller
      of that behaviour is left, so the workaround is removed rather than left in place unowned
- [ ] README: the flow now starts from the browser while logged in. **Delete the `api_key` URL
      Step 20 documented there** rather than leaving both — that URL is the exposure this step
      exists to remove, and a README that still offers it keeps offering it. Step 20's own
      "A limitation to accept knowingly" needs no deleting: CLAUDE.md has its whole entry go when
      Step 20 is done, so by the time this step is reachable the paragraph is already gone
- [ ] Tests: `start` without a session redirects to `/login` rather than 401; with one it
      redirects to Foursquare with a `state` bound to that user; a callback whose `state` names a
      different user than the session is refused; **no route in the flow accepts `api_key` any
      more**

**Settles**: that a browser-facing route outside `/api/v1` identifies its user by session, and
that the same is true on the leg that comes back from a third party.

**Done when**: with a browser logged in and no `api_key` anywhere in the URLs, the Swarm flow
completes and writes the `foursquare_accounts` row. That the stored token then collects check-ins
is `travelmap foursquare sync`'s to show, and Step 19 may not be built yet: Milestone I lets
Step 20 be taken at any point, so this step cannot rest on it.

### Step 31: The Swarm connection page

Follows Step 30. The page that makes the link visible and repeatable, rather than a URL to type.

- [ ] `store.FoursquareAccountRepository.ByUserID`. The repository has only `Create` and
      `ByFoursquareUserID` today, because until now a push arriving from Foursquare was the only
      thing that asked — **a page showing "connected as …" asks the other way round**
- [ ] A page showing whether a Swarm account is linked, which one, and how current it is, with a
      button starting Step 30's flow. The last of those reads
      `foursquare_accounts.synced_through`, which is **the use "foursquare_accounts" in
      docs/database.md reserves that column for** — reporting how current an account is, rather
      than resuming a fetch from it. This step is that column's first reader
- [ ] Disconnecting: a `Delete` on that repository behind a POST, so the flow can be run again
      against a different Swarm account. **Say on the page what it does not do** — check-ins
      already collected stay, because they are the user's history and not the link's
- [ ] Whether a re-link should be a `Delete` plus `Create` or an upsert is decided here, since
      this step is the first thing able to reach the same row twice
- [ ] README: the page, next to the sentence Milestone I's README section already carries about
      nothing being collected until an account is linked
- [ ] Tests: the page reports not-linked on a fresh account and linked after the row exists;
      disconnecting removes the row and leaves `checkins` untouched; one user cannot see or
      disconnect another's link

**Settles**: how a travelmap account's external links are presented and undone.

**Done when**: the page shows a fresh account as not connected, the button completes the flow and
the page then names the Swarm account, and disconnecting returns it to not connected while the
check-ins already collected remain.

### Still to plan

- [ ] Map screen (render points / tracks for a selected time range), reusing the existing
      `GET /api/v1/points` and `/tracks` without adding UI-only APIs. **This is the step that has
      to answer "Open question: how the browser authenticates against `/api/v1`"**, being the
      first to read data from it
- [ ] Statistics screen (using `daily_stats`)
- [ ] Settings screen. An import screen only if Milestone G's `/api/v1/imports` was implemented
- [ ] Vendor the map library into `embed.FS` to preserve the single binary, alongside the
      templates and the stylesheet already there; what is left is the one dependency that has to
      be fetched rather than written

**Done when**: logging in from a browser shows the user's history on a map, and deployment is
still one binary plus one SQLite file.

---

## Milestone I — Swarm check-in collection

Collect Swarm (Foursquare) check-ins, so that the explicitly recorded landmark sits alongside
the automatically recorded GPS trace. The first feature that is travelmap's own rather than
upstream's — read "Keeping the two parts apart" in `docs/api-notes.md` before adding a route
here.

Independent of the points/stats pipeline: it touches neither `points` nor `daily_stats`. What it
does need is the store foundation and the authenticated router, both already in place — the push
webhook already hangs a route off the same router, and Step 20 reuses the `api_key` credential.
So the remaining steps can be taken at any time after Milestone B, like Step 16.

External behaviour these steps rely on is in "Foursquare / Swarm Integration Notes"; the two
tables are in `internal/store/sqlite/schema.sql` and docs/database.md, per "Data Model".

There are seven `TRAVELMAP_FOURSQUARE_*` settings, and no step adds all of them:
`TRAVELMAP_FOURSQUARE_PUSH_SECRET` is already in place, the lookback and the API URL belong to
Step 19, the three OAuth settings to Step 20, the interval to Step 21. An eighth setting joins
whichever step needs it.

**Each step documents its own settings in the README, in the same pull request.** The README is
for someone about to run this server, so a knob that is listed there and does nothing yet is the
one kind of drift its reader cannot detect — and this milestone's settings come with procedures
(an OAuth URL, a CLI invocation) that would invite a reader to try something not built. The
settings and their defaults are recorded here in the meantime, which is what `TODO.md` is for.

The push webhook already opened a check-in section under the README's "Configuration", since it
collects check-ins on its own; Step 19 adds to what it wrote there rather than starting the
section itself. That section carries the one fact no single setting does: **nothing is collected
until an account is linked**, which until Step 20 exists means `travelmap foursquare connect`.
Without it a reader can set every variable, run `foursquare sync`, and be told nothing about why
the result is empty. Step 20 adds its browser flow to the same sentence when it lands, which is
its own README item.

Step 19 can be taken at any time now that the push webhook already collects check-ins on its own;
Step 21 follows Step 19, and Step 20 can be taken at any point or left until last, needing nothing
this milestone has not already shipped. **Milestone H's Steps 30 and 31 then finish it from the
browser** — the session that replaces its `api_key` URL, and the page that shows and undoes the
link — so taking Step 20 after Milestone H's Step 26 saves building the credential it already
accepts as a limitation. `travelmap foursquare connect` exists precisely so the collecting steps
can be finished and run for real before the OAuth flow is written. Until it is, the access token
comes out of the Foursquare application's own console, which issues one for the account that owns
the application; that is the whole of what Step 20 later automates.

**Step 19 grew a second step rather than one long checklist.** Calling the endpoint and knowing a
page-walk succeeded is one decision; how that client copes with a rate limit, an ambiguous error
code, or a check-in whose language disagrees between paths is another, confirmed empirically
rather than designed alongside the first. That second half is Step 22 — it follows Step 19 and
also reaches back to a check-in already collected by push for the locale comparison — and nothing
in this milestone waits on it in turn.

### Step 19: The Foursquare API client

One fetch, run by hand, against an API that is answering normally. Two things are split out of
this step, for two different reasons. The timer that repeats it is Step 21 — a client and a
scheduler settle different conventions, and this half already reaches real check-ins on its own.
This client's response to rate limits, ambiguous error codes and locale drift is Step 22 instead:
none of that is decided by this step's design, only confirmed once it exists — and, for locale,
once the push webhook's collection path has a check-in to compare against.

- [ ] `internal/foursquare`: a client for `GET /v2/users/self/checkins` — `v=` pinned, `m=swarm`,
      the token in an `Authorization: Bearer` header rather than the URL, `limit=250`,
      `sort=newestfirst`, and paging through `beforeTimestamp` rather than `offset`, all per
      "Fetching check-ins"
- [ ] The paging loop follows "Page with `beforeTimestamp`, not `offset`" exactly: the cursor is
      the page's oldest `createdAt` **plus** one second; a page shorter than `limit` ends the run;
      and a **full page that does not lower the cursor fails the run** without advancing
      `synced_through`
- [ ] Two tests, because ending and failing are not the same condition. A fake server returning a
      page shorter than `limit` ends the run normally. A fake server returning the **same full page
      every time** — so its oldest `createdAt` never falls and the cursor cannot advance, the shape
      an ignored `beforeTimestamp` produces — makes the run **fail**. Assert the failure, not merely
      that the run terminates: termination alone passes with the progress check missing, which is
      the whole reason for writing this one
- [ ] `internal/config`: `TRAVELMAP_FOURSQUARE_SYNC_LOOKBACK_DAYS` and
      `TRAVELMAP_FOURSQUARE_API_URL` (default `https://api.foursquare.com`)
- [ ] `internal/checkin`: one sync run — the lookback window from "The periodic fetch takes a
      window, not a cursor", an upsert per check-in through the same writer the webhook uses,
      and `synced_through` advanced on success
- [ ] `travelmap foursquare sync`, for a one-shot run and for backfilling further back than the
      window
- [ ] README: the lookback and the API URL, and `travelmap foursquare sync` under "Build and
      run"
- [ ] Test the client against `httptest.NewServer` serving a recorded response;
      `TRAVELMAP_FOURSQUARE_API_URL` exists so the test can point at it
- [ ] A test that the same check-in arriving by push and then by fetch leaves one row, with
      `source` still naming the first path
- [ ] **A test that pins the window against a cursor**: a check-in dated before the last
      successful run, added after it, is still collected by the next one. This step is where the
      window and `synced_through` are written, so it is where the test belongs — and it is what
      stops a later change from turning `synced_through` into the lower bound, which is the one
      mistake that would silently defeat the whole fetch path

**Settles**: the conventions for calling an external API from this server — timeouts, closing
bodies, and how a page-walk knows it succeeded.

**Confirms**, and records the answers in "Fetching check-ins":

- That `sort`, `afterTimestamp` and `beforeTimestamp` are honoured though the current reference
  page omits them. If they are not, that section names the fallback
- **That `sort` selects, and not merely returns 200.** Against a window holding more than `limit`
  check-ins, the first page must contain the most recent one. This is the one failure the runtime
  progress check cannot see — "Page with `beforeTimestamp`, not `offset`" explains why — so it is
  confirmed here, once, before the fetch path is trusted at all
- That an `Authorization: Bearer` header is accepted on **this** endpoint, whose own parameter
  table names only the `oauth_token` query parameter. If it is not, every request is a 401 and the
  fallback is that parameter
- Whether `beforeTimestamp` is inclusive or exclusive, and whether `count` narrows with
  `afterTimestamp`. Neither changes the design — the `+ 1` cursor, the short-page stop and the
  progress check hold under either boundary — but both are cheap to read off a real response

**Done when**: `travelmap foursquare sync` fetches real check-ins, and a second run changes no
row count.

### Step 20: Server-side OAuth

- [ ] `GET /foursquare/oauth/start` — **reuses `authenticate` and `requireUser` on its own
      group, without `dawarichHeaders`**: those two run inside `r.Route("/api/v1", …)` today, so
      a top-level route gets neither and `userFrom` would answer nothing. Attaching a different
      chain per prefix is what chi was chosen for. It then mints a `state` bound to that user and
      redirects to `https://foursquare.com/oauth2/authenticate`
- [ ] Leaving `dawarichHeaders` off is not enough on its own: `authenticate` writes those headers
      itself when the key lookup fails, because on that path the middleware it wraps never runs.
      Reusing it outside `/api/v1` therefore leaks a compatibility header onto a route that is
      not part of the compatibility surface, on exactly one response. Move that write out of
      `authenticate` or make it conditional — either way, decide it here rather than at
      implementation time
- [ ] The **callback carries no credential** — the browser is coming back from Foursquare — so
      it sits outside that group and `state` is the only thing naming the user. That is what
      makes single use and a short expiry load-bearing rather than tidy
- [ ] `GET /foursquare/oauth/callback` — verifies `state`, exchanges the code at
      `https://foursquare.com/oauth2/access_token`, calls `/v2/users/self` for the Foursquare
      user id, and writes the `foursquare_accounts` row. Both calls go in
      `internal/foursquare` alongside Step 19's client, so every request to Foursquare is
      configured in one place; this handler imports it directly, since it writes an account
      rather than a check-in
- [ ] `state` stored **in process**, with a short expiry and single use. One process serves
      this, and a `state` that does not survive a restart only costs the user a retry — which is
      why this step adds no migration. **Foursquare's reference documents neither `state` nor
      `scope`**, listing only `client_id`, `response_type` and `redirect_uri`. That it is echoed
      is an inference from two working clients that assume it — perkeep's Swarm importer through
      `golang.org/x/oauth2`, and Auth.js's Foursquare provider — not from an observation. Since
      `state` is the only thing naming the user on the callback, **Step 20 verifies the echo
      before anything depends on it**, and fails the flow closed if it is absent
- [ ] The documented token response is `{"access_token": …}` and nothing else. **That is the
      documentation's silence, not a fact**: whether a refresh token or an expiry comes back is
      untested, and the notes for this milestone treat a reference's silence as unsettled rather
      than as an absence. So **log the exchange response's field names** and record them here. The
      decision meanwhile is to schedule no renewal — a token that stops working shows up as a 401
      from the fetch path, which `travelmap foursquare connect` can already repair
- [ ] `TRAVELMAP_FOURSQUARE_REDIRECT_URL` has to match the URL registered on the application
      exactly, in both the redirect and the exchange
- [ ] `TRAVELMAP_FOURSQUARE_CLIENT_ID`, `TRAVELMAP_FOURSQUARE_CLIENT_SECRET` and
      `TRAVELMAP_FOURSQUARE_REDIRECT_URL` in `internal/config`
- [ ] README: those three, and how to start the flow. Write the URL that actually works once the
      middleware question above is settled, not the one planned here

**Settles**: how a browser-facing route outside `/api/v1` identifies a travelmap user before
Milestone H exists.

**A limitation to accept knowingly**: there is no way to establish a browser session until
Milestone H's Step 26, the login form, so the only way `start` can name a user is the `api_key`
query parameter. That is consistent —
every endpoint here accepts it — but **it puts the API key in browser history and in the `Referer`
of the redirect**. **Milestone H's Step 30 is what removes it**, moving both legs of the flow onto
the session. If the exposure is not acceptable meanwhile, **hold this step until Step 26** and keep
using `travelmap foursquare connect` — taken after a browser can log in, this step writes the
session version directly and there is no `api_key` leg to remove afterwards. The rest of the
milestone does not depend on it either way.

### Step 21: The periodic fetch worker

Follows Step 19, whose sync run this repeats on a timer; nothing in Step 20 is in the way, and
Step 22's hardening is not required first either — see that step's note on why.

- [ ] `internal/config`: `TRAVELMAP_FOURSQUARE_SYNC_INTERVAL` (default `1h`, `0` disabling the
      fetch), parsed with the duration parsing Milestone H's session lifetime setting already
      added to that package
- [ ] `internal/checkin`: the worker — a ticker on that interval calling Step 19's sync run,
      nothing more. Started from `cmd/travelmap/serve.go`, the only place
      holding both the signal-cancelled context and the concrete store
- [ ] Shut down with the server: the worker stops on the same cancelled context, and a run in
      flight is not left to write into a closing database
- [ ] README: the interval, including that `0` switches the fetch off and leaves only the
      webhook
- [ ] A test that a restart resumes: the ticker starts again, and the tick after it covers the
      time the process was down. What makes that possible is Step 19's window, tested there;
      what is tested here is that stopping and starting the worker loses no tick

**Settles**: the lifecycle of a background worker in this server — **without introducing the job
table**, per the "Scheduling" row under "Technical Decisions". Milestone H's Step 27 says the same
of its session sweep: whichever of the two lands first settles it, and the second follows it.

**Done when**: with the server left running and **`TRAVELMAP_FOURSQUARE_PUSH_SECRET` unset**, so
the webhook route is not registered at all, a check-in made in Swarm turns up in `checkins` on the
following tick. Unsetting it is what makes this observe the worker: with push configured, a
check-in may well arrive within seconds by that path — whether it also does so for a check-in
added after the fact is exactly what is not known — and the tick would then be proving nothing.

**Milestone done when**: with both paths configured, a check-in made in Swarm appears in `checkins`
within seconds, and a check-in added after the fact appears no later than the next fetch (Step 21,
or a hand-run `foursquare sync` before it).

**Neither of those exercises the fetch path on its own, and this milestone does not ask them to.**
With push configured, either check-in may well arrive by push within seconds — whether it does for
a retroactive one is the open question — so `source` here records which path won a race, not which
paths work. **Step 21's Done when is the fetch path's proof**, because it runs with the push secret
unset and nothing else can have collected the row. Read this condition as "both paths are
configured and nothing is lost", and that one as "the fetch path collects".

### Step 22: Rate limits, ambiguous errors and locale drift

Split out of Step 19 because none of this is decided by the calling convention or the paging
design — it is confirmed empirically, once a client exists to run and a check-in has arrived by
both paths to compare. Depends on Step 19, which this extends, and on a check-in already
collected by the push webhook for the locale bullet to compare against.

- [ ] The client **reads `meta`, not only the HTTP status**: it logs `meta.requestId` on every
      failure and surfaces `errorType: deprecated` on a 200 loudly, that being the only notice
      this design gets before a pinned `v` stops answering
- [ ] **Branch on `meta.errorType`, never on the status alone**: 403 is both
      `rate_limit_exceeded` and `not_authorized`, and 429 is `quota_exceeded`. A revoked
      authorisation must not be retried as a rate limit, and an exhausted hourly limit must not be
      reported as a permissions failure
- [ ] Read `X-RateLimit-Remaining` and `X-RateLimit-Reset` and **log them**, and when `Remaining`
      reaches zero **end the run rather than sending the request that would 403**. No sleeping
      inside a run: the window has moved on by the next tick anyway. Such a run **does not advance
      `synced_through`**, because it did not cover its window — and running it again walks the
      window from the top rather than resuming, there being no stored position to resume from
- [ ] Send **no locale**, for the reason in "Localisation is left unset on both paths" — this is
      a decision the fetch path has to make and the push path cannot
- [ ] **Measure whether that matches the push**: fetch a check-in that also arrived by push and
      compare `venue_name`, `country`, `city`, `state` and `category_name`. If they differ, take
      those columns out of what a repeat write refreshes and record the outcome in that section
- [ ] A test for the two `meta` cases the status code alone would hide: a 200 carrying
      `errorType: deprecated` is reported and its items are still stored, and a 403 is
      distinguished by `errorType` between the rate limit and a revoked authorisation

**Settles**: how this client behaves when Foursquare pushes back — an ambiguous status, an
exhausted quota, a deprecation notice — and what the two collection paths do when they disagree
about a check-in's language.

**Nothing else in the milestone waits on this.** Step 21's worker runs unattended, which is where
a stuck retry or a wasted request would actually bite — but "What the fetch path costs" shows
normal use nowhere near the 500-request budget, so Step 21 does not hold for this step, and an
account that runs into either problem before it lands sees the run fail with a plain HTTP error —
no branch yet to say whether it was a quota or a revoked token — until this step's logging exists
to tell them apart.

**Done when**: a fake server answering 403 with `errorType: not_authorized` is not retried as a
rate limit, one answering with `X-RateLimit-Remaining: 0` ends the run without a further request,
and a check-in observed by both paths either matches on the five columns or has them recorded as
excluded from refresh.

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
  instead of re-fetching. The `errorType: deprecated` check in Step 22 is the early warning.
- **What each collection path actually sees is only partly known.** Whether a push fires for a
  check-in added after the fact, and whether one fires for an edit to a check-in already stored,
  are both undocumented and unobserved. Nothing in the design rests on either answer — the fetch
  window is argued from what a cursor cannot see, under "The periodic fetch takes a window, not a
  cursor" — and nothing should start to. One retroactive check-in, made with both paths configured
  and `source` read afterwards, settles both questions and the `createdAt` one below at once.
- **Whether a retroactive check-in's `createdAt` is the visit time or the creation time is
  open.** It does not block Milestone I — the fetch window is correct either way — but it
  decides what `checked_in_at` means. See "The periodic fetch takes a window, not a cursor".
- **The push URL has to be reachable over HTTPS from the internet**, which a self-hosted
  instance behind a reverse proxy can do but a laptop cannot. Until then the fetch path alone
  collects check-ins, just with a delay. The README already says so, in the check-in
  configuration section the push webhook opened.
- **The push secret belongs to the Foursquare application, not to a user.** Keep the split it
  implies: the secret is server configuration, and identifying whose check-in arrived is
  `foursquare_user_id`'s job. Deriving a user from the secret would break the moment a second
  person connects.
- **Sign-up is open, so on a reachable instance anyone can create an account.** This is the
  decision recorded under "Technical Decisions", not an oversight, and it is written down here
  because three of its consequences are real on a self-hosted box rather than hypothetical. Every
  account can write points into **the same SQLite file**, so somebody else's history is on the
  operator's disk and inside their backups. `travelmap recalculate` walks every user with points,
  so it gets slower with each account that is not the operator's. And `/signup` and `/login` each
  spend one bcrypt hash per request, which is CPU an unauthenticated caller chooses to spend —
  `POST /api/v1/auth/login` already has that property, but it is not currently advertised by a
  form. **None of this bites on a LAN or behind a reverse proxy that authenticates first**, which
  is how a personal instance is normally run; it bites on one published to the internet, which the
  Swarm push webhook — already shipped — is a reason to do. If a gate is ever wanted, the cheapest
  one is an environment variable read at route registration, exactly as `POST /webhooks/foursquare`
  is registered only when `TRAVELMAP_FOURSQUARE_PUSH_SECRET` is set — which is why nothing in
  Steps 23 to 31 has to be designed for it now.
- **Revisit whether `internal/ingest` should be named `internal/usecase` (or `service`) instead.**
  `usecase`/`service` are the more familiar names for this layer in most Go codebases; `ingest`
  was picked because this milestone's whole job is literally ingesting device locations, which
  does not generalise as an argument once `internal/checkin` (Milestone I) is the same shape of
  layer over a different kind of write. Revisit once `internal/checkin` exists alongside
  `internal/ingest`, so the comparison is between two real packages rather than a naming
  preference argued in the abstract. A rename this late only costs an import-path edit — nothing
  in the layering itself depends on the name — so there is no cost to waiting for the evidence.
