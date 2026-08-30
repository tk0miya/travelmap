package model

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// User is an account on this server.
//
// It is issued once, from the browser sign-up screen through auth.Register,
// and then only read: the API key it carries is what every request from a
// device authenticates with.
type User struct {
	// ID is the primary key, and the `user_id` the API reports.
	ID int64

	// Email identifies the account, in the form [NormalizeEmail] returns.
	Email string

	// PasswordHash is the bcrypt digest of the password, as produced by
	// internal/auth. It never reaches a DTO: nothing the API returns includes
	// it, and POST /api/v1/auth/login only ever compares against it.
	PasswordHash string

	// APIKey is the token a device sends, as the `api_key` query parameter or
	// as an `Authorization: Bearer` header.
	//
	// It is stored as issued rather than hashed, because
	// POST /api/v1/auth/login has to hand the key itself back to the client:
	// a digest could not be turned back into one.
	APIKey string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NormalizeEmail trims, validates and lowercases an email address as typed by
// an operator, returning the form a [User] stores.
//
// Lowercasing is what makes "Alice@Example.com" and "alice@example.com" one
// account rather than two: the address is the login identity, and a user who
// typed it with a capital once will not remember doing so.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("email: empty")
	}

	// ParseAddress also accepts a display name ("Alice <a@example.com>"), which
	// is not an identity anyone should end up logging in with, so the parsed
	// address has to be all there was.
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", fmt.Errorf("email %q: %w", raw, err)
	}

	if addr.Address != trimmed {
		return "", fmt.Errorf("email %q: expected a bare address, not %q", raw, addr.Name)
	}

	return strings.ToLower(addr.Address), nil
}
