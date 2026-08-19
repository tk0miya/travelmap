package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

// apiKeyBytes is how much randomness an API key carries. 32 bytes is 256 bits,
// which is beyond guessing, and it is what the key is: a bearer credential with
// no expiry, sent by a phone over the network on every request.
const apiKeyBytes = 32

// The password lengths [HashPassword] accepts, in bytes.
//
// The upper bound is bcrypt's own: it hashes at most 72 bytes and
// [bcrypt.GenerateFromPassword] rejects anything longer rather than silently
// truncating. The lower bound is this project's choice — bcrypt has none — and
// is stated here because a caller asking for a password is the one that has to
// say so before the attempt.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 72
)

// ErrPasswordMismatch reports that a password does not match the digest it was
// checked against. It is deliberately the same answer for every wrong
// password, so a caller cannot learn anything from which error it gets.
var ErrPasswordMismatch = errors.New("password does not match")

// NewAPIKey issues an API key.
//
// The key is hex rather than base64, so that it survives being pasted into a
// URL as the `api_key` query parameter without escaping, and being read aloud
// or retyped from a phone screen.
func NewAPIKey() (string, error) {
	buf := make([]byte, apiKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating an API key: %w", err)
	}

	return hex.EncodeToString(buf), nil
}

// HashPassword returns the bcrypt digest to store for password.
//
// The cost is bcrypt's default rather than a configured one: it is what
// [CheckPassword] reads back out of the digest, so raising it later needs no
// migration — existing users keep verifying at the cost they were hashed with.
func HashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}

	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing the password: %w", err)
	}

	return string(digest), nil
}

// CheckPassword reports whether password is the one hash was made from,
// returning [ErrPasswordMismatch] when it is not.
func CheckPassword(hash, password string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}

		return fmt.Errorf("checking the password: %w", err)
	}

	return nil
}

// validatePassword reports whether a password may be used at all. It is not
// exported because [HashPassword] is the only way to use one, and it applies
// this itself; a caller that wants the bounds without hashing has
// [MinPasswordLength] and [MaxPasswordLength].
func validatePassword(password string) error {
	switch {
	case len(password) < MinPasswordLength:
		return fmt.Errorf("password: shorter than %d bytes", MinPasswordLength)
	case len(password) > MaxPasswordLength:
		// bcrypt would refuse this itself; saying so up front stops an
		// operator from concluding the password was accepted and truncated.
		return fmt.Errorf("password: longer than the %d bytes bcrypt hashes", MaxPasswordLength)
	default:
		return nil
	}
}

// CheckAbsentPassword spends what [CheckPassword] would have spent on a user
// that does not exist, and always reports [ErrPasswordMismatch].
//
// Without it, a login for an unknown address answers as fast as the database
// lookup while a login for a known one waits for bcrypt: the difference is
// tens of milliseconds over the network, and it tells anyone who measures it
// which addresses have accounts here. The digest is made from randomness, so
// no password matches it and nothing has to be kept in step with it.
func CheckAbsentPassword(password string) error {
	if err := CheckPassword(absentUserHash(), password); err != nil {
		return err
	}

	// Only reachable by guessing the random password below, which is reported
	// as the mismatch it has to be rather than as a successful login.
	return ErrPasswordMismatch
}

// absentUserHash is the digest [CheckAbsentPassword] compares against.
//
// It is built on first use rather than in an init function: hashing costs the
// same either way, and every command that is not `serve` would pay it for a
// comparison it never makes.
var absentUserHash = sync.OnceValue(func() string {
	// An API key is 64 hex characters, which is within what HashPassword
	// accepts and what bcrypt hashes whole.
	//
	// Both failures below are panics because neither leaves anything to fall
	// back on: they mean the operating system has no randomness to give, so
	// NewAPIKey cannot issue a key either and this server has nothing to
	// authenticate with.
	password, err := NewAPIKey()
	if err != nil {
		panic(fmt.Sprintf("auth: generating the absent-user password: %v", err))
	}

	digest, err := HashPassword(password)
	if err != nil {
		panic(fmt.Sprintf("auth: hashing the absent-user password: %v", err))
	}

	return digest
})
