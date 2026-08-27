# Architecture

Why travelmap is built the way it is: the technical decisions already implemented, the
third-party libraries chosen, and why chi is the router even before a web UI exists to need it.

A decision for something not built yet is still in [TODO.md](../TODO.md)'s own "Technical
Decisions" section, and moves here once the step that implements it lands.

## Technical decisions

| Item | Decision | Rationale |
| --- | --- | --- |
| Data store | SQLite (`modernc.org/sqlite`, no CGO) | Self-contained in a single static binary. No DB process, so the footprint is minimal. Chosen after benchmarking (commit 59d0e04) |
| Store abstraction | Repository layer behind an interface, reached through a `store.Store` that also runs transactions | Leaves room to add a PostgreSQL implementation later. Handing repositories out through one object is what lets a point insert and its `daily_stats` rebuild share a transaction |
| Migrations | `github.com/pressly/goose/v3` as a library, over numbered `.sql` files in an `embed.FS` | Measured by writing both against the same API: 86 lines of migrator and 107 of test with goose, against 213 and 137 hand-rolled, for one direct dependency and four indirect ones. Down migrations, Go-code migrations and an applied-at history come with it. The cost is having to read goose's bookkeeping rather than own it: **it creates and commits its version table, with a row for version 0, before the first migration runs**, so "the version table exists" is not "the schema exists" — a hand-rolled migrator bumping `user_version` inside the DDL's own transaction had no such state |
| Connections | One connection (`SetMaxOpenConns(1)`), WAL, `synchronous=NORMAL`, `BEGIN IMMEDIATE`, `busy_timeout=5s` | SQLite has a single writer, so a larger pool converts concurrent writes into `SQLITE_BUSY` for every caller to retry; with one they queue in `database/sql`. The timeout is for the second process — a CLI command run while the server is serving. Two races are left unhandled deliberately, both needing something the workflow does not do: several processes opening a **brand-new** database at the same moment, where converting the file to WAL answers `SQLITE_BUSY` and no timeout can wait it out; and two `travelmap migrate` processes at once, where goose has no lock for SQLite and the loser fails on the DDL instead of reporting nothing to do. The first run is one `travelmap migrate`, and retry logic for a race nobody reaches would cost more than it returns |
| SQL | Hand-written in `internal/store/sqlite`, no generator | The queries are few and shaped by the API's own filters, and a generator would add a code-generation step to a checkout that today needs nothing installed but Go |
| Schema | `STRICT` tables, times as integer Unix seconds | Without `STRICT`, type affinity stores a string in an `INTEGER` column and the mistake surfaces as a query that quietly stops matching. Times are integers because `points.timestamp` arrives as a Unix timestamp and is range-compared on every request |
| Indexes | A single `points(user_id, timestamp)` | App queries always narrow by user and time range first. See [`docs/database.md`](database.md) |
| Distance | Haversine in SQL for aggregation, `internal/geo` (Go) for one-off calculations | Pulling a whole-history aggregation into Go spends all its time transferring rows. See [`docs/database.md`](database.md) |
| Statistics | `daily_stats` precomputed table, updated during ingest | Aggregating `points` on demand is too slow to serve a request. See [`docs/database.md`](database.md) |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Chosen with the future web UI in mind — see "Router" below |
| Compatibility scope | Mobile-app subset | Everything except the non-goals in [TODO.md](../TODO.md)'s "Goal" section |
| User management | Issued via CLI. `auth/login` implemented, `auth/register` optional behind an env var, no 2FA | Self-hosted assumption |

## Libraries

Keep dependencies minimal.

| Purpose | Package | Notes |
| --- | --- | --- |
| Routing | `github.com/go-chi/chi/v5` | Rationale under "Router" below |
| SQLite driver | `modernc.org/sqlite` | Pure Go. No CGO. Requires Go 1.25+ |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Migrations | `github.com/pressly/goose/v3` | Used as a library, not a CLI: the provider API takes the `embed.FS` and the `*sql.DB` this server already has, so the migrations stay inside the binary. `.sql` files carry goose's `-- +goose Up` annotation, and **a comment inside one cannot name an annotation**, because goose reads any comment that does as the annotation itself |
| Test diffs | `github.com/google/go-cmp` | |
| Configuration | No extra dependency (a thin hand-written env-var loader) | |
| Logging | Standard `log/slog` | |

Candidates for a future web UI — not added yet — are in [TODO.md](../TODO.md)'s "Library Choices
for the Web UI".

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

Everything the web UI itself needs beyond the router — sessions, CSRF, templates, map rendering —
is still an open choice; see "Library Choices for the Web UI" in [TODO.md](../TODO.md).
