# Architecture

Why travelmap is built the way it is: the technical decisions already implemented, and why chi is
the router even before a web UI exists to need it.

A subsection here is one decision and the part of its rationale that has no other home. Where the
same ground is already covered by a schema.sql/migration comment, a package's own comment, or
`docs/database.md`, this file points there instead of repeating it.

## Technical decisions

### Data store

SQLite (`modernc.org/sqlite`, no CGO): self-contained in a single static binary, with no DB
process to run. Chosen after benchmarking (commit 59d0e04).

### Store abstraction

A repository layer behind an interface, reached through a `store.Store` that also runs
transactions. This leaves room to add a PostgreSQL implementation later, and handing repositories
out through one object is what lets a point insert and its `daily_stats` rebuild share a
transaction.

### Migrations

`github.com/pressly/goose/v3` as a library, over numbered `.sql` files in an `embed.FS`. Used as a
library rather than a CLI so the provider API takes the `embed.FS` and `*sql.DB` this server
already has, keeping migrations inside the binary. Down migrations, Go-code migrations and an
applied-at history come with it, at the cost of one direct dependency and four indirect ones — the
trade that won out over hand-rolling a migrator. `.sql` files carry goose's `-- +goose Up`
annotation, and **a comment inside one cannot name an annotation**, because goose reads any
comment that does as the annotation itself. How this package reads goose's own bookkeeping (a
version table that exists before the schema does) is commented in
`internal/store/sqlite/migrate.go`.

### Connections

One connection, WAL, `busy_timeout`, immediate transactions (the pragma-level rationale is
commented in `internal/store/sqlite/sqlite.go`). Two races are left unhandled deliberately, both
needing something the workflow does not do: several processes opening a **brand-new** database at
the same moment, where converting the file to WAL answers `SQLITE_BUSY` and no timeout can wait it
out; and two `travelmap migrate` processes at once, where goose has no lock for SQLite and the
loser fails on the DDL instead of reporting nothing to do. The first run is one
`travelmap migrate`, and retry logic for a race nobody reaches would cost more than it returns.

### SQL

Hand-written in `internal/store/sqlite`, no generator. The queries are few and shaped by the API's
own filters, and a generator would add a code-generation step to a checkout that today needs
nothing installed but Go.

### HTTP routing

`net/http` + `github.com/go-chi/chi/v5`, chosen with the future web UI in mind — see "Router"
below.

### Recalculation trigger

CLI only (`travelmap recalculate`), not exposed as `/api/v1/recalculations`. Rebuilding
`daily_stats` and tracks is only needed after an import, an inconsistency, or a `tracking.timezone`
/ `tracking.track_break_minutes` change — all operator-run locally on a self-hosted instance.
Revisit if triggering it from the app turns out to be necessary.

### HTML rendering

`html/template`, parsed once at startup, with the templates and the CSS in an `embed.FS`. Keeps
deployment a single binary, and adds neither a code-generation step nor a Node build chain to a
checkout that today needs nothing installed but Go.

### Browser sessions

`github.com/alexedwards/scs/v2` over a `sessions` table of this project's own. scs brings no
dependencies of its own: its `go.mod` has no `require` entries at all, the same property chi was
checked for. Its bundled `sqlite3store` is **not** usable — that module requires the CGO
`github.com/mattn/go-sqlite3` — so the store is written here against `internal/store` instead.
Chosen over a JWT, which has no defensible default for its signing key (one generated at startup
logs every user out on restart, so it becomes a required setting) and which cannot be revoked,
leaving `POST /logout` able only to clear the cookie while the token stays valid until it expires.
That is not a cost saved but a part of the feature missing.

### Browser CSRF

Standard `net/http.CrossOriginProtection`, on the browser routes only. Present in the toolchain
`go.mod` already names (`go 1.26.6`), and its `Handler` is a plain
`func(http.Handler) http.Handler`, so this costs no dependency, which is what needed confirming
before it could be chosen. `/api/v1` keeps Bearer / `api_key` only and needs none.

### Swarm check-ins

Collected by webhook push, with an API fetch as the backstop. Push is immediate but nothing
documented makes it reliable: Foursquare publishes no retry, records a timed-out push as a
failure, and reaches only a public HTTPS endpoint. What it does after a failure is not documented
at all. Fetching alone would lag every check-in by up to a poll interval. **What each path does
and does not see is only partly known** — whether a push fires for a check-in added after the
fact, or for an edit to one already stored, is undocumented — which is itself a reason to run
both. How the fetch is made is under "Fetching Swarm check-ins" below.

### Foursquare API version

v2 (`/v2/users/self/checkins`), with `v=` pinned as a constant. v2 is what returns Swarm
check-ins, and it is current rather than abandoned: it is documented today as the
"Personalization APIs", and the endpoints this uses are the ones that remain free. `v=` is a date
Foursquare uses to freeze response shape, so it is a constant raised deliberately after checking
behaviour, never "today". The request this client makes is under "Fetching Swarm check-ins"
below.

