# travelmap

A [Dawarich](https://github.com/Freika/dawarich)-compatible location-history API server, in Go.

Upstream Dawarich is a multi-container stack — Rails, PostgreSQL/PostGIS, Sidekiq and Redis —
which is a lot of runtime for a personal location log. travelmap aims to serve the same API
from **a single statically linked binary plus one SQLite file**, so that it can run on a NAS,
a small VPS or a home server without a container orchestrator behind it.

The target is the Dawarich iPhone app: point it at a travelmap server and record and browse
your location history. A web UI of our own comes after the API.

What it deliberately does not cover: photo-service integration, billing, family sharing, hex
maps / fog of war, and areas, places, notes, tags, digests and insights. See "Non-goals" in
[TODO.md](TODO.md).

## Build and run

A checkout and a Go toolchain are all it takes. The Go version comes from `go.mod`, so nothing
else has to be installed.

```sh
git clone https://github.com/tk0miya/travelmap
cd travelmap
make build              # builds bin/travelmap

./bin/travelmap serve
```

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

## Configuration

The server is configured through `TRAVELMAP_*` environment variables. Every one of them has a
default, so it runs with an empty environment.

| Variable | Default | Effect |
| --- | --- | --- |
| `TRAVELMAP_ADDR` | `:3000` | The address to listen on, as `host:port` |
| `TRAVELMAP_LOG_LEVEL` | `info` | The lowest level that is logged: `debug`, `info`, `warn` or `error` |
| `TRAVELMAP_TIMEZONE` | `UTC` | The timezone the day boundary is cut on |
| `TRAVELMAP_TRACK_BREAK_MINUTES` | `30` | Gaps longer than this are not counted as travelled distance |

The last two decide how stored statistics are computed, so changing either invalidates the ones
already stored; `travelmap recalculate` rebuilds them.

## Contributing

Every development tool is pinned in `go.mod` and invoked through `go tool`, so a fresh checkout
needs nothing installed either:

```sh
make test               # go test ./... -race -cover -shuffle=on
make lint               # golangci-lint, gofumpt, and a tidiness check on go.mod
make fmt                # gofumpt -w .
make vulncheck          # govulncheck over the dependencies
make run                # go run ./cmd/travelmap serve
```

CI runs `build`, `test`, `lint` and `vulncheck` on every pull request and on pushes to `main`.
Unformatted code fails `lint` rather than a target of its own, so `make fmt` before pushing.

[CLAUDE.md](CLAUDE.md) holds the project conventions: English as the project language, the
layering rules, the testing approach and the commit style. [TODO.md](TODO.md) holds the
development plan — one step there is one pull request.
