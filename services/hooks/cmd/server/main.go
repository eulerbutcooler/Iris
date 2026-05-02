package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eulerbutcooler/iris/packages/logger"
	"github.com/eulerbutcooler/iris/services/hooks/internal/api"
	"github.com/eulerbutcooler/iris/services/hooks/internal/config"
	"github.com/eulerbutcooler/iris/services/hooks/internal/queue"
)

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// ── Logger ────────────────────────────────────────────────────────────────
	log := logger.New("iris-hooks", slog.LevelInfo)
	log.Info("starting iris-hooks", "port", cfg.Port)

	// ── NATS Publisher ────────────────────────────────────────────────────────
	publisher, err := queue.NewPublisher(cfg.NATSURL)
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer publisher.Drain()
	log.Info("nats connected", "url", cfg.NATSURL)

	// ── Handler + Router ──────────────────────────────────────────────────────
	handler := api.NewHandler(publisher, log)
	router := api.NewRouter(handler, log)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown error", "err", err)
	}

	log.Info("iris-hooks stopped")
}
