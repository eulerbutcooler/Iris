package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/store"
	"github.com/nats-io/nats.go"
)

// ExecutionNotification is published by iris-worker after every execution completes.
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

// Notifier subscribes to NATS execution notifications and forwards them to Telegram.
type Notifier struct {
	js    nats.JetStreamContext
	nc    *nats.Conn
	sub   *nats.Subscription
	bot   *tgbotapi.BotAPI
	store *store.Store
	log   *slog.Logger
}

// NewNotifier connects to NATS and returns a Notifier.
func NewNotifier(natsURL string, bot *tgbotapi.BotAPI, store *store.Store, log *slog.Logger) (*Notifier, error) {
	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("notifier: connect nats: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Drain()
		return nil, fmt.Errorf("notifier: jetstream: %w", err)
	}

	// Ensure the NOTIFICATIONS stream exists
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     "NOTIFICATIONS",
		Subjects: []string{"notifications.>"},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		nc.Drain()
		return nil, fmt.Errorf("notifier: create stream: %w", err)
	}

	return &Notifier{js: js, nc: nc, bot: bot, store: store, log: log}, nil
}

// Start subscribes to notifications.> and sends Telegram messages on delivery.
func (n *Notifier) Start(ctx context.Context) error {
	sub, err := n.js.Subscribe(
		"notifications.>",
		func(msg *nats.Msg) {
			var notif ExecutionNotification
			if err := json.Unmarshal(msg.Data, &notif); err != nil {
				n.log.Error("notifier: unmarshal", "err", err)
				_ = msg.Ack()
				return
			}

			link, err := n.store.GetLinkByUserID(ctx, notif.UserID)
			if err != nil {
				// User not linked — ACK and skip silently
				_ = msg.Ack()
				return
			}

			text := formatNotification(notif)
			tgMsg := tgbotapi.NewMessage(link.TelegramUserID, text)
			tgMsg.ParseMode = tgbotapi.ModeMarkdown
			if _, err := n.bot.Send(tgMsg); err != nil {
				n.log.Error("notifier: send telegram message", "err", err)
			}

			_ = msg.Ack()
		},
		nats.Durable("iris-telegram-notifier"),
		nats.AckExplicit(),
		nats.DeliverAll(),
	)
	if err != nil {
		return fmt.Errorf("notifier: subscribe: %w", err)
	}

	n.sub = sub
	n.log.Info("notification subscriber started")
	return nil
}

// Stop drains the NATS subscription and connection.
func (n *Notifier) Stop() {
	if n.sub != nil {
		_ = n.sub.Drain()
	}
	_ = n.nc.Drain()
	n.log.Info("notification subscriber stopped")
}

// formatNotification formats an execution notification as a Telegram Markdown message.
func formatNotification(n ExecutionNotification) string {
	icon := "✅"
	if n.Status == "failed" {
		icon = "❌"
	}

	msg := fmt.Sprintf("%s *%s* — %s\nDuration: %dms\nExecution: `%s`",
		icon, n.RelayName, n.Status, n.DurationMs, n.ExecutionID[:8])

	if n.ErrorMsg != "" {
		msg += fmt.Sprintf("\nError: _%s_", n.ErrorMsg)
	}

	return msg
}
