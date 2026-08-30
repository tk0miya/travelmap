# travelmap

The requirements and design documentation for travelmap: what the system is, its purpose, and
the decisions made to satisfy it. For how to build, run and configure it, see the root
[README.md](../README.md).

## What travelmap is

travelmap turns a trip into a map: it tracks a journey from multiple sources and records and
displays it as a timeline.

Today it collects that data two ways. A
[Dawarich](https://github.com/Freika/dawarich)-compatible location-history API lets the Dawarich
iPhone app, or any client built for it, record and browse GPS history — upstream Dawarich is a
multi-container stack (Rails, PostgreSQL/PostGIS, Sidekiq, Redis), which is a lot of runtime for
a personal location log, so travelmap serves the same API from **a single statically linked
binary plus one SQLite file**, able to run on a NAS, a small VPS or a home server without a
container orchestrator behind it. Swarm (Foursquare) check-in collection is travelmap's own, not
upstream's: explicitly recorded landmark data alongside the automatically recorded GPS trace.
Why it keeps its own API surface, separate from the Dawarich-compatible one, is in
[api-notes.md](api-notes.md).

Once the API is settled, travelmap builds its own web UI on top of it — the timeline and map a
traveller actually looks at — rather than porting upstream's browser screens. How the web UI is
meant to reach the API is still an open question — see [TODO.md](../TODO.md)'s "Library Choices
for the Web UI".

## Non-goals

- Immich / Photoprism integration and photo-related APIs
- Billing and subscription APIs
- Family sharing (Families)
- H3 hex maps / fog of war
- Areas, Places, Notes, Tags, Digests, Insights — upstream's own concepts. travelmap's own
  check-ins are not one of them; see "What travelmap is" above

## Where each part of the design lives

A quick map of the documents in this directory; [CLAUDE.md](../CLAUDE.md)'s "Documents" table
holds the fuller description of what each one holds and where new content lands.

| Document | Covers |
| --- | --- |
| [architecture.md](architecture.md) | System-wide technical decisions |
| [api-notes.md](api-notes.md) | The API's two-part structure and upstream's quirks |
| [openapi.yaml](openapi.yaml) | The OpenAPI contract |
| [database.md](database.md) | Database internals beyond `schema.sql` |
| [toolchain.md](toolchain.md) | The Go toolchain and its development tools |

What is not yet built — the plan, its ordering, and decisions not yet settled — is
[TODO.md](../TODO.md), at the repository root.
