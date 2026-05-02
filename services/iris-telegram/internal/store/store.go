package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TelegramLink represents a Telegram user linked to an Iris account.
type TelegramLink struct {
	TelegramUserID int64
	UserID         string
	Token          string // Iris JWT
	Username       string
}

// Store handles all iris-telegram database operations.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a Store backed by the given pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Connect creates and validates a pgxpool connection, and runs schema setup.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse config: %w", err)
	}
	cfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// migrate ensures the telegram-specific tables exist.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS telegram_links (
			telegram_user_id BIGINT PRIMARY KEY,
			user_id          UUID NOT NULL,
			token            TEXT NOT NULL,
			username         TEXT,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS telegram_ai_sessions (
			telegram_user_id BIGINT PRIMARY KEY,
			messages         JSONB NOT NULL DEFAULT '[]',
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// ─── Telegram links ───────────────────────────────────────────────────────────

// LinkUser stores the association between a Telegram user and an Iris JWT.
func (s *Store) LinkUser(ctx context.Context, telegramUserID int64, userID, token, username string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO telegram_links (telegram_user_id, user_id, token, username)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (telegram_user_id) DO UPDATE
		   SET user_id = EXCLUDED.user_id, token = EXCLUDED.token, username = EXCLUDED.username`,
		telegramUserID, userID, token, username,
	)
	return err
}

// GetLinkByTelegramID returns the Iris link for a Telegram user.
// Returns ErrNotLinked if not found.
func (s *Store) GetLinkByTelegramID(ctx context.Context, telegramUserID int64) (*TelegramLink, error) {
	var l TelegramLink
	err := s.pool.QueryRow(ctx,
		`SELECT telegram_user_id, user_id, token, username
		 FROM telegram_links WHERE telegram_user_id = $1`,
		telegramUserID,
	).Scan(&l.TelegramUserID, &l.UserID, &l.Token, &l.Username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotLinked
		}
		return nil, fmt.Errorf("store: get link: %w", err)
	}
	return &l, nil
}

// GetLinkByUserID returns the Telegram link for an Iris user ID.
// Used by the notification subscriber to find who to notify.
func (s *Store) GetLinkByUserID(ctx context.Context, userID string) (*TelegramLink, error) {
	var l TelegramLink
	err := s.pool.QueryRow(ctx,
		`SELECT telegram_user_id, user_id, token, username
		 FROM telegram_links WHERE user_id = $1`,
		userID,
	).Scan(&l.TelegramUserID, &l.UserID, &l.Token, &l.Username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotLinked
		}
		return nil, fmt.Errorf("store: get link by user: %w", err)
	}
	return &l, nil
}

// UnlinkUser removes the association for a Telegram user.
func (s *Store) UnlinkUser(ctx context.Context, telegramUserID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM telegram_links WHERE telegram_user_id = $1`,
		telegramUserID,
	)
	return err
}

// ─── AI session persistence ───────────────────────────────────────────────────

// AIMessage is a single LLM conversation turn.
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SaveAISession persists the conversation history for a Telegram user.
func (s *Store) SaveAISession(ctx context.Context, telegramUserID int64, messages []AIMessage) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("store: marshal messages: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO telegram_ai_sessions (telegram_user_id, messages)
		 VALUES ($1, $2)
		 ON CONFLICT (telegram_user_id) DO UPDATE
		   SET messages = EXCLUDED.messages, updated_at = NOW()`,
		telegramUserID, data,
	)
	return err
}

// GetAISession loads the conversation history for a Telegram user.
// Returns an empty slice if no session exists.
func (s *Store) GetAISession(ctx context.Context, telegramUserID int64) ([]AIMessage, error) {
	var data []byte
	err := s.pool.QueryRow(ctx,
		`SELECT messages FROM telegram_ai_sessions WHERE telegram_user_id = $1`,
		telegramUserID,
	).Scan(&data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: get ai session: %w", err)
	}

	var messages []AIMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("store: unmarshal messages: %w", err)
	}
	return messages, nil
}

// ClearAISession deletes the conversation history for a Telegram user.
func (s *Store) ClearAISession(ctx context.Context, telegramUserID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM telegram_ai_sessions WHERE telegram_user_id = $1`,
		telegramUserID,
	)
	return err
}

// ─── Sentinel errors ──────────────────────────────────────────────────────────

// ErrNotLinked is returned when a Telegram user has no Iris account linked.
var ErrNotLinked = errors.New("store: telegram user is not linked to an Iris account")
