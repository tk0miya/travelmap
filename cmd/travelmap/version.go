package main

import (
	"runtime/debug"
	"strings"
)

// version is the release this binary was built from. It is set at link time
// with -ldflags "-X main.version=v1.2.3"; an unset one falls back to what Go
// stamps into the binary, so a `go build` from a checkout still reports the
// commit it came from.
var version string

// unknownVersion is what a binary built without VCS information reports —
// `go build` outside a repository, or from a dirty archive.
const unknownVersion = "unknown"

// buildVersion reports the version of this binary.
func buildVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return unknownVersion
	}

	// A binary installed with `go install module@version` carries the version
	// it was published under; one built from a checkout says "(devel)".
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	return vcsVersion(info)
}

// vcsVersion builds a version out of the VCS stamps Go embeds, in the form
// "(devel)-abcdef1" or "(devel)-abcdef1-dirty".
func vcsVersion(info *debug.BuildInfo) string {
	var revision string

	var modified bool

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if revision == "" {
		return unknownVersion
	}

	parts := []string{"(devel)", shortRevision(revision)}
	if modified {
		parts = append(parts, "dirty")
	}

	return strings.Join(parts, "-")
}

// shortRevision abbreviates a commit hash the way git does.
func shortRevision(revision string) string {
	const shortLen = 7

	if len(revision) <= shortLen {
		return revision
	}

	return revision[:shortLen]
}
