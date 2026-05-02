package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eulerbutcooler/iris/packages/logger"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/ai"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/bot"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/config"
	irisClient "github.com/eulerbutcooler/iris/services/iris-telegram/internal/iris"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/store"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New("iris-telegram", slog.LevelInfo)
	log.Info("starting iris-telegram")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("database connected")

	db := store.New(pool)

	// ── AI Client (optional) ──────────────────────────────────────────────────
	var aiClient *ai.Client
	if cfg.LLMAPIKey != "" {
		aiClient, err = ai.NewClient(cfg.LLMProvider, cfg.LLMAPIKey, cfg.LLMModel)
		if err != nil {
			log.Warn("ai client init failed — /new command disabled", "err", err)
		}
	} else {
		log.Warn("LLM_API_KEY not set — AI relay generation disabled")
	}

	// ── iris-core Client ──────────────────────────────────────────────────────
	iris := irisClient.NewClient(cfg.IrisCoreURL)

	// ── Session Manager ───────────────────────────────────────────────────────
	sessions := bot.NewSessionManager(cfg.SessionTTL)
	sessions.StartCleanup(ctx)

	// ── Bot ───────────────────────────────────────────────────────────────────
	b, err := bot.New(cfg.TelegramBotToken, sessions, aiClient, iris, db, log)
	if err != nil {
		log.Error("bot init failed", "err", err)
		os.Exit(1)
	}

	// ── Notification Subscriber (Phase 8) ─────────────────────────────────────
	// Directly access the underlying tgbotapi instance for notifier
	rawAPI, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Warn("notifier: second bot api init failed — notifications disabled", "err", err)
	} else {
		notifier, err := bot.NewNotifier(cfg.NATSURL, rawAPI, db, log)
		if err != nil {
			log.Warn("notification subscriber unavailable", "err", err)
		} else {
			if err := notifier.Start(ctx); err != nil {
				log.Warn("notification subscriber start failed", "err", err)
			} else {
				defer notifier.Stop()
			}
		}
	}

	// ── Run ───────────────────────────────────────────────────────────────────
	b.Start(ctx)
	log.Info("iris-telegram stopped")
}
