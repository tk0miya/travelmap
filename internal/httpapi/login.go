package httpapi

import (
	"errors"
	"net/http"

	"github.com/tk0miya/travelmap/internal/auth"
	"github.com/tk0miya/travelmap/internal/httpapi/dto"
	"github.com/tk0miya/travelmap/internal/model"
	"github.com/tk0miya/travelmap/internal/store"
)

// The subscription fields of the login response.
//
// Upstream reports its user's billing state here. Billing is a non-goal, so
// nothing is stored for it and these are the values a self-hosted upstream
// instance ends up with: it activates a user on creation with the Pro plan and
// no subscription source. A client that gates a feature on them therefore sees
// what it would see against upstream self-hosted.
const (
	loginStatus             = "active"
	loginPlan               = "pro"
	loginSubscriptionSource = "none"

	// Upstream sets active_until to a thousand years out on a self-hosted
	// instance rather than leaving it empty, so a client comparing it against
	// the clock finds an account that is not expired. This is that, as a
	// constant: there is no subscription here to expire.
	//
	// Seconds, not the milliseconds users/me sends: upstream renders this one
	// with an explicit `active_until&.iso8601`, which has no fractional part,
	// rather than handing a time to the JSON encoder that adds three places.
	loginActiveUntil = "9999-12-31T23:59:59Z"
)

// authFailedMessage is what upstream tells a client whose login was refused,
// word for word. It says nothing about which half was wrong, and neither does
// the answer given to an address with no account at all.
const authFailedMessage = "Invalid email or password"

// login answers POST /api/v1/auth/login: an email and a password in, the
// account's API key out.
//
// It is how a client that only has the account's password gets the credential
// every other endpoint wants. There is no 202 with a challenge token, because
// this server has no two-factor authentication to challenge with; see
// "Endpoints Deliberately Excluded" in TODO.md.
func (a *api) login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest

	if err := decodeJSON(w, r, &req); err != nil {
		a.logger.Warn("the login body could not be read",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusBadRequest, "invalid request body")

		return
	}

	// An address that is not an address matches no account, so it is answered
	// like one that is simply not registered rather than with a validation
	// error a client could tell the two apart by.
	email, err := model.NormalizeEmail(req.Email)
	if err != nil {
		a.loginFailed(w, r, req.Password)

		return
	}

	user, err := a.store.Users().ByEmail(r.Context(), email)

	switch {
	case errors.Is(err, store.ErrNotFound):
		a.loginFailed(w, r, req.Password)

		return
	case err != nil:
		a.logger.Error("looking up the user failed",
			"path", r.URL.Path,
			"error", err,
		)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		// A stored digest bcrypt cannot read is a broken row rather than a
		// wrong password, and the only place it can be noticed is here.
		if !errors.Is(err, auth.ErrPasswordMismatch) {
			a.logger.Error("checking the password failed",
				"user_id", user.ID,
				"error", err,
			)
		}

		a.writeAuthFailed(w, r)

		return
	}

	a.writeJSON(w, r, http.StatusOK, dto.Login{
		UserID:             user.ID,
		Email:              user.Email,
		APIKey:             user.APIKey,
		Status:             loginStatus,
		Plan:               loginPlan,
		SubscriptionSource: loginSubscriptionSource,
		ActiveUntil:        loginActiveUntil,
	})
}

// loginFailed answers a login for an address that has no account, having first
// spent what checking a password against one would have; the reason that
// matters is on [auth.CheckAbsentPassword].
func (a *api) loginFailed(w http.ResponseWriter, r *http.Request, password string) {
	_ = auth.CheckAbsentPassword(password)

	a.writeAuthFailed(w, r)
}

// writeAuthFailed answers a login that was refused.
func (a *api) writeAuthFailed(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, r, http.StatusUnauthorized, dto.AuthError{
		Error:   "auth_failed",
		Message: authFailedMessage,
	})
}
