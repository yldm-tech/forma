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
	"github.com/google/uuid"
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

	// EncryptionKey seals secrets stored at rest (two-factor secrets and backup codes). It has forma2's ENCRYPTION_KEY format, so an instance keeps the value it already has: 64 hex characters, or exactly 32 characters taken as Latin-1 bytes.
	EncryptionKey EncryptionKey `env:"ENCRYPTION_KEY,required,notEmpty"`
	// EncryptionKeyVersion is written in front of every value EncryptionKey seals, so the key can be rotated: give the new key a new version and move the old one to EncryptionPreviousKeys.
	EncryptionKeyVersion int `env:"ENCRYPTION_KEY_VERSION" envDefault:"1"`
	// EncryptionPreviousKeys are retired keys by version ("<version>:<key>,..."), still accepted to open what they sealed. A 32-character key that contains a comma must be given in hex.
	EncryptionPreviousKeys EncryptionKeyring `env:"ENCRYPTION_PREVIOUS_KEYS"`

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

	// IsFormaCloud marks the hosted service: public sign-up is open and personal email domains are refused at sign-up.
	IsFormaCloud bool `env:"IS_FORMA_CLOUD" envDefault:"0"`
	// SignupDomainCheckOnInvites applies Forma Cloud's personal email domain refusal to invited sign-ups too; by default an invite exempts its holder.
	SignupDomainCheckOnInvites bool `env:"SIGNUP_DOMAIN_CHECK_ON_INVITES" envDefault:"0"`
	// InviteDisabled switches inviting members off. Pending invites can still be accepted, revoked and shared.
	InviteDisabled bool `env:"INVITE_DISABLED" envDefault:"0"`
	// InviteRateLimitPer24Hours is how many invite recipients one organization may be sent per day on a self-hosted instance; Forma Cloud allows 500.
	InviteRateLimitPer24Hours int `env:"INVITE_RATE_LIMIT_PER_24_HOURS" envDefault:"50"`
	// E2ETesting opens public sign-up for end-to-end test runs, as on Forma Cloud.
	E2ETesting bool `env:"E2E_TESTING" envDefault:"0"`
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

	// SSO is single sign-on through Google, GitHub, Azure AD and a generic OpenID Connect provider.
	SSO SSOConfig

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

// SSOConfig is forma2's single sign-on configuration. A provider is offered exactly when its own credentials are set; there is no other switch.
type SSOConfig struct {
	GoogleClientID     string `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET"`
	GitHubID           string `env:"GITHUB_ID"`
	GitHubSecret       string `env:"GITHUB_SECRET"`
	// AzureADTenantID is the Microsoft Entra tenant: a directory ID or verified domain gets full id_token verification; unset means common, the multi-tenant authority.
	AzureADClientID     string `env:"AZUREAD_CLIENT_ID"`
	AzureADClientSecret string `env:"AZUREAD_CLIENT_SECRET"`
	AzureADTenantID     string `env:"AZUREAD_TENANT_ID"`
	// OIDCIssuer is the generic provider's issuer; its discovery document is read from OIDCIssuer + "/.well-known/openid-configuration". OIDCDisplayName labels its button.
	OIDCClientID     string `env:"OIDC_CLIENT_ID"`
	OIDCClientSecret string `env:"OIDC_CLIENT_SECRET"`
	OIDCIssuer       string `env:"OIDC_ISSUER"`
	OIDCDisplayName  string `env:"OIDC_DISPLAY_NAME"`
	// DefaultOrganizationID names the organization every new SSO user joins, created on the first such sign-up; DefaultOrganizationRole is the role later sign-ups get in it (manager when unset).
	DefaultOrganizationID   string `env:"AUTH_SSO_DEFAULT_ORGANIZATION_ID"`
	DefaultOrganizationRole string `env:"AUTH_SSO_DEFAULT_ORGANIZATION_ROLE"`
	// BetterAuthURL and NextAuthURL are forma2's auth base URLs. The first one set, else WEBAPP_URL, is where identity providers send the browser back, so the redirect URIs registered for forma2 keep working.
	BetterAuthURL *url.URL `env:"BETTER_AUTH_URL"`
	NextAuthURL   *url.URL `env:"NEXTAUTH_URL"`
}

