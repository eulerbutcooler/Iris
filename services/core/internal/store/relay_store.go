package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/eulerbutcooler/iris/services/core/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RelayStore handles relay CRUD plus DAG persistence and execution history.
type RelayStore struct {
	pool *pgxpool.Pool
}

// NewRelayStore creates a RelayStore backed by the given pool.
func NewRelayStore(pool *pgxpool.Pool) *RelayStore {
	return &RelayStore{pool: pool}
}

// ─── Relay CRUD ───────────────────────────────────────────────────────────────

// CreateRelay inserts a relay, its action nodes, and its edges atomically.
// next_run_at is set for cron relays using the caller-computed value (may be nil).
func (s *RelayStore) CreateRelay(
	ctx context.Context,
	userID string,
	req models.CreateRelayRequest,
	nextRunAt *string, // ISO timestamp or nil
) (*models.RelayWithActions, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("relay_store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// 1. Insert relay
	trigCfgJSON, err := marshalNullableJSON(req.TriggerConfig)
	if err != nil {
		return nil, err
	}

	var relay models.Relay
	err = tx.QueryRow(ctx,
		`INSERT INTO relays (user_id, name, description, trigger_type, trigger_config, next_run_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, name, description, is_active, trigger_type,
		           trigger_config, next_run_at, last_run_at, created_at, updated_at`,
		userID,
		req.Name,
		req.Description,
		coalesceStr(req.TriggerType, "webhook"),
		trigCfgJSON,
		nextRunAt,
	).Scan(
		&relay.ID, &relay.UserID, &relay.Name, &relay.Description,
		&relay.IsActive, &relay.TriggerType, &relay.TriggerConfig,
		&relay.NextRunAt, &relay.LastRunAt, &relay.CreatedAt, &relay.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: insert relay: %w", err)
	}

	// 2. Insert actions
	actions, err := insertActions(ctx, tx, relay.ID, req.Actions)
	if err != nil {
		return nil, err
	}

	// 3. Insert edges
	edges, err := insertEdges(ctx, tx, relay.ID, req.Edges)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("relay_store: commit: %w", err)
	}

	return &models.RelayWithActions{Relay: relay, Actions: actions, Edges: edges}, nil
}

