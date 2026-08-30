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
| Scheduling | A ticker goroutine re-running the fetch over the window it already takes, and **no job table** | The periodic fetch is one cron-like task with no queue of items, so the job table in the "Background work" row above would be scaffolding with nothing in it. Step 13's track splitting is the first genuine per-item consumer; the decision stands, its first use just is not here |

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

### Errors this client does not branch on yet

**Branch on `errorType`, not on the status.** A 403 carries two meanings on this API:
`not_authorized`, which is a revoked authorisation and permanent, and `rate_limit_exceeded`, which
is transient. Only `meta.errorType` tells them apart, so a client reading the status alone either
keeps trying against an account that will never answer again, or reports a passing refusal as a
permissions failure. An account whose authorisation is gone needs its operator told, and nothing
can tell them from a 403.

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

Sessions, CSRF and HTML rendering are settled and already implemented; their rows moved to
`docs/architecture.md`, which also covers the router, for the reasoning that applies here as
well. What is left undecided is what the remaining screens need.

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

**Steps 27 to 34 do not settle this and do not need to**: none of them adds a data endpoint, so
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

**A step's number says when it was planned, not when to take it.** Milestone H's Steps 23 to 34
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
- [ ] `POST /api/v1/auth/register`, **open like the browser sign-up screen**, not behind an
      environment variable. Two registration routes whose defaults disagree is exactly the
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

Steps 30 to 33 are the rest of the browser's way in: the Swarm link, which a browser is the only
sensible place to start from, and retiring the two CLI commands their browser equivalents make
redundant — the `sessions` table and its repository, the HTML route group and its one page, the
session middleware, the login screen, `auth.Register` and the sign-up screen are already in place.
**They add no data endpoint**, which is what leaves "Open question: how the browser authenticates
against `/api/v1`" for the map screen to answer rather than this half.

The map, statistics and settings screens keep their bullet form below. They are not planned yet,
and writing a checklist for a screen whose rendering approach is undecided would be inventing the
plan rather than recording it.

### Ordering

```
The sign-up screen ─→ Step 32 (remove user create)

The login screen + Milestone I's Step 20 ─→ Step 30 (Swarm over a session) ─→ Step 31 (the Swarm page) ─→ Step 33 (remove foursquare connect)
```

Step 27 (the session sweep) and Step 34 (the `/` redirect) can each be taken at any time, needing
nothing but the sessions store and the login screen, respectively, already in place.

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

### Step 34: Redirect an anonymous `GET /` to `/login`

Can be taken any time, needing nothing but the login screen already in place. Today `GET /`
answers an anonymous visitor with a status page reading "Not signed in", plus a link to `/login`
and one to `/signup` — a page that has to be read before it says what to do. A first-time visitor
opening the server's URL should land on the thing to act on, not a landing page pointing at it.

- [ ] `GET /` redirects an unauthenticated visitor to `/login` with a 302 Found — a plain GET
      redirect, not the 303 See Other a POST handler uses after processing a form, since nothing
      here is converting a POST into a GET
- [ ] A signed-in visitor keeps seeing the current page (the signed-in status and the logout
      button), unchanged
- [ ] Tests: an anonymous `GET /` redirects to `/login`; a signed-in `GET /` still renders the
      current page

**Settles**: that `/` is not a dead end for a first-time visitor — the browser's own entry point
sends them straight to the action they need, rather than a static message they have to read and
click through first.

**Done when**: opening `http://localhost:3000/` with no session lands directly on the login form.

### Step 30: Starting the Swarm flow from a browser session

Follows the login screen, already in place, and Milestone I's **Step 20**, which builds the OAuth
exchange itself. This step adds no new exchange — it changes what names the travelmap user on the
way in and on the way back, and it is what Step 20's "A limitation to accept knowingly" promises.

**If Step 20 is taken now that the login screen already exists**, as Milestone I's own ordering
note suggests, it writes the session version directly and there is no `api_key` leg to move or
README line to delete. What is left of this step is then the callback checking the session and
`state` against each other, and the tests for it — a much smaller change. Take that reading rather
than looking for an `api_key` route that was never built.

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
is `travelmap foursquare sync`'s to show, which this step does not repeat.

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

### Step 32: Remove `travelmap user create`

Follows the sign-up screen, already in place. Once the browser can sign up on an empty database,
the CLI path settles nothing a browser cannot: unlike Foursquare's OAuth exchange, nothing about
creating a travelmap account needs a redirect to reach the operator's own server from the outside,
so there is no deployment where sign-up is reachable and `user create` is not. Kept only as long as
it takes to prove sign-up out; this step is what removes it rather than leaving two paths that can
drift.

