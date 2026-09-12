package httpapi

import (
	"errors"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// createSession answers POST /api/session: an email and a password in, a
// session cookie out. It is the browser's own sign-in action, distinct from
// POST /api/v1/auth/login, which hands a Dawarich client its api_key
// instead.
//
// A refused attempt says what that endpoint says and takes as long, through
// the same auth.CheckAbsentPassword an unknown address spends on a digest
// that matches nothing.
func (a *api) createSession(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateSessionRequest

	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("the session request body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	email, err := model.NormalizeEmail(req.Email)
	if err != nil {
		_ = auth.CheckAbsentPassword(req.Password)
		a.writeError(w, r, http.StatusUnauthorized, authFailedMessage)

		return
	}

	user, err := a.store.Users().ByEmail(r.Context(), email)

	switch {
	case errors.Is(err, store.ErrNotFound):
		_ = auth.CheckAbsentPassword(req.Password)
		a.writeError(w, r, http.StatusUnauthorized, authFailedMessage)

		return
	case err != nil:
		a.logger.Error("looking up the user failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		if !errors.Is(err, auth.ErrPasswordMismatch) {
			a.logger.Error("checking the password failed", "user_id", user.ID, "error", err)
		}

		a.writeError(w, r, http.StatusUnauthorized, authFailedMessage)

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

	w.WriteHeader(http.StatusCreated)
}

// deleteSession answers DELETE /api/session: the browser's own sign-out
// action, replacing the old POST /logout. Destroy deletes the session row,
// and LoadAndSave writes the cookie that clears it client-side — clearing
// the cookie alone, without deleting the row, would leave a session anyone
// holding the old token could still present.
func (a *api) deleteSession(w http.ResponseWriter, r *http.Request) {
	if err := a.sessions.Destroy(r.Context()); err != nil {
		a.logger.Error("destroying the session failed", "path", r.URL.Path, "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}
