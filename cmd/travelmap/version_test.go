package main

import (
	"runtime/debug"
	"testing"
)

// TestVcsVersion covers the version a binary built from a checkout reports.
// The string is what an operator pastes into a bug report, so a broken format
// is only ever noticed by someone trying to reproduce the bug.
func TestVcsVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		settings []debug.BuildSetting
		want     string
	}{
		"a clean checkout": {
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "a84c236e6b1f0d0a5f2c9e1d4b7a3c8e9f0a1b2c"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "(devel)-a84c236",
		},
		"a checkout with uncommitted changes": {
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "a84c236e6b1f0d0a5f2c9e1d4b7a3c8e9f0a1b2c"},
				{Key: "vcs.modified", Value: "true"},
			},
			want: "(devel)-a84c236-dirty",
		},
		"a revision shorter than the abbreviation": {
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "a84c"}},
			want:     "(devel)-a84c",
		},
		// `go build` outside a repository stamps nothing, and a version made
		// up of no information at all would be worse than saying so.
		"no VCS information": {
			settings: nil,
			want:     unknownVersion,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := vcsVersion(&debug.BuildInfo{Settings: tt.settings}); got != tt.want {
				t.Errorf("vcsVersion = %q, want %q", got, tt.want)
			}
		})
	}
}
