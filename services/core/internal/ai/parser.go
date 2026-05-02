package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eulerbutcooler/iris/services/core/internal/models"
)

// ParsedResponse is the decoded LLM output.
type ParsedResponse struct {
	Ready     bool                       `json:"ready"`
	Questions []string                   `json:"questions"`
	Message   string                     `json:"message"`
	Relay     *models.CreateRelayRequest `json:"relay"`
}

// ParseResponse extracts a ParsedResponse from the raw LLM text.
// The LLM is instructed to return a bare JSON object; this function
// strips any markdown code fences before unmarshalling.
func ParseResponse(raw string) (*ParsedResponse, error) {
	cleaned := stripCodeFences(raw)

	var pr ParsedResponse
	if err := json.Unmarshal([]byte(cleaned), &pr); err != nil {
		return nil, fmt.Errorf("ai: parse LLM response: %w (raw: %.200s)", err, cleaned)
	}

	// Sanity check
	if pr.Ready && pr.Relay == nil {
		return nil, fmt.Errorf("ai: LLM set ready=true but provided no relay definition")
	}

	return &pr, nil
}

// stripCodeFences removes ```json ... ``` or ``` ... ``` wrappers that
// some models add even when instructed not to.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if after, ok := strings.CutPrefix(s, fence); ok {
			s = after
			s = strings.TrimSuffix(s, "```")
			s = strings.TrimSpace(s)
			break
		}
	}
	return s
}
