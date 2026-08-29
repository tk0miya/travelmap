package httpapi

import (
	"errors"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// loginData is what the login page's template renders.
type loginData struct {
	// Error is the message shown above the form after a refused login, or ""
	// on a fresh visit.
	Error string
}

// loginPage answers GET /login: the form, with no error to report yet.
func (a *api) loginPage(w http.ResponseWriter, r *http.Request) {
	a.renderPage(w, r, loginTemplate, loginData{})
}

// loginSubmit answers POST /login: an email and a password in, a session
// cookie out.
//
// A refused login says what POST /api/v1/auth/login says and takes as long,
// through the same auth.CheckAbsentPassword an unknown address spends on a
// digest that matches nothing.
func (a *api) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderPage(w, r, loginTemplate, loginData{Error: authFailedMessage})

		return
	}

	password := r.FormValue("password")

	email, err := model.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		_ = auth.CheckAbsentPassword(password)
		a.renderPage(w, r, loginTemplate, loginData{Error: authFailedMessage})

		return
	}

	user, err := a.store.Users().ByEmail(r.Context(), email)

	switch {
	case errors.Is(err, store.ErrNotFound):
		_ = auth.CheckAbsentPassword(password)
		a.renderPage(w, r, loginTemplate, loginData{Error: authFailedMessage})

		return
	case err != nil:
		a.logger.Error("looking up the user failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
		if !errors.Is(err, auth.ErrPasswordMismatch) {
			a.logger.Error("checking the password failed", "user_id", user.ID, "error", err)
		}

		a.renderPage(w, r, loginTemplate, loginData{Error: authFailedMessage})

		return
	}

	// Before the user id goes into the session: a token minted before the
	// browser authenticated must not still be the one it holds afterwards.
	if err := a.sessions.RenewToken(r.Context()); err != nil {
		a.logger.Error("renewing the session token failed", "user_id", user.ID, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	a.sessions.Put(r.Context(), sessionUserIDKey, user.ID)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout answers POST /logout: Destroy deletes the session row, and
// LoadAndSave writes the cookie that clears it client-side. Clearing the
// cookie alone, without deleting the row, would leave a session anyone
// holding the old token could still present.
func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context()); err != nil {
		a.logger.Error("destroying the session failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
