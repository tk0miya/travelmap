package httpapi

import (
	"net/http"

	"github.com/tk0miya/travelmap/internal/httpapi/dto"
)

// health answers GET /api/v1/health.
//
// It takes no authentication and says nothing about the server's state beyond
// being reachable; the X-Dawarich-Response and X-Dawarich-Version headers set
// on the way out are what a client actually reads it for.
func (a *api) health(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, r, http.StatusOK, dto.Health{Status: "ok"})
}
