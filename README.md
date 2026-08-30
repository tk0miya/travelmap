# travelmap

travelmap turns a trip into a map: it tracks a journey from multiple sources and records and
displays it as a timeline, in Go.

Today it collects that data through a
[Dawarich](https://github.com/Freika/dawarich)-compatible location-history API — point the
Dawarich iPhone app at a travelmap server to record and browse your location history — and Swarm
(Foursquare) check-in collection. Upstream Dawarich is a multi-container stack (Rails,
PostgreSQL/PostGIS, Sidekiq, Redis), which is a lot of runtime for a personal location log, so
travelmap serves the same API from **a single statically linked binary plus one SQLite file**,
able to run on a NAS, a small VPS or a home server without a container orchestrator behind it. A
web UI of our own comes after the API.

What it deliberately does not cover: photo-service integration, billing, family sharing, hex
maps / fog of war, and areas, places, notes, tags, digests and insights. See "Non-goals" in
[docs/README.md](docs/README.md).

## Foursquare developer setup

travelmap can collect your Swarm check-ins alongside the GPS trace the Dawarich app records, as
its own extension to the Dawarich API — see "Keeping the two parts apart" in
[docs/api-notes.md](docs/api-notes.md). This is set up once per travelmap instance, and has to
happen **before** travelmap itself can be configured: the next section's settings are filled in
from what this one produces.

Register a Foursquare application of your own at Foursquare's own developer console.
`foursquare.client_id` and `foursquare.client_secret` come from it; `foursquare.push_secret` is a
secret of your own choosing, set on both this application and in travelmap's own configuration.
Decide `server.base_url` now if you have not already — it has to match the redirect URI
(`<server.base_url>/foursquare/oauth/callback`) registered on this application exactly.

`POST /webhooks/foursquare` receives your check-ins in real time via the same application's
**"Push API notifications"** setting, reached from the app's page and its "Edit This App" button
— **not** the similarly-named "Configure Server Webhooks" page, an unrelated product that will
not deliver check-ins here. Set that to the same `foursquare.push_secret` you chose above, and
give Foursquare a URL it can reach over **HTTPS on port 443** — a self-signed certificate is
fine, but it has to run behind something terminating TLS, so this is not something to test
straight off a laptop.

## Configuration

The server is configured through a TOML file, `travelmap.toml` by default (in the current
directory) or wherever `--config <path>` points it.
[`travelmap.toml.example`](travelmap.toml.example) lists every setting, its default and what it
does — copy it to `travelmap.toml` and fill in the ones that have none: `server.base_url` and
the three `foursquare.*` settings from the Foursquare application set up above. A path given
explicitly with `--config` is expected to exist; the default path's own absence is not an error,
but those four settings still have to come from somewhere.

### Logging the requests a client makes

`server.debug_log_requests = true` logs every request the server receives — method, path, query,
status, response size, duration and the request headers — including the ones that matched no
route, which are answered `404` and are how you find out what a client wants that this server
does not serve yet. It exists because the Dawarich apps are closed source: pointing one at a
server running with this on is how the endpoint list gets confirmed.

Credentials do not reach the log. Any query parameter or header whose name suggests one —
`key`, `token`, `pass`, `pwd`, `secret`, `auth`, `credential`, `session`, `signature` or `jwt`
anywhere in the name, so `api_key` and `Authorization` among them — is logged as `[REDACTED]`,
and so is `Cookie`, whose name says nothing about what it carries. The name is kept, so you can
still see what the client sent, without the value. Request bodies are never logged at all.

The lines are written at `info`, and turning this on holds `server.log_level` down to `info` if
it was set higher — a capture that came out empty because the level swallowed it is one to run
again with the device back in hand.

Turn it off again when the capture is done: it prints the full headers of every request, which
is a lot of log for a server in service.

## Build and run

There is no tagged release yet. The
[`nightly` release](https://github.com/tk0miya/travelmap/releases/tag/nightly) carries prebuilt
binaries for Linux and macOS (Apple Silicon), rebuilt from `main` on every merge — pick one of
those to skip building from source, or build it yourself:

A checkout and a Go toolchain are all it takes. The Go version comes from `go.mod`, so nothing
else has to be installed.

```sh
git clone https://github.com/tk0miya/travelmap
cd travelmap
make build              # builds bin/travelmap

cp travelmap.toml.example travelmap.toml   # then fill in the required settings — see "Configuration" above
./bin/travelmap migrate                                          # creates travelmap.db
./bin/travelmap serve
```

`migrate` creates the SQLite file and brings its schema up to date. It is a separate command
rather than something the server does on the way up, because opening a SQLite database creates
whatever file it is pointed at: migrating implicitly would turn a mistyped `database.path` into
a server that comes up happily with none of your history in it. Running it again when there is
nothing to do is a no-op, so it is safe from an upgrade script. `serve` refuses to start against
a database that has not been migrated, and names this command when it does.

The server listens on port 3000 by default, the port upstream Dawarich uses. Ask it for its
health to see that it came up:

```console
$ curl http://localhost:3000/api/v1/health
{"status":"ok"}
```

`SIGINT` or `SIGTERM` stops it: it stops accepting connections and gives the requests already
running up to ten seconds to finish.

`bin/travelmap --version` reports the build the binary came from, which is the thing to quote
in a bug report.

`recalculate` rebuilds the precomputed statistics (`/stats`, `/points/tracked_months`) from the
points already stored. Run it after an import, after fixing an inconsistency, or after changing
`tracking.timezone` or `tracking.track_break_minutes` — see "Configuration" above.

## Using Swarm check-ins

**Nothing is collected for a particular user until they link their own Swarm account**, which
they do by signing in and opening the **Settings** page's "Connect your Swarm account" button.

Once an account is linked, the running server fetches its check-ins on its own, every
`foursquare.sync_interval` (an hour by default), from `foursquare.sync_lookback_days` back (14
by default) — a window re-read on every run rather than resumed from a cursor, so a check-in
added or edited after the fact is still picked up; a check-in already stored is updated in place
rather than duplicated. Setting `foursquare.sync_interval = "0"` turns this off, leaving only the
push webhook set up above to collect check-ins.

`travelmap foursquare sync` runs the same fetch by hand, on demand — for a first run before the
server's own timer would reach it, or with a wider `--lookback-days` for backfilling further back
than the routine window:

```sh
./bin/travelmap foursquare sync
./bin/travelmap foursquare sync --lookback-days 365
```

## Contributing

Every development tool is pinned in `go.mod` and invoked through `go tool`, so a fresh checkout
needs nothing installed either:

```sh
make test               # go test ./... -race -cover -shuffle=on
make lint               # golangci-lint, gofumpt, and a tidiness check on go.mod
make fmt                # gofumpt -w .
make check              # lint and test together, which is what a commit has to pass
make vulncheck          # govulncheck over the dependencies
make run                # go run ./cmd/travelmap serve
make migrate            # go run ./cmd/travelmap migrate
```

CI runs `build`, `test`, `lint` and `vulncheck` on every pull request and on pushes to `main`, and
raises the development tools in a pull request of its own once a week. A push to `main` also
rebuilds the binaries and republishes them as the `nightly` release.
Unformatted code fails `lint` rather than a target of its own, so `make fmt` before pushing.

[CLAUDE.md](CLAUDE.md) holds the project conventions: English as the project language, the
layering rules, the testing approach and the commit style. [TODO.md](TODO.md) holds the
development plan — one step there is one pull request. [docs/README.md](docs/README.md) is the
entry point to the requirements and design documentation, including
[docs/architecture.md](docs/architecture.md) for the technical decisions already implemented and
[docs/toolchain.md](docs/toolchain.md) for the Go toolchain setup beyond what `go.mod` and this
section already show.
