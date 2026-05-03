package iris

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Relay is a minimal relay record returned by iris-core.
type Relay struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	TriggerType string `json:"trigger_type"`
	CreatedAt   string `json:"created_at"`
}

// CreateRelayRequest is the payload for creating a new relay.
type CreateRelayRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	TriggerType   string            `json:"trigger_type"`
	TriggerConfig map[string]any    `json:"trigger_config,omitempty"`
	Actions       []ActionInput     `json:"actions"`
	Edges         []EdgeInput       `json:"edges"`
}

// ActionInput is a single action node in the relay graph.
type ActionInput struct {
	NodeID     string         `json:"node_id"`
	ActionType string         `json:"action_type"`
	Config     map[string]any `json:"config"`
	OrderIndex int            `json:"order_index,omitempty"`
}

// EdgeInput is a directed edge between two action nodes.
type EdgeInput struct {
	ParentNodeID string `json:"parent_node_id"`
	ChildNodeID  string `json:"child_node_id"`
}

// Execution is a single relay run record.
type Execution struct {
	ID         string  `json:"id"`
	RelayID    string  `json:"relay_id"`
	Status     string  `json:"status"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

// AIMessage is a single turn in the LLM conversation.
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIRelayResponse is the decoded response from POST /api/v1/ai/relay.
type AIRelayResponse struct {
	Ready     bool                 `json:"ready"`
	Questions []string             `json:"questions,omitempty"`
	Message   string               `json:"message,omitempty"`
	Relay     *CreateRelayRequest  `json:"relay,omitempty"`
	RelayID   string               `json:"relay_id,omitempty"`
}

// Client is an HTTP client for the iris-core REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Client targeting the given iris-core base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ValidateToken verifies the JWT token by calling GET /api/v1/relays.
// Returns true if the token is accepted (200), false otherwise.
func (c *Client) ValidateToken(ctx context.Context, token string) (bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/relays", token, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// ListRelays returns all relays for the authenticated user.
func (c *Client) ListRelays(ctx context.Context, token string) ([]Relay, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/relays", token, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iris: list relays: status %d", resp.StatusCode)
	}

	var relays []Relay
	return relays, json.NewDecoder(resp.Body).Decode(&relays)
}

// CreateRelay creates a new relay and returns it.
func (c *Client) CreateRelay(ctx context.Context, token string, req CreateRelayRequest) (*Relay, error) {
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/relays", token, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iris: create relay: status %d: %s", resp.StatusCode, body)
	}

	var relay Relay
	return &relay, json.NewDecoder(resp.Body).Decode(&relay)
}

// UpdateRelay replaces an existing relay's full definition via PUT /api/v1/relays/:id.
func (c *Client) UpdateRelay(ctx context.Context, token, relayID string, req CreateRelayRequest) (*Relay, error) {
	resp, err := c.do(ctx, http.MethodPut, "/api/v1/relays/"+relayID, token, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iris: update relay: status %d: %s", resp.StatusCode, body)
	}

	var relay Relay
	return &relay, json.NewDecoder(resp.Body).Decode(&relay)
}

// TriggerRelay manually triggers a relay execution.
func (c *Client) TriggerRelay(ctx context.Context, token, relayID string) error {
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/relays/"+relayID+"/trigger", token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("iris: trigger relay: status %d", resp.StatusCode)
	}
	return nil
}

// GetExecutions returns recent executions for a relay.
func (c *Client) GetExecutions(ctx context.Context, token, relayID string) ([]Execution, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/relays/"+relayID+"/executions", token, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iris: get executions: status %d", resp.StatusCode)
	}

	var execs []Execution
	return execs, json.NewDecoder(resp.Body).Decode(&execs)
}

// DeleteRelay deletes a relay by ID.
func (c *Client) DeleteRelay(ctx context.Context, token, relayID string) error {
	resp, err := c.do(ctx, http.MethodDelete, "/api/v1/relays/"+relayID, token, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("iris: delete relay: status %d", resp.StatusCode)
	}
	return nil
}

// GenerateRelay calls POST /api/v1/ai/relay on iris-core.
// message is the latest user text; conversation is the prior history.
// relayID is optional — set it when the user wants to edit an existing relay.
func (c *Client) GenerateRelay(ctx context.Context, token, message string, conversation []AIMessage, relayID string) (*AIRelayResponse, error) {
	body := map[string]any{
		"message":      message,
		"conversation": conversation,
	}
	if relayID != "" {
		body["relay_id"] = relayID
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/ai/relay", token, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iris: generate relay: status %d: %s", resp.StatusCode, b)
	}
	var result AIRelayResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// SetRelayActive sets the is_active state of a relay without modifying any other fields.
func (c *Client) SetRelayActive(ctx context.Context, token, relayID string, active bool) (*Relay, error) {
	body := map[string]any{"is_active": active}
	resp, err := c.do(ctx, http.MethodPut, "/api/v1/relays/"+relayID, token, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iris: set relay active: status %d: %s", resp.StatusCode, b)
	}
	var relay Relay
	return &relay, json.NewDecoder(resp.Body).Decode(&relay)
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path, token string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("iris client: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("iris client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iris client: %s %s: %w", method, path, err)
	}
	return resp, nil
}
