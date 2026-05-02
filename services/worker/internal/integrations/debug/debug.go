package debug

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
)

// Executor implements engine.ActionExecutor for the "debug_log" action type.
// It logs the message field and the full trigger payload, then returns {"logged": true}.
type Executor struct {
	log *slog.Logger
}

// New creates a debug_log executor.
func New(log *slog.Logger) *Executor {
	return &Executor{log: log}
}

func (e *Executor) Execute(
	ctx context.Context,
	config map[string]any,
	payload []byte,
	prevOutputs map[string]engine.StepOutput,
) (json.RawMessage, error) {
	msg, _ := config["message"].(string)
	if msg == "" {
		msg = "(no message)"
	}

	e.log.Info("debug_log",
		"message", msg,
		"payload", string(payload),
	)

	result := fmt.Sprintf(`{"logged":true,"message":%s}`, jsonStr(msg))
	return json.RawMessage(result), nil
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
