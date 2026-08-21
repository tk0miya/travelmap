package dto

// LoginRequest is the body of POST /api/v1/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login is the 200 response of POST /api/v1/auth/login.
//
// The last four fields describe an upstream Cloud subscription. What this
// server answers them with, and why it answers them at all, is on the constants
// the login handler fills them from.
type Login struct {
	UserID             int64  `json:"user_id"`
	Email              string `json:"email"`
	APIKey             string `json:"api_key"`
	Status             string `json:"status"`
	Plan               string `json:"plan"`
	SubscriptionSource string `json:"subscription_source"`
	ActiveUntil        string `json:"active_until"`
}

// AuthError is the body of a failed POST /api/v1/auth/login.
//
// It is the one failure that does not answer with [Error]: upstream's auth
// controllers render an `error` naming the kind of failure alongside a
// human-readable `message`, and the spec documents that pair as the 401 of
// this endpoint. A client showing the message would show an empty line if the
// field were dropped.
type AuthError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
