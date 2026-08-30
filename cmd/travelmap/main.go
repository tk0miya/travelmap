// Command travelmap runs the travelmap server.
//
// This package is wiring only: it reads the configuration, picks the concrete
// implementations and hands them to the packages that own the behaviour.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = `travelmap tracks a journey from multiple sources and records it as a timeline.
It currently does that via a Dawarich-compatible location-history API and Swarm check-in
collection.

Usage:
  travelmap <command> [flags]
  travelmap --version

Commands:
  serve                Run the HTTP server
  migrate              Bring the database schema up to date
  recalculate          Rebuild daily_stats from points
  foursquare connect   Link a travelmap account to a Swarm account
  foursquare sync      Fetch the linked accounts' Swarm check-ins once
`

// errUsage asks main for the exit status a misuse gets, which is not the same
// as the one a failed run gets.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)

	switch {
	case err == nil:
	case errors.Is(err, errUsage):
		// The bare sentinel means the flag package has already said what was
		// wrong; printing "usage" under its message would add nothing.
		if err != errUsage {
			fmt.Fprintln(os.Stderr, err)
		}

		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "travelmap:", err)
		os.Exit(1)
	}
}

// run is main with the process taken out of it, so that the argument handling
// can be tested without spawning a binary.
func run(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("travelmap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }

	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		// flag has already written the usage text and, for a malformed flag,
		// the reason as well; repeating it here would print it twice.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return errUsage
	}

	if *showVersion {
		fmt.Fprintln(stdout, buildVersion())

		return nil
	}

	// flag stops parsing at the first non-flag argument, so everything from the
	// command onwards is the command's own to parse — including its flags,
	// which the global flag set has never seen.
	cmd, cmdArgs := "", []string(nil)
	if fs.NArg() > 0 {
		cmd, cmdArgs = fs.Arg(0), fs.Args()[1:]
	}

	switch cmd {
	case "serve":
		if err := noArguments(fs, "serve", cmdArgs); err != nil {
			return err
		}

		return serve(getenv, stderr)
	case "migrate":
		if err := noArguments(fs, "migrate", cmdArgs); err != nil {
			return err
		}

		return migrate(getenv, stdout)
	case "foursquare":
		return foursquare(cmdArgs, getenv, stdin, stdout, stderr)
	case "recalculate":
		if err := noArguments(fs, "recalculate", cmdArgs); err != nil {
			return err
		}

		return recalculate(getenv, stdout)
	case "":
		fs.Usage()

		return fmt.Errorf("%w: no command given", errUsage)
	default:
		fs.Usage()

		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
}

// noArguments rejects anything following a command that takes none, so that
// `travelmap serve --help` explains itself rather than starting a server and
// `travelmap migrate somewhere.db` says that the path comes from the
// environment instead of migrating the configured database.
func noArguments(fs *flag.FlagSet, cmd string, args []string) error {
	if len(args) == 0 {
		return nil
	}

	fs.Usage()

	return fmt.Errorf("%w: %s takes no arguments, got %q", errUsage, cmd, args[0])
}