// GoogleEnabled, GitHubEnabled, AzureADEnabled and OIDCEnabled are forma2's *_OAUTH_ENABLED: a provider needs its client id and secret (and the generic one its issuer).
func (s SSOConfig) GoogleEnabled() bool { return s.GoogleClientID != "" && s.GoogleClientSecret != "" }

func (s SSOConfig) GitHubEnabled() bool { return s.GitHubID != "" && s.GitHubSecret != "" }

func (s SSOConfig) AzureADEnabled() bool {
	return s.AzureADClientID != "" && s.AzureADClientSecret != ""
}

func (s SSOConfig) OIDCEnabled() bool {
	return s.OIDCClientID != "" && s.OIDCClientSecret != "" && s.OIDCIssuer != ""
}

func (s SSOConfig) validate() error {
	if s.DefaultOrganizationID != "" {
		if _, err := uuid.Parse(s.DefaultOrganizationID); err != nil {
			return fmt.Errorf("AUTH_SSO_DEFAULT_ORGANIZATION_ID must be an organization id (a UUID), got %q", s.DefaultOrganizationID)
		}
	}
	switch s.DefaultOrganizationRole {
	case "", "owner", "manager", "member", "billing":
	default:
		return fmt.Errorf("AUTH_SSO_DEFAULT_ORGANIZATION_ROLE must be owner, manager, member or billing, got %q", s.DefaultOrganizationRole)
	}
	if s.OIDCEnabled() {
		if u, err := url.Parse(s.OIDCIssuer); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("OIDC_ISSUER must be an absolute http(s) URL, got %q", s.OIDCIssuer)
		}
	}
	for _, base := range []struct {
		name  string
		value *url.URL
	}{{"BETTER_AUTH_URL", s.BetterAuthURL}, {"NEXTAUTH_URL", s.NextAuthURL}} {
		if base.value != nil && ((base.value.Scheme != "http" && base.value.Scheme != "https") || base.value.Host == "") {
			return fmt.Errorf("%s must be an absolute http(s) URL, got %q", base.name, base.value.String())
		}
	}
	return nil
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
	if err := c.Authz.validate(); err != nil {
		return err
	}
	if err := c.SSO.validate(); err != nil {
		return err
	}
	if c.EncryptionKeyVersion < 1 || c.EncryptionKeyVersion > MaxEncryptionKeyVersion {
		return fmt.Errorf("ENCRYPTION_KEY_VERSION must be between 1 and %d, got %d", MaxEncryptionKeyVersion, c.EncryptionKeyVersion)
	}
	if _, ok := c.EncryptionPreviousKeys[c.EncryptionKeyVersion]; ok {
		return fmt.Errorf("ENCRYPTION_PREVIOUS_KEYS must not repeat the current ENCRYPTION_KEY_VERSION %d", c.EncryptionKeyVersion)
	}
	switch c.UserManagementMinimumRole {
	case "owner", "manager", "disabled":
	default:
		return fmt.Errorf("USER_MANAGEMENT_MINIMUM_ROLE must be owner, manager or disabled, got %q", c.UserManagementMinimumRole)
	}
	if c.PasswordResetTokenLifetimeMinutes < 5 || c.PasswordResetTokenLifetimeMinutes > 120 {
		return fmt.Errorf("PASSWORD_RESET_TOKEN_LIFETIME_MINUTES must be between 5 and 120, got %d", c.PasswordResetTokenLifetimeMinutes)
	}
	if c.InviteRateLimitPer24Hours < 1 {
		return fmt.Errorf("INVITE_RATE_LIMIT_PER_24_HOURS must be at least 1, got %d", c.InviteRateLimitPer24Hours)
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
