package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// TestUserCreate covers what Create fills in, since the caller has no other way
// to learn the id the row got.
func TestUserCreate(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	before := time.Now().UTC().Truncate(time.Second)

	got, err := db.Users().Create(t.Context(), testUser("created@example.com"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	if got.ID == 0 {
		t.Error("Create returned a user with no id")
	}

	if got.CreatedAt.Before(before) || got.CreatedAt.After(time.Now().UTC()) {
		t.Errorf("CreatedAt = %v, want a time from this test's run", got.CreatedAt)
	}

	if !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want CreatedAt %v on a user that has not been updated", got.UpdatedAt, got.CreatedAt)
	}
}

// TestUserLookups pins that a stored user comes back identical through either
// lookup, timestamps included — the round trip through Unix seconds is where a
// truncation or a timezone would show up.
func TestUserLookups(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	created, err := db.Users().Create(t.Context(), testUser("Alice@Example.com"))
	if err != nil {
		t.Fatalf("Create returned %v", err)
	}

	tests := map[string]struct {
		lookup func(context.Context) (model.User, error)
	}{
		"by email": {
			lookup: func(ctx context.Context) (model.User, error) {
				return db.Users().ByEmail(ctx, created.Email)
			},
		},
		// The address is one identity however it was typed, which is what the
		// NOCASE collation on the column is for.
		"by email in another case": {
			lookup: func(ctx context.Context) (model.User, error) {
				return db.Users().ByEmail(ctx, "alice@example.com")
			},
		},
		"by API key": {
			lookup: func(ctx context.Context) (model.User, error) {
				return db.Users().ByAPIKey(ctx, created.APIKey)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.lookup(t.Context())
			if err != nil {
				t.Fatalf("the lookup returned %v", err)
			}

			if diff := cmp.Diff(created, got); diff != "" {
				t.Errorf("the user differs (-want +got):\n%s", diff)
			}
		})
	}
}

// TestUserLookupsReportMissing pins ErrNotFound rather than a zero user, because
// Step 5's authentication turns exactly this into a 401 and would otherwise
// authenticate a request as a user with no id.
func TestUserLookupsReportMissing(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	tests := map[string]struct {
		lookup func(context.Context) (model.User, error)
	}{
		"by email": {
			lookup: func(ctx context.Context) (model.User, error) {
				return db.Users().ByEmail(ctx, "nobody@example.com")
			},
		},
		"by API key": {
			lookup: func(ctx context.Context) (model.User, error) {
				return db.Users().ByAPIKey(ctx, "not-a-key")
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.lookup(t.Context())
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("the lookup returned (%v, %v), want ErrNotFound", got, err)
			}
		})
	}
}

// TestUserCreateRejectsDuplicates pins ErrConflict on both unique columns. The
// email case matters most: an operator retyping an address with a capital must
// be told the account exists, not given a second one that the login lookup
// would then have to choose between.
func TestUserCreateRejectsDuplicates(t *testing.T) {
	t.Parallel()

	const (
		email  = "taken@example.com"
		apiKey = "the-one-key"
	)

	tests := map[string]struct {
		second model.User
	}{
		"the same email": {
			second: model.User{Email: email, PasswordHash: "digest", APIKey: "another-key"},
		},
		"the same email in another case": {
			second: model.User{Email: "Taken@Example.COM", PasswordHash: "digest", APIKey: "another-key"},
		},
		// Two users sharing an API key would make authentication ambiguous, so
		// the collision has to be reported rather than stored — however
		// improbable 256 bits of randomness colliding is.
		"the same API key": {
			second: model.User{Email: "other@example.com", PasswordHash: "digest", APIKey: apiKey},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db := newTestDB(t)

			first := model.User{Email: email, PasswordHash: "digest", APIKey: apiKey}
			if _, err := db.Users().Create(t.Context(), first); err != nil {
				t.Fatalf("the first Create returned %v", err)
			}

			if _, err := db.Users().Create(t.Context(), tt.second); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("the second Create returned %v, want ErrConflict", err)
			}
		})
	}
}
