package dto

// CreateSessionRequest is the body of POST /travelmap/web/session.
type CreateSessionRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