// GetRelay returns a relay with its full DAG, scoped to the owning user.
func (s *RelayStore) GetRelay(ctx context.Context, id, userID string) (*models.RelayWithActions, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, description, is_active, trigger_type,
		        trigger_config, next_run_at, last_run_at, created_at, updated_at
		 FROM relays WHERE id = $1 AND user_id = $2`,
		id, userID,
	)

	var relay models.Relay
	if err := scanRelay(row, &relay); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRelayNotFound
		}
		return nil, fmt.Errorf("relay_store: get: %w", err)
	}

	actions, err := s.loadActions(ctx, relay.ID)
	if err != nil {
		return nil, err
	}
	edges, err := s.loadEdges(ctx, relay.ID)
	if err != nil {
		return nil, err
	}

	return &models.RelayWithActions{Relay: relay, Actions: actions, Edges: edges}, nil
}

// GetRelayGraph returns raw action + edge rows for a relay (used by the worker,
// no user scoping — relies on the worker knowing the relay ID).
func (s *RelayStore) GetRelayGraph(ctx context.Context, relayID string) ([]models.RelayAction, []models.RelayEdge, error) {
	actions, err := s.loadActions(ctx, relayID)
	if err != nil {
		return nil, nil, err
	}
	edges, err := s.loadEdges(ctx, relayID)
	if err != nil {
		return nil, nil, err
	}
	return actions, edges, nil
}

// GetAllRelays returns all relays for a user (no actions/edges).
func (s *RelayStore) GetAllRelays(ctx context.Context, userID string) ([]models.Relay, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, description, is_active, trigger_type,
		        trigger_config, next_run_at, last_run_at, created_at, updated_at
		 FROM relays WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: list: %w", err)
	}
	defer rows.Close()

	var relays []models.Relay
	for rows.Next() {
		var r models.Relay
		if err := scanRelayRow(rows, &r); err != nil {
			return nil, fmt.Errorf("relay_store: list scan: %w", err)
		}
		relays = append(relays, r)
	}
	return relays, rows.Err()
}

// UpdateRelay updates top-level relay fields (name, description, is_active, trigger).
func (s *RelayStore) UpdateRelay(
	ctx context.Context,
	id, userID string,
	req models.UpdateRelayRequest,
	nextRunAt *string,
) (*models.Relay, error) {
	trigCfgJSON, err := marshalNullableJSON(req.TriggerConfig)
	if err != nil {
		return nil, err
	}

	var relay models.Relay

	// Build dynamic SET clause based on what's provided
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if req.Name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, req.Name)
		argIdx++
	}
	if req.Description != "" {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, req.Description)
		argIdx++
	}
	if req.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argIdx))
		args = append(args, *req.IsActive)
		argIdx++
	}
	if req.TriggerType != "" {
		setClauses = append(setClauses, fmt.Sprintf("trigger_type = $%d", argIdx))
		args = append(args, req.TriggerType)
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("trigger_config = $%d", argIdx))
		args = append(args, trigCfgJSON)
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("next_run_at = $%d", argIdx))
		args = append(args, nextRunAt)
		argIdx++
	}

	// WHERE clause args
	args = append(args, id, userID)

	query := fmt.Sprintf(
		`UPDATE relays SET %s WHERE id = $%d AND user_id = $%d
		 RETURNING id, user_id, name, description, is_active, trigger_type,
		           trigger_config, next_run_at, last_run_at, created_at, updated_at`,
		strings.Join(setClauses, ", "),
		argIdx, argIdx+1,
	)

	if err := scanRelay(s.pool.QueryRow(ctx, query, args...), &relay); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRelayNotFound
		}
		return nil, fmt.Errorf("relay_store: update: %w", err)
	}
	return &relay, nil
}

// UpdateRelayActions replaces all actions and edges for a relay atomically.
func (s *RelayStore) UpdateRelayActions(
	ctx context.Context,
	relayID, userID string,
	req models.UpdateRelayActionsRequest,
) (*models.RelayWithActions, error) {
	// Verify relay ownership first
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM relays WHERE id = $1 AND user_id = $2)`,
		relayID, userID,
	).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("relay_store: check ownership: %w", err)
	}
	if !exists {
		return nil, ErrRelayNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("relay_store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Delete all existing edges then actions (FK order)
	if _, err := tx.Exec(ctx, `DELETE FROM relay_edges WHERE relay_id = $1`, relayID); err != nil {
		return nil, fmt.Errorf("relay_store: delete edges: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM relay_actions WHERE relay_id = $1`, relayID); err != nil {
		return nil, fmt.Errorf("relay_store: delete actions: %w", err)
	}

	// Re-insert
	if _, err := insertActions(ctx, tx, relayID, req.Actions); err != nil {
		return nil, err
	}
	if _, err := insertEdges(ctx, tx, relayID, req.Edges); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("relay_store: commit: %w", err)
	}

	// Reload the full relay
	return s.GetRelay(ctx, relayID, userID)
}

// DeleteRelay removes a relay (cascades to actions, edges, executions).
func (s *RelayStore) DeleteRelay(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM relays WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("relay_store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRelayNotFound
	}
	return nil
}

// ─── Executions ───────────────────────────────────────────────────────────────

// GetExecutions returns all executions for a relay, most recent first.
func (s *RelayStore) GetExecutions(ctx context.Context, relayID, userID string) ([]models.Execution, error) {
	// Ensure relay belongs to user
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM relays WHERE id = $1 AND user_id = $2)`,
		relayID, userID,
	).Scan(&exists); err != nil || !exists {
		return nil, ErrRelayNotFound
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, relay_id, event_id, status, trigger_payload, error_message, started_at, finished_at
		 FROM executions WHERE relay_id = $1 ORDER BY started_at DESC`,
		relayID,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: get executions: %w", err)
	}
	defer rows.Close()

	var execs []models.Execution
	for rows.Next() {
		var e models.Execution
		if err := scanExecution(rows, &e); err != nil {
			return nil, fmt.Errorf("relay_store: scan execution: %w", err)
		}
		execs = append(execs, e)
	}
	return execs, rows.Err()
}

// GetExecution returns a single execution by ID, scoped to the relay owner.
func (s *RelayStore) GetExecution(ctx context.Context, id, userID string) (*models.Execution, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT e.id, e.relay_id, e.event_id, e.status, e.trigger_payload,
		        e.error_message, e.started_at, e.finished_at
		 FROM executions e
		 JOIN relays r ON r.id = e.relay_id
		 WHERE e.id = $1 AND r.user_id = $2`,
		id, userID,
	)
	var e models.Execution
	if err := scanExecution(row, &e); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExecutionNotFound
		}
		return nil, fmt.Errorf("relay_store: get execution: %w", err)
	}
	return &e, nil
}

// GetExecutionSteps returns all steps for an execution, scoped to relay owner.
func (s *RelayStore) GetExecutionSteps(ctx context.Context, executionID, userID string) ([]models.ExecutionStep, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT es.id, es.execution_id, es.node_id, es.action_type,
		        es.status, es.input, es.output, es.error_message, es.started_at, es.finished_at
		 FROM execution_steps es
		 JOIN executions e ON e.id = es.execution_id
		 JOIN relays r     ON r.id = e.relay_id
		 WHERE es.execution_id = $1 AND r.user_id = $2
		 ORDER BY es.started_at ASC`,
		executionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: get steps: %w", err)
	}
	defer rows.Close()

	var steps []models.ExecutionStep
	for rows.Next() {
		var st models.ExecutionStep
		if err := scanStep(rows, &st); err != nil {
			return nil, fmt.Errorf("relay_store: scan step: %w", err)
		}
		steps = append(steps, st)
	}
	return steps, rows.Err()
}

// DeleteExecution removes a single execution record (cascades to steps).
func (s *RelayStore) DeleteExecution(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM executions e
		 USING relays r
		 WHERE e.id = $1 AND e.relay_id = r.id AND r.user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("relay_store: delete execution: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExecutionNotFound
	}
	return nil
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

type txQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (interface{ RowsAffected() int64 }, error)
}

