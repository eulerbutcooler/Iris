package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/eulerbutcooler/iris/packages/encryptor"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RelayAction is a single action node loaded from the DB.
type RelayAction struct {
	ID         string
	RelayID    string
	NodeID     string
	ActionType string
	Config     map[string]any
}

// RelayEdge is a directed edge between two action nodes.
type RelayEdge struct {
	ParentNodeID string
	ChildNodeID  string
	Condition    map[string]any
}

// Relay is the minimal relay record needed by the worker.
type Relay struct {
	ID            string
	UserID        string
	TriggerType   string
	TriggerConfig map[string]any
	NextRunAt     *time.Time
}

// Store handles all worker-side database operations.
type Store struct {
	pool *pgxpool.Pool
	enc  *encryptor.Encryptor
}

// New creates a Store with the given pool and encryptor.
func New(pool *pgxpool.Pool, enc *encryptor.Encryptor) *Store {
	return &Store{pool: pool, enc: enc}
}

// Connect creates and validates a pgxpool connection.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse config: %w", err)
	}
	cfg.MaxConns = 10

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping failed: %w", err)
	}
	return pool, nil
}

// ─── Relay graph ──────────────────────────────────────────────────────────────

// GetRelayGraph loads all actions and edges for a relay.
func (s *Store) GetRelayGraph(ctx context.Context, relayID string) ([]RelayAction, []RelayEdge, error) {
	// Actions
	rows, err := s.pool.Query(ctx,
		`SELECT id, relay_id, node_id, action_type, config
		 FROM relay_actions WHERE relay_id = $1 ORDER BY order_index ASC`,
		relayID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("store: load actions: %w", err)
	}
	defer rows.Close()

	var actions []RelayAction
	for rows.Next() {
		var a RelayAction
		var cfgJSON []byte
		if err := rows.Scan(&a.ID, &a.RelayID, &a.NodeID, &a.ActionType, &cfgJSON); err != nil {
			return nil, nil, fmt.Errorf("store: scan action: %w", err)
		}
		if err := json.Unmarshal(cfgJSON, &a.Config); err != nil {
			return nil, nil, fmt.Errorf("store: unmarshal action config: %w", err)
		}
		actions = append(actions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// Edges
	erows, err := s.pool.Query(ctx,
		`SELECT parent_node_id, child_node_id, condition
		 FROM relay_edges WHERE relay_id = $1`,
		relayID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("store: load edges: %w", err)
	}
	defer erows.Close()

	var edges []RelayEdge
	for erows.Next() {
		var e RelayEdge
		var condJSON []byte
		if err := erows.Scan(&e.ParentNodeID, &e.ChildNodeID, &condJSON); err != nil {
			return nil, nil, fmt.Errorf("store: scan edge: %w", err)
		}
		if condJSON != nil {
			_ = json.Unmarshal(condJSON, &e.Condition)
		}
		edges = append(edges, e)
	}
	return actions, edges, erows.Err()
}

// GetRelayOwner returns the userID that owns the relay.
func (s *Store) GetRelayOwner(ctx context.Context, relayID string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM relays WHERE id = $1`, relayID,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("store: relay %s not found", relayID)
		}
		return "", fmt.Errorf("store: get relay owner: %w", err)
	}
	return userID, nil
}

// ─── Secrets ──────────────────────────────────────────────────────────────────

// GetSecret returns the decrypted plaintext value of a secret by name.
func (s *Store) GetSecret(ctx context.Context, userID, name string) (string, error) {
	var encrypted string
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM secrets WHERE user_id = $1 AND name = $2`,
		userID, name,
	).Scan(&encrypted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("store: secret %q not found for user %s", name, userID)
		}
		return "", fmt.Errorf("store: get secret: %w", err)
	}
	plaintext, err := s.enc.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("store: decrypt secret %q: %w", name, err)
	}
	return plaintext, nil
}

// ─── Deduplication ────────────────────────────────────────────────────────────

