# Architecture

Why travelmap is built the way it is: the technical decisions already implemented, and why chi is
the router even before a web UI exists to need it.

A row here is the decision and the part of its rationale that has no other home. Where the same
ground is already covered by a schema.sql/migration comment, a package's own comment, or
`docs/database.md`, this file points there instead of repeating it.

## Technical decisions

| Item | Decision | Rationale |
| --- | --- | --- |
| Data store | SQLite (`modernc.org/sqlite`, no CGO) | Self-contained in a single static binary, with no DB process to run. Chosen after benchmarking (commit 59d0e04) |
| Store abstraction | Repository layer behind an interface, reached through a `store.Store` that also runs transactions | Leaves room to add a PostgreSQL implementation later. Handing repositories out through one object is what lets a point insert and its `daily_stats` rebuild share a transaction |
| Migrations | `github.com/pressly/goose/v3` as a library, over numbered `.sql` files in an `embed.FS` | Used as a library rather than a CLI so the provider API takes the `embed.FS` and `*sql.DB` this server already has, keeping migrations inside the binary. Down migrations, Go-code migrations and an applied-at history come with it, at the cost of one direct dependency and four indirect ones — the trade that won out over hand-rolling a migrator. `.sql` files carry goose's `-- +goose Up` annotation, and **a comment inside one cannot name an annotation**, because goose reads any comment that does as the annotation itself. How this package reads goose's own bookkeeping (a version table that exists before the schema does) is commented in `internal/store/sqlite/migrate.go` |
| Connections | One connection, WAL, `busy_timeout`, immediate transactions (the pragma-level rationale is commented in `internal/store/sqlite/sqlite.go`) | Two races are left unhandled deliberately, both needing something the workflow does not do: several processes opening a **brand-new** database at the same moment, where converting the file to WAL answers `SQLITE_BUSY` and no timeout can wait it out; and two `travelmap migrate` processes at once, where goose has no lock for SQLite and the loser fails on the DDL instead of reporting nothing to do. The first run is one `travelmap migrate`, and retry logic for a race nobody reaches would cost more than it returns |
| SQL | Hand-written in `internal/store/sqlite`, no generator | The queries are few and shaped by the API's own filters, and a generator would add a code-generation step to a checkout that today needs nothing installed but Go |
| HTTP | `net/http` + `github.com/go-chi/chi/v5` | Chosen with the future web UI in mind — see "Router" below |
| User management | Issued via CLI. `auth/login` implemented, `auth/register` optional behind an env var, no 2FA | Self-hosted assumption |
| Check-in storage | Its own `checkins` table, not upstream's `visits` / `places` | Upstream's `visits` are detected from GPS dwell and then confirmed by the user (`status` is `suggested`/`confirmed`/`declined`, and its detector regenerates the suggested ones wholesale), and `places.source` is only `manual` or `photon`. A Swarm check-in is neither: putting it there would need a third `source` and would break what `status` means |
| Third-party credentials | A `foursquare_accounts` row per user, resolving a Foursquare user id to a travelmap user | A row rather than an env var because the webhook has to map a Foursquare user id to a travelmap user, which an env var cannot do. How the access token itself is stored is in `docs/database.md` |

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
