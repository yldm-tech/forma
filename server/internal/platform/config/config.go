// Package config loads process configuration from the environment once, at startup.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

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

	// Authz is the SpiceDB connection every authorization decision goes through.
	Authz AuthzConfig
	// UserManagementMinimumRole is the lowest organization role whose users may manage members and invites (owner, manager or disabled), forma2's USER_MANAGEMENT_MINIMUM_ROLE.
	UserManagementMinimumRole string `env:"USER_MANAGEMENT_MINIMUM_ROLE" envDefault:"manager"`

	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"json"`
}

// AuthzConfig is forma2's AUTHZED_* configuration. SpiceDB is the only authorization evaluator: while it is disabled every check fails closed with authzed_disabled and the projection outbox is left undelivered.
type AuthzConfig struct {
	Enabled bool `env:"AUTHZED_ENABLED" envDefault:"0"`
	// Endpoint is SpiceDB's gRPC address as host:port, without a scheme or path.
	Endpoint string `env:"AUTHZED_ENDPOINT"`
	// Token is the gRPC preshared key.
	Token string `env:"AUTHZED_TOKEN"`
	// Insecure talks plaintext gRPC instead of TLS.
	Insecure bool `env:"AUTHZED_INSECURE" envDefault:"0"`
	// Consistency is what permission checks and resource lookups ask SpiceDB for: fully_consistent (the default, and what forma2 always used) evaluates at the newest revision; minimize_latency lets SpiceDB answer from its quantized cache, so a delivered revocation can take effect a few seconds later. Operational reads always use fully_consistent.
	Consistency string `env:"AUTHZED_CONSISTENCY" envDefault:"fully_consistent"`
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
	if err := c.Authz.validate(); err != nil {
		return err
	}
	switch c.UserManagementMinimumRole {
	case "owner", "manager", "disabled":
	default:
		return fmt.Errorf("USER_MANAGEMENT_MINIMUM_ROLE must be owner, manager or disabled, got %q", c.UserManagementMinimumRole)
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

func (a AuthzConfig) validate() error {
	if a.Endpoint != "" && !validAuthzEndpoint(a.Endpoint) {
		return errors.New("AUTHZED_ENDPOINT must be a valid host:port without a URL scheme or path")
	}
	if a.Token != "" && strings.TrimSpace(a.Token) == "" {
		return errors.New("AUTHZED_TOKEN must not be empty")
	}
	if a.Consistency != "fully_consistent" && a.Consistency != "minimize_latency" {
		return fmt.Errorf("AUTHZED_CONSISTENCY must be minimize_latency or fully_consistent, got %q", a.Consistency)
	}
	if !a.Enabled {
		return nil
	}
	if a.Endpoint == "" {
		return errors.New("AUTHZED_ENDPOINT is required when AuthZed is enabled")
	}
	if a.Token == "" {
		return errors.New("AUTHZED_TOKEN is required when AuthZed is enabled")
	}
	return nil
}

// validAuthzEndpoint is forma2's check: host:port with a port in 1-65535, an IPv6 host in brackets, and nothing a URL would add (scheme, credentials, path, query or fragment).
func validAuthzEndpoint(value string) bool {
	if strings.ContainsAny(value, "/@?#") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return false
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return false
	}
	if strings.HasPrefix(value, "[") != strings.Contains(host, ":") {
		return false
	}
	if port == "" || strings.Trim(port, "0123456789") != "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
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

type authzEnv struct {
	DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	Authz       AuthzConfig
	LogLevel    slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat   string     `env:"LOG_FORMAT" envDefault:"json"`
}

// LoadForAuthz reads only what the `forma authz` operator commands need: the database and the SpiceDB connection.
func LoadForAuthz() (Config, error) {
	e, err := env.ParseAsWithOptions[authzEnv](parseOptions)
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if e.LogFormat != "json" && e.LogFormat != "text" {
		return Config{}, fmt.Errorf("load config: LOG_FORMAT must be json or text, got %q", e.LogFormat)
	}
	if err := e.Authz.validate(); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return Config{DatabaseURL: e.DatabaseURL, DatabaseMaxOpenConns: 4, Authz: e.Authz, LogLevel: e.LogLevel, LogFormat: e.LogFormat}, nil
}
