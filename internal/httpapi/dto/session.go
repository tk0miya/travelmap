package dto

// CreateSessionRequest is the body of POST /api/session.
type CreateSessionRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
