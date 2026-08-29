package auth

import (
	"context"
	"fmt"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// Register creates an account: it normalises email, hashes password, issues
// an API key and stores the result through users. It returns the user as
// stored, with ID, CreatedAt and UpdatedAt filled in, and [store.ErrConflict]
// if the email is already taken.
//
// It is the one path that creates a user: `travelmap user create` and the
// browser sign-up screen both call it, so an address or a password bound
// only has to be validated once.
func Register(ctx context.Context, users store.UserRepository, email, password string) (model.User, error) {
	address, err := model.NormalizeEmail(email)
	if err != nil {
		return model.User{}, err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return model.User{}, err
	}

	apiKey, err := NewAPIKey()
	if err != nil {
		return model.User{}, err
	}

	created, err := users.Create(ctx, model.User{
		Email:        address,
		PasswordHash: hash,
		APIKey:       apiKey,
	})
	if err != nil {
		return model.User{}, fmt.Errorf("auth: registering %s: %w", address, err)
	}

	return created, nil
}
