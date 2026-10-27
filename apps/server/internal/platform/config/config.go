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
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
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
	// IgnoreForma2Data starts the server on a database whose forma2 "User" table has rows while this server has no user yet, which it otherwise refuses (database.RefuseUnreadForma2Data): forma2's data is then left unread, and the first sign-up becomes the administrator.
	IgnoreForma2Data bool `env:"FORMA_IGNORE_FORMA2_DATA" envDefault:"0"`

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
	// LinkSurveyTokenSecret signs the link survey email verification tokens, and the PIN tokens unless LinkSurveyPinTokenSecret is set; changing one invalidates the links already sent. Without them the tokens are signed with a secret derived from ENCRYPTION_KEY. They were forma2's NEXTAUTH_SECRET and BETTER_AUTH_SECRET, still read under those names (renamedVariables).
	LinkSurveyTokenSecret    Secret `env:"LINK_SURVEY_TOKEN_SECRET"`
	LinkSurveyPinTokenSecret Secret `env:"LINK_SURVEY_PIN_TOKEN_SECRET"`

	// RateLimitingDisabled turns every rate limit into a no-op that allows the request.
	RateLimitingDisabled bool `env:"RATE_LIMITING_DISABLED" envDefault:"0"`
	// GatewayRateLimiting declares a gateway in front of the server that enforces forma2's Envoy rate-limit policy set, forma2's GATEWAY_RATE_LIMITING: the routes that policy covers (ratelimit.CoveredByGateway) then skip their in-app limit. Off by default, because the compose stack runs no gateway.
	GatewayRateLimiting bool `env:"GATEWAY_RATE_LIMITING" envDefault:"0"`
	// TrustedProxyHopCount is how many reverse proxies in front of the server append to X-Forwarded-For; the client address is the entry that many places from the right. 0 means no proxy: the address is the TCP peer. Setting it higher than the real chain lets a caller choose their own address by prepending entries.
	TrustedProxyHopCount int `env:"TRUSTED_PROXY_HOP_COUNT" envDefault:"1"`
	// ClientIPHeader names a request header that carries the client address, set by a proxy that overwrites whatever the caller sent (Cloudflare's CF-Connecting-IP, for example), for proxy chains where the client address never reaches X-Forwarded-For. It is read only when the address TrustedProxyHopCount selects (the TCP peer with 0, otherwise the X-Forwarded-For entry that many places from the right: the party that connected to the outermost trusted proxy) is inside TrustedProxyCIDRs; every other request, and every request without the header, keeps that address as before. Unset, the default, no header is read. It must be set together with TrustedProxyCIDRs.
	ClientIPHeader string `env:"CLIENT_IP_HEADER"`
	// TrustedProxyCIDRs are the addresses, as comma-separated CIDRs or single addresses, of the proxies that set ClientIPHeader, as they appear where TrustedProxyHopCount looks: behind an ingress that appends to X-Forwarded-For, the address the ingress records for that proxy (cloudflared's, for Cloudflare Tunnel), not the ingress's own. Keep it to those addresses alone: anything else that can reach the server or the ingress from inside the range can choose its own client address.
	TrustedProxyCIDRs ProxyCIDRs `env:"TRUSTED_PROXY_CIDRS"`

	// SMTP delivery. Mail is sent only when both SMTPHost and SMTPPort are set; otherwise sends are skipped and reported as not sent.
	SMTPHost string `env:"SMTP_HOST"`
	SMTPPort string `env:"SMTP_PORT"`
	// SMTPSecureEnabled selects implicit TLS. Port 465 implies it regardless of this flag.
	SMTPSecureEnabled         bool   `env:"SMTP_SECURE_ENABLED" envDefault:"0"`
	SMTPUser                  string `env:"SMTP_USER"`
	SMTPPassword              string `env:"SMTP_PASSWORD"`
	SMTPAuthenticated         bool   `env:"SMTP_AUTHENTICATED" envDefault:"1"`
	SMTPRejectUnauthorizedTLS bool   `env:"SMTP_REJECT_UNAUTHORIZED_TLS" envDefault:"1"`
	MailFrom                  string `env:"MAIL_FROM" envDefault:"noreply@forma.yldm.ai"`
	MailFromName              string `env:"MAIL_FROM_NAME" envDefault:"Forma"`

	// Legal links rendered in the footer of every email and of the public survey pages; TermsURL appears on the survey pages only.
	PrivacyURL     string `env:"PRIVACY_URL"`
	ImprintURL     string `env:"IMPRINT_URL"`
	ImprintAddress string `env:"IMPRINT_ADDRESS"`
	TermsURL       string `env:"TERMS_URL"`

	// AuditLogEnabled emits audit events; AuditLogGetUserIP records the client address in them instead of "unknown".
	AuditLogEnabled   bool `env:"AUDIT_LOG_ENABLED" envDefault:"1"`
	AuditLogGetUserIP bool `env:"AUDIT_LOG_GET_USER_IP" envDefault:"0"`

	// RetentionSweepDryRun keeps the daily retention sweep (internal/retention) counting what each organization's retention windows would delete without deleting it, forma2's RETENTION_SWEEP_DRY_RUN. Deleting product data is irreversible, so it is on by default: an operator reads the counts (the sweep's log line, each owner's Data retention card) before turning it off.
	RetentionSweepDryRun bool `env:"RETENTION_SWEEP_DRY_RUN" envDefault:"1"`

	// DangerouslyAllowWebhookInternalURLs lets webhooks target private, loopback, link-local and metadata addresses and follow redirects, for self-hosted instances whose receivers live on the internal network, forma2's DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS. A host name that does not resolve is still refused.
	DangerouslyAllowWebhookInternalURLs bool `env:"DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS" envDefault:"0"`

	// WorkerConcurrency is how many jobs `forma worker` runs at once on each of its default and webhooks queues; with the authz queue's two slots, a worker runs up to 2*WorkerConcurrency+2 jobs at once (jobs.queues). 0 derives it from the CPU count, as forma2 did. Unset, it is taken from forma2's BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT when either is set (resolveWorkerConcurrency).
	WorkerConcurrency int `env:"WORKER_CONCURRENCY" envDefault:"0"`

	// SurveySchedulingTimeZone, SurveySchedulingLocalHour and SurveySchedulingLocalMinute are when, each day, scheduled surveys are published and closed: a publish or close date is snapped to that local time of its calendar day in the zone, forma2's SURVEY_SCHEDULING_* settings. An organization can choose its own zone (organizations.survey_scheduling_time_zone); this one is the default of those that do not.
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

	// MCPOAuthJWKSURL is where /api/mcp fetches the keys OAuth access tokens are signed with, forma2's MCP_OAUTH_JWKS_URL, for deployments that verify against another instance's authorization server. It changes no public URL: the issuer, the redirects and the token audience stay derived from WEBAPP_URL and AUTH_URL. Unset, the keys are read from the database the authorization server writes them to.
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

	// Unsplash is the survey editor's image background search.
	Unsplash UnsplashConfig

	// Plain is the in-product support chat, off unless PLAIN_APP_ID is set.
	Plain PlainConfig

	// Brevo is Forma Cloud's contact sync to its CRM, off unless BREVO_API_KEY is set.
	Brevo BrevoConfig

	LogLevel  LogLevel `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string   `env:"LOG_FORMAT" envDefault:"json"`

	// Warnings are deprecated settings found while loading, still honoured. They are written once the logger exists (logger.New), since configuration is read before it.
	Warnings []string
	// readFrom names the old variable a renamed one was read from (renamedVariables), so an error names what the operator set.
	readFrom map[string]string
}

// variable is the name a setting was read from: name, or the deprecated name it was given under.
func (c Config) variable(name string) string {
	if old, ok := c.readFrom[name]; ok {
		return old
	}
	return name
}

// LevelFatal and LevelAudit are forma2's two levels above error. Nothing is logged at fatal: LOG_LEVEL=fatal keeps only audit events, as it did in forma2. LevelAudit sits above every other level, so no LOG_LEVEL but audit's own filters audit events out.
const (
	LevelFatal = slog.Level(10)
	LevelAudit = slog.Level(12)
)

// LogLevel is LOG_LEVEL, one of forma2's level names: debug, info, warn, error, fatal (only what is above error) or audit (only audit events). Upper case is accepted too, which slog's own names allowed; anything else fails the start. forma2 defaulted to warn in production; the default here is info, the level the processes log their lifecycle at.
type LogLevel slog.Level

func (l *LogLevel) UnmarshalText(text []byte) error {
	switch strings.ToLower(string(text)) {
	case "debug":
		*l = LogLevel(slog.LevelDebug)
	case "info":
		*l = LogLevel(slog.LevelInfo)
	case "warn":
		*l = LogLevel(slog.LevelWarn)
	case "error":
		*l = LogLevel(slog.LevelError)
	case "fatal":
		*l = LogLevel(LevelFatal)
	case "audit":
		*l = LogLevel(LevelAudit)
	default:
		return fmt.Errorf("must be debug, info, warn, error, fatal or audit, got %q", text)
	}
	return nil
}

// Level makes LogLevel a slog.Leveler.
func (l LogLevel) Level() slog.Level { return slog.Level(l) }

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
	// AuthURL is the auth base URL, forma2's BETTER_AUTH_URL (else NEXTAUTH_URL), still read under those names (renamedVariables). It, else WEBAPP_URL, is where identity providers send the browser back and where the OAuth authorization server's issuer lives, so it must be the origin the redirect URIs were registered under. Leave it unset unless that origin is not WEBAPP_URL's.
	AuthURL *url.URL `env:"AUTH_URL"`
	// SAML offers SAML SSO, with the connection read from saml-connection/*.xml at start-up. forma2 switched it on by setting SAML_DATABASE_URL, the separate database it kept its SAML state in; that variable is still read as SAML_ENABLED=1 (renamedVariables), since here the connection lives in the main database.
	SAML bool `env:"SAML_ENABLED" envDefault:"0"`
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
func (s SSOConfig) SAMLEnabled() bool { return s.SAML }

// DefaultOrganization is the organization AUTH_SSO_DEFAULT_ORGANIZATION_ID names, if set.
func (s SSOConfig) DefaultOrganization() (uuid.UUID, bool) {
	return rowid.Parse(s.DefaultOrganizationID)
}

// validate checks the SSO settings; authURLName is the variable AuthURL was read from.
func (s SSOConfig) validate(authURLName string) error {
	if id := s.DefaultOrganizationID; id != "" {
		if _, ok := rowid.Parse(id); !ok {
			return fmt.Errorf("AUTH_SSO_DEFAULT_ORGANIZATION_ID must be an organization id (a UUID), got %q", id)
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
	if u := s.AuthURL; u != nil && ((u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
		return fmt.Errorf("%s must be an absolute http(s) URL, got %q", authURLName, u.String())
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

// CustomScripts says whether the public survey pages of this instance may carry custom head scripts, and why not.
type CustomScripts string

const (
	// CustomScriptsAvailable: a self-hosted instance whose survey pages have an origin of their own.
	CustomScriptsAvailable CustomScripts = "available"
	// CustomScriptsFormaCloud: the hosted service never runs them.
	CustomScriptsFormaCloud CustomScripts = "forma_cloud"
	// CustomScriptsNeedPublicURL: PUBLIC_URL is unset or names WEBAPP_URL's host, so the survey pages are served on the admin origin.
	CustomScriptsNeedPublicURL CustomScripts = "requires_public_url"
)

// CustomScripts is whether workspaces and surveys may set custom head scripts, which run unsanitized on the public survey pages. Forma Cloud never allows them. A self-hosted instance allows them only when PUBLIC_URL names a host other than WEBAPP_URL's: otherwise the survey pages share the admin origin, where a script runs with the session of every signed-in user who opens the survey (the session cookie is SameSite=Lax, and the /api/app CSRF check accepts any same-origin request), so anyone who may write one could act as the instance's owners. With a separate PUBLIC_URL the admin host refuses the survey pages (httpserver.PublicDomainGate), and the admin API answers no cross-origin request.
func (c Config) CustomScripts() CustomScripts {
	switch {
	case c.IsFormaCloud:
		return CustomScriptsFormaCloud
	case c.PublicURL == nil || c.WebAppURL == nil || strings.EqualFold(c.PublicURL.Host, c.WebAppURL.Host):
		return CustomScriptsNeedPublicURL
	default:
		return CustomScriptsAvailable
	}
}

// CustomScriptsRefusal is the message a write that sets custom head scripts gets on an instance that does not allow them, for the operator as much as for the user; empty when they are allowed.
func (c Config) CustomScriptsRefusal() string {
	switch c.CustomScripts() {
	case CustomScriptsFormaCloud:
		return "Custom head scripts are not available on Forma Cloud"
	case CustomScriptsNeedPublicURL:
		return "Custom head scripts are turned off on this instance: its survey pages are served on the admin domain, where a script could act as any signed-in user who opens the survey. The operator can turn them on by setting PUBLIC_URL to a separate domain for surveys."
	default:
		return ""
	}
}

// RecaptchaConfigured reports whether surveys may turn on reCAPTCHA spam protection.
func (c Config) RecaptchaConfigured() bool {
	return c.RecaptchaSiteKey != "" && c.RecaptchaSecretKey != ""
}

// LinkSurveyEmailSecret signs the link survey email verification tokens: LINK_SURVEY_TOKEN_SECRET, else the derived secret.
func (c Config) LinkSurveyEmailSecret() string {
	if c.LinkSurveyTokenSecret != "" {
		return string(c.LinkSurveyTokenSecret)
	}
	return c.derivedTokenSecret()
}

// LinkSurveyPinSecret signs the link survey PIN tokens: LINK_SURVEY_PIN_TOKEN_SECRET, else what signs the email tokens.
func (c Config) LinkSurveyPinSecret() string {
	if c.LinkSurveyPinTokenSecret != "" {
		return string(c.LinkSurveyPinTokenSecret)
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

// AppBasePath is the path the admin SPA is served under on WEBAPP_URL. Every page a signed-in or signing-in user navigates lives below it; the APIs, the public survey pages, the SDK bundles, stored files and the OAuth and health endpoints keep their root paths, because links already sent and snippets embedded on customer sites carry them.
const AppBasePath = "/app"

// AppURL is the admin SPA's base URL: WEBAPP_URL with AppBasePath appended to its path, without query, fragment or trailing slash. Every absolute link to an admin page (emails, redirects after a sign-in, an integration's or Stripe's return) is built on it; API, storage and SDK URLs stay on WEBAPP_URL.
func (c Config) AppURL() *url.URL {
	u := *c.WebAppURL
	u.Path = strings.TrimRight(u.Path, "/") + AppBasePath
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u
}

// AppPageURL is the absolute URL of the admin page at path, a path inside the app such as "/auth/login".
func (c Config) AppPageURL(path string) *url.URL {
	u := c.AppURL()
	u.Path += path
	return u
}

// SecureCookies reports whether cookies get the Secure attribute and the __Secure- name prefix, which browsers only accept over HTTPS.
func (c Config) SecureCookies() bool {
	return c.WebAppURL != nil && c.WebAppURL.Scheme == "https"
}

// renamedVariables are the variables that had kept forma2's names, under their current names, each with the old names still read in its place, in order of precedence. An old name is honoured only while the current one is unset, and either way earns a deprecation warning at start-up (Config.Warnings); the old names will stop being read in a later release. value turns an old name's value into the current one's (nil keeps it as it is).
var renamedVariables = []struct {
	name  string
	old   []string
	value func(string) string
}{
	{name: "AUTH_URL", old: []string{"BETTER_AUTH_URL", "NEXTAUTH_URL"}},
	{name: "LINK_SURVEY_TOKEN_SECRET", old: []string{"NEXTAUTH_SECRET"}},
	{name: "LINK_SURVEY_PIN_TOKEN_SECRET", old: []string{"BETTER_AUTH_SECRET"}},
	{name: "WORKER_ENABLED", old: []string{"BULLMQ_WORKER_ENABLED"}},
	// Any value of forma2's SAML_DATABASE_URL switched SAML on.
	{name: "SAML_ENABLED", old: []string{"SAML_DATABASE_URL"}, value: func(string) string { return "1" }},
}

// environment is the process environment with every renamed variable resolved: an old name's value is given to the current name when that is unset or empty. It also returns which variable each current name was read from (only those read from an old name) and the deprecation warnings, one per old name set.
func environment() (vars, readFrom map[string]string, warnings []string) {
	vars = env.ToMap(os.Environ())
	readFrom = map[string]string{}
	for _, r := range renamedVariables {
		source := r.name
		for _, old := range r.old {
			value := vars[old]
			if value == "" {
				continue
			}
			if vars[r.name] != "" {
				warnings = append(warnings, fmt.Sprintf("%s is deprecated and ignored because %s is set; remove it", old, source))
				continue
			}
			if r.value != nil {
				value = r.value(value)
			}
			vars[r.name], readFrom[r.name], source = value, old, old
			warnings = append(warnings, fmt.Sprintf("%s is deprecated; rename it to %s", old, r.name))
		}
	}
	return vars, readFrom, warnings
}

// optionsFor is parseOptions reading vars instead of the process environment.
func optionsFor(vars map[string]string) env.Options {
	opts := parseOptions
	opts.Environment = vars
	return opts
}

// Load reads the environment. It fails on the first missing or malformed variable so a misconfigured pod never starts.
func Load() (Config, error) {
	vars, readFrom, warnings := environment()
	cfg, err := env.ParseAsWithOptions[Config](optionsFor(vars))
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	cfg.Warnings = warnings
	cfg.readFrom = readFrom
	if cfg.DatabaseURL, err = databaseURL("DATABASE_URL", cfg.DatabaseURL, true); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.resolveWorkerConcurrency(); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// prismaOnlyParams are the connection URL keys forma2's Prisma adapter (packages/database/src/prisma-adapter.ts) consumed itself and removed before node-postgres saw the URL: pool sizing and waits (connection_limit, pool_timeout, max_connection_lifetime, max_idle_connection_lifetime), Prisma engine settings (socket_timeout, statement_cache_size), node-postgres's client-side query_timeout, and pgbouncer=true, with which Prisma avoided named prepared statements. None of them means anything to pgx, which would send each one to the server as a run-time setting that PostgreSQL and PgBouncer reject, so they are dropped. Named prepared statements need no switch here: PgBouncer 1.21 and later tracks them in transaction pooling (max_prepared_statements, 200 by default).
var prismaOnlyParams = map[string]bool{
	"connection_limit":             true,
	"pool_timeout":                 true,
	"max_connection_lifetime":      true,
	"max_idle_connection_lifetime": true,
	"socket_timeout":               true,
	"statement_cache_size":         true,
	"query_timeout":                true,
	"pgbouncer":                    true,
}

// databaseURL accepts a connection URL written for forma2 and returns one pgx connects with. Prisma took the schema to use as a schema query parameter (forma2's .env.example, compose file and guides all appended ?schema=public): schema=public, where every table lives, is dropped, and any other schema fails the start. The keys in prismaOnlyParams are dropped. forma2 read statement_timeout and idle_in_transaction_session_timeout in seconds where PostgreSQL reads a bare number as milliseconds, so for the application's connections (appTimeouts) a bare number gets PostgreSQL's unit s and keeps forma2's meaning; for migrations and other operator runs they are dropped, as forma2's migration runner and backfill ignored them. Prisma's TLS keys sslaccept and sslidentity have no pgx counterpart and fail the start rather than change how the server's certificate is checked. Everything else, libpq's and pgx's own parameters included, is kept byte for byte. A key=value connection string is returned as it is.
func databaseURL(name, raw string, appTimeouts bool) (string, error) {
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://") {
		return raw, nil
	}
	base, query, ok := strings.Cut(raw, "?")
	if !ok {
		return raw, nil
	}
	query, fragment, hasFragment := strings.Cut(query, "#")
	params := strings.Split(query, "&")
	kept := make([]string, 0, len(params))
	for _, p := range params {
		k, v, _ := strings.Cut(p, "=")
		key, err := url.QueryUnescape(k)
		if err != nil {
			kept = append(kept, p)
			continue
		}
		switch key = strings.ToLower(key); {
		case key == "schema":
			if value, err := url.QueryUnescape(v); err != nil || value != "public" {
				return "", fmt.Errorf("%s sets schema=%s, Prisma's schema parameter: the Forma server keeps its tables in the public schema and takes no schema parameter. Remove it (schema=public is ignored)", name, v)
			}
		case prismaOnlyParams[key]:
		case key == "sslaccept":
			return "", fmt.Errorf("%s sets sslaccept, Prisma's certificate check, which the Forma server does not read: say it with sslmode instead (sslmode=require for accept_invalid_certs, sslmode=verify-full with sslrootcert for strict)", name)
		case key == "sslidentity":
			return "", fmt.Errorf("%s sets sslidentity, Prisma's PKCS#12 client certificate, which the Forma server does not read: give the certificate and key in PEM files as sslcert and sslkey instead", name)
		case key == "statement_timeout" || key == "idle_in_transaction_session_timeout":
			if !appTimeouts {
				continue
			}
			if v != "" && strings.Trim(v, "0123456789") == "" {
				p += "s"
			}
			kept = append(kept, p)
		default:
			kept = append(kept, p)
		}
	}
	out := base
	if len(kept) > 0 {
		out += "?" + strings.Join(kept, "&")
	}
	if hasFragment {
		out += "#" + fragment
	}
	return out, nil
}

// forma2's BullMQ worker settings. Its parallelism was BULLMQ_WORKER_COUNT workers of BULLMQ_WORKER_CONCURRENCY jobs each.
type bullMQEnv struct {
	Concurrency int `env:"BULLMQ_WORKER_CONCURRENCY"`
	Count       int `env:"BULLMQ_WORKER_COUNT"`
}

// resolveWorkerConcurrency honours the deprecated BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT, which an older .env may still set: with WORKER_CONCURRENCY unset, the worker runs count x concurrency jobs at once, a missing one taking its default (one worker; DefaultWorkerConcurrency jobs). WORKER_CONCURRENCY wins when both styles are set. Either way the deprecation is reported once at start-up.
func (c *Config) resolveWorkerConcurrency() error {
	bull, err := env.ParseAs[bullMQEnv]()
	if err != nil {
		return err
	}
	concurrencySet, countSet := os.Getenv("BULLMQ_WORKER_CONCURRENCY") != "", os.Getenv("BULLMQ_WORKER_COUNT") != ""
	if !concurrencySet && !countSet {
		return nil
	}
	if (concurrencySet && bull.Concurrency < 1) || (countSet && bull.Count < 1) {
		return fmt.Errorf("BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT must be at least 1, got %q and %q", os.Getenv("BULLMQ_WORKER_CONCURRENCY"), os.Getenv("BULLMQ_WORKER_COUNT"))
	}
	if os.Getenv("WORKER_CONCURRENCY") != "" {
		c.Warnings = append(c.Warnings, "BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT are ignored because WORKER_CONCURRENCY is set; remove them")
		return nil
	}
	if !concurrencySet {
		bull.Concurrency = DefaultWorkerConcurrency()
	}
	if !countSet {
		bull.Count = 1
	}
	c.WorkerConcurrency = bull.Concurrency * bull.Count
	c.Warnings = append(c.Warnings, fmt.Sprintf("BULLMQ_WORKER_CONCURRENCY and BULLMQ_WORKER_COUNT are deprecated; set WORKER_CONCURRENCY=%d instead", c.WorkerConcurrency))
	return nil
}

// DefaultWorkerConcurrency is forma2's default for how many jobs run at once, between two and four by CPU count. It now sizes each of the default and webhooks queues, so a worker runs up to twice that plus the authz and ai queues' two each: every running job can hold a database connection, so at four that is up to twelve connections from the jobs alone; size DATABASE_MAX_OPEN_CONNS for that total.
func DefaultWorkerConcurrency() int {
	return max(2, min(4, runtime.NumCPU()))
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
	if err := c.validateClientIPHeader(); err != nil {
		return err
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
	if err := c.SSO.validate(c.variable("AUTH_URL")); err != nil {
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
	if err := c.Brevo.validate(); err != nil {
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
	}{{c.variable("LINK_SURVEY_TOKEN_SECRET"), c.LinkSurveyTokenSecret}, {c.variable("LINK_SURVEY_PIN_TOKEN_SECRET"), c.LinkSurveyPinTokenSecret}} {
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

// headerName is an HTTP header field name (RFC 9110 token characters).
var headerName = regexp.MustCompile("^[A-Za-z0-9!#$%&'*+.^_`|~-]+$")

