// Package config loads process configuration from the environment once, at startup.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// HTTPAddr is the listen address of `forma serve`.
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":3000"`
	// ShutdownTimeout bounds how long in-flight requests get to finish after SIGTERM.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s"`

	// WebAppURL is the admin origin; PublicURL is the optional separate origin for surveys and the client API.
	WebAppURL *url.URL `env:"WEBAPP_URL,required,notEmpty"`
	PublicURL *url.URL `env:"PUBLIC_URL"`

	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	// DatabaseMaxOpenConns caps the pool of one process; every forma process holds its own pool.
	DatabaseMaxOpenConns int `env:"DATABASE_MAX_OPEN_CONNS" envDefault:"20"`

	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"json"`
}

// Load reads the environment. It fails on the first missing or malformed variable so a misconfigured pod never starts.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		return Config{}, fmt.Errorf("load config: LOG_FORMAT must be json or text, got %q", cfg.LogFormat)
	}
	if cfg.DatabaseMaxOpenConns < 1 {
		return Config{}, fmt.Errorf("load config: DATABASE_MAX_OPEN_CONNS must be at least 1, got %d", cfg.DatabaseMaxOpenConns)
	}
	return cfg, nil
}

type migrateEnv struct {
	DatabaseURL string     `env:"DATABASE_URL,required,notEmpty"`
	LogLevel    slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat   string     `env:"LOG_FORMAT" envDefault:"json"`
}

// LoadForMigrate reads only what `forma migrate` needs, so a migration job can run with the database credentials alone.
func LoadForMigrate() (Config, error) {
	e, err := env.ParseAs[migrateEnv]()
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if e.LogFormat != "json" && e.LogFormat != "text" {
		return Config{}, fmt.Errorf("load config: LOG_FORMAT must be json or text, got %q", e.LogFormat)
	}
	return Config{DatabaseURL: e.DatabaseURL, LogLevel: e.LogLevel, LogFormat: e.LogFormat}, nil
}
