package config

import (
	"log/slog"
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("WEBAPP_URL", "http://localhost:3000")
	t.Setenv("DATABASE_URL", "postgres://forma:forma@localhost:5432/forma?sslmode=disable")
}

func TestLoadDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":3000" {
		t.Errorf("HTTPAddr = %q, want :3000", cfg.HTTPAddr)
	}
	if cfg.ShutdownTimeout != 20*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 20s", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", cfg.LogLevel)
	}
	if cfg.WebAppURL.Host != "localhost:3000" {
		t.Errorf("WebAppURL.Host = %q, want localhost:3000", cfg.WebAppURL.Host)
	}
	if cfg.PublicURL != nil {
		t.Errorf("PublicURL = %v, want nil when unset", cfg.PublicURL)
	}
}

func TestLoadRejectsMissingRequired(t *testing.T) {
	t.Setenv("WEBAPP_URL", "http://localhost:3000")
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded without DATABASE_URL")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string][2]string{
		"unknown log format":   {"LOG_FORMAT", "yaml"},
		"zero pool size":       {"DATABASE_MAX_OPEN_CONNS", "0"},
		"unparseable duration": {"SHUTDOWN_TIMEOUT", "soon"},
		"unparseable level":    {"LOG_LEVEL", "loud"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(kv[0], kv[1])
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=%q", kv[0], kv[1])
			}
		})
	}
}

func TestLoadForMigrateNeedsOnlyTheDatabase(t *testing.T) {
	t.Setenv("WEBAPP_URL", "")
	t.Setenv("DATABASE_URL", "postgres://forma:forma@localhost:5432/forma?sslmode=disable")

	cfg, err := LoadForMigrate()
	if err != nil {
		t.Fatalf("LoadForMigrate: %v", err)
	}
	if cfg.DatabaseURL == "" || cfg.LogFormat != "json" {
		t.Fatalf("cfg = %+v, want the database URL and default log format", cfg)
	}

	t.Setenv("DATABASE_URL", "")
	if _, err := LoadForMigrate(); err == nil {
		t.Fatal("LoadForMigrate succeeded without DATABASE_URL")
	}
}
