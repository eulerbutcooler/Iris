package engine

import (
	"context"
	"encoding/json"
	"fmt"
)

// StepOutput holds the result of a completed DAG node execution.
type StepOutput struct {
	Output map[string]any
	Error  string
}

// ActionExecutor is the interface every integration plugin must implement.
type ActionExecutor interface {
	// Execute runs the action and returns a JSON-encoded result.
	// config: the action's resolved config (secrets already substituted)
	// payload: the raw trigger payload bytes
	// prevOutputs: outputs of all nodes that completed before this one
	Execute(ctx context.Context, config map[string]any, payload []byte, prevOutputs map[string]StepOutput) (json.RawMessage, error)
}

// Registry maps action type strings to their executor implementations.
type Registry struct {
	executors map[string]ActionExecutor
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]ActionExecutor)}
}

// Register adds an executor for the given action type.
func (r *Registry) Register(actionType string, exec ActionExecutor) {
	r.executors[actionType] = exec
}

// Get returns the executor for actionType, or false if unregistered.
func (r *Registry) Get(actionType string) (ActionExecutor, bool) {
	exec, ok := r.executors[actionType]
	return exec, ok
}

// MustGet returns the executor or returns an error if not registered.
func (r *Registry) MustGet(actionType string) (ActionExecutor, error) {
	exec, ok := r.executors[actionType]
	if !ok {
		return nil, fmt.Errorf("registry: no executor registered for action type %q", actionType)
	}
	return exec, nil
}
