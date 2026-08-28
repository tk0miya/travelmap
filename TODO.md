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

These are the defaults as of planning. If any turns out to be wrong during implementation,
change it — after updating this file.

## Data Model

Columns and indexes for the tables already migrated (`users`, `points`, `daily_stats`, `checkins`,
`foursquare_accounts`) are in `internal/store/sqlite/schema.sql`, kept current by `TestSchema`;
the rationale for how a column or index is shaped is a comment in the migration that adds it.
Behaviour that spans tables or does not attach to a single column — invariants, algorithms,
config effects — is in `docs/database.md`.

## Dawarich API Compatibility Notes

Upstream quirks for endpoints not yet implemented. For an endpoint already implemented,
`docs/api-notes.md` covers it — this section holds only what is still ahead.

### Per-endpoint

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

### `GET /api/v1/points` must answer HTTP HEAD

Clients issue a `HEAD /api/v1/points` with the same query parameters first, read
`X-Total-Pages` from the response, and only then fetch the pages. **If `X-Total-Pages` is absent
or 0 the client concludes there is nothing to fetch and stops** — the map silently shows no
points. This endpoint therefore needs the pagination headers computed on the HEAD path too, not
only on GET.

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

1. **A real push captured with a webhook recorder** — everything under "The push payload".
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

### The push payload

```
POST <the configured push URL>
Content-Type: application/x-www-form-urlencoded
User-Agent: FoursquarePush/1.0

checkin=<JSON>&user=<JSON>&secret=<the application's push secret>
```

Three form parameters, each a JSON string except `secret`. So this route parses a form; the
`decodeJSON` helper and its `maxRequestBody` do not apply to it.

- `checkin`: `id` (24 hex characters), `createdAt` (Unix seconds), `type`, `visibility`,
  `timeZoneOffset` (minutes), `canonicalUrl`, `editableUntil` (**milliseconds**), `user`, `venue`
- `checkin.venue`: `id`, `name`, `location` (`address`, `lat`, `lng`, `labeledLatLngs`,
  `postalCode`, `cc`, `city`, `state`, `country`, `formattedAddress`), `categories` (`id`,
  `name`, `pluralName`, `shortName`, `icon`, `categoryCode`, `mapIcon`, `primary`), `timeZone`
- `checkins.venue_id` and `.venue_name` are nullable for a check-in made without one, per
  "checkins" in docs/database.md, but **the venueless shape has not been observed** — the
  captured push had a venue — so confirm where its coordinates sit before relying on it; Step
  18's fixture covers only the shape actually seen. The documentation will not settle it either.
  The schema on `reference/get-checkin-details` describes `venue` without saying whether it can
  be absent, and that schema is demonstrably incomplete — it omits `shout`, `visibility` and
  `editableUntil`, which the observed push carries — so its silence is not evidence either way.
  Only an observation decides this one
- `shout` was **absent as a key** in the observed push, which carried no comment — so treat it
  as optional rather than as an empty string
- `checkin.user.id` is a quoted string, and is the join key onto `foursquare_accounts`. The
  separate `user` parameter repeats it with extra profile fields; **read the key off
  `checkin.user.id`**, which keeps the identity inside the object being stored
- The display text is **localised**, and what decides the language is unknown — see "Localisation
  is left unset on both paths" and "checkins" in docs/database.md. In an observed push, `cc` was
  `JP` while `country` and the category name came back as Japanese
- `editableUntil` says a check-in stays editable long after it is made, which is why the write
  path is an upsert and not an insert-if-absent

The documentation confirms the envelope and contradicts nothing above, but its own sample payload
is **narrower than what arrives**: it has no `shout`, `visibility`, `canonicalUrl` or
`editableUntil`, its categories carry `parents` where the observed ones carry `categoryCode` and
`mapIcon`, and its venue carries `contact`, `stats`, `url` and `verified`, which the observed one
did not. Two consequences for the wire struct in `internal/foursquare`: **unknown fields are
ignored, and almost every field is optional.** It does confirm `timeZoneOffset` in minutes, and it
prints `user.id` as `"1"` — quoted, like the observed `"1709193"` and like the `string` that
`/v2/users/self` declares. That is three independent reasons for `foursquare_user_id` being TEXT.

