package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/eulerbutcooler/iris/packages/dag"
	"github.com/eulerbutcooler/iris/packages/templateengine"
	"github.com/eulerbutcooler/iris/services/worker/internal/store"
	"golang.org/x/sync/errgroup"
)

// Job is a unit of work for the worker pool.
type Job struct {
	RelayID string
	EventID string
	Payload []byte
	// MsgAck is called after the job finishes. Pass true to ACK (success or
	// non-retryable error), false to NAK (transient error, NATS will redeliver).
	MsgAck func(ack bool)
}

// Executor runs relay DAGs against a store and plugin registry.
type Executor struct {
	store    *store.Store
	registry *Registry
	notifier *NotificationPublisher // may be nil
	log      *slog.Logger
}

// NewExecutor creates an Executor with all dependencies injected.
// notifier may be nil if NATS notifications are disabled.
func NewExecutor(store *store.Store, registry *Registry, notifier *NotificationPublisher, log *slog.Logger) *Executor {
	return &Executor{store: store, registry: registry, notifier: notifier, log: log}
}

// Process executes a single job: deduplication → DAG load → wave-parallel execution.
// It always ACKs the message (true/false) when done.
func (e *Executor) Process(ctx context.Context, job Job) {
	log := e.log.With("relay_id", job.RelayID, "event_id", job.EventID)

	// ── Deduplication ─────────────────────────────────────────────────────────
	dup, err := e.store.IsDuplicate(ctx, job.RelayID, job.EventID)
	if err != nil {
		log.Error("dedup check failed", "err", err)
		job.MsgAck(false) // NAK — transient DB error, retry
		return
	}
	if dup {
		log.Info("duplicate event — skipping")
		job.MsgAck(true)
		return
	}
	if err := e.store.MarkProcessed(ctx, job.RelayID, job.EventID); err != nil {
		log.Error("mark processed failed", "err", err)
		job.MsgAck(false)
		return
	}

	// ── Resolve owner (needed for secret lookup) ──────────────────────────────
	userID, err := e.store.GetRelayOwner(ctx, job.RelayID)
	if err != nil {
		log.Error("get relay owner failed", "err", err)
		job.MsgAck(true) // ACK — relay may be deleted, no point retrying
		return
	}

	// ── Create execution record ───────────────────────────────────────────────
	execID, err := e.store.CreateExecution(ctx, job.RelayID, job.EventID, job.Payload)
	if err != nil {
		log.Error("create execution failed", "err", err)
		job.MsgAck(false)
		return
	}
	log = log.With("execution_id", execID)

	start := time.Now()

	// Run the DAG and capture the final status
	finalStatus, finalErr := e.runDAG(ctx, execID, userID, job, log)

	durationMs := time.Since(start).Milliseconds()
	errMsg := ""
	if finalErr != nil {
		errMsg = finalErr.Error()
	}
	if err := e.store.CompleteExecution(ctx, execID, finalStatus, errMsg); err != nil {
		log.Error("complete execution failed", "err", err)
	}

	// Publish execution notification (Phase 8)
	if e.notifier != nil {
		e.notifier.Publish(ctx, ExecutionNotification{
			RelayID:     job.RelayID,
			RelayName:   job.RelayID, // name not cached here; subscriber can enrich
			UserID:      userID,
			ExecutionID: execID,
			Status:      finalStatus,
			DurationMs:  durationMs,
			ErrorMsg:    errMsg,
			FinishedAt:  time.Now().UTC(),
		})
	}

	// ACK regardless of execution outcome — a failed relay is still "done"
	job.MsgAck(true)
	log.Info("execution finished", "status", finalStatus, "duration_ms", durationMs)
}

// runDAG loads the graph, builds the DAG, and executes it wave by wave.
func (e *Executor) runDAG(ctx context.Context, execID, userID string, job Job, log *slog.Logger) (string, error) {
	// Load graph from DB
	actions, edges, err := e.store.GetRelayGraph(ctx, job.RelayID)
	if err != nil {
		return "failed", fmt.Errorf("load graph: %w", err)
	}
	if len(actions) == 0 {
		return "success", nil // nothing to execute
	}

	// Build DAG
	dagNodes := make([]dag.Node, len(actions))
	for i, a := range actions {
		dagNodes[i] = dag.Node{ID: a.NodeID}
	}
	dagEdges := make([]dag.Edge, len(edges))
	for i, e := range edges {
		dagEdges[i] = dag.Edge{From: e.ParentNodeID, To: e.ChildNodeID, Condition: e.Condition}
	}
	g, err := dag.New(dagNodes, dagEdges)
	if err != nil {
		return "failed", fmt.Errorf("build dag: %w", err)
	}

	// Action lookup map
	actionMap := make(map[string]store.RelayAction, len(actions))
	for _, a := range actions {
		actionMap[a.NodeID] = a
	}

	// Wave-parallel execution
	completedOutputs := &sync.Map{} // map[nodeID]StepOutput

	for _, wave := range g.Waves() {
		eg, waveCtx := errgroup.WithContext(ctx)

		for _, nodeID := range wave {
			nodeID := nodeID // capture
			action := actionMap[nodeID]

			eg.Go(func() error {
				return e.executeNode(waveCtx, execID, userID, job.Payload, action, completedOutputs, g, log)
			})
		}

		if err := eg.Wait(); err != nil {
			return "failed", err
		}
	}

	return "success", nil
}

