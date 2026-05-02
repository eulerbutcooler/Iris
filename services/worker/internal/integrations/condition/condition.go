package condition

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
)

// Executor implements engine.ActionExecutor for the "condition" action type.
// It evaluates a simple boolean expression against the resolved config and
// step outputs. Outbound edges with a matching condition will be followed.
//
// Supported expr examples:
//
//	"true"
//	"false"
//	steps['fetch'].output.status == 200   (evaluated via simple string comparison)
//
// Note: For safety we do NOT use eval/reflect magic — we support a limited
// set of expressions. Complex logic should use http_request to call an external
// service. The result {"result": true/false} is stored for conditional edge routing.
type Executor struct{}

func New() *Executor { return &Executor{} }

func (e *Executor) Execute(
	ctx context.Context,
	config map[string]any,
	payload []byte,
	prevOutputs map[string]engine.StepOutput,
) (json.RawMessage, error) {
	expr, _ := config["expr"].(string)
	if expr == "" {
		return nil, fmt.Errorf("condition: expr is required")
	}

	result, err := evalExpr(strings.TrimSpace(expr), prevOutputs)
	if err != nil {
		return nil, fmt.Errorf("condition: %w", err)
	}

	out, _ := json.Marshal(map[string]any{"result": result, "expr": expr})
	return json.RawMessage(out), nil
}

// evalExpr evaluates a simple boolean expression.
// Supported forms:
//   - "true" / "false" literals
//   - "<value> == <value>" or "<value> != <value>"
//   - Values can be literals (numbers, quoted strings) or step output references
func evalExpr(expr string, outputs map[string]engine.StepOutput) (bool, error) {
	// Literal booleans
	switch strings.ToLower(expr) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}

	// Try == operator
	if parts := splitOp(expr, "=="); len(parts) == 2 {
		left := resolveValue(strings.TrimSpace(parts[0]), outputs)
		right := strings.TrimSpace(parts[1])
		return fmt.Sprintf("%v", left) == unquote(right), nil
	}

	// Try != operator
	if parts := splitOp(expr, "!="); len(parts) == 2 {
		left := resolveValue(strings.TrimSpace(parts[0]), outputs)
		right := strings.TrimSpace(parts[1])
		return fmt.Sprintf("%v", left) != unquote(right), nil
	}

	return false, fmt.Errorf("unsupported expression format: %q (supported: true/false, value == value, value != value)", expr)
}

// resolveValue resolves a step output reference like steps['node'].output.field
// or returns the literal value as-is.
func resolveValue(s string, outputs map[string]engine.StepOutput) any {
	if !strings.HasPrefix(s, "steps[") {
		return s
	}

	start := strings.Index(s, "'")
	end := strings.LastIndex(s, "'")
	if start == -1 || end == start {
		return s
	}
	nodeID := s[start+1 : end]
	rest := s[end+2:] // skip ']'

	step, ok := outputs[nodeID]
	if !ok {
		return nil
	}

	if rest == ".error" {
		return step.Error
	}
	if rest == ".output" {
		return step.Output
	}
	if strings.HasPrefix(rest, ".output.") {
		field := strings.TrimPrefix(rest, ".output.")
		return deepGet(step.Output, strings.Split(field, "."))
	}
	return nil
}

func deepGet(m map[string]any, path []string) any {
	if m == nil || len(path) == 0 {
		return nil
	}
	val, ok := m[path[0]]
	if !ok || len(path) == 1 {
		return val
	}
	if nested, ok := val.(map[string]any); ok {
		return deepGet(nested, path[1:])
	}
	return nil
}

func splitOp(expr, op string) []string {
	idx := strings.Index(expr, op)
	if idx == -1 {
		return nil
	}
	return []string{expr[:idx], expr[idx+len(op):]}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