// validateClientIPHeader checks the trusted-header mode: the header and the proxies allowed to set it come together, the header is a single-address header, and no range trusts every peer.
func (c Config) validateClientIPHeader() error {
	if (c.ClientIPHeader == "") != (len(c.TrustedProxyCIDRs) == 0) {
		return errors.New("CLIENT_IP_HEADER and TRUSTED_PROXY_CIDRS must be set together: the header is read only when the address TRUSTED_PROXY_HOP_COUNT selects is inside TRUSTED_PROXY_CIDRS")
	}
	if c.ClientIPHeader == "" {
		return nil
	}
	if !headerName.MatchString(c.ClientIPHeader) {
		return fmt.Errorf("CLIENT_IP_HEADER must be a header name such as CF-Connecting-IP, got %q", c.ClientIPHeader)
	}
	if strings.EqualFold(c.ClientIPHeader, "X-Forwarded-For") || strings.EqualFold(c.ClientIPHeader, "Forwarded") {
		return fmt.Errorf("CLIENT_IP_HEADER must name a header that carries one address; %s is a chain, which TRUSTED_PROXY_HOP_COUNT reads", c.ClientIPHeader)
	}
	for _, p := range c.TrustedProxyCIDRs {
		if p.Bits() == 0 {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS must not contain %s: it trusts every peer, so any caller could set CLIENT_IP_HEADER to an address of its choice", p)
		}
	}
	return nil
}

