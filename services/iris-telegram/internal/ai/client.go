package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/iris"
	openai "github.com/sashabaranov/go-openai"
)

// Message is a single LLM conversation turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMResponse is the decoded LLM output for a relay generation request.
type LLMResponse struct {
	Ready     bool                      `json:"ready"`
	Questions []string                  `json:"questions,omitempty"`
	Relay     *iris.CreateRelayRequest  `json:"relay,omitempty"`
	Message   string                    `json:"message,omitempty"`
}

// Client wraps the LLM SDK.
type Client struct {
	openai *openai.Client
	model  string
}

// NewClient creates an LLM client for the given provider.
func NewClient(provider, apiKey, model string) (*Client, error) {
	switch provider {
	case "openai":
		return &Client{openai: openai.NewClient(apiKey), model: model}, nil
	default:
		return nil, fmt.Errorf("ai: unsupported provider %q", provider)
	}
}

// Chat sends a conversation and returns the raw LLM text.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	oaiMsgs := make([]openai.ChatCompletionMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
	}

	resp, err := c.openai.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    oaiMsgs,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("ai: chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("ai: no choices returned")
	}
	return resp.Choices[0].Message.Content, nil
}

// ParseResponse decodes the LLM JSON response, stripping code fences if present.
func ParseResponse(raw string) (*LLMResponse, error) {
	s := strings.TrimSpace(raw)
	for _, fence := range []string{"```json", "```"} {
		if strings.HasPrefix(s, fence) {
			s = strings.TrimPrefix(s, fence)
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}

	var resp LLMResponse
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		return nil, fmt.Errorf("ai: parse response: %w", err)
	}
	return &resp, nil
}