Only `checkin` ever arrives on the User Push API. The same push format is shared with the **Venue
Push API**, where the second parameter is instead one of `like`, `tip` or `photo`, and this server
does not subscribe to that. So the handler reads the `checkin` parameter **by name** and treats its
absence as a body it has nothing to do with — **not** as a shape to grow branches for. There is no
`like`, `tip` or `photo` handling in this milestone and none is wanted.

**There is no IP allowlist.** Foursquare does publish a range — "you must whitelist the IP range
`199.38.176.0/22` in order to receive pushes" — and the observed request did not come from it; it
came from an AWS us-east-1 address. That is **not evidence that the range is wrong**: the push was
captured with a webhook recorder, so the address seen is as likely to be the recorder's egress as
Foursquare's. The published range stands unrefuted and untested.

So the reason for not filtering is not the range's accuracy but what filtering would buy. `secret`
already authenticates the push, and it authenticates it against forgery rather than against an
address that a proxy, a tunnel or a future migration rewrites. An allowlist would add a failure
mode with no symptom — pushes silently dropped by a rule nobody re-checks — for a credential
check it does not strengthen. If one is ever wanted, the range has to be verified first against an
endpoint terminating HTTPS directly, not through a recorder.

The push URL must accept **HTTPS on the default port (443)**, which the documentation requires.
It also says self-signed certificates are fine, so what a reverse proxy has to provide is TLS on
443, not a publicly trusted chain. The URL is configured on the application itself, not per user.

### The push body carries a credential and personal data

Besides the push secret, the body holds the checked-in user's `email`, `birthday` and `gender`.
Two consequences, both recorded where they apply: Step 6's request logger never logs a request
body, which already covers this one, and a payload committed as a test fixture has its secret and
email redacted.

Foursquare's own framing of the channel goes further than the secret. The Real Time documentation
says a User Push application receives **all** of a user's check-ins — "public, friend restricted,
and private" — and that an application is expected "not to share, retransmit to third parties,
retransmit unsecurely, or cache indefinitely any information we transmit to you". `checkins.raw`
is an indefinite cache of that payload by design, so the trade is written down rather than
assumed: a self-hosted server storing its own operator's check-ins on the operator's own disk is
the mildest reading of it, the redaction rule above is what keeps the payload out of the
repository, and nothing in this milestone forwards a check-in anywhere.

### Webhook responses

| Situation | Response | Why |
| --- | --- | --- |
| `secret` missing or not matching | 401, empty body | Matches how `requireUser` answers elsewhere |
| The form does not parse | 400 | |
| No `checkin` parameter in a form that otherwise parsed | **200** | A User Push never omits it, so the body is malformed or meant for something else. Refusing it buys nothing and the consequence of a non-200 is unknown, so it is logged and dropped |
| `foursquare_user_id` is not registered here | **200** | A push application can be authorised by users this server has never heard of. Their check-in is not this server's to store, so the request was handled correctly and says so. Logged and dropped |
| The store fails | 500 | The write did not happen, so the response does not claim it did. What Foursquare makes of a 500 is unknown, per below — this row is honesty, not a redelivery request |

`secret` is compared in constant time (`crypto/subtle`), in `internal/auth` — it sits above
`store` and below `httpapi`, and `CheckAbsentPassword` is already the precedent for treating
timing as part of a credential check.

These codes are for this server's own diagnosis, and **nothing in the table above expects
Foursquare to act on them.** No retry is documented, and neither is any consequence of a non-200
reply. What the documentation covers is the timeout: a push that does not get a `200 OK` quickly
enough "will timeout ... and this will be recorded as a push failure", and time-consuming work
should be done asynchronously so the 200 can go out immediately. Whether
a 4xx or 5xx is counted the same way, whether anything is redelivered, and whether a URL that
keeps failing is disabled are all **unknown and must not be assumed** — those are exactly the
things a reader would reach for to justify the table above, and the table does not rest on them.

