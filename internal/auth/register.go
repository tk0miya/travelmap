package auth

import (
	"context"
	"fmt"

	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// Register creates an account: it normalises email, hashes password, issues
// an API key, and stores the result together with a default settings row —
// in the same transaction, so a stored user never exists without one. It
// returns the user as stored, with ID, CreatedAt and UpdatedAt filled in, and
// [store.ErrConflict] if the email is already taken.
//
// It is the one path that creates a user: the browser sign-up screen is its
// only caller, so an address or a password bound only has to be validated
// once, and GET/PATCH /api/v1/settings(/mobile) never have to fall back to
// computed defaults for a row that does not exist yet.
func Register(ctx context.Context, st store.Store, email, password string) (model.User, error) {
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

	var created model.User

	err = st.Tx(ctx, func(ctx context.Context, tx store.Store) error {
		var err error

		created, err = tx.Users().Create(ctx, model.User{
			Email:        address,
			PasswordHash: hash,
			APIKey:       apiKey,
		})
		if err != nil {
			return err
		}

		_, err = tx.Settings().Create(ctx, model.DefaultSettings(created.ID))

		return err
	})
	if err != nil {
		return model.User{}, fmt.Errorf("auth: registering %s: %w", address, err)
	}

	return created, nil
}
