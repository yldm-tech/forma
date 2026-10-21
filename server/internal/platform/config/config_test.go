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
	t.Setenv("REDIS_URL", "redis://localhost:6379")
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
	if cfg.TrustedProxyHopCount != 1 {
		t.Errorf("TrustedProxyHopCount = %d, want 1", cfg.TrustedProxyHopCount)
	}
	if cfg.RateLimitingDisabled {
		t.Error("RateLimitingDisabled defaults to true, want false")
	}
	if !cfg.AuditLogEnabled || cfg.AuditLogGetUserIP {
		t.Errorf("audit flags = %v/%v, want enabled without the user IP", cfg.AuditLogEnabled, cfg.AuditLogGetUserIP)
	}
	if !cfg.SMTPAuthenticated || !cfg.SMTPRejectUnauthorizedTLS || cfg.SMTPSecureEnabled {
		t.Errorf("SMTP flags = auth %v, reject-unauthorized %v, secure %v; want true, true, false", cfg.SMTPAuthenticated, cfg.SMTPRejectUnauthorizedTLS, cfg.SMTPSecureEnabled)
	}
	if cfg.MailFrom != "noreply@forma.ylam.ai" || cfg.MailFromName != "Forma" {
		t.Errorf("mail from = %q <%s>, want Forma <noreply@forma.ylam.ai>", cfg.MailFromName, cfg.MailFrom)
	}
	if cfg.SMTPConfigured() {
		t.Error("SMTPConfigured() = true without SMTP_HOST and SMTP_PORT")
	}
	if cfg.PasswordResetTokenLifetimeMinutes != 30 || cfg.SessionMaxAge != 86400 {
		t.Errorf("reset lifetime %d min, session max age %d s; want 30 and 86400", cfg.PasswordResetTokenLifetimeMinutes, cfg.SessionMaxAge)
	}
	if cfg.EmailAuthDisabled || cfg.EmailVerificationDisabled || cfg.PasswordResetDisabled || cfg.PasswordHIBPCheckDisabled {
		t.Error("an auth kill switch defaults to on")
	}
	if cfg.SignupEnabled() || cfg.TurnstileConfigured() || cfg.SecureCookies() {
		t.Errorf("SignupEnabled %v, TurnstileConfigured %v, SecureCookies %v; want all false on a plain http self-hosted instance", cfg.SignupEnabled(), cfg.TurnstileConfigured(), cfg.SecureCookies())
	}
}

func TestDerivedAuthSettings(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		signup    bool
		turnstile bool
		secure    bool
	}{
		{"forma cloud", map[string]string{"IS_FORMA_CLOUD": "1"}, true, false, false},
		{"e2e testing", map[string]string{"E2E_TESTING": "1"}, true, false, false},
		{"turnstile needs both keys", map[string]string{"TURNSTILE_SITE_KEY": "site"}, false, false, false},
		{"turnstile", map[string]string{"TURNSTILE_SITE_KEY": "site", "TURNSTILE_SECRET_KEY": "secret"}, false, true, false},
		{"https web app", map[string]string{"WEBAPP_URL": "https://app.example.com"}, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.SignupEnabled() != tc.signup || cfg.TurnstileConfigured() != tc.turnstile || cfg.SecureCookies() != tc.secure {
				t.Fatalf("SignupEnabled %v, TurnstileConfigured %v, SecureCookies %v; want %v, %v, %v", cfg.SignupEnabled(), cfg.TurnstileConfigured(), cfg.SecureCookies(), tc.signup, tc.turnstile, tc.secure)
			}
		})
	}
}

// An empty TRUSTED_PROXY_HOP_COUNT is what docker-compose and Helm render for an unset value; forma2 read it as unset (1) rather than as the "trust nothing" 0.
func TestLoadTreatsEmptyHopCountAsUnset(t *testing.T) {
	setRequired(t)
	t.Setenv("TRUSTED_PROXY_HOP_COUNT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TrustedProxyHopCount != 1 {
		t.Fatalf("TrustedProxyHopCount = %d, want 1", cfg.TrustedProxyHopCount)
	}

	t.Setenv("TRUSTED_PROXY_HOP_COUNT", "0")
	if cfg, err = Load(); err != nil || cfg.TrustedProxyHopCount != 0 {
		t.Fatalf("explicit 0: hop count %d, err %v; want 0", cfg.TrustedProxyHopCount, err)
	}
}

func TestLoadFlags(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMITING_DISABLED", "1")
	t.Setenv("SMTP_AUTHENTICATED", "0")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "465")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RateLimitingDisabled || cfg.SMTPAuthenticated || !cfg.SMTPConfigured() {
		t.Fatalf("cfg = %+v, want rate limiting disabled, SMTP unauthenticated and configured", cfg)
	}
}

func TestLoadRejectsMissingRequired(t *testing.T) {
	for _, name := range []string{"WEBAPP_URL", "DATABASE_URL", "REDIS_URL"} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(name, "")
			if _, err := Load(); err == nil {
				t.Fatalf("Load succeeded without %s", name)
			}
		})
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string][2]string{
		"unknown log format":   {"LOG_FORMAT", "yaml"},
		"zero pool size":       {"DATABASE_MAX_OPEN_CONNS", "0"},
		"unparseable duration": {"SHUTDOWN_TIMEOUT", "soon"},
		"unparseable level":    {"LOG_LEVEL", "loud"},
		"hop count too high":   {"TRUSTED_PROXY_HOP_COUNT", "11"},
		"negative hop count":   {"TRUSTED_PROXY_HOP_COUNT", "-1"},
		"flag spelled true":    {"RATE_LIMITING_DISABLED", "true"},
		"non-numeric port":     {"SMTP_PORT", "smtp"},
		"port out of range":    {"SMTP_PORT", "70000"},
		"mail from with name":  {"MAIL_FROM", "Forma <noreply@example.com>"},
		"mail from not email":  {"MAIL_FROM", "noreply"},
		"relative privacy url": {"PRIVACY_URL", "/privacy"},
		"negative concurrency": {"WORKER_CONCURRENCY", "-1"},
		"reset lifetime short": {"PASSWORD_RESET_TOKEN_LIFETIME_MINUTES", "4"},
		"reset lifetime long":  {"PASSWORD_RESET_TOKEN_LIFETIME_MINUTES", "121"},
		"zero session max age": {"SESSION_MAX_AGE", "0"},
		"session max age text": {"SESSION_MAX_AGE", "1d"},
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
