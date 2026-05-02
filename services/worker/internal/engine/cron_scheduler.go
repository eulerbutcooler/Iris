package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/eulerbutcooler/iris/packages/cronutil"
	"github.com/eulerbutcooler/iris/services/worker/internal/store"
	"github.com/google/uuid"
)

// CronScheduler polls the database every interval for cron relays that are due
// and enqueues them as jobs.
type CronScheduler struct {
	db       *store.Store
	jobQueue chan<- Job
	interval time.Duration
	log      *slog.Logger
	once     sync.Once
	stop     chan struct{}
}

// NewCronScheduler creates a CronScheduler that polls at the given interval.
func NewCronScheduler(db *store.Store, jobQueue chan<- Job, interval time.Duration, log *slog.Logger) *CronScheduler {
	return &CronScheduler{
		db:       db,
		jobQueue: jobQueue,
		interval: interval,
		log:      log,
		stop:     make(chan struct{}),
	}
}

// Start begins the polling loop in a goroutine.
func (s *CronScheduler) Start(ctx context.Context) {
	go s.run(ctx)
	s.log.Info("cron scheduler started", "interval", s.interval)
}

// Stop signals the polling loop to exit and waits for it.
func (s *CronScheduler) Stop() {
	s.once.Do(func() { close(s.stop) })
}

func (s *CronScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Poll immediately on startup to catch any overdue relays
	s.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

// poll fetches all due cron relays, enqueues them, and advances their next_run_at.
func (s *CronScheduler) poll(ctx context.Context) {
	relays, err := s.db.GetCronRelays(ctx)
	if err != nil {
		s.log.Error("cron poll: get relays failed", "err", err)
		return
	}

	for _, relay := range relays {
		// Compute next_run_at before enqueuing so the relay won't be picked up again
		cronExpr, _ := relay.TriggerConfig["cron"].(string)
		if cronExpr == "" {
			s.log.Warn("cron relay has no cron expression — skipping", "relay_id", relay.ID)
			continue
		}

		next, err := cronutil.NextRun(cronExpr, time.Now())
		if err != nil {
			s.log.Error("cron: invalid expression", "relay_id", relay.ID, "expr", cronExpr, "err", err)
			continue
		}

		// Advance next_run_at first to prevent double-firing if the job takes a long time
		if err := s.db.UpdateRelayNextRun(ctx, relay.ID, next); err != nil {
			s.log.Error("cron: update next_run_at failed", "relay_id", relay.ID, "err", err)
			continue
		}

		job := Job{
			RelayID: relay.ID,
			EventID: uuid.New().String(),
			Payload: []byte(`{}`),
			MsgAck:  func(bool) {}, // cron jobs have no NATS message to ACK
		}

		select {
		case s.jobQueue <- job:
			s.log.Info("cron job enqueued", "relay_id", relay.ID, "next_run_at", next)
		default:
			// Job queue is full — log and skip; will retry on next poll
			s.log.Warn("cron: job queue full — relay skipped", "relay_id", relay.ID)
		}
	}
}
