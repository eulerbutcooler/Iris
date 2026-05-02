package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Executor implements engine.ActionExecutor for "discord_send".
type Executor struct{}

func New() *Executor { return &Executor{} }

func (e *Executor) Execute(
	ctx context.Context,
	config map[string]any,
	payload []byte,
	prevOutputs map[string]engine.StepOutput,
) (json.RawMessage, error) {
	// webhook_url is resolved from the secret ref by the executor before this runs
	webhookURL, _ := config["webhook_url"].(string)
	message, _ := config["message"].(string)

	if webhookURL == "" {
		return nil, fmt.Errorf("discord_send: webhook_url is required")
	}
	if message == "" {
		return nil, fmt.Errorf("discord_send: message is required")
	}

	body, _ := json.Marshal(map[string]string{"content": message})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("discord_send: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord_send: send: %w", err)
	}
	defer resp.Body.Close()

	// Discord returns 204 No Content on success
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord_send: unexpected status %d", resp.StatusCode)
	}

	out, _ := json.Marshal(map[string]any{"sent": true, "status_code": resp.StatusCode})
	return json.RawMessage(out), nil
}
