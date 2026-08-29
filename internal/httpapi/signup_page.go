package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// signupData is what the sign-up page's template renders.
type signupData struct {
	// Email is the address as typed, so a rejected submission does not make
	// the visitor retype it. Left "" on a fresh visit.
	Email string

	// EmailError, PasswordError and ConfirmError sit next to the field they
	// describe, or "" when that field was accepted. Passwords are never
	// echoed back, so there is nothing to repopulate them with.
	EmailError    string
	PasswordError string
	ConfirmError  string

	// APIKey is set once sign-up succeeds, which switches the template from
	// the form to the confirmation that shows it.
	APIKey string
}

// signupPage answers GET /signup: the form, with no error to report yet.
func (a *api) signupPage(w http.ResponseWriter, r *http.Request) {
	a.renderPage(w, r, signupTemplate, signupData{})
}

// signupSubmit answers POST /signup: an address, a password and its
// confirmation in, a signed-in session and the account's API key out.
//
// A rejected submission re-renders with the reason against the field it
// belongs to, unlike loginSubmit, which deliberately gives the same answer
// for every failure so as not to say which half was wrong: sign-up has no
// such secret to keep, since the address either has an account already or it
// does not — [store.ErrConflict] is what says which.
func (a *api) signupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderPage(w, r, signupTemplate, signupData{EmailError: "could not read the form"})

		return
	}

	rawEmail := r.FormValue("email")
	password := r.FormValue("password")
	confirmation := r.FormValue("password_confirmation")

	data := signupData{Email: rawEmail}

	email, emailErr := model.NormalizeEmail(rawEmail)
	if emailErr != nil {
		data.EmailError = "enter a valid email address"
	}

	// Stated in bytes, not characters: bcrypt's 72 is a byte limit, and a
	// password in a multi-byte script reaches it at well under 72 characters
	// typed.
	switch {
	case len(password) < auth.MinPasswordLength:
		data.PasswordError = fmt.Sprintf("must be at least %d bytes", auth.MinPasswordLength)
	case len(password) > auth.MaxPasswordLength:
		data.PasswordError = fmt.Sprintf("must be at most %d bytes", auth.MaxPasswordLength)
	}

	// Checked regardless of what the password bounds above found, so a typo
	// in an otherwise-invalid password is not hidden behind the length error.
	if confirmation != password {
		data.ConfirmError = "does not match the password"
	}

	if data.EmailError != "" || data.PasswordError != "" || data.ConfirmError != "" {
		a.renderPage(w, r, signupTemplate, data)

		return
	}

	user, err := auth.Register(r.Context(), a.store.Users(), email, password)

	switch {
	case errors.Is(err, store.ErrConflict):
		data.EmailError = "an account with this address already exists"
		a.renderPage(w, r, signupTemplate, data)

		return
	case err != nil:
		a.logger.Error("registering the account failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	// Before the user id goes into the session, for the same reason
	// loginSubmit renews it: a token minted before the browser authenticated
	// must not still be the one it holds afterwards.
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		a.logger.Error("renewing the session token failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.sessions.Put(r.Context(), sessionUserIDKey, user.ID)

	a.renderPage(w, r, signupTemplate, signupData{Email: user.Email, APIKey: user.APIKey})
}
