# API

This explains travelmap's API. It has two parts: a subset compatible with
[Dawarich](https://github.com/Freika/dawarich)'s own API, for any client built for the Dawarich
mobile app; and travelmap's own extensions, for functionality Dawarich has no equivalent of.

`docs/openapi.yaml` is the accompanying source for the API's contract itself — paths, schemas,
headers, status codes for the endpoints actually implemented. Read it first; what follows here
is whatever does not fit there: upstream's quirks, why travelmap's behaviour deliberately
differs, and the client evidence behind each choice.

travelmap also serves a small HTML surface for a browser — `GET /` so far — at routes outside
`/api/v1`. `docs/openapi.yaml`'s contract is for the JSON API above; an HTML route is not part of
it, and this document does not cover it either.

## Keeping the two parts apart

Not everything travelmap stores has to come from upstream. Swarm (Foursquare) check-in
collection is the first feature that is travelmap's own: explicitly recorded landmark data,
collected to enrich the automatically recorded GPS trace.

Such a feature gets **its own tables and its own routes at the top level, never a path under
`/api/v1`**. Dawarich has no version negotiation, so clients read a 404 under `/api/v1` as
"feature unsupported" (see "An endpoint this server does not implement answers 404" below);
inventing paths in that namespace would make that signal meaningless. Keeping the compatibility
surface exactly upstream's is what keeps the 404 rule true.

## Dawarich compatibility

For this part, upstream's own OpenAPI document is the compatibility source of truth, and
`docs/openapi.yaml` is travelmap's contract against it.

- Source: `https://raw.githubusercontent.com/Freika/dawarich/master/swagger/v1/swagger.yaml`
- Fetched: 2026-08-17
- Fingerprint: 5680 lines / `sha256:a16411a389e0130d9e0b04b54cfc80726c234b8a017cc76d9d921bfc91adc89a`

Upstream changes continuously, so update the fingerprint above whenever the spec is re-fetched.
A running Dawarich instance also serves the same document at `/api-docs`.

### Authentication

Either an `api_key` query parameter or an `Authorization: Bearer {api_key}` header is accepted,
on every endpoint. Upstream's own spec documents only one of the two per endpoint, but the
community Android client sends `Authorization: Bearer` on everything, including
`/api/v1/points`, which the spec documents as query-only — so the spec's per-endpoint split does
not reflect what clients actually do.

### `GET /api/v1/health`

Answers on the assumption that the app's server-URL validation goes through here (unconfirmed
against a real device, but the endpoint is needed regardless, so being wrong costs no rework).

**The `X-Dawarich-Response` and `X-Dawarich-Version` headers belong on every `/api/v1` response,
not just this one** — a client is free to read the version off any response it gets, and
`X-Dawarich-Response` reports whether the request authenticated.

One consequence: a request **carrying a key** is answered 500 if the database cannot be read,
even here — that is upstream's own behaviour too, and it is the honest answer, since a server
that cannot read its database is not one a client should be told is fine. A request carrying
**no** key never triggers that lookup at all, which is a deliberate difference: upstream looks
up a user whose `api_key` is NULL either way.

### `POST /api/v1/auth/login`

With 2FA enabled upstream returns 202 plus a `challenge_token`; this project always returns 200,
having no two-factor authentication to challenge with.

The last four response fields (`status`, `plan`, `subscription_source`, `active_until`) are
upstream Cloud's billing fields, which this project does not have. They answer with what a
**self-hosted upstream instance** ends up reporting, so that a client gating a feature on them
sees what it would see there: `status` `active`, `plan` `pro`, `subscription_source` `none`, and
an `active_until` far enough out never to have passed — upstream activates a self-hosted user
with `active_until: 1000.years.from_now`, and this server sends the constant
`9999-12-31T23:59:59Z` because it has no subscription to expire. Note that it carries **no
milliseconds**, unlike the timestamps of `users/me` below: upstream renders this one without
them, and the two really do differ.

**A refused login is the one 401 with a body**: upstream renders
`{"error": "auth_failed", "message": "..."}`, which is also the 401 the spec documents for this
endpoint. Every way of failing gets that same answer — wrong password, unknown address, an
address that is not one, no password field at all — and an unknown address takes as long to
refuse as a wrong password does, so that how long the refusal takes does not say which addresses
have accounts.

### `GET /api/v1/users/me`

**The spec documents no response body for it** ("user found", no schema), so the shape was read
off upstream's own implementation instead. Three things follow. The user object **carries no
id** — a client that needs one has the `user_id` of `auth/login`. The `subscription` key beside
`user` is Cloud-only, so it is not sent on a self-hosted instance. And `settings` is a **smaller
set than `GET /api/v1/settings` answers with**: the keys upstream picks (`maps` among them), in
its order. Nothing stores them yet, so this endpoint answers upstream's own defaults;
`immich_url`, `photoprism_url` and `speed_color_scale` are `null`, and the first two stay that
way, being a non-goal.