// ProxyCIDRs are the networks of trusted proxies, read from comma-separated CIDRs or single addresses.
type ProxyCIDRs []netip.Prefix

// UnmarshalText reads "10.42.0.0/16, 192.0.2.7, 2001:db8::/48": a bare address is a network of that one address. IPv4-mapped IPv6 forms are stored as IPv4, the form a peer address is compared in.
func (p *ProxyCIDRs) UnmarshalText(text []byte) error {
	var out ProxyCIDRs
	for entry := range strings.SplitSeq(string(text), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		var prefix netip.Prefix
		if strings.Contains(entry, "/") {
			parsed, err := netip.ParsePrefix(entry)
			if err != nil {
				return fmt.Errorf("%q is not a CIDR such as 10.42.0.0/16", entry)
			}
			prefix = parsed
		} else {
			addr, err := netip.ParseAddr(entry)
			if err != nil || addr.Zone() != "" {
				return fmt.Errorf("%q is not an IP address or a CIDR such as 10.42.0.0/16", entry)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		if addr := prefix.Addr(); addr.Is4In6() {
			if prefix.Bits() < 96 {
				return fmt.Errorf("%q mixes IPv4-mapped and plain IPv6 addresses; write the IPv4 network instead", entry)
			}
			prefix = netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96)
		}
		out = append(out, prefix.Masked())
	}
	*p = out
	return nil
}

// Contains reports whether addr is inside one of the networks. An IPv4-mapped IPv6 address is compared as IPv4.
func (p ProxyCIDRs) Contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range p {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
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
	MigrateDatabaseURL string   `env:"MIGRATE_DATABASE_URL"`
	LogLevel           LogLevel `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat          string   `env:"LOG_FORMAT" envDefault:"json"`
	// IgnoreForma2Data is Config.IgnoreForma2Data: `forma migrate` refuses a database holding forma2's unread data as the server does, so that no migration runs there before the operator decides.
	IgnoreForma2Data bool `env:"FORMA_IGNORE_FORMA2_DATA" envDefault:"0"`
}

// LoadForMigrate reads only what `forma migrate` needs, so a migration job can run with the database credentials alone. Its DatabaseURL is MIGRATE_DATABASE_URL when that is set (ignoring surrounding whitespace, as forma2 did), else DATABASE_URL.
func LoadForMigrate() (Config, error) {
	e, err := env.ParseAsWithOptions[migrateEnv](parseOptions)
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	if e.LogFormat != "json" && e.LogFormat != "text" {
		return Config{}, fmt.Errorf("load config: LOG_FORMAT must be json or text, got %q", e.LogFormat)
	}
	name, raw := "MIGRATE_DATABASE_URL", strings.TrimSpace(e.MigrateDatabaseURL)
	if raw == "" {
		name, raw = "DATABASE_URL", strings.TrimSpace(e.DatabaseURL)
	}
	if raw == "" {
		return Config{}, errors.New("load config: MIGRATE_DATABASE_URL or DATABASE_URL is required")
	}
	dsn, err := databaseURL(name, raw, false)
	if err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return Config{DatabaseURL: dsn, LogLevel: e.LogLevel, LogFormat: e.LogFormat, IgnoreForma2Data: e.IgnoreForma2Data}, nil
}

// Startup is what `forma start`, the container's default command, decides before it serves: forma2's entrypoint switches, under their names.
type Startup struct {
	// SkipMigration leaves the schema to a separate migration job (Helm, docker compose) instead of migrating before serving, forma2's SKIP_STARTUP_MIGRATION.
	SkipMigration bool
	// WorkerEnabled runs the background job worker in the same process as the HTTP server, as forma2's web container did unless BULLMQ_WORKER_ENABLED was 0. Deployments that run `forma worker` separately turn it off with WORKER_ENABLED=0.
	WorkerEnabled bool
}

type startupEnv struct {
	SkipStartupMigration string `env:"SKIP_STARTUP_MIGRATION"`
	WorkerEnabled        bool   `env:"WORKER_ENABLED" envDefault:"1"`
}

// LoadStartup reads the switches of `forma start`. SKIP_STARTUP_MIGRATION keeps forma2's spelling, "true", which its chart and compose file set; 1 and 0 are accepted too, and anything else fails the start. WORKER_ENABLED is also read as BULLMQ_WORKER_ENABLED; the deprecation warning is Load's, which `forma start` runs first.
func LoadStartup() (Startup, error) {
	vars, _, _ := environment()
	e, err := env.ParseAsWithOptions[startupEnv](optionsFor(vars))
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
	LogLevel    LogLevel `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat   string   `env:"LOG_FORMAT" envDefault:"json"`
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
	if e.DatabaseURL, err = databaseURL("DATABASE_URL", e.DatabaseURL, false); err != nil {
		return Config{}, fmt.Errorf("load config: %w", err)
	}
	return Config{DatabaseURL: e.DatabaseURL, DatabaseMaxOpenConns: 4, Authz: e.Authz, LogLevel: e.LogLevel, LogFormat: e.LogFormat}, nil
}