That leaves a timeout as the failure mode to design against rather than a retry storm. One SQLite
upsert is not time-consuming work, so **the write stays synchronous** and the 200 is the truthful
answer rather than a receipt; if that ever stops being true, the documented shape of the fix is to
answer 200 first and write after. The fetch path is what covers a push that never landed, since
nothing is known to bring it back.

`http.Request.ParseForm` reads up to 10 MiB of form body by default. The observed payload was
3554 bytes, so the handler wraps the body in an explicit `http.MaxBytesReader` rather than
inheriting that.

Where the push URL is configured is worth pinning down, because the documentation describes two
unrelated mechanisms under similar names. The one this milestone uses is **"Push API
notifications" on the application's own page**, reached from the app list and its "Edit This App"
button. The page titled "Configure Server Webhooks" is a different product — Movement SDK
webhooks, authenticated by a `Pilgrim-Secret` **header** rather than a `secret` form field — and
following it would produce a route that never receives a check-in.

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

Not decided yet — Milestone H has not started. `docs/architecture.md` covers the one thing
already settled (the router), for the reasoning that also applies here.

**Candidates for Milestone H**

| Purpose | Candidate | Notes |
| --- | --- | --- |
| Sessions | `github.com/alexedwards/scs/v2` | Has a SQLite store; better maintained than `gorilla/sessions` |
| CSRF | Standard `net/http.CrossOriginProtection` | **Landed in the standard library in Go 1.25** (`Sec-Fetch-Site` based). Confirm on starting whether an external dependency can be avoided |
| Templates | `github.com/a-h/templ` or standard `html/template` | templ is type-safe but adds a code-generation step |
| Page updates | htmx | Avoids pulling in a Node build chain |
| Map rendering | MapLibre GL JS or Leaflet | The one place JS is unavoidable. Vendor it into `embed.FS` rather than using a CDN, to keep a single binary |

An SPA (React, etc.) can also keep the single-binary property by embedding the build output in
`embed.FS`. The only trade-off there is needing a Node build chain, and the router choice is the
same either way.

**Open question: how the browser authenticates against `/api/v1`**

**The policy is to reuse the existing `/api/v1` rather than add UI-only data endpoints**
(browser-specific routes such as login and sessions are added in this milestone instead), so the
browser will also call `/api/v1/points` and friends — but `/api/v1` accepts only Bearer /
`api_key`, with the session cookie planned for everything else. To be decided when Milestone H
starts. The current front-runner is **(a)**.

- **(a) The `/api/v1` middleware also accepts the session cookie** — lets the UI simply fetch.
  Accepting cookies means `/api/v1` needs CSRF protection too, but Go 1.25's
  `CrossOriginProtection` can be applied server-wide, so the added cost is small.
- (b) The UI calls handlers/store directly in-process — keeps the authentication split, but
  changes "reuse the API" from reusing the HTTP API to reusing the implementation.
- (c) Hand the api_key to the UI at login and call with Bearer — not recommended, since XSS
  would leak the API key.

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

---

## Milestone B — The app connects

### Step 6: Request logging for endpoint discovery

Small, but its output is a planning input for everything after: the iOS app is closed source,
so this is how the remaining endpoint list gets confirmed.

- [x] Request-logging middleware behind `TRAVELMAP_DEBUG_LOG_REQUESTS=1`, logging unmatched
      routes too. Everything not yet implemented keeps returning 404 (see
      "An endpoint this server does not implement answers 404" in `docs/api-notes.md`), which is
      both the correct answer and what makes the log a complete list of what the app wants
- [x] **Redact `api_key` and `Authorization` before logging.** The whole point is to capture
      real device traffic, which carries live credentials