- [ ] Delete the `user create` subcommand and its own tests
- [ ] `docs/architecture.md`'s "User management" row drops the CLI entirely: issued via browser
      sign-up, full stop
- [ ] `0001_users.sql`'s leading comment on `users` and `model.User`'s doc comment — both rewritten
      once already, by the sign-up screen, to say the CLI is one path rather than the path — are
      rewritten again to say there is no command-line path at all
- [ ] README: the `user create` walkthrough in "Build and run" is replaced with signing up at
      `/signup`; `travelmap foursquare connect --email …` keeps working unchanged, since it only
      ever needed an existing account's address, not the command that created it. Two places in
      its own `--help` text name the command being removed here, both rewritten to point at
      `/signup` instead: the `--email` flag's own description ("created with `travelmap user
      create`") and the "no user with the email …, run `travelmap user create` first" error
      message. A third comment there, on `readToken`, names `userCreate` itself ("the same concern
      `userCreate`'s password reading answers") — rewritten too, since that function stops
      existing here
- [ ] Tests: `travelmap --help` no longer lists `user create`; whatever this repository's own
      tests use to seed a user for another command's tests (`foursquare connect`, `recalculate`)
      does not go through it either, and is checked here rather than assumed

**Settles**: that a travelmap account has exactly one way to be created.

**Done when**: `travelmap user create` no longer exists, and the README's setup walkthrough signs
up at `/signup` instead.

### Step 33: Remove `travelmap foursquare connect`

Follows Step 31. Once a logged-in session can link a Swarm account from its own page, the CLI path
settles nothing a browser cannot: getting an access token out of Foursquare's own developer
console takes exactly the same human-at-a-browser step as clicking through its OAuth consent
screen, so the command enables no automation the browser flow does not already offer. Kept only as
long as it takes to prove that flow out; this step is what removes it rather than leaving two paths
that can drift.

- [ ] Delete the `foursquare connect` subcommand and its own tests. What that leaves of the
      `foursquare` dispatcher is `foursquare sync`, its one remaining subcommand
- [ ] `docs/database.md`'s `foursquare_accounts` entry ("Created by `travelmap foursquare
      connect`") is rewritten to say created from the Swarm connection page instead. Three more
      places say the same thing and go with it: `internal/model.FoursquareAccount`'s doc comment,
      `store.FoursquareAccountRepository`'s doc comment, and `0005_checkins.sql`'s leading comment
      on `foursquare_accounts`, which already reads "created by `travelmap foursquare connect` or,
      once it lands, a browser-driven OAuth flow" — sitting outside every statement, so still
      editable after the fact — and now drops the CLI half entirely
- [ ] README: the `foursquare connect` walkthrough under "Swarm (Foursquare) check-ins" is replaced
      with signing in and visiting the Swarm connection page; the "until the OAuth flow exists, get
      an access token from the Foursquare application's own console" caveat is dropped along with
      it, since nothing here still needs that console
- [ ] Tests: neither `travelmap --help` nor `travelmap foursquare --help` lists `connect` any more;
      `internal/httpapi`'s own `linkFoursquareAccount` test helper already seeds a
      `foursquare_accounts` row through the store directly rather than the CLI, and is checked here
      rather than assumed

**Settles**: that a Swarm account links to a travelmap account through exactly one path.

**Done when**: `travelmap foursquare connect` no longer exists, and the README documents the Swarm
connection page as the only way to link an account.

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

Four `TRAVELMAP_FOURSQUARE_*` settings are still to add: the three OAuth ones in Step 20 and the
interval in Step 21. A fifth joins whichever step needs it.

**Each step documents its own settings in the README, in the same pull request.** The README is
for someone about to run this server, so a knob that is listed there and does nothing yet is the
one kind of drift its reader cannot detect — and this milestone's settings come with procedures
(an OAuth URL, a CLI invocation) that would invite a reader to try something not built. The
settings and their defaults are recorded here in the meantime, which is what `TODO.md` is for.

The README's check-in section says that **nothing is collected until an account is linked**, the
one fact no single setting carries — without it a reader can set every variable, run `foursquare
sync`, and be told nothing about why the result is empty. Step 20 adds its browser flow to that
sentence when it lands, which is its own README item.

Step 21 follows the fetch path already in place, and Step 20 can be taken at any point or left
until last, needing nothing this milestone has not already shipped. **Milestone H's Steps 30 and
31 then finish it from the browser** — the session that replaces its `api_key` URL, and the page
that shows and undoes the link — so taking Step 20 now that Milestone H's login screen already
exists saves building the credential it already accepts as a limitation. `travelmap foursquare
connect` exists precisely so the collecting steps can be finished and run for real before the
OAuth flow is written. Until it is, the access token comes out of the Foursquare application's
own console, which issues one for the account that owns the application; that is the whole of
what Step 20 later automates.

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
      `internal/foursquare` alongside the check-in client, so every request to Foursquare is
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

**Settles**: how `GET /foursquare/oauth/start`, a browser-facing route outside `/api/v1`,
identifies a travelmap user.

**A limitation to accept knowingly**: the checklist above still names a user with the `api_key`
query parameter rather than Milestone H's login screen, which already exists by the time this
step is taken — see Step 30's own note for the alternative that reading opens up. That is
consistent — every endpoint here accepts `api_key` — but **it puts the API key in browser history
and in the `Referer` of the redirect**. **Milestone H's Step 30 is what removes it**, moving both
legs of the flow onto the session. If the exposure is not acceptable meanwhile, take Step 20
against the session directly instead, per Step 30's note, and keep using
`travelmap foursquare connect` until then — this step's own `api_key` leg is then never built
rather than removed afterwards.

### Step 21: The periodic fetch worker

Repeats `internal/checkin`'s sync run on a timer; nothing in Step 20 is in the way, and Step 22's
hardening is not required first either — see that step's note on why.

- [ ] `internal/config`: `TRAVELMAP_FOURSQUARE_SYNC_INTERVAL` (default `1h`, `0` disabling the
      fetch), parsed with the duration parsing Milestone H's session lifetime setting already
      added to that package
- [ ] `internal/checkin`: the worker — a ticker on that interval calling that package's own
      sync run, nothing more. Started from `cmd/travelmap/serve.go`, the only place holding both
      the signal-cancelled context and the concrete store
- [ ] Shut down with the server: the worker stops on the same cancelled context, and a run in
      flight is not left to write into a closing database
- [ ] README: the interval, including that `0` switches the fetch off and leaves only the
      webhook
- [ ] A test that a restart resumes: the ticker starts again, and the tick after it covers the
      time the process was down. What makes that possible is the recomputed window, tested in
      `internal/checkin` already; what is tested here is that stopping and starting the worker
      loses no tick

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

### Step 22: Ambiguous errors and locale drift

Split out of the API client's own step because none of this is decided by the calling convention
or the paging design — it is confirmed empirically, once a client exists to run and a check-in has
arrived by both paths to compare. It extends the client already in `internal/foursquare`, and
depends on a check-in already collected by the push webhook for the locale bullet to compare
against.

- [ ] **Branch on `meta.errorType`, never on the status alone**: 403 is both
      `rate_limit_exceeded` and `not_authorized`. A revoked authorisation, which is permanent,
      must not be reported as the transient one
- [ ] **Measure whether the fetch's rendering matches the push's**: fetch a check-in that also
      arrived by push and compare `venue_name`, `country`, `city`, `state` and `category_name`.
      If they differ, take those columns out of what a repeat write refreshes, and record the
      outcome under "Whether the two paths agree on a check-in's language"
- [ ] A test that a 403 is distinguished by `errorType` between the two meanings it carries — the
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
- **Sign-up is open, so on a reachable instance anyone can create an account.** This is the
  decision recorded in the "User management" row of `docs/architecture.md`, not an oversight, and
  it is written down here because three of its consequences are real on a self-hosted box rather
  than hypothetical. Every
  account can write points into **the same SQLite file**, so somebody else's history is on the
  operator's disk and inside their backups. `travelmap recalculate` walks every user with points,
  so it gets slower with each account that is not the operator's. And `/signup` and `/login` each
  spend one bcrypt hash per request, which is CPU an unauthenticated caller chooses to spend — both
  `POST /api/v1/auth/login` and the browser's own `/login` form now have that property, the second
  advertising it to anyone who can reach the page. **None of this bites on a LAN or behind a
  reverse proxy that authenticates first**, which is how a personal instance is normally run; it
  bites on one published to the internet, which the Swarm push webhook — already shipped — is a
  reason to do. If a gate is ever wanted, the cheapest one is an environment variable read at route
  registration, exactly as `POST /webhooks/foursquare` is registered only when
  `TRAVELMAP_FOURSQUARE_PUSH_SECRET` is set — which is why nothing in Steps 27 to 34 has to be
  designed for it now.
- **No brute-force protection on login: neither `POST /api/v1/auth/login` nor the browser's own
  `/login` limits how many attempts an email address or an IP gets.** Both already spend one
  bcrypt hash per attempt and refuse a wrong password at the same cost as a right one, which
  narrows the attack to throughput rather than timing, but nothing here bounds that throughput
  itself — a caller can simply keep asking. Unmitigated today; the same "None of this bites on a
  LAN or behind a reverse proxy that authenticates first" reasoning above applies, so it matters
  once more on an internet-reachable instance. Adding a per-address or per-account attempt limiter
  is new work with no home in the current plan.
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
