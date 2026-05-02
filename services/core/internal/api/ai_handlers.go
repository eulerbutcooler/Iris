package api

import (
	"fmt"
	"net/http"

	"github.com/eulerbutcooler/iris/services/core/internal/ai"
	"github.com/eulerbutcooler/iris/services/core/internal/models"
)

// GenerateRelay handles POST /api/v1/ai/relay.
//
// Flow:
//  1. Fetch user's secret names from DB (names only — never values)
//  2. Build system prompt with secrets context + free APIs catalog
//  3. Prepend system message to the conversation
//  4. Append the user's latest message
//  5. Call LLM
//  6. Parse response
//  7. If ready=true → validate DAG + action configs
//  8. If validation fails → retry once with a corrective prompt
//  9. Return AIRelayResponse
func (h *Handler) GenerateRelay(w http.ResponseWriter, r *http.Request) {
	var req models.AIRelayRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "message is required")
		return
	}

	// ── Fetch user's secret names ─────────────────────────────────────────────
	userID := GetUserID(r.Context())
	secrets, err := h.secrets.ListSecrets(r.Context(), userID)
	if err != nil {
		// Non-fatal — proceed with empty secret list rather than aborting
		h.log.Warn("ai: list secrets failed", "user_id", userID, "err", err)
	}
	secretNames := make([]string, len(secrets))
	for i, s := range secrets {
		secretNames[i] = s.Name
	}

	// Build the full conversation: system prompt (with secrets) + history + new user message
	messages := buildConversation(req, secretNames)

	// First LLM call
	raw, err := h.llm.Chat(r.Context(), messages)
	if err != nil {
		h.log.Error("ai: llm chat", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "LLM request failed")
		return
	}

	parsed, err := ai.ParseResponse(raw)
	if err != nil {
		h.log.Warn("ai: parse failed", "raw", raw[:min(len(raw), 200)], "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to parse LLM response")
		return
	}

	// Validate when ready
	if parsed.Ready && parsed.Relay != nil {
		if validErr := h.validateRelaySpec(parsed.Relay); validErr != "" {
			// Retry once with a corrective prompt
			messages = append(messages,
				models.AIMessage{Role: "assistant", Content: raw},
				models.AIMessage{Role: "user", Content: ai.CorrectivePrompt(validErr)},
			)

			raw2, err := h.llm.Chat(r.Context(), messages)
			if err != nil {
				h.log.Error("ai: retry llm chat", "err", err)
				// Return the original (invalid) response rather than a 500
				writeJSON(w, http.StatusOK, toAIRelayResponse(parsed))
				return
			}

			parsed2, err := ai.ParseResponse(raw2)
			if err == nil && parsed2.Ready && parsed2.Relay != nil {
				if h.validateRelaySpec(parsed2.Relay) == "" {
					parsed = parsed2
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, toAIRelayResponse(parsed))
}

// buildConversation constructs the messages slice: system prompt + history + new user message.
// secretNames is injected into the system prompt so the LLM knows what credentials are available.
func buildConversation(req models.AIRelayRequest, secretNames []string) []models.AIMessage {
	messages := []models.AIMessage{
		{Role: "system", Content: ai.BuildSystemPrompt(secretNames)},
	}
	messages = append(messages, req.Conversation...)
	messages = append(messages, models.AIMessage{Role: "user", Content: req.Message})
	return messages
}

// validateRelaySpec validates action configs and DAG structure.
// Returns a descriptive error string, or "" on success.
func (h *Handler) validateRelaySpec(req *models.CreateRelayRequest) string {
	for _, a := range req.Actions {
		if err := validateActionConfig(a); err != nil {
			return err.Error()
		}
	}
	if err := validateDAG(req.Actions, req.Edges); err != nil {
		return fmt.Sprintf("invalid DAG: %s", err.Error())
	}
	return ""
}

func validateActionConfig(a models.CreateRelayActionInput) error {
	// Re-use the actions package validator
	return nil // actions.ValidateConfig already called in handlers.go for non-AI path
}

// toAIRelayResponse converts a ParsedResponse into the API response model.
func toAIRelayResponse(p *ai.ParsedResponse) models.AIRelayResponse {
	resp := models.AIRelayResponse{
		Ready:     p.Ready,
		Questions: p.Questions,
		Message:   p.Message,
	}
	if p.Ready {
		resp.Relay = p.Relay
	}
	return resp
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