func insertActions(ctx context.Context, tx pgx.Tx, relayID string, inputs []models.CreateRelayActionInput) ([]models.RelayAction, error) {
	actions := make([]models.RelayAction, 0, len(inputs))
	for i, inp := range inputs {
		cfgJSON, err := json.Marshal(inp.Config)
		if err != nil {
			return nil, fmt.Errorf("relay_store: marshal action config: %w", err)
		}

		orderIdx := inp.OrderIndex
		if orderIdx == 0 {
			orderIdx = i
		}

		var a models.RelayAction
		if err := tx.QueryRow(ctx,
			`INSERT INTO relay_actions (relay_id, node_id, action_type, config, order_index)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, relay_id, node_id, action_type, config, order_index`,
			relayID, inp.NodeID, inp.ActionType, cfgJSON, orderIdx,
		).Scan(&a.ID, &a.RelayID, &a.NodeID, &a.ActionType, &a.Config, &a.OrderIndex); err != nil {
			return nil, fmt.Errorf("relay_store: insert action %q: %w", inp.NodeID, err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}

func insertEdges(ctx context.Context, tx pgx.Tx, relayID string, inputs []models.CreateRelayEdgeInput) ([]models.RelayEdge, error) {
	edges := make([]models.RelayEdge, 0, len(inputs))
	for _, inp := range inputs {
		condJSON, err := marshalNullableJSON(inp.Condition)
		if err != nil {
			return nil, err
		}

		var e models.RelayEdge
		if err := tx.QueryRow(ctx,
			`INSERT INTO relay_edges (relay_id, parent_node_id, child_node_id, condition)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, relay_id, parent_node_id, child_node_id, condition`,
			relayID, inp.ParentNodeID, inp.ChildNodeID, condJSON,
		).Scan(&e.ID, &e.RelayID, &e.ParentNodeID, &e.ChildNodeID, &e.Condition); err != nil {
			return nil, fmt.Errorf("relay_store: insert edge %s→%s: %w", inp.ParentNodeID, inp.ChildNodeID, err)
		}
		edges = append(edges, e)
	}
	return edges, nil
}

func (s *RelayStore) loadActions(ctx context.Context, relayID string) ([]models.RelayAction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, relay_id, node_id, action_type, config, order_index
		 FROM relay_actions WHERE relay_id = $1 ORDER BY order_index ASC`,
		relayID,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: load actions: %w", err)
	}
	defer rows.Close()

	var actions []models.RelayAction
	for rows.Next() {
		var a models.RelayAction
		if err := rows.Scan(&a.ID, &a.RelayID, &a.NodeID, &a.ActionType, &a.Config, &a.OrderIndex); err != nil {
			return nil, fmt.Errorf("relay_store: scan action: %w", err)
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

func (s *RelayStore) loadEdges(ctx context.Context, relayID string) ([]models.RelayEdge, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, relay_id, parent_node_id, child_node_id, condition
		 FROM relay_edges WHERE relay_id = $1`,
		relayID,
	)
	if err != nil {
		return nil, fmt.Errorf("relay_store: load edges: %w", err)
	}
	defer rows.Close()

	var edges []models.RelayEdge
	for rows.Next() {
		var e models.RelayEdge
		if err := rows.Scan(&e.ID, &e.RelayID, &e.ParentNodeID, &e.ChildNodeID, &e.Condition); err != nil {
			return nil, fmt.Errorf("relay_store: scan edge: %w", err)
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanRelay(row scanner, r *models.Relay) error {
	return row.Scan(
		&r.ID, &r.UserID, &r.Name, &r.Description,
		&r.IsActive, &r.TriggerType, &r.TriggerConfig,
		&r.NextRunAt, &r.LastRunAt, &r.CreatedAt, &r.UpdatedAt,
	)
}

func scanRelayRow(row scanner, r *models.Relay) error {
	return scanRelay(row, r)
}

func scanExecution(row scanner, e *models.Execution) error {
	return row.Scan(
		&e.ID, &e.RelayID, &e.EventID, &e.Status,
		&e.TriggerPayload, &e.ErrorMessage, &e.StartedAt, &e.FinishedAt,
	)
}

func scanStep(row scanner, s *models.ExecutionStep) error {
	return row.Scan(
		&s.ID, &s.ExecutionID, &s.NodeID, &s.ActionType,
		&s.Status, &s.Input, &s.Output, &s.ErrorMessage, &s.StartedAt, &s.FinishedAt,
	)
}

// ─── Misc helpers ─────────────────────────────────────────────────────────────

func marshalNullableJSON(v map[string]any) ([]byte, error) {
	if len(v) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("relay_store: marshal json: %w", err)
	}
	return b, nil
}

func coalesceStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// ─── Sentinel errors ──────────────────────────────────────────────────────────

// ErrRelayNotFound is returned when a relay cannot be found or doesn't belong to the user.
var ErrRelayNotFound = errors.New("store: relay not found")

// ErrExecutionNotFound is returned when an execution cannot be found.
var ErrExecutionNotFound = errors.New("store: execution not found")
