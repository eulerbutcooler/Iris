package api

import (
	"errors"
	"net/http"

	"github.com/eulerbutcooler/iris/packages/actions"
	"github.com/eulerbutcooler/iris/packages/cronutil"
	"github.com/eulerbutcooler/iris/packages/dag"
	"github.com/eulerbutcooler/iris/services/core/internal/models"
	"github.com/eulerbutcooler/iris/services/core/internal/queue"
	"github.com/eulerbutcooler/iris/services/core/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"time"
)

// ─── Relay handlers ───────────────────────────────────────────────────────────

// CreateRelay handles POST /api/v1/relays.
func (h *Handler) CreateRelay(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	var req models.CreateRelayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name is required")
		return
	}
	if req.TriggerType == "" {
		req.TriggerType = "webhook"
	}

	// Validate action configs
	for _, a := range req.Actions {
		if err := actions.ValidateConfig(a.ActionType, a.Config); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
	}

	// DAG cycle check
	if err := validateDAG(req.Actions, req.Edges); err != nil {
		writeError(w, http.StatusBadRequest, "CYCLE_DETECTED", err.Error())
		return
	}

	// Compute next_run_at for cron relays
	nextRunAt, err := computeNextRun(req.TriggerType, req.TriggerConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	relay, err := h.relays.CreateRelay(r.Context(), userID, req, nextRunAt)
	if err != nil {
		h.log.Error("create relay", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create relay")
		return
	}

	writeJSON(w, http.StatusCreated, relay)
}

// GetAllRelays handles GET /api/v1/relays.
func (h *Handler) GetAllRelays(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	relays, err := h.relays.GetAllRelays(r.Context(), userID)
	if err != nil {
		h.log.Error("get all relays", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch relays")
		return
	}

	if relays == nil {
		relays = []models.Relay{}
	}
	writeJSON(w, http.StatusOK, relays)
}

// GetRelay handles GET /api/v1/relays/{id}.
func (h *Handler) GetRelay(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	relay, err := h.relays.GetRelay(r.Context(), id, userID)
	if err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("get relay", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch relay")
		return
	}

	writeJSON(w, http.StatusOK, relay)
}

// UpdateRelay handles PUT /api/v1/relays/{id}.
func (h *Handler) UpdateRelay(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	var req models.UpdateRelayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	nextRunAt, err := computeNextRun(req.TriggerType, req.TriggerConfig)
	if err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}

	relay, err := h.relays.UpdateRelay(r.Context(), id, userID, req, nextRunAt)
	if err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("update relay", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update relay")
		return
	}

	writeJSON(w, http.StatusOK, relay)
}

// UpdateRelayActions handles PUT /api/v1/relays/{id}/actions.
func (h *Handler) UpdateRelayActions(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	var req models.UpdateRelayActionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}

	// Validate action configs
	for _, a := range req.Actions {
		if err := actions.ValidateConfig(a.ActionType, a.Config); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
	}

	// DAG cycle check
	if err := validateDAG(req.Actions, req.Edges); err != nil {
		writeError(w, http.StatusBadRequest, "CYCLE_DETECTED", err.Error())
		return
	}

	relay, err := h.relays.UpdateRelayActions(r.Context(), id, userID, req)
	if err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("update relay actions", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update relay actions")
		return
	}

	writeJSON(w, http.StatusOK, relay)
}

