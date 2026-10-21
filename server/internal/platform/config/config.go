// Package config loads process configuration from the environment once, at startup.
package config

import (
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	// HTTPAddr is the listen address of `forma serve`.
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":3000"`
	// ShutdownTimeout bounds how long in-flight requests (and, in `forma worker`, running jobs) get to finish after SIGTERM.
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s"`

	// WebAppURL is the admin origin; PublicURL is the optional separate origin for surveys and the client API.
	WebAppURL *url.URL `env:"WEBAPP_URL,required,notEmpty"`
	PublicURL *url.URL `env:"PUBLIC_URL"`

	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	// DatabaseMaxOpenConns caps the pool of one process; every forma process holds its own pool.
	DatabaseMaxOpenConns int `env:"DATABASE_MAX_OPEN_CONNS" envDefault:"20"`

	// RedisURL points at Valkey, which holds the rate-limit counters. forma2 required it outside its test environment, and so does every forma process.
	RedisURL string `env:"REDIS_URL,required,notEmpty"`

	// RateLimitingDisabled turns every rate limit into a no-op that allows the request.
	RateLimitingDisabled bool `env:"RATE_LIMITING_DISABLED" envDefault:"0"`
	// TrustedProxyHopCount is how many reverse proxies in front of the server append to X-Forwarded-For; the client address is the entry that many places from the right. 0 means no proxy: the address is the TCP peer. Setting it higher than the real chain lets a caller choose their own address by prepending entries.
	TrustedProxyHopCount int `env:"TRUSTED_PROXY_HOP_COUNT" envDefault:"1"`

	// SMTP delivery. Mail is sent only when both SMTPHost and SMTPPort are set; otherwise sends are skipped and reported as not sent.
	SMTPHost string `env:"SMTP_HOST"`
	SMTPPort string `env:"SMTP_PORT"`
	// SMTPSecureEnabled selects implicit TLS. Port 465 implies it regardless of this flag.
	SMTPSecureEnabled         bool   `env:"SMTP_SECURE_ENABLED" envDefault:"0"`
	SMTPUser                  string `env:"SMTP_USER"`
	SMTPPassword              string `env:"SMTP_PASSWORD"`
	SMTPAuthenticated         bool   `env:"SMTP_AUTHENTICATED" envDefault:"1"`
	SMTPRejectUnauthorizedTLS bool   `env:"SMTP_REJECT_UNAUTHORIZED_TLS" envDefault:"1"`
	MailFrom                  string `env:"MAIL_FROM" envDefault:"noreply@forma.ylam.ai"`
	MailFromName              string `env:"MAIL_FROM_NAME" envDefault:"Forma"`

	// Legal links rendered in the footer of every email.
	PrivacyURL     string `env:"PRIVACY_URL"`
	ImprintURL     string `env:"IMPRINT_URL"`
	ImprintAddress string `env:"IMPRINT_ADDRESS"`

	// AuditLogEnabled emits audit events; AuditLogGetUserIP records the client address in them instead of "unknown".
	AuditLogEnabled   bool `env:"AUDIT_LOG_ENABLED" envDefault:"1"`
	AuditLogGetUserIP bool `env:"AUDIT_LOG_GET_USER_IP" envDefault:"0"`

	// WorkerConcurrency is how many jobs `forma worker` runs at once. 0 derives it from the CPU count, as forma2 did.
	WorkerConcurrency int `env:"WORKER_CONCURRENCY" envDefault:"0"`

	// IsFormaCloud marks the hosted service: public sign-up is open and personal email domains are refused at sign-up.
	IsFormaCloud bool `env:"IS_FORMA_CLOUD" envDefault:"0"`
	// E2ETesting opens public sign-up for end-to-end test runs, as on Forma Cloud.
	E2ETesting bool `env:"E2E_TESTING" envDefault:"0"`
	// SignupDomainCheckOnInvites extends the Forma Cloud email domain block to invited sign-ups, which are exempt by default.
	SignupDomainCheckOnInvites bool `env:"SIGNUP_DOMAIN_CHECK_ON_INVITES" envDefault:"0"`
	// EmailAuthDisabled switches email and password sign-up and sign-in off, for SSO-only instances.
	EmailAuthDisabled bool `env:"EMAIL_AUTH_DISABLED" envDefault:"0"`
	// EmailVerificationDisabled lets unverified users sign in and applies email changes without confirmation. The verification mail is still sent at sign-up.
	EmailVerificationDisabled bool `env:"EMAIL_VERIFICATION_DISABLED" envDefault:"0"`
	// PasswordResetDisabled refuses every password reset request and redemption.
	PasswordResetDisabled bool `env:"PASSWORD_RESET_DISABLED" envDefault:"0"`
	// PasswordResetTokenLifetimeMinutes is how long a reset link stays valid, 5 to 120 minutes.
	PasswordResetTokenLifetimeMinutes int `env:"PASSWORD_RESET_TOKEN_LIFETIME_MINUTES" envDefault:"30"`
	// PasswordHIBPCheckDisabled skips the Have I Been Pwned lookup when a password is set, for deployments that allow no egress.
	PasswordHIBPCheckDisabled bool `env:"PASSWORD_HIBP_CHECK_DISABLED" envDefault:"0"`
	// SessionMaxAge is a session's lifetime in seconds.
	SessionMaxAge int `env:"SESSION_MAX_AGE" envDefault:"86400"`
	// Cloudflare Turnstile protects sign-up only when both keys are set.
	TurnstileSiteKey   string `env:"TURNSTILE_SITE_KEY"`
	TurnstileSecretKey string `env:"TURNSTILE_SECRET_KEY"`

	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"json"`
}

// parseFlag reads every bool as a "1"/"0" switch. forma2 accepted exactly those two values, so anything else fails the start instead of being read as one or the other.
func parseFlag(v string) (any, error) {
	switch v {
	case "1":
		return true, nil
	case "0":
		return false, nil
	default:
		return nil, fmt.Errorf("must be 1 or 0, got %q", v)
	}
}

var parseOptions = env.Options{FuncMap: map[reflect.Type]env.ParserFunc{reflect.TypeFor[bool](): parseFlag}}

// SMTPConfigured reports whether mail can be sent at all.
func (c Config) SMTPConfigured() bool {
	return c.SMTPHost != "" && c.SMTPPort != ""
}

// SignupEnabled is forma2's SIGNUP_ENABLED: public sign-up is open on Forma Cloud and in end-to-end test runs. A self-hosted instance only admits its first user and invitees.
func (c Config) SignupEnabled() bool {
	return c.IsFormaCloud || c.E2ETesting
}

// TurnstileConfigured reports whether sign-up requires a Turnstile challenge.
func (c Config) TurnstileConfigured() bool {
	return c.TurnstileSiteKey != "" && c.TurnstileSecretKey != ""
}

// SecureCookies reports whether cookies get the Secure attribute and the __Secure- name prefix, which browsers only accept over HTTPS.
func (c Config) SecureCookies() bool {
	return c.WebAppURL != nil && c.WebAppURL.Scheme == "https"
}

// Load reads the environment. It fails on the first missing or malformed variable so a misconfigured pod never starts.
func Load() (Config, error) {
	cfg, err := env.ParseAsWithOptions[Config](parseOptions)
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return fmt.Errorf("LOG_FORMAT must be json or text, got %q", c.LogFormat)
	}
	if c.DatabaseMaxOpenConns < 1 {
		return fmt.Errorf("DATABASE_MAX_OPEN_CONNS must be at least 1, got %d", c.DatabaseMaxOpenConns)
	}
	if c.TrustedProxyHopCount < 0 || c.TrustedProxyHopCount > 10 {
		return fmt.Errorf("TRUSTED_PROXY_HOP_COUNT must be between 0 and 10, got %d", c.TrustedProxyHopCount)
	}
	if c.WorkerConcurrency < 0 {
		return fmt.Errorf("WORKER_CONCURRENCY must not be negative, got %d", c.WorkerConcurrency)
	}
	if c.PasswordResetTokenLifetimeMinutes < 5 || c.PasswordResetTokenLifetimeMinutes > 120 {
		return fmt.Errorf("PASSWORD_RESET_TOKEN_LIFETIME_MINUTES must be between 5 and 120, got %d", c.PasswordResetTokenLifetimeMinutes)
	}
	if c.SessionMaxAge < 1 {
		return fmt.Errorf("SESSION_MAX_AGE must be a positive number of seconds, got %d", c.SessionMaxAge)
	}
	if c.SMTPPort != "" {
		if port, err := strconv.Atoi(c.SMTPPort); err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("SMTP_PORT must be a port number, got %q", c.SMTPPort)
		}
	}
	if addr, err := mail.ParseAddress(c.MailFrom); err != nil || addr.Name != "" || addr.Address != c.MailFrom {
		return fmt.Errorf("MAIL_FROM must be a plain email address, got %q", c.MailFrom)
	}
	for _, link := range []struct{ name, value string }{{"PRIVACY_URL", c.PrivacyURL}, {"IMPRINT_URL", c.ImprintURL}} {
		if link.value == "" {
			continue
		}
		if u, err := url.Parse(link.value); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be an absolute http(s) URL, got %q", link.name, link.value)
		}
	}
	return nil
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