Timestamps are written **RFC 3339 with milliseconds** (`2026-02-03T04:05:06.000Z`), matching
upstream's own encoding — a client parsing with a fixed format string would fail on a value
without them.

### `POST /api/v1/points` and `POST /api/v1/overland/batches`

Both take the same GeoJSON batch and differ only in the success status: 200 for points, 201 for
overland.

`speed_accuracy` and `track_id` are accepted but not stored: upstream itself does not persist
either, keeping them only in the raw device payload it archives — which this server does not
have, having no use for it.

**Neither response body is part of the compatibility contract.** Upstream answers
`{"data": [...]}` and `{"result": "ok"}` respectively, but the community Android client checks
only the status code, so both answer with the count of points actually inserted after
deduplication.

A Feature missing usable coordinates or a parseable timestamp is dropped rather than failing the
whole batch, matching upstream's own tolerance: a batch is a stream of device samples, and one
bad sample should not cost the rest of it.

A 1000-point batch runs to roughly 435 KB as JSON, comfortably inside the request size limit.

`timestamp` is accepted in both RFC 3339's own zone-offset form (`-07:00`) and the original
Overland iOS app's (`-0700`, no colon — see its README's example payload): `/overland/batches`
exists to accept that app's own format.

### `GET /api/v1/points`

A purely numeric `start_at`/`end_at` is read as Unix seconds deliberately, not as a side effect of
a lenient parser: upstream's own `SafeTimestampParser` accepts one on purpose, its comment calling
out treating a numeric string as a timestamp. An unparseable value here answers 400 rather than
upstream's own fallback to "now" for anything its parser cannot read — not worth reproducing,
since no known client sends anything but the documented formats. `page` and `per_page`'s fallback
values match upstream's own `to_i; default unless positive?`.

**`latitude`, `longitude`, `velocity`, `course` and `course_accuracy` are JSON strings, not
numbers, which this project trusts over the published OpenAPI schema's declared `number`
type** — read off upstream's actual `Api::PointSerializer` and its `points` schema
(`db/schema.rb`), not the document. Upstream's serializer renders latitude/longitude with an
explicit `.to_s`; velocity is a string column in its own schema; course and course_accuracy are
`decimal` columns, which Rails' JSON encoder always renders as a string to avoid the precision
loss a JSON number would risk. The community Android client's `ApiPointDTO` casts exactly these
fields (`latitude`, `longitude`, `velocity`) to `String` with no numeric fallback — an
unconditional cast that fails outright on a bare JSON number — which is the client evidence this
rests on; course and course_accuracy are not in that DTO or the published schema at all, but
travelmap stores both, so they are sent anyway, as strings, for the same column-type reason.

Every field upstream's serializer sends that travelmap has no data for yet (`docs/openapi.yaml`'s
`Point` schema says which) is sent as what a fresh upstream point already looks like before
reverse geocoding, an import or a visit ever touches it — the columns' own defaults in upstream's
schema, not an arbitrary placeholder invented here.

Upstream also accepts a `slim=true` parameter that answers a narrower per-point shape
(`id`, `latitude`, `longitude`, `timestamp`, `velocity`, `country_name`, `tracker_id`); it is not
in the published spec at all, only in the community client's own request code. This server
always answers the full shape above regardless of `slim`: an unrecognised query parameter is
ignored rather than acted on, the same as any other endpoint here, and every field the slim shape
asks for is already present in the full one, so a client that requested `slim` still gets
everything it parses.

### `GET /api/v1/points/tracked_months`

Answers `[{"year": 2024, "months": ["Jan", "Feb", ...]}]`, read from `daily_stats` rather than
aggregating `points` directly — the same source `/stats` uses, and the reason both can be wrong
together if `daily_stats` itself ever drifts. Years are most-recent-first; within a year, months
are in calendar order, matching upstream's own `years_tracked` SQL, which walks backward from the
latest point one month at a time.

No known client actually calls this endpoint: the community Android client defines the URL as a
constant but never issues a request to it, so its own behaviour rests on the published spec's
example rather than observed traffic.

### `GET /api/v1/stats`

The only endpoint using **camelCase** (`totalDistanceKm`, `totalPointsTracked`,
`totalReverseGeocodedPoints`, `totalCountriesVisited`, `totalCitiesVisited`,
`yearlyStats[].monthlyDistanceKm.january`, …) — everything else here is snake_case. All five
top-level totals, and the yearly/monthly breakdown, are aggregated from `daily_stats` alone; see
`docs/database.md` for why `daily_stats` is the only thing allowed to feed them.