// IsDuplicate returns true if the (relayID, eventID) pair has already been processed.
func (s *Store) IsDuplicate(ctx context.Context, relayID, eventID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM processed_events WHERE relay_id = $1 AND event_id = $2)`,
		relayID, eventID,
	).Scan(&exists)
	return exists, err
}

// MarkProcessed records a (relayID, eventID) pair as processed.
// Ignores duplicate inserts (idempotent).
func (s *Store) MarkProcessed(ctx context.Context, relayID, eventID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO processed_events (relay_id, event_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		relayID, eventID,
	)
	return err
}

// ─── Executions ───────────────────────────────────────────────────────────────

// CreateExecution inserts a new execution record with status 'running'.
func (s *Store) CreateExecution(ctx context.Context, relayID, eventID string, payload []byte) (string, error) {
	var id string
	var payloadJSON interface{}
	if len(payload) > 0 && json.Valid(payload) {
		payloadJSON = payload
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO executions (relay_id, event_id, status, trigger_payload)
		 VALUES ($1, $2, 'running', $3)
		 RETURNING id`,
		relayID, eventID, payloadJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: create execution: %w", err)
	}
	return id, nil
}

// CompleteExecution updates an execution's final status and finished_at.
func (s *Store) CompleteExecution(ctx context.Context, executionID, status, errorMsg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE executions
		 SET status = $1, error_message = $2, finished_at = NOW()
		 WHERE id = $3`,
		status, errorMsg, executionID,
	)
	return err
}

// CreateExecutionStep inserts a step record with status 'running'.
func (s *Store) CreateExecutionStep(ctx context.Context, executionID, nodeID, actionType string, inputJSON []byte) (string, error) {
	var id string
	var inputVal interface{}
	if len(inputJSON) > 0 && json.Valid(inputJSON) {
		inputVal = inputJSON
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO execution_steps (execution_id, node_id, action_type, status, input)
		 VALUES ($1, $2, $3, 'running', $4)
		 RETURNING id`,
		executionID, nodeID, actionType, inputVal,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: create step: %w", err)
	}
	return id, nil
}

// CompleteExecutionStep updates a step's status, output, error, and finished_at.
func (s *Store) CompleteExecutionStep(ctx context.Context, stepID, status string, output json.RawMessage, errorMsg string) error {
	var outputVal interface{}
	if len(output) > 0 {
		outputVal = []byte(output)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE execution_steps
		 SET status = $1, output = $2, error_message = $3, finished_at = NOW()
		 WHERE id = $4`,
		status, outputVal, errorMsg, stepID,
	)
	return err
}

// ─── Cron ─────────────────────────────────────────────────────────────────────

// GetCronRelays returns all active cron relays whose next_run_at is due.
func (s *Store) GetCronRelays(ctx context.Context) ([]Relay, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, trigger_type, trigger_config, next_run_at
		 FROM relays
		 WHERE trigger_type = 'cron'
		   AND is_active = TRUE
		   AND next_run_at <= NOW()`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get cron relays: %w", err)
	}
	defer rows.Close()

	var relays []Relay
	for rows.Next() {
		var r Relay
		var cfgJSON []byte
		if err := rows.Scan(&r.ID, &r.UserID, &r.TriggerType, &cfgJSON, &r.NextRunAt); err != nil {
			return nil, fmt.Errorf("store: scan cron relay: %w", err)
		}
		if cfgJSON != nil {
			_ = json.Unmarshal(cfgJSON, &r.TriggerConfig)
		}
		relays = append(relays, r)
	}
	return relays, rows.Err()
}

// UpdateRelayNextRun sets the next_run_at and last_run_at for a cron relay.
func (s *Store) UpdateRelayNextRun(ctx context.Context, relayID string, nextRun time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE relays SET next_run_at = $1, last_run_at = NOW() WHERE id = $2`,
		nextRun, relayID,
	)
	return err
}
