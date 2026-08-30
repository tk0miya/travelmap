package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tk0miya/travelmap/internal/checkin"
	"github.com/tk0miya/travelmap/internal/config"
	// Aliased because this file's own dispatcher is named after the
	// subcommand, which is the name every other command in this package uses
	// for its entry point.
	foursquareapi "github.com/tk0miya/travelmap/internal/foursquare"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

const foursquareUsage = `travelmap foursquare manages Swarm (Foursquare) check-in collection.

Usage:
  travelmap foursquare connect --email <address> --foursquare-user-id <id>
  travelmap foursquare sync [--lookback-days <n>]
`

const foursquareConnectUsage = `travelmap foursquare connect links a travelmap account to a Swarm account.

Usage:
  travelmap foursquare connect --email <address> --foursquare-user-id <id>

Flags:
  --email                 The travelmap account to link, created with
                           "travelmap user create"
  --foursquare-user-id    The Swarm account's own id, as it appears in
                           checkin.user.id on an incoming push

The access token is read from the first line of standard input, with no
prompt: at a terminal the command waits in silence, so that path is for a
script or a unit file redirecting a file. This is the only way to give it, so
that it stays out of ps output and the shell history. Until the OAuth flow
exists, get one from the Foursquare application's own console.
`

// foursquare dispatches the `travelmap foursquare` subcommands: `connect`
// links an account, `sync` fetches its check-ins once. The push webhook needs
// none of its own, and nothing here supports listing or removing a link yet.
func foursquare(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, foursquareUsage)

		return fmt.Errorf("%w: foursquare: no subcommand given", errUsage)
	}

	switch sub := args[0]; sub {
	case "connect":
		return foursquareConnect(args[1:], getenv, stdin, stdout, stderr)
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

// foursquareConnect links a travelmap account to a Swarm account, so that the
// push webhook and the periodic fetch (once they exist) have somewhere to
// write a collected check-in.
func foursquareConnect(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("travelmap foursquare connect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, foursquareConnectUsage) }

	email := fs.String("email", "", "the travelmap account to link")
	foursquareUserID := fs.String("foursquare-user-id", "", "the Swarm account's own id")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return errUsage
	}

	if fs.NArg() > 0 {
		fs.Usage()

		return fmt.Errorf("%w: foursquare connect takes no arguments, got %q", errUsage, fs.Arg(0))
	}

	address, err := model.NormalizeEmail(*email)
	if err != nil {
		fs.Usage()

		return fmt.Errorf("%w: %s", errUsage, err)
	}

	if *foursquareUserID == "" {
		fs.Usage()

		return fmt.Errorf("%w: --foursquare-user-id is required", errUsage)
	}

	token, err := readToken(stdin)
	if err != nil {
		fs.Usage()

		return fmt.Errorf("%w: %s", errUsage, err)
	}

	ctx := context.Background()

	db, path, err := openDatabase(ctx, getenv)
	if err != nil {
		return err
	}

	defer closeDatabase(db)

	if err := requireMigrated(ctx, db, path); err != nil {
		return err
	}

	user, err := db.Users().ByEmail(ctx, address)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%s: no user with the email %s, run \"travelmap user create\" first", path, address)
		}

		return err
	}

	account, err := db.FoursquareAccounts().Create(ctx, model.FoursquareAccount{
		UserID:           user.ID,
		FoursquareUserID: *foursquareUserID,
		AccessToken:      token,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%s: %s is already linked to a Foursquare account, or %s is already linked to a travelmap user",
				path, address, *foursquareUserID)
		}

		return err
	}

	fmt.Fprintf(stdout, "linked user %d (%s) to Foursquare user %s\n", account.UserID, address, account.FoursquareUserID)

	return nil
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
		fmt.Fprintf(stdout, "%s: no Foursquare account is linked, run \"travelmap foursquare connect\" first\n", path)

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

// readToken takes the Foursquare access token off the first line of standard
// input — the way to hand it to this command without it appearing in `ps`
// output or the shell history, the same concern userCreate's password reading
// answers.
func readToken(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the access token from standard input: %w", err)
	}

	token := strings.TrimRight(line, "\r\n")
	if token == "" {
		return "", errors.New("no access token: write it to standard input")
	}

	return token, nil
}
