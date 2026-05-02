package httpreq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Executor implements engine.ActionExecutor for the "http_request" action type.
type Executor struct{}

// New creates an http_request executor.
func New() *Executor { return &Executor{} }

func (e *Executor) Execute(
	ctx context.Context,
	config map[string]any,
	payload []byte,
	prevOutputs map[string]engine.StepOutput,
) (json.RawMessage, error) {
	url, _ := config["url"].(string)
	method, _ := config["method"].(string)
	body, _ := config["body"].(string)

	if url == "" {
		return nil, fmt.Errorf("http_request: url is required")
	}
	if method == "" {
		method = "GET"
	}
	method = strings.ToUpper(method)

	// Build request
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http_request: build request: %w", err)
	}

	// Apply headers
	if headers, ok := config["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	// Execute
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_request: execute: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return nil, fmt.Errorf("http_request: read response: %w", err)
	}

	// Collect response headers
	respHeaders := make(map[string]string, len(resp.Header))
	for k, vv := range resp.Header {
		respHeaders[k] = strings.Join(vv, ", ")
	}

	result := map[string]any{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
		"headers":     respHeaders,
	}
	out, _ := json.Marshal(result)
	return json.RawMessage(out), nil
}
