package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/auth"
)

// password is a valid one, within the bounds HashPassword applies.
const password = "correct horse battery"

// TestNewAPIKey pins the shape of a key, because it goes into a URL as the
// `api_key` query parameter: anything needing escaping there would be a key
// that works from one client and not another.
func TestNewAPIKey(t *testing.T) {
	t.Parallel()

	const wantLength = 64 // 32 bytes as hex

	key, err := auth.NewAPIKey()
	if err != nil {
		t.Fatalf("NewAPIKey returned %v", err)
	}

	if len(key) != wantLength {
		t.Errorf("NewAPIKey returned %d characters, want %d", len(key), wantLength)
	}

	if strings.Trim(key, "0123456789abcdef") != "" {
		t.Errorf("NewAPIKey returned %q, want lowercase hex", key)
	}
}

// TestNewAPIKeyIsUnique is a smoke test on the randomness: a generator that
// returned a constant would pass every other test here, and would hand every
// user the same credential.
func TestNewAPIKeyIsUnique(t *testing.T) {
	t.Parallel()

	const keys = 100

	seen := make(map[string]bool, keys)

	for range keys {
		key, err := auth.NewAPIKey()
		if err != nil {
			t.Fatalf("NewAPIKey returned %v", err)
		}

		if seen[key] {
			t.Fatalf("NewAPIKey returned %q twice", key)
		}

		seen[key] = true
	}
}

func TestHashPasswordAndCheckPassword(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned %v", err)
	}

	if strings.Contains(hash, password) {
		t.Fatalf("the digest %q contains the password", hash)
	}

	if err := auth.CheckPassword(hash, password); err != nil {
		t.Errorf("CheckPassword on the right password returned %v", err)
	}

	if err := auth.CheckPassword(hash, password+"!"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Errorf("CheckPassword on the wrong password returned %v, want ErrPasswordMismatch", err)
	}
}

// TestHashPasswordSalts pins that two users with the same password do not share
// a digest, so that a leaked database does not say who to attack first.
func TestHashPasswordSalts(t *testing.T) {
	t.Parallel()

	first, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned %v", err)
	}

	second, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned %v", err)
	}

	if first == second {
		t.Errorf("the same password hashed to %q twice", first)
	}
}

// TestCheckPasswordRejectsANonDigest covers a database column holding something
// that is not bcrypt output, which must be an error rather than a match.
func TestCheckPasswordRejectsANonDigest(t *testing.T) {
	t.Parallel()

	err := auth.CheckPassword("not a digest", password)
	if err == nil {
		t.Fatal("CheckPassword against a malformed digest returned nil")
	}

	if errors.Is(err, auth.ErrPasswordMismatch) {
		t.Errorf("CheckPassword reported %v, want the malformed digest to be distinguishable", err)
	}
}

// TestHashPasswordEnforcesTheLengthBounds pins the two limits on what may be
// used as a password, through the only function that applies them.
func TestHashPasswordEnforcesTheLengthBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		password string
		wantErr  bool
	}{
		"a password of the minimum length": {password: strings.Repeat("a", auth.MinPasswordLength)},
		"a password of the maximum length": {password: strings.Repeat("a", auth.MaxPasswordLength)},
		"one byte too short":               {password: strings.Repeat("a", auth.MinPasswordLength-1), wantErr: true},
		// bcrypt hashes at most 72 bytes and refuses more rather than
		// truncating, so a longer password has to be rejected here too — with a
		// message that says why.
		"one byte too long": {password: strings.Repeat("a", auth.MaxPasswordLength+1), wantErr: true},
		"empty":             {password: "", wantErr: true},
		// Multi-byte characters count as bytes, which is what bcrypt limits:
		// 24 three-byte runes are 72 bytes and still allowed.
		"a multi-byte password within the byte limit": {password: strings.Repeat("あ", 24)},
		"a multi-byte password over the byte limit":   {password: strings.Repeat("あ", 25), wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			hash, err := auth.HashPassword(tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("HashPassword(%d bytes) returned a digest, want an error", len(tt.password))
				}

				return
			}

			if err != nil {
				t.Fatalf("HashPassword(%d bytes) returned %v", len(tt.password), err)
			}

			// A password the bounds accept has to be one bcrypt accepts too,
			// which is why the upper bound is bcrypt's own limit and not a
			// number of our choosing.
			if err := auth.CheckPassword(hash, tt.password); err != nil {
				t.Errorf("CheckPassword on the password just hashed returned %v", err)
			}
		})
	}
}
