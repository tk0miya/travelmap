package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// createUser answers POST /travelmap/web/users: an email, a password and its
// confirmation in, a signed-in session and the account's API key out.
//
// Open to anyone — no environment variable, no invite code, no
// first-user-only rule.
func (a *api) createUser(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateUserRequest

	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("the sign-up request body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	email, err := model.NormalizeEmail(req.Email)
	if err != nil {
		a.writeJSON(w, r, http.StatusUnprocessableEntity, dto.CreateUserError{EmailError: "not a valid email address"})

		return
	}

	if msg := passwordLengthError(req.Password); msg != "" {
		a.writeJSON(w, r, http.StatusUnprocessableEntity, dto.CreateUserError{PasswordError: msg})

		return
	}

	// Compared before anything is written: a typo in the confirmation field
	// must not create an account nobody can log in to.
	if req.Password != req.PasswordConfirmation {
		a.writeJSON(w, r, http.StatusUnprocessableEntity, dto.CreateUserError{ConfirmError: "does not match the password"})

		return
	}

	user, err := auth.Register(r.Context(), a.store, email, req.Password)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			a.writeJSON(w, r, http.StatusUnprocessableEntity, dto.CreateUserError{EmailError: "already registered"})

			return
		}

		a.logger.Error("registering the account failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	if !a.startSession(w, r, user) {
		return
	}

	// The API key is what configures the phone app, and this is the only
	// place it is ever shown, so the response the sign-up lands on carries it.
	a.writeJSON(w, r, http.StatusCreated, dto.CreateUserResponse{APIKey: user.APIKey})
}

// passwordLengthError reports why password is outside the bounds
// auth.HashPassword enforces, in bytes rather than characters — bcrypt's
// 72-byte limit is a byte limit, and a Japanese password reaches it at 24
// characters, where a message speaking of characters would be wrong for the
// users most likely to hit it. "" if password is within bounds.
func passwordLengthError(password string) string {
	switch {
	case len(password) < auth.MinPasswordLength:
		return fmt.Sprintf("must be at least %d bytes", auth.MinPasswordLength)
	case len(password) > auth.MaxPasswordLength:
		return fmt.Sprintf("must be at most %d bytes, which is what bcrypt hashes", auth.MaxPasswordLength)
	default:
		return ""
	}
}
