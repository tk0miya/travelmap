package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/store"
	"github.com/tk0miya/travelmap/internal/store/storetest"
)

// TestRegister pins what Register does to a fresh account: the email is
// normalised, an id and API key are issued, and the stored digest verifies
// against the password that was passed in, not the password itself.
func TestRegister(t *testing.T) {
	t.Parallel()

	st := storetest.New(t)

	created, err := auth.Register(t.Context(), st.Users(), "Alice@Example.com", password)
	if err != nil {
		t.Fatalf("Register returned %v", err)
	}

	if created.Email != "alice@example.com" {
		t.Errorf("Register stored email %q, want it normalised", created.Email)
	}

	if created.ID == 0 {
		t.Error("Register returned a user with no id")
	}

	if created.APIKey == "" {
		t.Error("Register returned a user with no API key")
	}

	if err := auth.CheckPassword(created.PasswordHash, password); err != nil {
		t.Errorf("CheckPassword on the password just registered returned %v", err)
	}
}

// TestRegisterRejectsADuplicate pins that a second registration for the same
// address, differing only in case, fails rather than creating a second
// account.
func TestRegisterRejectsADuplicate(t *testing.T) {
	t.Parallel()

	st := storetest.New(t)

	if _, err := auth.Register(t.Context(), st.Users(), "alice@example.com", password); err != nil {
		t.Fatalf("the first Register returned %v", err)
	}

	_, err := auth.Register(t.Context(), st.Users(), "Alice@example.com", password)
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("the second Register returned %v, want ErrConflict", err)
	}
}

// TestRegisterRejectsBadInput covers what has to fail before anything is
// stored: an address that is not one, and a password outside the bounds
// HashPassword enforces.
func TestRegisterRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		email, password string
	}{
		"not an address":       {email: "not-an-address", password: password},
		"too short a password": {email: "alice@example.com", password: strings.Repeat("a", auth.MinPasswordLength-1)},
		"too long a password":  {email: "alice@example.com", password: strings.Repeat("a", auth.MaxPasswordLength+1)},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			st := storetest.New(t)

			if _, err := auth.Register(t.Context(), st.Users(), tt.email, tt.password); err == nil {
				t.Fatal("Register returned nil, want an error")
			}
		})
	}
}