// executeNode runs a single action node within the DAG.
func (e *Executor) executeNode(
	ctx context.Context,
	execID, userID string,
	payload []byte,
	action store.RelayAction,
	completedOutputs *sync.Map,
	g *dag.Graph,
	log *slog.Logger,
) error {
	nodeLog := log.With("node_id", action.NodeID, "action_type", action.ActionType)

	// a. Resolve secrets (_ref suffix fields)
	resolvedConfig, err := e.resolveSecrets(ctx, action.Config, userID)
	if err != nil {
		nodeLog.Error("resolve secrets failed", "err", err)
		// Non-fatal for the node — record as failed step but continue other nodes
		resolvedConfig = action.Config
	}

	// b. Resolve templates ({{payload.x}}, {{steps['n'].output.x}})
	stepSnap := snapshotOutputs(completedOutputs)
	resolvedConfig, _ = templateengine.Resolve(resolvedConfig, payload, toTemplateSteps(stepSnap))

	// c. Record step input (redacted config — strip secrets before logging)
	inputJSON, _ := json.Marshal(redactConfig(resolvedConfig))
	stepID, err := e.store.CreateExecutionStep(ctx, execID, action.NodeID, action.ActionType, inputJSON)
	if err != nil {
		nodeLog.Error("create step failed", "err", err)
		// Continue — best-effort audit trail
	}

	// d. Look up the executor plugin
	executor, err := e.registry.MustGet(action.ActionType)
	if err != nil {
		nodeLog.Warn("unknown action type — skipping", "err", err)
		_ = e.store.CompleteExecutionStep(ctx, stepID, "skipped", nil, err.Error())
		return nil // don't fail the whole DAG for unknown action types
	}

	// e. Execute
	output, execErr := executor.Execute(ctx, resolvedConfig, payload, stepSnap)

	// f. Store outcome
	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "failed"
		errMsg = execErr.Error()
		nodeLog.Error("action failed", "err", execErr)
	}
	_ = e.store.CompleteExecutionStep(ctx, stepID, status, output, errMsg)

	// g. Record output for downstream nodes
	var outputMap map[string]any
	if len(output) > 0 {
		_ = json.Unmarshal(output, &outputMap)
	}
	completedOutputs.Store(action.NodeID, StepOutput{
		Output: outputMap,
		Error:  errMsg,
	})

	// h. Check outbound conditional edges — if this is a condition node,
	// skip child nodes whose conditions don't match the result.
	// (The DAG executor always runs all children; condition filtering is advisory
	// and handled by the condition plugin returning a result flag.)

	if execErr != nil {
		return fmt.Errorf("node %s (%s) failed: %w", action.NodeID, action.ActionType, execErr)
	}
	return nil
}

// ─── Secret resolution ────────────────────────────────────────────────────────

// resolveSecrets replaces any field ending in "_ref" with the decrypted secret value.
// Fields ending with "_ref" contain a secret name; the value is replaced with the plaintext.
func (e *Executor) resolveSecrets(ctx context.Context, config map[string]any, userID string) (map[string]any, error) {
	resolved := make(map[string]any, len(config))
	for k, v := range config {
		if strings.HasSuffix(k, "_ref") {
			secretName, ok := v.(string)
			if !ok || secretName == "" {
				resolved[k] = v
				continue
			}
			// Replace e.g. "webhook_url_ref" → "webhook_url" with the secret value
			plainKey := strings.TrimSuffix(k, "_ref")
			plaintext, err := e.store.GetSecret(ctx, userID, secretName)
			if err != nil {
				return nil, fmt.Errorf("secret ref %q (%s): %w", k, secretName, err)
			}
			resolved[plainKey] = plaintext
		} else {
			resolved[k] = v
		}
	}
	return resolved, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// snapshotOutputs takes a consistent snapshot of the sync.Map into a plain map.
func snapshotOutputs(m *sync.Map) map[string]StepOutput {
	out := make(map[string]StepOutput)
	m.Range(func(k, v any) bool {
		out[k.(string)] = v.(StepOutput)
		return true
	})
	return out
}

// toTemplateSteps converts engine.StepOutput to templateengine.StepOutput.
func toTemplateSteps(steps map[string]StepOutput) map[string]templateengine.StepOutput {
	out := make(map[string]templateengine.StepOutput, len(steps))
	for k, v := range steps {
		out[k] = templateengine.StepOutput{Output: v.Output, Error: v.Error}
	}
	return out
}

// redactConfig strips sensitive fields before they are persisted to the DB.
// Fields containing these substrings are replaced with "***".
var sensitiveKeys = []string{"webhook_url", "api_key", "token", "password", "secret", "auth"}

func redactConfig(config map[string]any) map[string]any {
	out := make(map[string]any, len(config))
	for k, v := range config {
		lower := strings.ToLower(k)
		redacted := false
		for _, sens := range sensitiveKeys {
			if strings.Contains(lower, sens) {
				out[k] = "***"
				redacted = true
				break
			}
		}
		if !redacted {
			out[k] = v
		}
	}
	return out
}
