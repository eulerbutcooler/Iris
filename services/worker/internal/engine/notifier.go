package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// ExecutionNotification is published to NATS after every execution completes.
// iris-telegram subscribes to "notifications.<userID>" to forward to Telegram.
type ExecutionNotification struct {
	RelayID     string    `json:"relay_id"`
	RelayName   string    `json:"relay_name"`
	UserID      string    `json:"user_id"`
	ExecutionID string    `json:"execution_id"`
	Status      string    `json:"status"` // "success" | "failed"
	DurationMs  int64     `json:"duration_ms"`
	ErrorMsg    string    `json:"error_message,omitempty"`
	FinishedAt  time.Time `json:"finished_at"`
}

// NotificationPublisher publishes execution results to the NOTIFICATIONS stream.
type NotificationPublisher struct {
	js  nats.JetStreamContext
	nc  *nats.Conn
	log *slog.Logger
}

// NewNotificationPublisher connects to NATS and ensures the NOTIFICATIONS stream exists.
func NewNotificationPublisher(natsURL string, log *slog.Logger) (*NotificationPublisher, error) {
	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(5),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("notif_publisher: connect: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Drain()
		return nil, fmt.Errorf("notif_publisher: jetstream: %w", err)
	}

	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "NOTIFICATIONS",
		Subjects: []string{"notifications.>"},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		nc.Drain()
		return nil, fmt.Errorf("notif_publisher: create stream: %w", err)
	}

	return &NotificationPublisher{js: js, nc: nc, log: log}, nil
}

// Publish sends an execution notification to notifications.<userID>.
func (p *NotificationPublisher) Publish(ctx context.Context, notif ExecutionNotification) {
	data, err := json.Marshal(notif)
	if err != nil {
		p.log.Error("notif_publisher: marshal", "err", err)
		return
	}

	subject := "notifications." + notif.UserID
	if _, err := p.js.PublishAsync(subject, data); err != nil {
		p.log.Error("notif_publisher: publish", "subject", subject, "err", err)
	}
}

// Drain flushes and closes the NATS connection.
func (p *NotificationPublisher) Drain() {
	_ = p.nc.Drain()
}