**Every distance is a whole number of kilometres, not a fractional one, despite the published
schema declaring `number`.** Upstream stores distance in metres as a Ruby `Integer` column, and
`Integer / Integer` division in Ruby floors rather than rounds — so `totalDistanceKm` and its
yearly and monthly counterparts are always upstream's own metres-to-kilometres floor division,
never a precise float. The community Android client's `StatsDTO` (and `YearlyStatsDTO`,
`MonthlyStatsDTO`) declare these fields as a Dart `int`; Dart's JSON decoder produces a `double`
for any number carrying a decimal point, and assigning that to a declared `int` field throws at
parse time. Sending the fractional kilometre total `daily_stats.km` actually stores would crash
that client's stats screen, so travelmap truncates to `int64` at the response boundary instead of
rounding — matching upstream's own precision loss, not just avoiding the crash by coincidence.

### Every response carries `Content-Type: application/json; charset=utf-8`

Upstream sends the charset, and it is worth copying rather than sending a bare
`application/json`: Dart's `http` package — what the community Android client is built on —
decodes a body whose `Content-Type` names no charset as latin1, so every non-ASCII city or
country name would arrive as mojibake. Every response carries it, error ones included.

### Error responses are a bare `{"error": "..."}`

Upstream renders `{"error": "<message>"}` and nothing else — no code, no details, no field a
client could match on other than the message. That is the error body of every failing request
here.

One exception is already visible in upstream's own behaviour: a request that fails
authentication is answered with no body at all, so a **401 has an empty body**. This server
reproduces that rather than sending the error body, since a client parsing the body of a 401 is
parsing nothing.

The other exception is `POST /api/v1/auth/login`, which renders `error` **and** `message`. So a
401 has an empty body everywhere except the one endpoint whose whole purpose is to tell a client
whether its credentials work.

### An endpoint this server does not implement answers 404

**Never an empty array or empty object with 200.** Dawarich has no version negotiation. Feature
detection is done by calling the endpoint and treating **404 as "this server does not support
the feature", at which point the client hides it entirely** (upstream PR #3067 introduces
`/api/v1/demo_data` on exactly this basis). A 200 with an empty body therefore tells the app the
feature *exists*, and it will surface UI that then misbehaves.

### Every GET route also answers HTTP HEAD

Clients probe with `HEAD` before fetching a GET route's body — for example, reading
`X-Total-Pages` off a paginated list before deciding whether to fetch any pages at all. Every GET
route this server serves therefore also answers HEAD, with the same status and headers.

Verified in the community Android client.

## travelmap's own extensions

Functionality with no Dawarich equivalent, at routes outside `/api/v1` — see "Keeping the two
parts apart" above for why. None of the rules above apply to this part: no Dawarich headers, no
`api_key`, and none of it answers a bare `{"error": "..."}`, since there is no Dawarich client
reading these responses to keep the shape of.

### `POST /webhooks/foursquare`

A Swarm User Push notification. Registered only when `TRAVELMAP_FOURSQUARE_PUSH_SECRET` is set;
an unconfigured server answers 404 like any endpoint it does not implement, rather than 401 to
every request — consistent with "An endpoint this server does not implement answers 404" above
even though this route is outside the Dawarich-compatible surface that rule was written for.

The status codes and their (always empty) response bodies are in `docs/openapi.yaml`; what
follows is why they are what they are.

Neither 200 case is an error. A User Push notification is not documented to ever omit `checkin`,
so a request that lacks it, or carries something that does not parse as JSON, is handled rather
than refused — the same form shape is shared with the Venue Push API, whose second parameter is
one of `like`, `tip` or `photo` instead, so such a request simply has no `checkin` parameter to
find. A `checkin.user.id` this server has no linked account for is likewise not an error: the
check-in is not this server's to store. Foursquare does not document what a non-2xx reply does —
only that a push not answered `200 OK` quickly enough "will timeout ... and this will be recorded
as a push failure", with no stated retry or disable behaviour either way — so neither case is
refused. A 200 here means the check-in has already been stored, not merely accepted for later
processing. Delivering the same check-in more than once updates the same stored row rather than
creating a duplicate.

The payload's shape and its quirks — which fields are actually optional, what is localised and
what is not, what a quoted-looking-numeric id means — are on the `FoursquareCheckin` schema and
the schemas it references.

There is no IP allowlist. Foursquare publishes one (`199.38.176.0/22`), but an observed push
arrived from an AWS us-east-1 address, not from it — inconclusive, since the push was captured
through a webhook recorder whose own egress could equally explain it. `secret` already
authenticates the push against forgery, which an address-based filter would not strengthen.

The secret is compared in constant time, so a request cannot learn anything about it from how
long the comparison takes.
