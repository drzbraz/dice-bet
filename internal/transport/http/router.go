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
	mux.HandleFunc("GET /api/v1/clients", controller.ListClients)
	mux.HandleFunc("GET /api/v1/clients/{clientId}/wallet", controller.GetWallet)
	mux.HandleFunc("POST /api/v1/plays", controller.PostPlay)
	mux.HandleFunc("POST /api/v1/plays/end", controller.PostEndPlay)
	mux.HandleFunc("GET /health", healthHandler(pingFn))
	return withCORS(mux)
}

// withCORS allows any browser origin to call this HTTP mirror: the
// frontend (served from its own dev/deployed origin) fetches GET
// /api/v1/clients directly, so without this a browser blocks the request
// before it ever reaches the mux. This mirror is already auth-less by
// assessment-scope design (see README "Assumptions and trade-offs"), so an
// open origin policy adds no new exposure -- there is no cookie/session to
// protect. A production deployment would restrict this to the frontend's
// actual origin(s) via an allowlist instead of "*".
func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
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
