# Project conventions

travelmap is a Dawarich-compatible location-history API server in Go, built to run as a single
binary plus one SQLite file.

This file holds the conventions every change follows. The development plan itself — what gets
built, in what order, and why — lives in [TODO.md](TODO.md).

## Language

**English is the project language.** Everything committed to the repository is written in
English: code, identifiers, comments, documentation, commit messages, branch names, pull
request titles and bodies, and issue text. Conversation outside the repository may happen in
any language; what lands in git does not.

## Documents

| File | Holds |
| --- | --- |
| `TODO.md` | The development plan: goal, step checklists, technical decisions not yet settled by an implemented step, and upstream quirks for endpoints not yet implemented. **The step checklists are the single source of truth for what gets implemented.** Also the data model for a table not yet migrated, until the step that adds it |
| `CLAUDE.md` | This file — the conventions, so that no pull request has to re-argue them |
| `README.md` | What the project is, and how to build, run and configure it. Written for someone approaching the project from outside, whether to run it or to start contributing |
| `docs/architecture.md` | Technical decisions already implemented that span the system — what was chosen and why. A decision not yet implemented stays in `TODO.md`'s own "Technical Decisions" section until the step that implements it lands, then moves here or to `docs/database.md`, whichever already owns that ground |
| `docs/toolchain.md` | What explains the Go toolchain and its development tools beyond what `go.mod`, `Makefile` and `.golangci.yml` already show by themselves |
| `docs/api-notes.md` | What explains the API: its two-part structure (Dawarich-compatible and travelmap's own), and — for the Dawarich-compatible part, endpoints already implemented only — upstream's quirks, the deliberate differences from it, and the client evidence behind each |
| `docs/openapi.yaml` | The accompanying OpenAPI contract for the subset actually implemented here — paths, schemas, headers, status codes (upstream's own spec stays the compatibility source of truth for the Dawarich-compatible part) |
| `docs/database.md` | What explains the database beyond `schema.sql` itself: invariants, algorithms, or table/column detail too long for a schema.sql comment (see "Testing") |
| Package doc comments (`doc.go`) | What belongs in that package |

`TODO.md` is the plan for what is still ahead, not a record of what shipped, so it never
accumulates finished work — everything else in this table is where a finished decision ends up.

Eight rules keep them from drifting:

- **A decision that changes is changed in `TODO.md` in the same pull request.** The plan
  records defaults as of planning; when implementation proves one wrong, the plan is updated,
  not silently ignored.
- **Tick the step's checkboxes in `TODO.md` in the pull request that completes them.** A step
  is done when its box is ticked, not when the code merges. Once a step is done and everything
  it decided has a permanent home (`schema.sql`/`docs/database.md`, `docs/api-notes.md`/
  `docs/openapi.yaml`, `docs/architecture.md`, this file, a package's `doc.go`), **delete its
  `TODO.md` entry entirely, heading included** — the checklist, the `Settles`/`Done when` lines,
  the narrative and the step's own number and title all belong to planning, not to the finished
  record, and git history already keeps them. A milestone whose every step is gone this way is
  itself deleted, heading and all.
- **A pull request that finishes a step also moves that step's now-settled rows out of
  `TODO.md`'s "Technical Decisions" table into whichever document already owns that ground** —
  `docs/architecture.md` for a decision that spans the system, `docs/database.md` for one scoped
  to a single table or column's design. That table holds only decisions for work not yet built;
  one for something already shipped belongs with the rest of the settled rationale, not beside
  the plan.
- **A pull request that changes a request/response shape, header or status code updates
  `docs/openapi.yaml` in the same pull request.** The contract is what a client actually sees;
  a stale copy of it is worse than no copy at all.
- **A pull request that finishes an endpoint moves its design notes from `TODO.md` to
  `docs/api-notes.md`.** `docs/api-notes.md` and `docs/openapi.yaml` describe only what is
  implemented. Source code is written from the requirements and design these documents already
  settled, not the other way around, so a comment neither repeats what `docs/` already says nor
  cites `docs/`, `TODO.md` or a step number back at it: a past step's number stops resolving to
  anything once that step is deleted by the rule above, and a future step is a plan that can
  still change, which a comment has no business promising.
- **A migration writes its own rationale as a comment, not as prose elsewhere.** A comment written
  inside a `CREATE TABLE`'s or a multi-column `CREATE INDEX`'s own parentheses also reaches a
  reader through `schema.sql` (see "Testing"); `docs/database.md`'s own opening explains why. One
  that cannot be written inside such a statement — about a table as a whole, or an absence —
  stays in the migration regardless, in `internal/store/sqlite/migrations/`, rather than moving to
  `docs/database.md`, which is for what does not fit `schema.sql` at all.
- **A comment inside a statement's own parentheses is one line.** `schema.sql` is for scanning the
  current structure at a glance, and a multi-line comment there breaks that up.
- **Only write one in a migration that has not been merged yet.** A merged migration may already
  be applied to a real database, and editing the exact text of a statement it contains does not
  reach a database that ran the version before the edit — only what sits outside every statement
  (a table's own leading comment, a standalone note) is free to edit after the fact, because no
  database depends on its exact text.

Long-form rationale belongs in the commit message. Facts that later work depends on belong in
`TODO.md`, because nobody reads a commit message they do not know exists.

## Packages

Each package's `doc.go` says what belongs in it and the layering below says what it may import,
so a new package needs no entry here. The one placement rule neither of them carries:
**`cmd/travelmap` is wiring only** — it is the only place that picks concrete implementations,
and a subcommand that grows business logic hands it to the package that owns the behaviour.

## Layering

Imports point downwards only. Each package may import the ones below it, never above:

```
cmd/travelmap
    ↓
httpapi  →  httpapi/dto
    ↓
ingest   checkin   auth
    ↓
store  ←  store/sqlite  ←  store/storetest (tests only)
    ↓
model      geo      config      foursquare
```

The rules that matter, stated directly:

- **Only `cmd/travelmap` and `internal/store/storetest` import `internal/store/sqlite`.**
  Everything else depends on the `internal/store` interfaces, which is what keeps a second
  backend possible. `storetest` is the deliberate second place: handing a test a real database
  means naming an implementation. It hands back a `store.Store` all the same.
- **Nothing imports `internal/httpapi`** except `cmd/travelmap`. The HTTP surface is the
  outermost layer, reached only through the entry point.
- **DTOs do not escape `internal/httpapi`.** `internal/ingest`, `internal/store` and
  `internal/model` never see a `dto` type; handlers convert at the boundary.
- **`internal/model`, `internal/geo`, `internal/config` and `internal/foursquare` are leaves**
  and import nothing from this module. A cycle back into them is a sign that logic was put in
  the wrong package.
- **`internal/store` imports `internal/model` only.**
- **Every mutation of a point goes through `internal/ingest`.** Nothing else calls the point
  repository's write methods — not a handler, not a CLI subcommand, not an importer. Scattered
  writes would each have to remember to rebuild `daily_stats`, and one that forgets leaves
  `/stats` reporting a distance that no longer matches the points.
- **Every write of a check-in goes through `internal/checkin`.** Two collection paths — the push
  webhook and the periodic fetch — have to agree on how a duplicate is recognised and on which
  fields a repeat write overwrites. A second writer would settle that twice, and the two answers
  would drift. `internal/checkin` reaches `internal/store` as `internal/ingest` does.
- **`internal/foursquare` returns the shapes Foursquare sends**, and the package that owns the
  record converts them into `internal/model` types — check-ins in `internal/checkin`, the account
  row in the handler that writes it. Being a leaf it cannot name a `model` type itself, and the
  boundary is there for the reason DTOs stop at `internal/httpapi`.

## Testing

- **Table-driven tests**, with `github.com/google/go-cmp` for diffs.
- **Handlers are tested through `net/http/httptest`** against the real router, not by calling
  the handler function directly — the middleware chain and the route registration are part of
  what is being tested.
- **Golden files pin the JSON.** Key names, types and casing are the compatibility contract, so
  they are compared against files in `testdata/golden/` rather than asserted field by field.
  Regenerate with `-update` on the packages that own golden files (`go test
  ./internal/httpapi/... -update`); a golden diff in a pull request is a compatibility change
  and gets read as one.
- **A test that needs a store gets a real temporary SQLite database**: `newTestDB` inside
  `internal/store/sqlite`, `internal/store/storetest` above it. A fake would have to reimplement
  what the schema enforces and grow a repository per table, and the paths that answer 500 are
  reached by breaking the database instead — `storetest.Unavailable` closes it,
  `storetest.UnavailablePoints` drops the table the write needs. Substitute a store only for what
  a database cannot show: which calls a unit made, or one repository failing while the rest work
  (`internal/ingest`).
- **The current schema is a generated snapshot, not documentation to hand-maintain.**
  `internal/store/sqlite/schema.sql` is a dump of a freshly migrated database — the structure
  every migration adds up to, in one file, rather than something read by replaying diffs.
  Regenerate it with `go test ./internal/store/sqlite/... -update` whenever a migration changes
  the schema; `TestSchema` compares it against a fresh migration run on every other invocation, so
  a stale file fails `make test` and the pre-commit hook exactly as a stale JSON golden does — no
  separate rule or CI step is needed to catch a forgotten regeneration.
- **A test that only passes in a particular order is a broken test**: `make test` shuffles.
- Tests live beside the code as `_test.go`. Use an external `_test` package when the test
  should only see the exported surface.

## Working on a change

- **One step from TODO.md = one pull request.** Steps are sized so that each carries one
  decision worth reviewing; splitting or merging them defeats that sizing.
- **`make check` before committing** (a pre-commit hook runs it too). See the `Makefile` for
  what each target runs.
- **Development tools go in `go.mod` as `tool` directives**, invoked via `go tool`. Do not add
  a step that requires installing a binary — a fresh checkout needs nothing but Go.
- **Workflow conventions**, when touching `.github/workflows/`: a `permissions:` block on every
  workflow, third-party actions pinned to a full commit SHA with a `# vX.Y.Z` comment, and
  `persist-credentials: false` on `actions/checkout`.

## Commits

- **Subject: imperative mood, capitalised, no trailing period**, about 50 characters —
  "Set up the Go toolchain, package skeleton and CI". Not a Conventional Commits prefix; the
  subject says what the commit does.
- **Body: why, wrapped at 72 columns.** The diff already says what changed. The body carries
  the reasoning, the measurements behind a decision, and the alternative that was rejected —
  this is where a reviewer finds out whether the change is right, and where the next person
  finds out why the code looks the way it does.
- **One logical change per commit.** Formatting-only churn goes in its own commit rather than
  hiding a behaviour change inside it.
- Reference the step being completed when it is not obvious from the diff.
