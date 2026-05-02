package api

import (
	"net/http"

	"github.com/eulerbutcooler/iris/services/core/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter wires all routes and middleware onto a chi.Mux.
func NewRouter(h *Handler, cfg *config.Config) *chi.Mux {
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(RequestLogger(h.log))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{cfg.FrontendURL},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ── Health ─────────────────────────────────────────────────────────────────
	r.Get("/health", h.HealthCheck)

	// ── API v1 ────────────────────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {

		// Public endpoints (no JWT required)
		r.Post("/auth/register", h.Register)
		r.Post("/auth/login", h.Login)

		// Protected endpoints
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(cfg.JWTSecret))

			// Relays
			r.Post("/relays", h.CreateRelay)
			r.Get("/relays", h.GetAllRelays)
			r.Get("/relays/{id}", h.GetRelay)
			r.Put("/relays/{id}", h.UpdateRelay)
			r.Put("/relays/{id}/actions", h.UpdateRelayActions)
			r.Delete("/relays/{id}", h.DeleteRelay)
			r.Post("/relays/{id}/trigger", h.TriggerRelay)

			// Executions — relay-scoped
			r.Get("/relays/{id}/executions", h.GetExecutions)

			// Executions — execution-scoped
			r.Get("/executions/{id}", h.GetExecution)
			r.Get("/executions/{id}/steps", h.GetExecutionSteps)
			r.Delete("/executions/{id}", h.DeleteExecution)

			// Secrets
			r.Get("/secrets", h.ListSecrets)
			r.Post("/secrets", h.CreateSecret)
			r.Delete("/secrets/{id}", h.DeleteSecret)

			// AI relay generation
			r.Post("/ai/relay", h.GenerateRelay)
		})
	})

	return r
}
