package config

import (
	"errors"
	"os"
)

// Config holds all environment-driven configuration for iris-hooks.
// Deliberately minimal — no DB, no JWT, no encryption.
type Config struct {
	Port    string
	NATSURL string
}

// Load reads config from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{
		Port:    getEnv("HOOKS_PORT", "8080"),
		NATSURL: getEnv("NATS_URL", "nats://localhost:4222"),
	}
	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.NATSURL == "" {
		return errors.New("config: NATS_URL is required")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
