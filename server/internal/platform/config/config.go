// Package config loads process configuration from the environment once, at startup.
package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
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
	// EncryptionKeyText is ENCRYPTION_KEY exactly as configured. forma2 keyed its HMAC signatures and JWTs with the variable's text rather than the decoded key, so the links it issued (signed single-use links, personal contact links) verify only against the same text.
	EncryptionKeyText Secret `env:"ENCRYPTION_KEY"`
	// NextAuthSecret and BetterAuthSecret are forma2's NEXTAUTH_SECRET and BETTER_AUTH_SECRET. They signed the link survey tokens (email verification links, PIN tokens); an instance migrated from forma2 keeps them so the links it already sent keep working. Without them the tokens are signed with a secret derived from ENCRYPTION_KEY.
	NextAuthSecret   Secret `env:"NEXTAUTH_SECRET"`
	BetterAuthSecret Secret `env:"BETTER_AUTH_SECRET"`

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

	// Legal links rendered in the footer of every email and of the public survey pages; TermsURL appears on the survey pages only.
	PrivacyURL     string `env:"PRIVACY_URL"`
	ImprintURL     string `env:"IMPRINT_URL"`
	ImprintAddress string `env:"IMPRINT_ADDRESS"`
	TermsURL       string `env:"TERMS_URL"`

	// AuditLogEnabled emits audit events; AuditLogGetUserIP records the client address in them instead of "unknown".
	AuditLogEnabled   bool `env:"AUDIT_LOG_ENABLED" envDefault:"1"`
	AuditLogGetUserIP bool `env:"AUDIT_LOG_GET_USER_IP" envDefault:"0"`

	// DangerouslyAllowWebhookInternalURLs lets webhooks target private, loopback, link-local and metadata addresses and follow redirects, for self-hosted instances whose receivers live on the internal network, forma2's DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS. A host name that does not resolve is still refused.
	DangerouslyAllowWebhookInternalURLs bool `env:"DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS" envDefault:"0"`

	// WorkerConcurrency is how many jobs `forma worker` runs at once. 0 derives it from the CPU count, as forma2 did.
	WorkerConcurrency int `env:"WORKER_CONCURRENCY" envDefault:"0"`

	// SurveySchedulingTimeZone, SurveySchedulingLocalHour and SurveySchedulingLocalMinute are when, each day, scheduled surveys are published and closed: a publish or close date is snapped to that local time of its calendar day in the zone, forma2's SURVEY_SCHEDULING_* settings.
	SurveySchedulingTimeZone    string `env:"SURVEY_SCHEDULING_TIME_ZONE" envDefault:"Europe/Berlin"`
	SurveySchedulingLocalHour   int    `env:"SURVEY_SCHEDULING_LOCAL_HOUR" envDefault:"0"`
	SurveySchedulingLocalMinute int    `env:"SURVEY_SCHEDULING_LOCAL_MINUTE" envDefault:"0"`

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
	// RecaptchaSiteKey and RecaptchaSecretKey configure Google reCAPTCHA for survey spam protection; a survey may enable it only when both are set, forma2's IS_RECAPTCHA_CONFIGURED.
	RecaptchaSiteKey   string `env:"RECAPTCHA_SITE_KEY"`
	RecaptchaSecretKey string `env:"RECAPTCHA_SECRET_KEY"`
	// Cloudflare Turnstile protects sign-up only when both keys are set.
	TurnstileSiteKey   string `env:"TURNSTILE_SITE_KEY"`
	TurnstileSecretKey string `env:"TURNSTILE_SECRET_KEY"`

	// SSO is single sign-on through Google, GitHub, Azure AD, a generic OpenID Connect provider and SAML.
	SSO SSOConfig

	// MCPOAuthJWKSURL is where /api/mcp fetches the keys OAuth access tokens are signed with, forma2's MCP_OAUTH_JWKS_URL, for deployments that verify against another instance's authorization server. It changes no public URL: the issuer, the redirects and the token audience stay derived from WEBAPP_URL and BETTER_AUTH_URL. Unset, the keys are read from the database the authorization server writes them to.
	MCPOAuthJWKSURL *url.URL `env:"MCP_OAUTH_JWKS_URL"`

	// S3 is the object store uploaded files live in.
	S3 S3Config

	// AI is the instance's AI provider, forma2's AI_* variables.
	AI AIConfig

	// Integrations are the OAuth apps of Google Sheets, Airtable, Notion and Slack.
	Integrations IntegrationsConfig

	// Stripe is Forma Cloud's billing account. Billing runs only on Forma Cloud with STRIPE_SECRET_KEY set; everywhere else it is off.
	Stripe StripeConfig

	// Telemetry is OpenTelemetry, Sentry, PostHog and the daily usage report, all off unless configured.
	Telemetry TelemetryConfig

	LogLevel  slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string     `env:"LOG_FORMAT" envDefault:"json"`
}

