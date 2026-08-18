// Command travelmap is the Dawarich-compatible API server.
//
// This package is wiring only: it reads the configuration, picks the concrete
// implementations and hands them to the packages that own the behaviour.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tk0miya/travelmap/internal/config"
	"github.com/tk0miya/travelmap/internal/httpapi"
)

const usage = `travelmap is a Dawarich-compatible location-history API server.

Usage:
  travelmap <command>
  travelmap --version

Commands:
  serve   Run the HTTP server
`

// errUsage asks main for the exit status a misuse gets, which is not the same
// as the one a failed run gets.
var errUsage = errors.New("usage")

func main() {
	err := run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)

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
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
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

	// flag stops parsing at the first non-flag argument, so anything after the
	// command would otherwise be dropped in silence — `travelmap serve --help`
	// would start the server rather than explain itself.
	if fs.NArg() > 1 {
		fs.Usage()

		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(1))
	}

	switch cmd := fs.Arg(0); cmd {
	case "serve":
		return serve(getenv, stderr)
	case "":
		fs.Usage()

		return fmt.Errorf("%w: no command given", errUsage)
	default:
		fs.Usage()

		return fmt.Errorf("%w: unknown command %q", errUsage, cmd)
	}
}

// serve runs the HTTP server until the process is asked to stop.
func serve(getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	logger := cfg.NewLogger(stderr)

	// SIGINT and SIGTERM are what a terminal and an init system send; both mean
	// "stop", so both start the graceful shutdown rather than killing requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting travelmap", "version", buildVersion())

	return httpapi.Serve(ctx, cfg.Addr, httpapi.New(httpapi.Options{Logger: logger}), logger)
}
