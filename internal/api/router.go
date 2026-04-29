package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter constructs the production HTTP router. The /api/v1 subgroup
// matches the OpenAPI base path. /healthz lives outside the API group so
// it can be hit without auth or device-id resolution — handy for liveness
// probes and pre-simulator sanity checks.
//
// Middleware ordering matters: RequestID first so every subsequent log line
// has a request_id; Logger before Recoverer so panic recovery doesn't swallow
// the request line; Recoverer last so it sits closest to the handler.
//
// When debug is true, DebugBodyLogger is added after Recoverer so it logs
// the body for every successful AND failed POST device request — including
// the 500 cases we want to diagnose against the simulator.
func NewRouter(h *Handlers, debug bool) *chi.Mux {
	r := chi.NewRouter()
	applyDefaultMiddleware(r)
	if debug {
		r.Use(DebugBodyLogger)
	}

	r.Get("/healthz", healthz)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/devices/{device_id}/heartbeat", h.Heartbeat)
		r.Post("/devices/{device_id}/stats", h.PostStats)
		r.Get("/devices/{device_id}/stats", h.GetStats)
	})

	return r
}

// applyDefaultMiddleware is shared with tests so that panic-recovery tests
// exercise the exact same middleware stack as production.
func applyDefaultMiddleware(r chi.Router) {
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
}

// healthz reports liveness. Not under /api/v1 because it's an operational
// concern, not part of the device API. Three lines is intentional — health
// checks shouldn't depend on registry, store, or any other state.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
