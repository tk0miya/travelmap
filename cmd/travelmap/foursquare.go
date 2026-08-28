package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

const foursquareUsage = `travelmap foursquare manages Swarm (Foursquare) check-in collection.

Usage:
  travelmap foursquare connect --email <address> --foursquare-user-id <id>
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

// foursquare dispatches the `travelmap foursquare` subcommands. Only
// `connect` exists: the push webhook and the periodic fetch do not need one
// of their own, and nothing here supports listing or removing a link yet.
func foursquare(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, foursquareUsage)

		return fmt.Errorf("%w: foursquare: no subcommand given", errUsage)
	}

	switch sub := args[0]; sub {
	case "connect":
		return foursquareConnect(args[1:], getenv, stdin, stdout, stderr)
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
