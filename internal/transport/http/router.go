package http

import (
	"context"
	"encoding/json"
	"net/http"
)

// NewRouter builds the HTTP mux for the REST mirror of the WebSocket use
// cases plus /health. pingFn checks database connectivity for the health
// check (see rule: "/health checks database connectivity").
func NewRouter(controller *Controller, pingFn func(ctx context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clients/{clientId}/wallet", controller.GetWallet)
	mux.HandleFunc("POST /api/v1/plays", controller.PostPlay)
	mux.HandleFunc("POST /api/v1/plays/end", controller.PostEndPlay)
	mux.HandleFunc("GET /health", healthHandler(pingFn))
	return mux
}

func healthHandler(pingFn func(ctx context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := pingFn(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
