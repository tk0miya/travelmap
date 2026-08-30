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

The same account made with `user create` also logs in at `http://localhost:3000/login`, in a
browser.

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
| `TRAVELMAP_BASE_URL` | unset | This server's own externally reachable URL, no trailing path. See below |
| `TRAVELMAP_DEBUG_LOG_REQUESTS` | off | Log one line per request, credentials redacted. See below |
| `TRAVELMAP_LOG_LEVEL` | `info` | The lowest level that is logged: `debug`, `info`, `warn` or `error` |
| `TRAVELMAP_TIMEZONE` | `UTC` | The timezone the day boundary is cut on |
| `TRAVELMAP_TRACK_BREAK_MINUTES` | `30` | Gaps longer than this are not counted as travelled distance |
| `TRAVELMAP_FOURSQUARE_PUSH_SECRET` | unset | Shared secret for the Swarm (Foursquare) push webhook. See below |
| `TRAVELMAP_FOURSQUARE_CLIENT_ID` | unset | The Swarm OAuth application's client id. See below |
| `TRAVELMAP_FOURSQUARE_CLIENT_SECRET` | unset | The Swarm OAuth application's client secret. See below |
| `TRAVELMAP_SESSION_LIFETIME` | `720h` | How long a browser session lasts before it needs a fresh login |
| `TRAVELMAP_SESSION_COOKIE_SECURE` | on | Send the session cookie only over HTTPS. See below |
| `TRAVELMAP_FOURSQUARE_SYNC_LOOKBACK_DAYS` | `14` | How far back each check-in fetch re-reads, whether run by the server itself or by hand. See below |
| `TRAVELMAP_FOURSQUARE_API_URL` | `https://api.foursquare.com` | The Foursquare API base URL, used by both the check-in fetch client and the OAuth flow. Exists so a test can point either at a fake server |
| `TRAVELMAP_FOURSQUARE_SYNC_INTERVAL` | `1h` | How often the server repeats the check-in fetch on its own. `0` disables it, leaving only the push webhook. See below |

`TRAVELMAP_TIMEZONE` and `TRAVELMAP_TRACK_BREAK_MINUTES` decide how stored statistics are
computed, so changing either invalidates the ones already stored; `travelmap recalculate`
rebuilds them.

`TRAVELMAP_BASE_URL` is this server's own externally reachable URL (e.g.
`https://travelmap.example.com`, no trailing path). The Swarm OAuth flow below appends its own
fixed `/foursquare/oauth/callback` path to it to build the URL Foursquare redirects back to — that
combined URL has to match the one registered on the Foursquare application exactly.

`TRAVELMAP_SESSION_COOKIE_SECURE` defaults to on, which is what a login form needs to keep
working over plain HTTP: browsers treat `http://localhost` as a secure context, so this costs a
developer nothing there. A server reachable only over a plain-HTTP LAN sets
`TRAVELMAP_SESSION_COOKIE_SECURE=0`, or the session cookie never leaves the browser at all.

### Swarm (Foursquare) check-ins

travelmap can collect your Swarm check-ins alongside the GPS trace the app records, as its own
extension to the Dawarich API — see "Keeping the two parts apart" in
[docs/api-notes.md](docs/api-notes.md). **Nothing is collected until an account is linked**, and
every way of linking one starts from the same prerequisite: a Foursquare application of your own,
registered at Foursquare's own developer console.

To link an account from a browser, once that application exists: set its client id and secret as
`TRAVELMAP_FOURSQUARE_CLIENT_ID` and `TRAVELMAP_FOURSQUARE_CLIENT_SECRET`, its redirect URI to
`<TRAVELMAP_BASE_URL>/foursquare/oauth/callback`, and `TRAVELMAP_BASE_URL` itself — this server's
own externally reachable URL:

```sh
TRAVELMAP_BASE_URL=http://localhost:3000 \
TRAVELMAP_FOURSQUARE_CLIENT_ID=<client id> \
TRAVELMAP_FOURSQUARE_CLIENT_SECRET=<client secret> \
    ./bin/travelmap serve
```

With those three settings in place, sign in and open the **Settings** page to connect your Swarm
account with its "Connect your Swarm account" button. Without them, the settings page has no
Swarm section to show, the same as the webhook below when `TRAVELMAP_FOURSQUARE_PUSH_SECRET` is
unset.

To link one from the command line instead — no browser, just the access token the same
application's own console issues for the account that owns it —
`travelmap foursquare connect` reads it from standard input rather than a flag, the same concern
`user create`'s password reading answers:

```sh
./bin/travelmap foursquare connect --email you@example.com --foursquare-user-id <your Swarm user id> \
    < /run/secrets/foursquare-access-token
```

Once an account is linked, the running server fetches its check-ins on its own, every
`TRAVELMAP_FOURSQUARE_SYNC_INTERVAL` (an hour by default), from
`TRAVELMAP_FOURSQUARE_SYNC_LOOKBACK_DAYS` back (14 by default) — a window re-read on every run
rather than resumed from a cursor, so a check-in added or edited after the fact is still picked up;
a check-in already stored is updated in place rather than duplicated. Setting
`TRAVELMAP_FOURSQUARE_SYNC_INTERVAL=0` turns this off, leaving only the push webhook (if
configured) to collect check-ins.

`travelmap foursquare sync` runs the same fetch by hand, on demand — for a first run before the
server's own timer would reach it, or with a wider `--lookback-days` for backfilling further back
than the routine window:

```sh
./bin/travelmap foursquare sync
./bin/travelmap foursquare sync --lookback-days 365
```

`TRAVELMAP_FOURSQUARE_PUSH_SECRET` turns on `POST /webhooks/foursquare`, which receives your
check-ins in real time via the same application's **"Push API notifications"** setting, reached
from the app's page and its "Edit This App" button — **not** the similarly-named
"Configure Server Webhooks" page, an unrelated product that will not deliver check-ins here. Set
the variable to the same secret configured there, and give Foursquare a URL it can reach over
**HTTPS on port 443** — a self-signed certificate is fine, but it has to run behind something
terminating TLS, so this is not something to test straight off a laptop.

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
