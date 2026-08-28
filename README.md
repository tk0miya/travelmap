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

## Build and run

A checkout and a Go toolchain are all it takes. The Go version comes from `go.mod`, so nothing
else has to be installed.

```sh
git clone https://github.com/tk0miya/travelmap
cd travelmap
make build              # builds bin/travelmap

./bin/travelmap migrate                                          # creates travelmap.db
./bin/travelmap user create --email you@example.com --password '<password>'
./bin/travelmap serve
```

`migrate` creates the SQLite file and brings its schema up to date. It is a separate command
rather than something the server does on the way up, because opening a SQLite database creates
whatever file it is pointed at: migrating implicitly would turn a mistyped `TRAVELMAP_DATABASE`
into a server that comes up happily with none of your history in it. Running it again when there
is nothing to do is a no-op, so it is safe from an upgrade script. `serve` refuses to start
against a database that has not been migrated, and names this command when it does.

`user create` issues an account and prints its API key, which is what a client authenticates
with. Neither way of giving it the password is a good one yet, so pick by which exposure you
mind less:

- `--password` puts it in `ps` output, where every user on the host can read it while the
  command runs, and in the shell history file.
- Leaving `--password` out reads the first line of standard input, with no prompt — so at a
  terminal the command simply waits in silence. It is there for a setup script or a systemd
  unit, which should redirect a file rather than pipe from `printf` or `echo`:

  ```sh
  ./bin/travelmap user create --email you@example.com < /run/secrets/travelmap-password
  ```

An echo-off prompt is the fix for both, and it is planned rather than done — see "Milestone G"
in [TODO.md](TODO.md).

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
`TRAVELMAP_TIMEZONE` or `TRAVELMAP_TRACK_BREAK_MINUTES` — see "Configuration" below.

## Configuration

The server is configured through `TRAVELMAP_*` environment variables. Every one of them has a
default, so it runs with an empty environment.

| Variable | Default | Effect |
| --- | --- | --- |
| `TRAVELMAP_ADDR` | `:3000` | The address to listen on, as `host:port` |
| `TRAVELMAP_DATABASE` | `travelmap.db` | The SQLite file holding everything the server stores |
| `TRAVELMAP_DEBUG_LOG_REQUESTS` | off | Log one line per request, credentials redacted. See below |
| `TRAVELMAP_LOG_LEVEL` | `info` | The lowest level that is logged: `debug`, `info`, `warn` or `error` |
| `TRAVELMAP_TIMEZONE` | `UTC` | The timezone the day boundary is cut on |
| `TRAVELMAP_TRACK_BREAK_MINUTES` | `30` | Gaps longer than this are not counted as travelled distance |

`TRAVELMAP_TIMEZONE` and `TRAVELMAP_TRACK_BREAK_MINUTES` decide how stored statistics are
computed, so changing either invalidates the ones already stored; `travelmap recalculate`
rebuilds them.

### Logging the requests a client makes

`TRAVELMAP_DEBUG_LOG_REQUESTS=1` logs every request the server receives — method, path, query,
status, response size, duration and the request headers — including the ones that matched no
route, which are answered `404` and are how you find out what a client wants that this server
does not serve yet. It exists because the Dawarich apps are closed source: pointing one at a
server running with this on is how the endpoint list gets confirmed.

Credentials do not reach the log. Any query parameter or header whose name suggests one —
`key`, `token`, `pass`, `pwd`, `secret`, `auth`, `credential`, `session`, `signature` or `jwt`
anywhere in the name, so `api_key` and `Authorization` among them — is logged as `[REDACTED]`,
and so is `Cookie`, whose name says nothing about what it carries. The name is kept, so you can
still see what the client sent, without the value. Request bodies are never logged at all.

The lines are written at `info`, and turning this on holds `TRAVELMAP_LOG_LEVEL` down to `info`
if it was set higher — a capture that came out empty because the level swallowed it is one to
run again with the device back in hand.

Turn it off again when the capture is done: it prints the full headers of every request, which
is a lot of log for a server in service.

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
raises the development tools in a pull request of its own once a week.
Unformatted code fails `lint` rather than a target of its own, so `make fmt` before pushing.

[CLAUDE.md](CLAUDE.md) holds the project conventions: English as the project language, the
layering rules, the testing approach and the commit style. [TODO.md](TODO.md) holds the
development plan — one step there is one pull request. [docs/README.md](docs/README.md) is the
entry point to the requirements and design documentation, including
[docs/architecture.md](docs/architecture.md) for the technical decisions already implemented and
[docs/toolchain.md](docs/toolchain.md) for the Go toolchain setup beyond what `go.mod` and this
section already show.
