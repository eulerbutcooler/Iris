package config

import (
	"errors"
	"os"
	"time"
)

// Config holds all environment-driven configuration for iris-telegram.
type Config struct {
	TelegramBotToken string
	LLMProvider      string        // "openai"
	LLMAPIKey        string
	LLMModel         string
	IrisCoreURL      string        // e.g. "http://localhost:3000"
	DatabaseURL      string        // for telegram_links table
	NATSURL          string        // for execution notifications
	SessionTTL       time.Duration // default 24h
}

// Load reads config from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		LLMProvider:      getEnv("LLM_PROVIDER", "openai"),
		LLMAPIKey:        os.Getenv("LLM_API_KEY"),
		LLMModel:         getEnv("LLM_MODEL", "gpt-4o-mini"),
		IrisCoreURL:      getEnv("IRIS_CORE_URL", "http://localhost:3000"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		NATSURL:          getEnv("NATS_URL", "nats://localhost:4222"),
		SessionTTL:       getEnvDuration("SESSION_TTL", 24*time.Hour),
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var missing []string
	if c.TelegramBotToken == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
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

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func join(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
