package dto

// Health is the body of GET /api/v1/health: {"status":"ok"}.
type Health struct {
	Status string `json:"status"`
}