- [ ] Record the endpoints a real device actually hits in this file, and diff them against the
      list the community Android client uses (see "About the iOS app")
- [ ] **Never log a request body.** `POST /api/v1/auth/login` already carries a password in its
      own body, so this is not a hypothetical: a logger written as "log the body unless told
      otherwise" leaks one on the first login it sees. Later routes then inherit the default
      instead of each having to remember — Step 18 adds one whose body carries a shared secret

**Settles**: what may and may not appear in logs — bodies as well as the credentials that arrive
in a header or the query string.

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

## Milestone E — Browsing

Steps 10 and 11 are independent of each other and can run in parallel.

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

---

## Milestone I — Swarm check-in collection

Collect Swarm (Foursquare) check-ins, so that the explicitly recorded landmark sits alongside
the automatically recorded GPS trace. The first feature that is travelmap's own rather than
upstream's — read "Keeping the two parts apart" in `docs/api-notes.md` before adding a route
here.

Independent of the points/stats pipeline: it touches neither `points` nor `daily_stats`. What it
does need is the store foundation and the authenticated router, both already in place — Step
18 hangs a route off the same router, and Step 20 reuses the `api_key` credential. So it can be
taken at any time after Milestone B, like Step 16.

External behaviour these steps rely on is in "Foursquare / Swarm Integration Notes"; the two
tables are in `internal/store/sqlite/schema.sql` and docs/database.md, per "Data Model".

There are seven `TRAVELMAP_FOURSQUARE_*` settings, and no step adds all of them: the push secret
belongs to Step 18, the lookback and the API URL to Step 19, the three OAuth settings to Step 20,
the interval to Step 21. Steps 18 and 19 run in parallel, so neither can wait on the other for a
variable. An eighth setting joins whichever step needs it.

**Each step documents its own settings in the README, in the same pull request.** The README is
for someone about to run this server, so a knob that is listed there and does nothing yet is the
one kind of drift its reader cannot detect — and this milestone's settings come with procedures
(an OAuth URL, a CLI invocation) that would invite a reader to try something not built. The
settings and their defaults are recorded here in the meantime, which is what `TODO.md` is for.

Whichever of Steps 18 and 19 lands first opens a check-in section under the README's
"Configuration" — either one collects check-ins on its own, so neither can claim to be the step
that starts the feature, and the later one adds to what the earlier one wrote. That section
carries the one fact no single setting does: **nothing is collected until an account is linked**,
which until Step 20 exists means `travelmap foursquare connect`. Without it a reader can set
every variable, run `foursquare sync`, and be told nothing about why the result is empty.
Step 20 adds its browser flow to the same sentence when it lands, which is its own README item.

Steps 18 and 19 are parallel; Step 21 follows Step 19, and Step 20 can be taken at any point or
left until last, needing nothing this milestone has not already shipped.
`travelmap foursquare connect` exists precisely so the collecting steps can be finished and run
for real before the OAuth flow is written. Until it is, the access token comes out of the
Foursquare application's own console, which issues one for the account that owns the application;
that is the whole of what Step 20 later automates.

**Step 19 grew a second step rather than one long checklist.** Calling the endpoint and knowing a
page-walk succeeded is one decision; how that client copes with a rate limit, an ambiguous error
code, or a check-in whose language disagrees between paths is another, confirmed empirically
rather than designed alongside the first. That second half is Step 22 — it follows both 18 and
19, and nothing in this milestone waits on it in turn.

### Step 18: The push webhook

- [ ] `POST /webhooks/foursquare`, registered **at the top level, outside
      `r.Route("/api/v1", …)`** — no `authenticate`, no `dawarichHeaders`, no `requireUser`
- [ ] `TRAVELMAP_FOURSQUARE_PUSH_SECRET` in `internal/config`, and the route registered **only
      when it is set**, so an unconfigured server answers 404 like any route it does not
      implement
- [ ] Form parsing under an explicit `http.MaxBytesReader` in the handler, and a constant-time
      secret comparison in `internal/auth`
