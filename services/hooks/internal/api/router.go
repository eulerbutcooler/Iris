package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the chi router for iris-hooks.
// No auth middleware — webhook endpoints are public by design.
// Rate limiting / IP allowlisting should be handled at the reverse-proxy layer.
func NewRouter(h *Handler, log *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	// ── Global middleware ──────────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(log))

	// ── Routes ────────────────────────────────────────────────────────────────
	r.Get("/health", h.HealthCheck)
	r.Post("/hooks/{relayID}", h.IngestWebhook)

	return r
}

// requestLogger logs method, path, status, and duration for every request.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
