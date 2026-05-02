package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
	"github.com/nats-io/nats.go"
)

const (
	streamName    = "EVENTS"
	consumerName  = "iris-worker"
	subscribeSubj = "events.>"
)

// ExecutionEvent is the wire format published by iris-core and iris-hooks.
type ExecutionEvent struct {
	RelayID    string          `json:"relay_id"`
	EventID    string          `json:"event_id"`
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt time.Time       `json:"received_at"`
}

// Consumer subscribes to the NATS EVENTS stream and forwards messages to the job queue.
type Consumer struct {
	js       nats.JetStreamContext
	nc       *nats.Conn
	sub      *nats.Subscription
	jobQueue chan<- engine.Job
	log      *slog.Logger
}

// NewConsumer connects to NATS and creates a durable push consumer on the EVENTS stream.
func NewConsumer(natsURL string, jobQueue chan<- engine.Job, log *slog.Logger) (*Consumer, error) {
	nc, err := nats.Connect(natsURL,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(10),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("consumer: connect nats: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Drain()
		return nil, fmt.Errorf("consumer: jetstream context: %w", err)
	}

	// Ensure the stream exists (idempotent)
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{"events.>"},
		Storage:  nats.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		nc.Drain()
		return nil, fmt.Errorf("consumer: ensure stream: %w", err)
	}

	return &Consumer{
		js:       js,
		nc:       nc,
		jobQueue: jobQueue,
		log:      log,
	}, nil
}

// Start begins consuming messages from the EVENTS stream.
// Messages are converted to engine.Job and pushed to the job queue.
// The MsgAck callback ACKs or NAKs the NATS message based on processing outcome.
func (c *Consumer) Start(ctx context.Context) error {
	sub, err := c.js.Subscribe(
		subscribeSubj,
		func(msg *nats.Msg) {
			var event ExecutionEvent
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				c.log.Error("consumer: unmarshal message", "err", err)
				_ = msg.Ack() // bad message — ACK to avoid infinite retry
				return
			}

			job := engine.Job{
				RelayID: event.RelayID,
				EventID: event.EventID,
				Payload: event.Payload,
				MsgAck: func(ok bool) {
					if ok {
						_ = msg.Ack()
					} else {
						_ = msg.Nak()
					}
				},
			}

			select {
			case c.jobQueue <- job:
				// successfully enqueued
			case <-ctx.Done():
				_ = msg.Nak() // context cancelled — NAK so the message gets redelivered
			}
		},
		nats.Durable(consumerName),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.MaxAckPending(50), // backpressure: don't deliver more than 50 unacked at once
	)
	if err != nil {
		return fmt.Errorf("consumer: subscribe: %w", err)
	}

	c.sub = sub
	c.log.Info("nats consumer started", "subject", subscribeSubj, "consumer", consumerName)
	return nil
}

// Stop drains the subscription and closes the NATS connection.
func (c *Consumer) Stop() {
	if c.sub != nil {
		_ = c.sub.Drain()
	}
	_ = c.nc.Drain()
	c.log.Info("nats consumer stopped")
}
