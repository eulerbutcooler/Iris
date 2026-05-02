package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/eulerbutcooler/iris/services/hooks/internal/queue"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20 // 1 MB

// Handler holds the dependencies for the webhook ingestion handler.
type Handler struct {
	publisher *queue.Publisher
	log       *slog.Logger
}

// NewHandler creates a Handler with the given publisher and logger.
func NewHandler(publisher *queue.Publisher, log *slog.Logger) *Handler {
	return &Handler{publisher: publisher, log: log}
}

// HealthCheck handles GET /health.
func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "iris-hooks"})
}

// IngestWebhook handles POST /hooks/{relayID}.
//
// Flow:
//  1. Read body (max 1 MB)
//  2. Extract or generate event_id
//  3. Build ExecutionEvent and publish to NATS
//  4. Return 200 {"accepted": true, "event_id": "..."}
func (h *Handler) IngestWebhook(w http.ResponseWriter, r *http.Request) {
	relayID := chi.URLParam(r, "relayID")

	// ── Read body ────────────────────────────────────────────────────────────
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		if err.Error() == "http: request body too large" {
			writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body exceeds 1 MB limit")
			return
		}
		h.log.Error("read body", "relay_id", relayID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read request body")
		return
	}

	// ── Event ID ─────────────────────────────────────────────────────────────
	// Prefer X-Event-ID header, then ?event_id query param, then generate one.
	eventID := r.Header.Get("X-Event-ID")
	if eventID == "" {
		eventID = r.URL.Query().Get("event_id")
	}
	if eventID == "" {
		eventID = uuid.New().String()
	}

	// ── Payload ──────────────────────────────────────────────────────────────
	// Accept the raw body as-is. If it's not valid JSON, wrap it in a string field
	// so downstream consumers always get a valid JSON payload.
	var payload json.RawMessage
	if json.Valid(body) {
		payload = json.RawMessage(body)
	} else {
		wrapped, _ := json.Marshal(map[string]string{"raw": string(body)})
		payload = json.RawMessage(wrapped)
	}

	// ── Publish ──────────────────────────────────────────────────────────────
	event := queue.ExecutionEvent{
		RelayID:    relayID,
		EventID:    eventID,
		Payload:    payload,
		ReceivedAt: time.Now().UTC(),
	}

	if err := h.publisher.Publish(r.Context(), event); err != nil {
		h.log.Error("publish event", "relay_id", relayID, "event_id", eventID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to queue event")
		return
	}

	h.log.Info("webhook ingested", "relay_id", relayID, "event_id", eventID, "body_bytes", len(body))

	writeJSON(w, http.StatusOK, map[string]string{
		"accepted": "true",
		"event_id": eventID,
	})
}

// ─── JSON helpers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
