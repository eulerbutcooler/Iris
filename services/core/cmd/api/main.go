package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eulerbutcooler/iris/packages/encryptor"
	"github.com/eulerbutcooler/iris/packages/logger"
	"github.com/eulerbutcooler/iris/services/core/internal/ai"
	"github.com/eulerbutcooler/iris/services/core/internal/api"
	"github.com/eulerbutcooler/iris/services/core/internal/config"
	"github.com/eulerbutcooler/iris/services/core/internal/db"
	"github.com/eulerbutcooler/iris/services/core/internal/queue"
	"github.com/eulerbutcooler/iris/services/core/internal/store"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New("iris-core", slog.LevelInfo)
	log.Info("starting iris-core", "port", cfg.Port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Database ──────────────────────────────────────────────────────────────
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
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

	// ── NATS Publisher ────────────────────────────────────────────────────────
	publisher, err := queue.NewPublisher(ctx, cfg.NATSURL)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	log.Info("nats connected")

	// ── LLM Client ────────────────────────────────────────────────────────────
	// LLM is optional — if no API key is provided, the AI endpoint will be
	// unavailable but all other endpoints remain functional.
	var llmClient ai.LLMClient
	if cfg.LLMAPIKey != "" {
		llmClient, err = ai.NewClient(cfg.LLMProvider, cfg.LLMAPIKey, cfg.LLMModel)
		if err != nil {
			log.Warn("llm client init failed — AI endpoint disabled", "err", err)
		} else {
			log.Info("llm client ready", "provider", cfg.LLMProvider, "model", cfg.LLMModel)
		}
	} else {
		log.Warn("LLM_API_KEY not set — AI relay generation endpoint disabled")
	}

	// ── Stores ────────────────────────────────────────────────────────────────
	userStore := store.NewUserStore(pool)
	secretStore := store.NewSecretStore(pool)
	relayStore := store.NewRelayStore(pool)

	// ── Handler + Router ──────────────────────────────────────────────────────
	handler := api.NewHandler(relayStore, secretStore, userStore, publisher, llmClient, enc, cfg, log)
	router := api.NewRouter(handler, cfg)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start in background
	go func() {
		log.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received — draining...")
	cancel() // cancel background context

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http server shutdown error", "err", err)
	}

	log.Info("iris-core stopped")
}