// AuthzConfig is forma2's AUTHZED_* configuration. SpiceDB is the only authorization evaluator: while it is disabled every check fails closed with authzed_disabled and the projection outbox is left undelivered.
type AuthzConfig struct {
	Enabled AuthzedFlag `env:"AUTHZED_ENABLED" envDefault:"0"`
	// Endpoint is SpiceDB's gRPC address as host:port, without a scheme or path.
	Endpoint string `env:"AUTHZED_ENDPOINT"`
	// Token is the gRPC preshared key.
	Token string `env:"AUTHZED_TOKEN"`
	// Insecure talks plaintext gRPC instead of TLS.
	Insecure AuthzedFlag `env:"AUTHZED_INSECURE" envDefault:"0"`
	// Consistency is what permission checks and resource lookups ask SpiceDB for: fully_consistent (the default, and what forma2 always used) evaluates at the newest revision; minimize_latency lets SpiceDB answer from its quantized cache, so a delivered revocation can take effect a few seconds later. Operational reads always use fully_consistent.
	Consistency string `env:"AUTHZED_CONSISTENCY" envDefault:"fully_consistent"`
}

// S3Config is forma2's file storage configuration. Only the bucket is required, and file storage is configured exactly when it is set (forma2's IS_STORAGE_CONFIGURED). The region is passed on only when set, and the keys only when both are set; otherwise the AWS default credential chain applies (environment, shared credentials file, web identity, container and instance roles).
type S3Config struct {
	AccessKey string `env:"S3_ACCESS_KEY"`
	SecretKey string `env:"S3_SECRET_KEY"`
	Region    string `env:"S3_REGION"`
	// EndpointURL is an S3-compatible service (RustFS, MinIO, StorJ, ...) as scheme://host[:port]; unset means Amazon S3.
	EndpointURL *url.URL `env:"S3_ENDPOINT_URL"`
	// ForcePathStyle addresses the bucket as <endpoint>/<bucket> instead of <bucket>.<endpoint>, which most self-hosted services need.
	ForcePathStyle bool   `env:"S3_FORCE_PATH_STYLE" envDefault:"0"`
	BucketName     string `env:"S3_BUCKET_NAME"`
}

// Configured is forma2's IS_STORAGE_CONFIGURED.
func (s S3Config) Configured() bool { return s.BucketName != "" }

func (s S3Config) validate() error {
	if u := s.EndpointURL; u != nil {
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("S3_ENDPOINT_URL must be an http(s) URL without a path, got %q", u.String())
		}
	}
	return nil
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
	// SAMLDatabaseURL is forma2's switch for SAML SSO: set, the SAML button is offered and the connection is read from saml-connection/*.xml at start-up. forma2 kept its SAML state in that separate database; here the connection lives in the main one, so only whether it is set matters.
	SAMLDatabaseURL Secret `env:"SAML_DATABASE_URL"`
}

// StripeConfig is forma2's STRIPE_* variables, all optional.
type StripeConfig struct {
	// SecretKey is the API key every Stripe call is made with; without it billing does nothing.
	SecretKey Secret `env:"STRIPE_SECRET_KEY"`
	// WebhookSecret verifies the signature of the events Stripe posts to /api/billing/stripe-webhook; without it the endpoint answers 503.
	WebhookSecret Secret `env:"STRIPE_WEBHOOK_SECRET"`
	// PublishableKey is handed to Stripe.js on the billing page to confirm a card payment that needs 3D Secure.
	PublishableKey string `env:"STRIPE_PUBLISHABLE_KEY"`
}

