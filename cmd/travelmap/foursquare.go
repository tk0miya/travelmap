package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/config"
	// Aliased because this file's own dispatcher is named after the
	// subcommand, which is the name every other command in this package uses
	// for its entry point.
	foursquareapi "github.com/tk0miya/travelmap/internal/foursquare"
)

const foursquareUsage = `travelmap foursquare manages Swarm (Foursquare) check-in collection.

Usage:
  travelmap foursquare sync [--lookback-days <n>]
`

// foursquare dispatches the `travelmap foursquare` subcommands — today just
// `sync`, which fetches every linked account's check-ins once. The push
// webhook needs none of its own, and nothing here supports linking, listing
// or removing a link: that is the settings page's own job now.
func foursquare(args []string, getenv func(string) string, _ io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, foursquareUsage)

		return fmt.Errorf("%w: foursquare: no subcommand given", errUsage)
	}

	switch sub := args[0]; sub {
	case "sync":
		return foursquareSync(args[1:], getenv, stdout, stderr)
	case "-h", "--help":
		fmt.Fprint(stderr, foursquareUsage)

		return nil
	default:
		fmt.Fprint(stderr, foursquareUsage)

		return fmt.Errorf("%w: foursquare: unknown subcommand %q", errUsage, sub)
	}
}

const foursquareSyncUsage = `travelmap foursquare sync fetches the linked accounts' Swarm check-ins.

Usage:
  travelmap foursquare sync [--lookback-days <n>]

Flags:
  --lookback-days   How far back to fetch, in days, for this run only.
                     Defaults to TRAVELMAP_FOURSQUARE_SYNC_LOOKBACK_DAYS

Every run re-reads its whole window rather than resuming where the last one
stopped, because a check-in can be added or edited after the fact; a check-in
already stored is updated in place rather than duplicated. A wider
--lookback-days is therefore how a backfill reaches further back than the
window a routine run takes.
`

// foursquareSync fetches every linked account's recent check-ins once. It is
// the manual run; the timer that repeats it is a later step.
func foursquareSync(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("travelmap foursquare sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, foursquareSyncUsage) }

	lookbackDays := fs.Int("lookback-days", 0, "how far back to fetch, in days")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return errUsage
	}

	if fs.NArg() > 0 {
		fs.Usage()

		return fmt.Errorf("%w: foursquare sync takes no arguments, got %q", errUsage, fs.Arg(0))
	}

	if *lookbackDays < 0 {
		fs.Usage()

		return fmt.Errorf("%w: --lookback-days must be a positive number of days", errUsage)
	}

	ctx := context.Background()

	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

	// Zero means the flag was not given, which is the same shape the
	// environment variable's own absence takes: the configured window stands.
	if *lookbackDays > 0 {
		cfg.FoursquareSyncLookbackDays = *lookbackDays
	}

	db, path, err := openConfiguredDatabase(ctx, cfg)
	if err != nil {
		return err
	}

	defer closeDatabase(db)

	if err := requireMigrated(ctx, db, path); err != nil {
		return err
	}

	accounts, err := db.FoursquareAccounts().All(ctx)
	if err != nil {
		return err
	}

	if len(accounts) == 0 {
		fmt.Fprintf(stdout, "%s: no Foursquare account is linked, sign in and connect one from Settings first\n", path)

		return nil
	}

	client := foursquareapi.NewClient(cfg.FoursquareAPIURL, cfg.NewLogger(stderr))

	// One account's failure does not cancel the others: they hold separate
	// tokens, and a revoked or rate-limited one says nothing about the rest.
	var failures []error

	for _, account := range accounts {
		collected, err := checkin.Sync(ctx, db, client, account, cfg.FoursquareSyncLookback())
		if err != nil {
			failures = append(failures, err)

			continue
		}

		fmt.Fprintf(stdout, "%s: %s collected for user %d (Foursquare user %s)\n",
			path, countedCheckins(collected), account.UserID, account.FoursquareUserID)
	}

	return errors.Join(failures...)
}

// countedCheckins renders a collected count with its noun in the right
// number: a quiet fortnight collecting exactly one is the ordinary case, not
// an edge one.
func countedCheckins(n int) string {
	if n == 1 {
		return "1 check-in"
	}

	return fmt.Sprintf("%d check-ins", n)
}
