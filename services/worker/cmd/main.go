package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eulerbutcooler/iris/packages/encryptor"
	"github.com/eulerbutcooler/iris/packages/logger"
	"github.com/eulerbutcooler/iris/services/worker/internal/config"
	"github.com/eulerbutcooler/iris/services/worker/internal/engine"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/condition"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/debug"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/discord"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/email"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/httpreq"
	"github.com/eulerbutcooler/iris/services/worker/internal/integrations/slack"
	"github.com/eulerbutcooler/iris/services/worker/internal/queue"
	"github.com/eulerbutcooler/iris/services/worker/internal/store"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New("iris-worker", slog.LevelInfo)
	log.Info("starting iris-worker", "workers", cfg.MaxWorkers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("database connected")

	// ── Encryptor ─────────────────────────────────────────────────────────────
	enc, err := encryptor.New(cfg.EncryptionKey)
	if err != nil {
		log.Error("encryptor init failed", "err", err)
		os.Exit(1)
	}

	// ── Store ─────────────────────────────────────────────────────────────────
	db := store.New(pool, enc)

	// ── Action Registry ───────────────────────────────────────────────────────
	registry := engine.NewRegistry()
	registry.Register("debug_log", debug.New(log))
	registry.Register("discord_send", discord.New())
	registry.Register("slack_send", slack.New())
	registry.Register("http_request", httpreq.New())
	registry.Register("email_send", email.New())
	registry.Register("condition", condition.New())
	log.Info("action registry loaded", "types", 6)

	// ── Notification Publisher (Phase 8) ─────────────────────────────────────
	// Optional — if NATS is unavailable, worker still runs without notifications.
	var notifier *engine.NotificationPublisher
	notifier, err = engine.NewNotificationPublisher(cfg.NATSURL, log)
	if err != nil {
		log.Warn("notification publisher unavailable — execution notifications disabled", "err", err)
		notifier = nil
	} else {
		defer notifier.Drain()
		log.Info("notification publisher ready")
	}

	// ── Executor + Worker Pool ────────────────────────────────────────────────
	executor := engine.NewExecutor(db, registry, notifier, log)
	pool2 := engine.NewWorkerPool(cfg.MaxWorkers, executor, log)

	// ── Cron Scheduler ────────────────────────────────────────────────────────
	cronSched := engine.NewCronScheduler(db, pool2.JobQueue, cfg.CronInterval, log)

	// ── NATS Consumer ─────────────────────────────────────────────────────────
	consumer, err := queue.NewConsumer(cfg.NATSURL, pool2.JobQueue, log)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	log.Info("nats connected")

	// ── Start everything ──────────────────────────────────────────────────────
	pool2.Start(ctx)
	cronSched.Start(ctx)
	if err := consumer.Start(ctx); err != nil {
		log.Error("consumer start failed", "err", err)
		os.Exit(1)
	}

	log.Info("iris-worker ready")

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	// Correct shutdown order (from roadmap §3.7):
	//   1. Stop NATS consumer  → no new messages enter the job channel
	//   2. Stop cron scheduler → no new cron jobs enqueued
	//   3. Shutdown worker pool → wait for in-flight jobs to complete
	//   4. Close DB pool        → clean connection teardown (via defer)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received")
	cancel() // cancel context so workers stop picking new jobs

	consumer.Stop()    // 1. stop NATS intake
	cronSched.Stop()   // 2. stop cron enqueue
	pool2.Shutdown()   // 3. drain in-flight jobs

	log.Info("iris-worker stopped")
}