// IntegrationsConfig is forma2's OAuth app credentials of the third-party integrations, under its variable names. An integration can be connected exactly when its variables are set.
type IntegrationsConfig struct {
	// GoogleSheetsRedirectURL is used verbatim as the OAuth redirect URI; it is <WEBAPP_URL>/api/google-sheet/callback unless the app was registered under another origin.
	GoogleSheetsClientID     string `env:"GOOGLE_SHEETS_CLIENT_ID"`
	GoogleSheetsClientSecret string `env:"GOOGLE_SHEETS_CLIENT_SECRET"`
	GoogleSheetsRedirectURL  string `env:"GOOGLE_SHEETS_REDIRECT_URL"`
	// AirtableClientID is a public OAuth client: Airtable is authorized with PKCE and no secret.
	AirtableClientID   string `env:"AIRTABLE_CLIENT_ID"`
	NotionClientID     string `env:"NOTION_OAUTH_CLIENT_ID"`
	NotionClientSecret string `env:"NOTION_OAUTH_CLIENT_SECRET"`
	SlackClientID      string `env:"SLACK_CLIENT_ID"`
	SlackClientSecret  string `env:"SLACK_CLIENT_SECRET"`
}

// GoogleSheetsEnabled, AirtableEnabled, NotionEnabled and SlackEnabled are what forma2's integration pages checked before offering to connect.
func (c IntegrationsConfig) GoogleSheetsEnabled() bool {
	return c.GoogleSheetsClientID != "" && c.GoogleSheetsClientSecret != "" && c.GoogleSheetsRedirectURL != ""
}

func (c IntegrationsConfig) AirtableEnabled() bool { return c.AirtableClientID != "" }

func (c IntegrationsConfig) NotionEnabled() bool {
	return c.NotionClientID != "" && c.NotionClientSecret != ""
}

