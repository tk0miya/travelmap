# Toolchain

Why the Go toolchain and its development tools are set up the way they are, beyond what `go.mod`
and `Makefile` show by themselves.

**`go.mod` names `go 1.26.6`** (the current stable release; the floor is 1.25, required by
`modernc.org/sqlite` v1.56.0), and it names a patch version, not just `1.26`, deliberately:
`actions/setup-go` takes the version from `go-version-file: go.mod` and runs with
`GOTOOLCHAIN=local`, so this line alone decides which toolchain CI builds with, and
`GOTOOLCHAIN=auto` fetches the same one locally. Since `govulncheck` reports a standard-library
advisory against the toolchain the code would be built with, a stdlib advisory is fixed by
**raising this line** — and nothing does it automatically, because Dependabot's `gomod` ecosystem
updates requirements and not the `go` directive. The `vulncheck` job going red with
`Standard library` in the output is that signal.

**Development tools are declared with `tool` directives in `go.mod`**, not as separately
installed binaries:

```
tool (
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	golang.org/x/vuln/cmd/govulncheck
	mvdan.cc/gofumpt
)
```

added with `go get -tool <path>@<version>` (one `-tool` per invocation) and run as
`go tool golangci-lint run`.

This matters for more than convenience. `golangci-lint` refuses to analyse a module whose `go`
directive is newer than the Go version the linter binary was built with:

```
can't load config: the Go language version (go1.25) used to build golangci-lint
is lower than the targeted Go version (1.26.0)
```

A pre-installed binary therefore caps the `go` directive at whatever it happens to be built
with. **`go tool` removes the ceiling by construction**: the linter is rebuilt from source with
the module's own toolchain, so it always matches. Versions are pinned in `go.mod`, and CI and
local runs use the same build.

The installed Go does not have to match the directive: `GOTOOLCHAIN=auto` fetches the pinned
toolchain once and builds, tests with `-race` and vets the module fine regardless of what is
preinstalled. So there is no environment prerequisite before starting, and no setup script to
maintain.

Three caveats:

- **`go get -tool` pulls the tools' entire dependency trees into `go.mod` as `// indirect`** —
  214 entries for the three above. If `go.mod` becomes unreadable because of them, move the
  tools into a separate `tools/go.mod`.
- **The tool modules themselves are recorded as `// indirect` too**, because no package in this
  module imports them — `go mod tidy` restores the marker if it is removed by hand. Dependabot's
  scheduled `gomod` updates cover direct dependencies only, so it never offers the tools, and the
  one setting that would reach them, `allow: dependency-type: "all"`, reaches the 211 modules they
  are built from as well. **`.github/workflows/go-tools.yml` does it instead**: a weekly
  `go get -u tool` and `go mod tidy`, opened as a pull request carrying the `auto-merge` label, so
  a golangci-lint release that finds something new turns `lint` red and waits for a human rather
  than merging. The `tool` meta-pattern expands to the tools declared in `go.mod`, so no module
  path is written in the workflow; `-u` stops at the latest minor or patch, and a new major, being
  a different module path, is adopted by hand. `.github/dependabot.yml` therefore keeps the
  default scope, which is also what makes a chi or SQLite-driver bump arrive as its own pull
  request.
- **`govulncheck` cannot run in the development container**: the egress proxy blocks
  `vuln.go.dev` (`Forbidden`). Treat it as a CI-only check.
