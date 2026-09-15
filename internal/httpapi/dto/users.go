package dto

// CreateUserRequest is the body of POST /travelmap/web/users.
type CreateUserRequest struct {
	Email                string `json:"email"`
	Password             string `json:"password"`
	PasswordConfirmation string `json:"password_confirmation"`
}

// CreateUserResponse is the 201 body of POST /travelmap/web/users: the new
// account's API key, the only place it is ever shown again.
type CreateUserResponse struct {
	APIKey string `json:"api_key"`
}

// CreateUserError is the 422 body of POST /travelmap/web/users: which field a
// refused submission's problem belongs to, "" on every other field. At most
// one is ever set, since validation stops at the first failure.
type CreateUserError struct {
	EmailError    string `json:"email_error,omitempty"`
	PasswordError string `json:"password_error,omitempty"`
	ConfirmError  string `json:"confirm_error,omitempty"`
}