func (c IntegrationsConfig) SlackEnabled() bool {
	return c.SlackClientID != "" && c.SlackClientSecret != ""
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

// SAMLEnabled is forma2's SAML_OAUTH_ENABLED. Its licence check always granted SAML, so the variable is the only gate.
func (s SSOConfig) SAMLEnabled() bool { return s.SAMLDatabaseURL != "" }

// forma2ID is zod's cuid2 pattern, which forma2 validated AUTH_SSO_DEFAULT_ORGANIZATION_ID against.
var forma2ID = regexp.MustCompile(`^[0-9a-z]+$`)

// DefaultOrganization is the organization AUTH_SSO_DEFAULT_ORGANIZATION_ID names, if set. A forma2 id resolves to the row it was migrated to, so a value set for forma2 keeps naming the same organization.
func (s SSOConfig) DefaultOrganization() (uuid.UUID, bool) {
	return legacyid.Resolve(s.DefaultOrganizationID)
}

func (s SSOConfig) validate() error {
	if id := s.DefaultOrganizationID; id != "" {
		if _, err := uuid.Parse(id); (err != nil || len(id) != 36) && !forma2ID.MatchString(id) {
			return fmt.Errorf("AUTH_SSO_DEFAULT_ORGANIZATION_ID must be an organization id (a UUID, or the cuid2 id forma2 knew it by), got %q", id)
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

// parseFlag reads every bool as a "1"/"0" switch. forma2 accepted exactly those two values for its switches, so anything else fails the start instead of being read as one or the other.
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

// AuthzedFlag is a switch that also takes "true" and "false": forma2 read AUTHZED_ENABLED and AUTHZED_INSECURE with z.enum(["true", "false", "1", "0"]), its installer writes AUTHZED_ENABLED=true into .env and its compose file and README used true/false, so those deployments keep starting.
type AuthzedFlag bool

func parseAuthzedFlag(v string) (any, error) {
	switch v {
	case "1", "true":
		return AuthzedFlag(true), nil
	case "0", "false":
		return AuthzedFlag(false), nil
	default:
		return nil, fmt.Errorf("must be true, false, 1 or 0, got %q", v)
	}
}

var parseOptions = env.Options{FuncMap: map[reflect.Type]env.ParserFunc{
	reflect.TypeFor[bool]():        parseFlag,
	reflect.TypeFor[AuthzedFlag](): parseAuthzedFlag,
}}

// SMTPConfigured reports whether mail can be sent at all.
func (c Config) SMTPConfigured() bool {
	return c.SMTPHost != "" && c.SMTPPort != ""
}

// SignupEnabled is forma2's SIGNUP_ENABLED: public sign-up is open on Forma Cloud and in end-to-end test runs. A self-hosted instance only admits its first user and invitees.
func (c Config) SignupEnabled() bool {
	return c.IsFormaCloud || c.E2ETesting
}

// RecaptchaConfigured reports whether surveys may turn on reCAPTCHA spam protection.
func (c Config) RecaptchaConfigured() bool {
	return c.RecaptchaSiteKey != "" && c.RecaptchaSecretKey != ""
}

// LinkSurveyEmailSecret signs the link survey email verification tokens, forma2's NEXTAUTH_SECRET.
func (c Config) LinkSurveyEmailSecret() string {
	if c.NextAuthSecret != "" {
		return string(c.NextAuthSecret)
	}
	return c.derivedTokenSecret()
}

// LinkSurveyPinSecret signs the link survey PIN tokens, forma2's BETTER_AUTH_SECRET falling back to NEXTAUTH_SECRET.
func (c Config) LinkSurveyPinSecret() string {
	if c.BetterAuthSecret != "" {
		return string(c.BetterAuthSecret)
	}
	return c.LinkSurveyEmailSecret()
}

// derivedTokenSecret is the token signing secret of an instance that never ran forma2: HMAC-SHA256 of a fixed label under the encryption key, so it needs no configuration of its own and changes only when the key does.
func (c Config) derivedTokenSecret() string {
	mac := hmac.New(sha256.New, c.EncryptionKey)
	mac.Write([]byte("forma.link-survey-token-secret.v1"))
	return hex.EncodeToString(mac.Sum(nil))
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
	if _, err := time.LoadLocation(c.SurveySchedulingTimeZone); err != nil || c.SurveySchedulingTimeZone == "" || c.SurveySchedulingTimeZone == "Local" {
		return fmt.Errorf("SURVEY_SCHEDULING_TIME_ZONE must be an IANA time zone, got %q", c.SurveySchedulingTimeZone)
	}
	if c.SurveySchedulingLocalHour < 0 || c.SurveySchedulingLocalHour > 23 {
		return fmt.Errorf("SURVEY_SCHEDULING_LOCAL_HOUR must be between 0 and 23, got %d", c.SurveySchedulingLocalHour)
	}
	if c.SurveySchedulingLocalMinute < 0 || c.SurveySchedulingLocalMinute > 59 {
		return fmt.Errorf("SURVEY_SCHEDULING_LOCAL_MINUTE must be between 0 and 59, got %d", c.SurveySchedulingLocalMinute)
	}
	if err := c.Authz.validate(); err != nil {
		return err
	}
	if err := c.SSO.validate(); err != nil {
		return err
	}
	if u := c.MCPOAuthJWKSURL; u != nil && ((u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
		return fmt.Errorf("MCP_OAUTH_JWKS_URL must be an absolute http(s) URL, got %q", u.String())
	}
	if err := c.S3.validate(); err != nil {
		return err
	}
	if err := c.AI.validate(); err != nil {
		return err
	}
	if err := c.Telemetry.validate(); err != nil {
		return err
	}
	if c.EncryptionKeyVersion < 1 || c.EncryptionKeyVersion > MaxEncryptionKeyVersion {
		return fmt.Errorf("ENCRYPTION_KEY_VERSION must be between 1 and %d, got %d", MaxEncryptionKeyVersion, c.EncryptionKeyVersion)
	}
	if _, ok := c.EncryptionPreviousKeys[c.EncryptionKeyVersion]; ok {
		return fmt.Errorf("ENCRYPTION_PREVIOUS_KEYS must not repeat the current ENCRYPTION_KEY_VERSION %d", c.EncryptionKeyVersion)
	}
	for _, s := range []struct {
		name  string
		value Secret
	}{{"NEXTAUTH_SECRET", c.NextAuthSecret}, {"BETTER_AUTH_SECRET", c.BetterAuthSecret}} {
		// HS256 needs a key at least as long as its hash; the JOSE library refuses a shorter one, so a short secret would silently invalidate every link survey token.
		if s.value != "" && len(s.value) < 32 {
			return fmt.Errorf("%s must be at least 32 characters (openssl rand -hex 32)", s.name)
		}
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
	for _, link := range []struct{ name, value string }{{"PRIVACY_URL", c.PrivacyURL}, {"IMPRINT_URL", c.ImprintURL}, {"TERMS_URL", c.TermsURL}} {
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
	DatabaseURL string `env:"DATABASE_URL"`
	// MigrateDatabaseURL is forma2's MIGRATE_DATABASE_URL: a connection with the privileges schema changes need, used for migrations only while the processes keep DATABASE_URL.
	MigrateDatabaseURL string     `env:"MIGRATE_DATABASE_URL"`
	LogLevel           slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat          string     `env:"LOG_FORMAT" envDefault:"json"`
}

// LoadForMigrate reads only what `forma migrate` needs, so a migration job can run with the database credentials alone. Its DatabaseURL is MIGRATE_DATABASE_URL when that is set (ignoring surrounding whitespace, as forma2 did), else DATABASE_URL.
func LoadForMigrate() (Config, error) {
	e, err := env.ParseAs[migrateEnv]()
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if e.LogFormat != "json" && e.LogFormat != "text" {
		return Config{}, fmt.Errorf("load config: LOG_FORMAT must be json or text, got %q", e.LogFormat)
	}
	databaseURL := strings.TrimSpace(e.MigrateDatabaseURL)
	if databaseURL == "" {
		databaseURL = strings.TrimSpace(e.DatabaseURL)
	}
	if databaseURL == "" {
		return Config{}, errors.New("load config: MIGRATE_DATABASE_URL or DATABASE_URL is required")
	}
	return Config{DatabaseURL: databaseURL, LogLevel: e.LogLevel, LogFormat: e.LogFormat}, nil
}

// Startup is what `forma start`, the container's default command, decides before it serves: forma2's entrypoint switches, under their names.
type Startup struct {
	// SkipMigration leaves the schema to a separate migration job (Helm, docker compose) instead of migrating before serving, forma2's SKIP_STARTUP_MIGRATION.
	SkipMigration bool
	// WorkerEnabled runs the background job worker in the same process as the HTTP server, as forma2's web container did unless BULLMQ_WORKER_ENABLED was 0. Deployments that run `forma worker` separately turn it off.
	WorkerEnabled bool
}

type startupEnv struct {
	SkipStartupMigration string `env:"SKIP_STARTUP_MIGRATION"`
	WorkerEnabled        bool   `env:"BULLMQ_WORKER_ENABLED" envDefault:"1"`
}

// LoadStartup reads the switches of `forma start`. SKIP_STARTUP_MIGRATION keeps forma2's spelling, "true", which its chart and compose file set; 1 and 0 are accepted too, and anything else fails the start.
func LoadStartup() (Startup, error) {
	e, err := env.ParseAsWithOptions[startupEnv](parseOptions)
	if err != nil {
		return Startup{}, fmt.Errorf("load config: %w", err)
	}
	s := Startup{WorkerEnabled: e.WorkerEnabled}
	switch e.SkipStartupMigration {
	case "true", "1":
		s.SkipMigration = true
	case "", "false", "0":
	default:
		return Startup{}, fmt.Errorf("load config: SKIP_STARTUP_MIGRATION must be true or false, got %q", e.SkipStartupMigration)
	}
	return s, nil
}

type healthcheckEnv struct {
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":3000"`
}

// LoadForHealthcheck reads only the listen address, which `forma healthcheck` probes.
func LoadForHealthcheck() (Config, error) {
	e, err := env.ParseAs[healthcheckEnv]()
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return Config{HTTPAddr: e.HTTPAddr}, nil
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