- [ ] The handler then hands the raw `checkin` value to `internal/checkin`, which parses it
      through `internal/foursquare`, resolves `checkin.user.id` against `foursquare_accounts`
      and writes. Keeping the parse behind that package is what lets Step 19 reach the store by
      the same road
- [ ] The response codes in "Webhook responses" — in particular **200 for a Foursquare user
      this server does not know**
- [ ] README: the push secret, and that the webhook needs a URL Foursquare can reach over HTTPS
      — so it works behind a reverse proxy and not on a laptop
- [ ] The wire struct **ignores unknown fields and makes almost every field optional**, per "The
      push payload" — the documented sample and the observed one disagree about which fields exist
      at all, so a struct that requires any of them breaks on the next payload Foursquare widens
- [ ] Pin the parse with a fixture: the recorded push body in `internal/foursquare/testdata/`
      **with the secret and the email redacted**, and the wire struct it parses to compared by
      go-cmp — the wire shape, not `model.Checkin`, since `internal/checkin` owns that
      conversion (see CLAUDE.md's layering rules). This fixture is the compatibility contract
      for this route, the role golden files play for responses
- [ ] Confirm the request logger leaves this route's body alone — Step 6's "Never log a
      request body" already decides it, and this is the route that would hurt most

**Settles**: where a non-Dawarich route lives and how it authenticates, and that a body carrying
a credential is never logged.

**Done when**: replaying the recorded payload against a running server stores one check-in, and
replaying it again still leaves one.

### Step 19: The Foursquare API client

One fetch, run by hand, against an API that is answering normally. Two things are split out of
this step, for two different reasons. The timer that repeats it is Step 21 — a client and a
scheduler settle different conventions, and this half already reaches real check-ins on its own.
This client's response to rate limits, ambiguous error codes and locale drift is Step 22 instead:
none of that is decided by this step's design, only confirmed once it exists — and, for locale,
once Step 18's push path has a check-in to compare against.

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

**A limitation to accept knowingly**: there are no browser sessions until Milestone H, so the
only way `start` can name a user is the `api_key` query parameter. That is consistent — every
endpoint here accepts it — but **it puts the API key in browser history and in the `Referer` of
the redirect**. Replace it with session authentication when Milestone H lands. If that is not
acceptable, hold this step until Milestone H and keep using `foursquare connect` in the
meantime; the rest of the milestone does not depend on it.

### Step 21: The periodic fetch worker

Follows Step 19, whose sync run this repeats on a timer; nothing in Step 20 is in the way, and
Step 22's hardening is not required first either — see that step's note on why.

- [ ] `internal/config`: `TRAVELMAP_FOURSQUARE_SYNC_INTERVAL` (default `1h`, `0` disabling the
      fetch), which brings duration parsing into that package
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
table**, per the "Scheduling" row under "Technical Decisions".

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
both paths to compare. Depends on Step 19, which this extends, and on Step 18 for the check-in
the locale bullet compares against.

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
  collects check-ins, just with a delay. Step 18 says so in the README when it adds the route.
- **The push secret belongs to the Foursquare application, not to a user.** Keep the split it
  implies: the secret is server configuration, and identifying whose check-in arrived is
  `foursquare_user_id`'s job. Deriving a user from the secret would break the moment a second
  person connects.
- **Revisit whether `internal/ingest` should be named `internal/usecase` (or `service`) instead.**
  `usecase`/`service` are the more familiar names for this layer in most Go codebases; `ingest`
  was picked because this milestone's whole job is literally ingesting device locations, which
  does not generalise as an argument once `internal/checkin` (Milestone I) is the same shape of
  layer over a different kind of write. Revisit once `internal/checkin` exists alongside
  `internal/ingest`, so the comparison is between two real packages rather than a naming
  preference argued in the abstract. A rename this late only costs an import-path edit — nothing
  in the layering itself depends on the name — so there is no cost to waiting for the evidence.