### Background workers

A ticker goroutine per periodic task, over the signal-cancelled `context.Context`
`cmd/travelmap/serve.go` already holds, and no job table for a worker that just re-scans on every
tick. One process, so nothing between ticks needs recording, and `cmd/travelmap/serve.go` is the
only place holding both that context and the concrete store — which is why every worker of this
shape starts there. The session sweep (`cmd/travelmap/sweep.go`) is the first; why its interval is
a constant rather than a setting is commented on `sessionSweepInterval` there. A job table is worth
its own row only for the rare worker whose work is genuinely per-item rather than a periodic
re-scan — `internal/track`'s track-splitting worker is that exception, draining one row per user
that `internal/ingest` enqueued after a point write. What a specific worker's own interval is, and
whether it is this per-item exception, is documented next to that worker rather than listed here,
so this row does not grow with every worker added.

### Swarm OAuth linking

`GET /settings/foursquare/connect` and `GET /foursquare/oauth/callback` identify their user by the
browser session, not `api_key`. The login screen already existed by the time this was built, so
there was no reason to name the user with `api_key` in the query string first and move it onto
the session afterwards. The callback also requires the session's user and the single-use `state`
it minted to name the same one, since the browser returns from Foursquare by a top-level GET that
carries the `SameSite=Lax` session cookie.

### `server.base_url`

This server's own externally reachable URL, general-purpose rather than named for its one current
reader. The Foursquare OAuth callback URL is this plus a fixed path
(`/foursquare/oauth/callback`), not a second setting of its own: the path is already fixed by the
route this server registers, so asking an operator to type it out too would only invite a
mismatch between the two. Not derived from the request's own `Host` header either — a reverse
proxy or a spoofed header could disagree with what is actually registered on the Foursquare
application, where a byte-for-byte match is what OAuth requires.

Schema-shape decisions (`STRICT` tables, Unix-second timestamps, indexes, the distance and
statistics precomputation) are in `internal/store/sqlite/schema.sql`'s own comments and
[`docs/database.md`](database.md), not repeated here.

## Router

Since Go 1.22 `ServeMux` supports methods and wildcards, so the standard library would be enough
for an API server on its own. But a web UI is planned, and once it exists there are **two
authentication schemes**:

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

A travelmap-only route with its own authentication follows the same shape: registered directly
on `r`, outside `r.Route("/api/v1", ...)`, so it carries none of that group's middleware —
`POST /webhooks/foursquare` is the first of these. Its body carries a credential like
`auth/login`'s, which is already why the request logger never reads one at all (see
`internal/httpapi/requestlog.go`), route grouping aside.

## Fetching Swarm check-ins

The other half of check-in collection, alongside the push webhook: `internal/foursquare` calls
[`GET /v2/users/self/checkins`](https://docs.foursquare.com/developer/reference/get-user-checkins)
and `internal/checkin` writes what comes back. That page, and
[v2 authentication](https://docs.foursquare.com/developer/reference/v2-authentication), are the
reference for everything not stated here. The request is

```
GET /v2/users/self/checkins?v=<pinned date>&limit=250&offset=<check-ins already read>
Host: api.foursquare.com
Authorization: Bearer <access token>
```

The token is sent in a `Bearer` header, per v2 authentication.

### The walk tolerates what `offset` does under it

An `offset` walks a list that an arriving check-in shifts underneath it, so a page boundary can
hand a check-in back twice or step over one. Neither is prevented. A repeat is an upsert onto the
row already there, and `internal/checkin` keeps the run's own set of ids so it is skipped before
it is written; a check-in stepped over is inside the window the next run recomputes, so it arrives
one interval late rather than never. That is what makes the recomputed window below load-bearing
rather than merely convenient.

A walk stops at the window's start rather than at the end of the account's history, which is sound
only because the endpoint answers **newest first**: what has already been read is everything newer.

### The window is recomputed, never resumed

Every run re-reads a whole window — `foursquare.sync_lookback_days` (default 14) back
from now — rather than resuming from where the last one stopped.

A high-water-mark cursor cannot see two things, both of which happen whatever the push path does.
A check-in whose own timestamp sorts *before* the stored cursor is skipped, and then skipped
forever, which is the shape Swarm's retro check-in flow produces. And an **edit** to a check-in
already stored moves no timestamp at all, so a cursor never revisits it; `editableUntil` says edits
are expected long after the fact. The unique index on `foursquare_checkin_id` absorbs the overlap
that re-reading a window costs.

So `foursquare_accounts.synced_through` advances on success but is never the lower bound of what is
asked for — what it is and is not used for is under "`foursquare_accounts`" in
[`docs/database.md`](database.md). A wider `--lookback-days` on `travelmap foursquare sync` is
therefore how a backfill reaches further back than a routine run's window.
