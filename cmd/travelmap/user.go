package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

const userUsage = `travelmap user manages the accounts the API authenticates.

Usage:
  travelmap user create --email <address> [--password <password>]
`

const userCreateUsage = `travelmap user create issues a user and prints its API key.

Usage:
  travelmap user create --email <address> [--password <password>]

Flags:
  --email      The address the user logs in with
  --password   The password. Every user on the host can read it through ps
               while this runs, and it stays in the shell history

Left out, the first line of standard input is read instead, with no prompt: at
a terminal the command waits in silence, so that path is for a script or a unit
file redirecting a file.
`

// user dispatches the `travelmap user` subcommands. Only `create` exists: on a
// self-hosted instance the accounts are issued once, and listing or deleting
// them is what sqlite3 is for until something needs more.
func user(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, userUsage)

		return fmt.Errorf("%w: user: no subcommand given", errUsage)
	}

	switch sub := args[0]; sub {
	case "create":
		return userCreate(args[1:], getenv, stdin, stdout, stderr)
	case "-h", "--help":
		// What the flag package answers for the commands that have a flag set
		// of their own, so `user` answers it too rather than reporting a
		// subcommand nobody was asking for.
		fmt.Fprint(stderr, userUsage)

		return nil
	default:
		fmt.Fprint(stderr, userUsage)

		return fmt.Errorf("%w: user: unknown subcommand %q", errUsage, sub)
	}
}

// userCreate issues a user and prints its API key, which is what a device is
// configured with. The key is stored as issued, so this is not the only chance
// to see it — but it is the convenient one.
func userCreate(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("travelmap user create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, userCreateUsage) }

	email := fs.String("email", "", "the address the user logs in with")
	password := fs.String("password", "", "the password (read from standard input when omitted)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return errUsage
	}

	if fs.NArg() > 0 {
		fs.Usage()

		return fmt.Errorf("%w: user create takes no arguments, got %q", errUsage, fs.Arg(0))
	}

	address, err := model.NormalizeEmail(*email)
	if err != nil {
		fs.Usage()

		return fmt.Errorf("%w: %s", errUsage, err)
	}

	secret := *password
	if secret == "" {
		if secret, err = readPassword(stdin); err != nil {
			fs.Usage()

			return fmt.Errorf("%w: %s", errUsage, err)
		}
	}

	// Checked before the database is opened, so a password bcrypt will not take
	// is reported without a database file having been created. auth.Register
	// applies the same bounds again, through auth.HashPassword.
	if err := checkPasswordLength(secret); err != nil {
		return err
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

	created, err := auth.Register(ctx, db.Users(), address, secret)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%s: a user with the email %s already exists", path, address)
		}

		return err
	}

	fmt.Fprintf(stdout, "created user %d: %s\n", created.ID, created.Email)
	fmt.Fprintf(stdout, "api_key: %s\n", created.APIKey)

	return nil
}

// checkPasswordLength rejects a password bcrypt would not take. It exists so
// that check can run before a database is opened for it; auth.MinPasswordLength
// and auth.MaxPasswordLength are exported for exactly this.
func checkPasswordLength(password string) error {
	switch {
	case len(password) < auth.MinPasswordLength:
		return fmt.Errorf("password: shorter than %d bytes", auth.MinPasswordLength)
	case len(password) > auth.MaxPasswordLength:
		return fmt.Errorf("password: longer than the %d bytes bcrypt hashes", auth.MaxPasswordLength)
	default:
		return nil
	}
}

// readPassword takes the password off the first line of standard input.
//
// This is the way to give a password to a script or a systemd unit without it
// appearing in `ps` output, where every user on the host can read it, and in
// the shell history file. Only the line terminator is stripped: a password may
// legitimately end in a space.
func readPassword(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the password from standard input: %w", err)
	}

	secret := strings.TrimRight(line, "\r\n")
	if secret == "" {
		return "", errors.New("no password: pass --password, or write one to standard input")
	}

	return secret, nil
}
