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
	// Done is set once the account has been created; the template then shows
	// the account's API key instead of the form.
	Done   bool
	APIKey string

	// Email is echoed back on a re-render, so a rejected submission does not
	// make someone retype it. The password fields are never echoed.
	Email string

	// EmailError, PasswordError and ConfirmError report a submission's
	// problem against the field it belongs to; "" when that field had none.
	EmailError    string
	PasswordError string
	ConfirmError  string
}

// signupPage answers GET /signup: the form, with no error to report yet.
func (a *api) signupPage(w http.ResponseWriter, r *http.Request) {
	a.renderPage(w, r, signupTemplate, signupData{})
}

// signupSubmit answers POST /signup: an email, a password and its
// confirmation in, a signed-in session and the account's API key out.
//
// Open to anyone — no environment variable, no invite code, no
// first-user-only rule, per the "User management" row in docs/architecture.md.
func (a *api) signupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderPage(w, r, signupTemplate, signupData{EmailError: "the form could not be read; try again"})

		return
	}

	rawEmail := r.FormValue("email")
	password := r.FormValue("password")
	confirmation := r.FormValue("password_confirmation")

	email, err := model.NormalizeEmail(rawEmail)
	if err != nil {
		a.renderPage(w, r, signupTemplate, signupData{Email: rawEmail, EmailError: "not a valid email address"})

		return
	}

	if msg := passwordLengthError(password); msg != "" {
		a.renderPage(w, r, signupTemplate, signupData{Email: email, PasswordError: msg})

		return
	}

	// Compared before anything is written: a typo in the confirmation field
	// must not create an account nobody can log in to.
	if password != confirmation {
		a.renderPage(w, r, signupTemplate, signupData{Email: email, ConfirmError: "does not match the password"})

		return
	}

	user, err := auth.Register(r.Context(), a.store.Users(), email, password)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			a.renderPage(w, r, signupTemplate, signupData{Email: email, EmailError: "already registered"})

			return
		}

		a.logger.Error("registering the account failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	// Before the user id goes into the session, matching loginSubmit: a
	// token minted before the browser authenticated must not still be the
	// one it holds afterwards.
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		a.logger.Error("renewing the session token failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.sessions.Put(r.Context(), sessionUserIDKey, user.ID)

	// Sign-up replaces travelmap user create, whose entire output is the API
	// key, so the page it lands on shows it: without it, someone who signed
	// up in a browser has no way to configure the phone app.
	a.renderPage(w, r, signupTemplate, signupData{Done: true, APIKey: user.APIKey})
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
