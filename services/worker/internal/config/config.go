package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config holds all environment-driven configuration for iris-worker.
type Config struct {
	DatabaseURL   string
	NATSURL       string
	EncryptionKey string
	MaxWorkers    int           // goroutine pool size (default 10)
	CronInterval  time.Duration // how often to poll for due cron relays (default 30s)
}

// Load reads config from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		NATSURL:       getEnv("NATS_URL", "nats://localhost:4222"),
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		MaxWorkers:    getEnvInt("MAX_WORKERS", 10),
		CronInterval:  getEnvDuration("CRON_INTERVAL", 30*time.Second),
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.EncryptionKey == "" {
		missing = append(missing, "ENCRYPTION_KEY")
	}
	if len(missing) > 0 {
		return errors.New("config: missing required env vars: " + join(missing))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
