package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// noEnv is the environment of a run with nothing configured.
func noEnv(string) string { return "" }

// noStdin is the standard input of a run that is not given any. It is a
// function rather than a value because the tests run in parallel and a reader
// is not safe to share.
func noStdin() io.Reader { return strings.NewReader("") }

// TestRunArguments covers the argument handling only. `serve` is left out
// because it blocks until the process is signalled; what it wires together is
// tested in internal/config and internal/httpapi.
func TestRunArguments(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args         []string
		wantErr      error
		wantUsage    bool
		wantOnStdout string
	}{
		// The version goes to stdout, where a script can capture it, rather
		// than to stderr with the diagnostics.
		"--version prints the build version": {
			args:         []string{"--version"},
			wantOnStdout: buildVersion(),
		},
		"no command is a usage error": {
			args:      nil,
			wantErr:   errUsage,
			wantUsage: true,
		},
		"an unknown command is a usage error": {
			args:      []string{"teleport"},
			wantErr:   errUsage,
			wantUsage: true,
		},
		"an unknown flag is a usage error": {
			args:      []string{"--nope"},
			wantErr:   errUsage,
			wantUsage: true,
		},
		// flag stops at the command, so what follows it is only checked
		// because run checks it; `serve --help` starting the server would be
		// a surprise nobody could debug from the outside.
		"an argument after the command is a usage error": {
			args:      []string{"serve", "--help"},
			wantErr:   errUsage,
			wantUsage: true,
		},
		// The database comes from TRAVELMAP_DATABASE, so a path here is
		// someone expecting to migrate a database other than the configured
		// one — which is exactly the misunderstanding not to migrate under.
		"an argument after migrate is a usage error": {
			args:      []string{"migrate", "other.db"},
			wantErr:   errUsage,
			wantUsage: true,
		},
		"user without a subcommand is a usage error": {
			args:    []string{"user"},
			wantErr: errUsage,
		},
		"an unknown user subcommand is a usage error": {
			args:    []string{"user", "promote"},
			wantErr: errUsage,
		},
		// --help is what the flag package answers itself; it is a successful
		// run, and a wrapper script that calls it must not see an exit status.
		"--help is not an error": {
			args:      []string{"--help"},
			wantUsage: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			err := run(tt.args, noEnv, noStdin(), &stdout, &stderr)

			// Which error matters, not just that there is one: errUsage is
			// what makes the process exit 2 rather than 1, and a wrapper
			// script tells a misuse from a failure by that status.
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run returned %v, want %v", err, tt.wantErr)
			}

			if tt.wantOnStdout != "" && !strings.Contains(stdout.String(), tt.wantOnStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantOnStdout)
			}

			if tt.wantUsage && !strings.Contains(stderr.String(), "Usage:") {
				t.Errorf("stderr = %q, want the usage text", stderr.String())
			}
		})
	}
}