// DeleteRelay handles DELETE /api/v1/relays/{id}.
func (h *Handler) DeleteRelay(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.relays.DeleteRelay(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("delete relay", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete relay")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TriggerRelay handles POST /api/v1/relays/{id}/trigger.
// Publishes a manual execution event to NATS JetStream.
func (h *Handler) TriggerRelay(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	// Verify relay exists and belongs to this user
	if _, err := h.relays.GetRelay(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("trigger: get relay", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to verify relay")
		return
	}

	eventID := uuid.New().String()
	event := queue.ExecutionEvent{
		RelayID:    id,
		EventID:    eventID,
		Payload:    nil,
		ReceivedAt: time.Now().UTC(),
	}

	if err := h.publisher.Publish(r.Context(), event); err != nil {
		h.log.Error("trigger: publish", "relay_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to trigger relay")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"accepted": "true",
		"event_id": eventID,
	})
}

// ─── Execution handlers ───────────────────────────────────────────────────────

// GetExecutions handles GET /api/v1/relays/{id}/executions.
func (h *Handler) GetExecutions(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	relayID := chi.URLParam(r, "id")

	execs, err := h.relays.GetExecutions(r.Context(), relayID, userID)
	if err != nil {
		if errors.Is(err, store.ErrRelayNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "relay not found")
			return
		}
		h.log.Error("get executions", "relay_id", relayID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch executions")
		return
	}

	if execs == nil {
		execs = []models.Execution{}
	}
	writeJSON(w, http.StatusOK, execs)
}

// GetExecution handles GET /api/v1/executions/{id}.
func (h *Handler) GetExecution(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	exec, err := h.relays.GetExecution(r.Context(), id, userID)
	if err != nil {
		if errors.Is(err, store.ErrExecutionNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "execution not found")
			return
		}
		h.log.Error("get execution", "execution_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch execution")
		return
	}

	writeJSON(w, http.StatusOK, exec)
}

// GetExecutionSteps handles GET /api/v1/executions/{id}/steps.
func (h *Handler) GetExecutionSteps(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	steps, err := h.relays.GetExecutionSteps(r.Context(), id, userID)
	if err != nil {
		if errors.Is(err, store.ErrExecutionNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "execution not found")
			return
		}
		h.log.Error("get execution steps", "execution_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch steps")
		return
	}

	if steps == nil {
		steps = []models.ExecutionStep{}
	}
	writeJSON(w, http.StatusOK, steps)
}

// DeleteExecution handles DELETE /api/v1/executions/{id}.
func (h *Handler) DeleteExecution(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.relays.DeleteExecution(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrExecutionNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "execution not found")
			return
		}
		h.log.Error("delete execution", "execution_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete execution")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── Secret handlers ──────────────────────────────────────────────────────────

// ListSecrets handles GET /api/v1/secrets.
func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	secrets, err := h.secrets.ListSecrets(r.Context(), userID)
	if err != nil {
		h.log.Error("list secrets", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list secrets")
		return
	}

	if secrets == nil {
		secrets = []models.Secret{}
	}
	writeJSON(w, http.StatusOK, secrets)
}

// CreateSecret handles POST /api/v1/secrets.
func (h *Handler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	var req models.CreateSecretRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "name and value are required")
		return
	}

	encrypted, err := h.enc.Encrypt(req.Value)
	if err != nil {
		h.log.Error("encrypt secret", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to encrypt secret")
		return
	}

	secret, err := h.secrets.CreateSecret(r.Context(), userID, req.Name, encrypted)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateSecret) {
			writeError(w, http.StatusConflict, "DUPLICATE_SECRET", "a secret with that name already exists")
			return
		}
		h.log.Error("create secret", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create secret")
		return
	}

	writeJSON(w, http.StatusCreated, secret)
}

// DeleteSecret handles DELETE /api/v1/secrets/{id}.
func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.secrets.DeleteSecret(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrSecretNotFound) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "secret not found")
			return
		}
		h.log.Error("delete secret", "secret_id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete secret")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── DAG validation ───────────────────────────────────────────────────────────

// validateDAG runs cycle detection via the dag package before any DB write.
func validateDAG(actionInputs []models.CreateRelayActionInput, edgeInputs []models.CreateRelayEdgeInput) error {
	nodes := make([]dag.Node, len(actionInputs))
	for i, a := range actionInputs {
		nodes[i] = dag.Node{ID: a.NodeID}
	}

	edges := make([]dag.Edge, len(edgeInputs))
	for i, e := range edgeInputs {
		edges[i] = dag.Edge{
			From:      e.ParentNodeID,
			To:        e.ChildNodeID,
			Condition: e.Condition,
		}
	}

	_, err := dag.New(nodes, edges)
	return err
}

// ─── Cron helpers ─────────────────────────────────────────────────────────────

// computeNextRun returns the ISO timestamp string for next_run_at when
// trigger_type is "cron", or nil otherwise.
func computeNextRun(triggerType string, triggerConfig map[string]any) (*string, error) {
	if triggerType != "cron" {
		return nil, nil
	}

	expr, _ := triggerConfig["cron"].(string)
	if expr == "" {
		return nil, errors.New("trigger_config.cron is required for cron trigger type")
	}

	next, err := cronutil.NextRun(expr, time.Now())
	if err != nil {
		return nil, errors.New("invalid cron expression: " + err.Error())
	}

	ts := next.UTC().Format(time.RFC3339)
	return &ts, nil
}
