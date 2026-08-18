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

```sh
git clone https://github.com/tk0miya/travelmap
cd travelmap

make build      # build bin/travelmap
make run        # go run ./cmd/travelmap serve
make test       # go test ./... -race -cover -shuffle=on
make lint       # golangci-lint, gofumpt, and a tidiness check on go.mod
make fmt        # gofumpt -w .
make migrate    # apply database migrations
make clean      # remove bin/
```

`make vulncheck` runs govulncheck over the dependencies; CI runs it on pushes to `main`
and on pull requests.

## Configuration

The server is configured through `TRAVELMAP_*` environment variables. Two of them decide how
stored aggregates are computed:

| Variable | Default | Effect |
| --- | --- | --- |
| `TRAVELMAP_TIMEZONE` | `UTC` | The timezone the day boundary is cut on |
| `TRAVELMAP_TRACK_BREAK_MINUTES` | `30` | Gaps longer than this are not counted as travelled distance |

Changing either invalidates the precomputed statistics, so `travelmap recalculate` rebuilds
them.

## Contributing

[CLAUDE.md](CLAUDE.md) holds the project conventions: English as the project language, the
layering rules, the testing approach and the commit style. [TODO.md](TODO.md) holds the
development plan — one step there is one pull request.
