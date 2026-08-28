# travelmap

The requirements and design documentation for travelmap: what the system is, its purpose, and
the decisions made to satisfy it. For how to build, run and configure it, see the root
[README.md](../README.md).

## What travelmap is

travelmap is a [Dawarich](https://github.com/Freika/dawarich)-compatible location-history API
server. Upstream Dawarich is a multi-container stack — Rails, PostgreSQL/PostGIS, Sidekiq and
Redis — which is a lot of runtime for a personal location log. travelmap serves the same API
from **a single statically linked binary plus one SQLite file**, so it can run on a NAS, a small
VPS or a home server without a container orchestrator behind it.

The target is the Dawarich iPhone app: point it at a travelmap server and record and browse your
location history. Once the API is settled, travelmap builds its own web UI on top of it, rather
than porting upstream's browser screens. How the web UI is meant to reach the API is still an
open question — see [TODO.md](../TODO.md)'s "Library Choices for the Web UI".

Not everything travelmap stores has to come from upstream: Swarm (Foursquare) check-in
collection is the first feature that is travelmap's own rather than upstream's. Why it keeps its
own API surface, separate from the Dawarich-compatible one, is in [api-notes.md](api-notes.md).

## Non-goals

- Immich / Photoprism integration and photo-related APIs
- Billing and subscription APIs
- Family sharing (Families)
- H3 hex maps / fog of war
- Areas, Places, Notes, Tags, Digests, Insights — upstream's own concepts. travelmap's own
  check-ins are not one of them; see "What travelmap is" above

## Where each part of the design lives

| Document | Covers |
| --- | --- |
| [architecture.md](architecture.md) | Technical decisions already implemented — what was chosen and why |
| [api-notes.md](api-notes.md) | The API: its two-part structure (Dawarich-compatible and travelmap's own), and upstream's quirks for the part already implemented |
| [openapi.yaml](openapi.yaml) | The OpenAPI contract for the subset actually implemented |
| [database.md](database.md) | Database internals beyond what `schema.sql` already shows by itself |
| [toolchain.md](toolchain.md) | The Go toolchain and its development tools |

What is not yet built — the plan, its ordering, and decisions not yet settled — is
[TODO.md](../TODO.md), at the repository root.
