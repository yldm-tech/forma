package config

import (
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// unsetEnv removes a variable for the duration of the test; t.Setenv first records the value to restore.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("WEBAPP_URL", "http://localhost:3000")
	t.Setenv("DATABASE_URL", "postgres://forma:forma@localhost:5432/forma?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("ENCRYPTION_KEY", testHexKey)
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
	if cfg.LogLevel.Level() != slog.LevelInfo {
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
	if cfg.RateLimitingDisabled || cfg.GatewayRateLimiting {
		t.Errorf("RateLimitingDisabled %v, GatewayRateLimiting %v; want both off by default", cfg.RateLimitingDisabled, cfg.GatewayRateLimiting)
	}
	if cfg.WorkerConcurrency != 0 || len(cfg.Warnings) != 0 {
		t.Errorf("WorkerConcurrency %d, warnings %q; want 0 and none", cfg.WorkerConcurrency, cfg.Warnings)
	}
	if !cfg.AuditLogEnabled || cfg.AuditLogGetUserIP {
		t.Errorf("audit flags = %v/%v, want enabled without the user IP", cfg.AuditLogEnabled, cfg.AuditLogGetUserIP)
	}
	if !cfg.RetentionSweepDryRun {
		t.Error("RetentionSweepDryRun = false, want the retention sweep to only count by default")
	}
	if !cfg.SMTPAuthenticated || !cfg.SMTPRejectUnauthorizedTLS || cfg.SMTPSecureEnabled {
		t.Errorf("SMTP flags = auth %v, reject-unauthorized %v, secure %v; want true, true, false", cfg.SMTPAuthenticated, cfg.SMTPRejectUnauthorizedTLS, cfg.SMTPSecureEnabled)
	}
	if cfg.MailFrom != "noreply@forma.yldm.ai" || cfg.MailFromName != "Forma" {
		t.Errorf("mail from = %q <%s>, want Forma <noreply@forma.yldm.ai>", cfg.MailFromName, cfg.MailFrom)
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
	if cfg.InviteDisabled || cfg.InviteRateLimitPer24Hours != 50 || cfg.SignupDomainCheckOnInvites {
		t.Errorf("invites disabled %v, limit %d, domain check on invites %v; want enabled, 50, false", cfg.InviteDisabled, cfg.InviteRateLimitPer24Hours, cfg.SignupDomainCheckOnInvites)
	}
	if cfg.SurveySchedulingTimeZone != "Europe/Berlin" || cfg.SurveySchedulingLocalHour != 0 || cfg.SurveySchedulingLocalMinute != 0 {
		t.Errorf("survey scheduling %s %02d:%02d, want Europe/Berlin 00:00", cfg.SurveySchedulingTimeZone, cfg.SurveySchedulingLocalHour, cfg.SurveySchedulingLocalMinute)
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
	t.Setenv("INVITE_DISABLED", "1")
	t.Setenv("INVITE_RATE_LIMIT_PER_24_HOURS", "7")
	t.Setenv("SIGNUP_DOMAIN_CHECK_ON_INVITES", "1")
	t.Setenv("GATEWAY_RATE_LIMITING", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RateLimitingDisabled || cfg.SMTPAuthenticated || !cfg.SMTPConfigured() || !cfg.GatewayRateLimiting {
		t.Fatalf("cfg = %+v, want rate limiting disabled, a gateway declared, SMTP unauthenticated and configured", cfg)
	}
	if !cfg.InviteDisabled || cfg.InviteRateLimitPer24Hours != 7 || !cfg.SignupDomainCheckOnInvites {
		t.Fatalf("invites disabled %v, limit %d, domain check on invites %v; want true, 7, true", cfg.InviteDisabled, cfg.InviteRateLimitPer24Hours, cfg.SignupDomainCheckOnInvites)
	}
}

func TestLoadRejectsMissingRequired(t *testing.T) {
	for _, name := range []string{"WEBAPP_URL", "DATABASE_URL", "REDIS_URL", "ENCRYPTION_KEY"} {
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
		"unknown log format":       {"LOG_FORMAT", "yaml"},
		"zero pool size":           {"DATABASE_MAX_OPEN_CONNS", "0"},
		"unparseable duration":     {"SHUTDOWN_TIMEOUT", "soon"},
		"unparseable level":        {"LOG_LEVEL", "loud"},
		"level with offset":        {"LOG_LEVEL", "info+2"},
		"pino trace level":         {"LOG_LEVEL", "trace"},
		"gateway flag spelled":     {"GATEWAY_RATE_LIMITING", "true"},
		"zero bullmq concurrency":  {"BULLMQ_WORKER_CONCURRENCY", "0"},
		"bullmq count text":        {"BULLMQ_WORKER_COUNT", "two"},
		"prisma schema":            {"DATABASE_URL", "postgres://forma@db/forma?schema=forma"},
		"hop count too high":       {"TRUSTED_PROXY_HOP_COUNT", "11"},
		"negative hop count":       {"TRUSTED_PROXY_HOP_COUNT", "-1"},
		"flag spelled true":        {"RATE_LIMITING_DISABLED", "true"},
		"non-numeric port":         {"SMTP_PORT", "smtp"},
		"port out of range":        {"SMTP_PORT", "70000"},
		"mail from with name":      {"MAIL_FROM", "Forma <noreply@example.com>"},
		"mail from not email":      {"MAIL_FROM", "noreply"},
		"relative privacy url":     {"PRIVACY_URL", "/privacy"},
		"relative terms url":       {"TERMS_URL", "terms"},
		"short token secret":       {"LINK_SURVEY_TOKEN_SECRET", "too-short"},
		"short pin token secret":   {"LINK_SURVEY_PIN_TOKEN_SECRET", "too-short"},
		"short nextauth secret":    {"NEXTAUTH_SECRET", "too-short"},
		"short better auth secret": {"BETTER_AUTH_SECRET", "too-short"},
		"negative concurrency":     {"WORKER_CONCURRENCY", "-1"},
		"reset lifetime short":     {"PASSWORD_RESET_TOKEN_LIFETIME_MINUTES", "4"},
		"reset lifetime long":      {"PASSWORD_RESET_TOKEN_LIFETIME_MINUTES", "121"},
		"zero session max age":     {"SESSION_MAX_AGE", "0"},
		"session max age text":     {"SESSION_MAX_AGE", "1d"},
		"default org uppercase":    {"AUTH_SSO_DEFAULT_ORGANIZATION_ID", "CLXYZ123ABC"},
		"default org braced uuid":  {"AUTH_SSO_DEFAULT_ORGANIZATION_ID", "{0199e0a1-7b1c-7a8e-9d1e-0f5c3a7b2d10}"},
		"default org role":         {"AUTH_SSO_DEFAULT_ORGANIZATION_ROLE", "admin"},
		"relative auth url":        {"AUTH_URL", "/api/auth"},
		"better auth url relative": {"BETTER_AUTH_URL", "/api/auth"},
		"nextauth url scheme":      {"NEXTAUTH_URL", "ftp://auth.example.com"},
		"saml flag spelled":        {"SAML_ENABLED", "true"},
		"zero invite limit":        {"INVITE_RATE_LIMIT_PER_24_HOURS", "0"},
		"invite limit text":        {"INVITE_RATE_LIMIT_PER_24_HOURS", "many"},
		"invite flag spelled":      {"INVITE_DISABLED", "yes"},
		"unknown time zone":        {"SURVEY_SCHEDULING_TIME_ZONE", "Mars/Olympus"},
		"brevo list text":          {"BREVO_LIST_ID", "newsletter"},
		"brevo list zero":          {"BREVO_LIST_ID", "0"},
		"local time zone":          {"SURVEY_SCHEDULING_TIME_ZONE", "Local"},
		"scheduling hour":          {"SURVEY_SCHEDULING_LOCAL_HOUR", "24"},
		"scheduling minute":        {"SURVEY_SCHEDULING_LOCAL_MINUTE", "60"},
		"s3 endpoint scheme":       {"S3_ENDPOINT_URL", "s3://bucket"},
		"s3 endpoint path":         {"S3_ENDPOINT_URL", "https://gateway.example.com/forma"},
		"s3 endpoint relative":     {"S3_ENDPOINT_URL", "localhost:9000"},
		"s3 path style flag":       {"S3_FORCE_PATH_STYLE", "true"},
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

// `forma migrate` runs the forma2 refusal before migrating, so it reads the switch that skips it, with the server's 1/0 syntax.
func TestLoadForMigrateReadsIgnoreForma2Data(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://forma:forma@localhost:5432/forma?sslmode=disable")
	unsetEnv(t, "FORMA_IGNORE_FORMA2_DATA")
	if cfg, err := LoadForMigrate(); err != nil || cfg.IgnoreForma2Data {
		t.Fatalf("unset: IgnoreForma2Data %v, err %v; want false", cfg.IgnoreForma2Data, err)
	}
	t.Setenv("FORMA_IGNORE_FORMA2_DATA", "1")
	if cfg, err := LoadForMigrate(); err != nil || !cfg.IgnoreForma2Data {
		t.Fatalf("1: IgnoreForma2Data %v, err %v; want true", cfg.IgnoreForma2Data, err)
	}
	t.Setenv("FORMA_IGNORE_FORMA2_DATA", "yes")
	if _, err := LoadForMigrate(); err == nil {
		t.Fatal("LoadForMigrate accepted FORMA_IGNORE_FORMA2_DATA=yes")
	}
}

func TestLoadForMigratePrefersMigrateDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://app@db/forma")
	t.Setenv("MIGRATE_DATABASE_URL", " postgres://owner@db/forma ")
	cfg, err := LoadForMigrate()
	if err != nil {
		t.Fatalf("LoadForMigrate: %v", err)
	}
	if cfg.DatabaseURL != "postgres://owner@db/forma" {
		t.Fatalf("DatabaseURL = %q, want the trimmed MIGRATE_DATABASE_URL", cfg.DatabaseURL)
	}

	t.Setenv("MIGRATE_DATABASE_URL", " ")
	if cfg, err = LoadForMigrate(); err != nil || cfg.DatabaseURL != "postgres://app@db/forma" {
		t.Fatalf("blank MIGRATE_DATABASE_URL: cfg %q, err %v; want DATABASE_URL", cfg.DatabaseURL, err)
	}

	t.Setenv("DATABASE_URL", "")
	t.Setenv("MIGRATE_DATABASE_URL", "postgres://owner@db/forma")
	if cfg, err = LoadForMigrate(); err != nil || cfg.DatabaseURL != "postgres://owner@db/forma" {
		t.Fatalf("MIGRATE_DATABASE_URL alone: cfg %q, err %v", cfg.DatabaseURL, err)
	}
}

func TestLoadStartup(t *testing.T) {
	cases := []struct {
		skip, worker      string
		wantSkip, wantRun bool
		wantErr           bool
	}{
		{"", "", false, true, false},
		{"true", "", true, true, false},
		{"1", "0", true, false, false},
		{"false", "1", false, true, false},
		{"0", "", false, true, false},
		{"yes", "", false, false, true},
		{"TRUE", "", false, false, true},
		{"", "true", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.skip+"/"+tc.worker, func(t *testing.T) {
			t.Setenv("SKIP_STARTUP_MIGRATION", tc.skip)
			unsetEnv(t, "BULLMQ_WORKER_ENABLED")
			t.Setenv("WORKER_ENABLED", tc.worker)
			if tc.worker == "" {
				unsetEnv(t, "WORKER_ENABLED")
			}
			s, err := LoadStartup()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LoadStartup accepted SKIP_STARTUP_MIGRATION=%q WORKER_ENABLED=%q", tc.skip, tc.worker)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadStartup: %v", err)
			}
			if s.SkipMigration != tc.wantSkip || s.WorkerEnabled != tc.wantRun {
				t.Fatalf("startup = %+v, want skip %v worker %v", s, tc.wantSkip, tc.wantRun)
			}
		})
	}
}

func TestLoadForHealthcheck(t *testing.T) {
	unsetEnv(t, "HTTP_ADDR")
	if cfg, err := LoadForHealthcheck(); err != nil || cfg.HTTPAddr != ":3000" {
		t.Fatalf("default: %q, %v", cfg.HTTPAddr, err)
	}
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	if cfg, err := LoadForHealthcheck(); err != nil || cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("HTTP_ADDR: %q, %v", cfg.HTTPAddr, err)
	}
}

func TestLoadAuthz(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Authz.Enabled || cfg.Authz.Insecure || cfg.Authz.Consistency != "fully_consistent" || cfg.UserManagementMinimumRole != "manager" {
		t.Fatalf("defaults = %+v, role %q; want disabled, TLS, fully_consistent, manager", cfg.Authz, cfg.UserManagementMinimumRole)
	}

	t.Setenv("AUTHZED_ENABLED", "1")
	t.Setenv("AUTHZED_ENDPOINT", "[::1]:50051")
	t.Setenv("AUTHZED_TOKEN", "secret")
	t.Setenv("AUTHZED_INSECURE", "1")
	t.Setenv("AUTHZED_CONSISTENCY", "minimize_latency")
	t.Setenv("USER_MANAGEMENT_MINIMUM_ROLE", "disabled")
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := AuthzConfig{Enabled: true, Endpoint: "[::1]:50051", Token: "secret", Insecure: true, Consistency: "minimize_latency"}
	if cfg.Authz != want || cfg.UserManagementMinimumRole != "disabled" {
		t.Fatalf("Authz = %+v, role %q; want %+v, disabled", cfg.Authz, cfg.UserManagementMinimumRole, want)
	}
}

// AUTHZED_ENABLED and AUTHZED_INSECURE take true/false as well as 1/0, the spellings existing .env files use; every other switch stays 1/0.
func TestLoadAcceptsForma2AuthzedBooleans(t *testing.T) {
	for _, tc := range []struct {
		enabled, insecure string
		want              bool
	}{{"true", "true", true}, {"false", "false", false}, {"1", "1", true}, {"0", "0", false}} {
		setRequired(t)
		t.Setenv("AUTHZED_ENABLED", tc.enabled)
		t.Setenv("AUTHZED_INSECURE", tc.insecure)
		t.Setenv("AUTHZED_ENDPOINT", "localhost:50051")
		t.Setenv("AUTHZED_TOKEN", "secret")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load with %q/%q: %v", tc.enabled, tc.insecure, err)
		}
		if bool(cfg.Authz.Enabled) != tc.want || bool(cfg.Authz.Insecure) != tc.want {
			t.Errorf("%q/%q read as %v/%v, want %v", tc.enabled, tc.insecure, cfg.Authz.Enabled, cfg.Authz.Insecure, tc.want)
		}
		if cfg, err := LoadForAuthz(); err != nil || bool(cfg.Authz.Enabled) != tc.want {
			t.Errorf("LoadForAuthz with %q: %+v, %v", tc.enabled, cfg.Authz, err)
		}
	}
	setRequired(t)
	t.Setenv("S3_FORCE_PATH_STYLE", "true")
	if _, err := Load(); err == nil {
		t.Error("S3_FORCE_PATH_STYLE=true accepted; only the AUTHZED_* switches take true/false")
	}
}

func TestLoadRejectsInvalidAuthz(t *testing.T) {
	cases := map[string]map[string]string{
		"enabled without endpoint":  {"AUTHZED_ENABLED": "1", "AUTHZED_TOKEN": "secret"},
		"enabled without token":     {"AUTHZED_ENABLED": "1", "AUTHZED_ENDPOINT": "localhost:50051"},
		"enabled spelled yes":       {"AUTHZED_ENABLED": "yes"},
		"insecure spelled TRUE":     {"AUTHZED_INSECURE": "TRUE"},
		"blank token":               {"AUTHZED_TOKEN": "   "},
		"endpoint with scheme":      {"AUTHZED_ENDPOINT": "http://localhost:50051"},
		"endpoint with path":        {"AUTHZED_ENDPOINT": "localhost:50051/v1"},
		"endpoint with credentials": {"AUTHZED_ENDPOINT": "user@localhost:50051"},
		"endpoint without port":     {"AUTHZED_ENDPOINT": "localhost"},
		"endpoint port zero":        {"AUTHZED_ENDPOINT": "localhost:0"},
		"endpoint port too high":    {"AUTHZED_ENDPOINT": "localhost:65536"},
		"endpoint signed port":      {"AUTHZED_ENDPOINT": "localhost:+1"},
		"unbracketed ipv6":          {"AUTHZED_ENDPOINT": "::1:50051"},
		"bracketed hostname":        {"AUTHZED_ENDPOINT": "[localhost]:50051"},
		"endpoint with space":       {"AUTHZED_ENDPOINT": "local host:50051"},
		"unknown consistency":       {"AUTHZED_CONSISTENCY": "at_least_as_fresh"},
		"unknown management role":   {"USER_MANAGEMENT_MINIMUM_ROLE": "member"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			for k, v := range vars {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %v", vars)
			}
		})
	}
}

func TestLoadForAuthzNeedsTheDatabaseAndSpiceDB(t *testing.T) {
	t.Setenv("WEBAPP_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("DATABASE_URL", "postgres://forma:forma@localhost:5432/forma?sslmode=disable")
	t.Setenv("AUTHZED_ENABLED", "1")
	t.Setenv("AUTHZED_ENDPOINT", "localhost:50051")
	t.Setenv("AUTHZED_TOKEN", "secret")

	cfg, err := LoadForAuthz()
	if err != nil {
		t.Fatalf("LoadForAuthz: %v", err)
	}
	if !cfg.Authz.Enabled || cfg.Authz.Endpoint != "localhost:50051" || cfg.DatabaseMaxOpenConns < 1 {
		t.Fatalf("cfg = %+v", cfg)
	}

	t.Setenv("AUTHZED_TOKEN", "")
	if _, err := LoadForAuthz(); err == nil {
		t.Fatal("LoadForAuthz accepted an enabled SpiceDB without a token")
	}
}

func TestLoadSSO(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SSO.GoogleEnabled() || cfg.SSO.GitHubEnabled() || cfg.SSO.AzureADEnabled() || cfg.SSO.OIDCEnabled() || cfg.SSO.SAMLEnabled() || cfg.SSO.AuthURL != nil {
		t.Fatalf("SSO = %+v, want every provider off", cfg.SSO)
	}

	for k, v := range map[string]string{
		"GOOGLE_CLIENT_ID": "g", "GOOGLE_CLIENT_SECRET": "gs", "GITHUB_ID": "h", "GITHUB_SECRET": "hs",
		"AZUREAD_CLIENT_ID": "a", "AZUREAD_CLIENT_SECRET": "as", "AZUREAD_TENANT_ID": "common",
		"OIDC_CLIENT_ID": "o", "OIDC_CLIENT_SECRET": "os", "OIDC_ISSUER": "https://idp.example.com/realms/forma", "OIDC_DISPLAY_NAME": "Okta",
		"AUTH_SSO_DEFAULT_ORGANIZATION_ID": "0199e0a1-7b1c-7a8e-9d1e-0f5c3a7b2d10", "AUTH_SSO_DEFAULT_ORGANIZATION_ROLE": "member",
		"AUTH_URL": "https://auth.example.com/api/auth", "SAML_ENABLED": "1",
	} {
		t.Setenv(k, v)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.SSO
	if !s.GoogleEnabled() || !s.GitHubEnabled() || !s.AzureADEnabled() || !s.OIDCEnabled() || !s.SAMLEnabled() || s.AzureADTenantID != "common" || s.OIDCDisplayName != "Okta" ||
		s.DefaultOrganizationRole != "member" || s.AuthURL.Host != "auth.example.com" {
		t.Fatalf("SSO = %+v", s)
	}

	if id, ok := s.DefaultOrganization(); !ok || id.String() != "0199e0a1-7b1c-7a8e-9d1e-0f5c3a7b2d10" {
		t.Fatalf("DefaultOrganization = %s, %v", id, ok)
	}
	// Organization ids are UUIDs; a cuid2 names no organization, so it fails the start rather than being ignored.
	t.Setenv("AUTH_SSO_DEFAULT_ORGANIZATION_ID", "clxbivtla014iye2vfrn436xd")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AUTH_SSO_DEFAULT_ORGANIZATION_ID") {
		t.Fatalf("Load with a cuid2 organization id: %v", err)
	}
	t.Setenv("AUTH_SSO_DEFAULT_ORGANIZATION_ID", "0199e0a1-7b1c-7a8e-9d1e-0f5c3a7b2d10")

	t.Setenv("OIDC_ISSUER", "idp.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an OIDC_ISSUER that is not an absolute URL")
	}
	// The issuer is only checked when the provider is enabled.
	t.Setenv("OIDC_CLIENT_SECRET", "")
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadS3(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.S3.Configured() || cfg.S3.ForcePathStyle || cfg.S3.EndpointURL != nil {
		t.Fatalf("S3 = %+v, want storage unconfigured by default", cfg.S3)
	}

	for k, v := range map[string]string{
		"S3_ACCESS_KEY": "devrustfs-service", "S3_SECRET_KEY": "devrustfs-service123", "S3_REGION": "us-east-1",
		"S3_BUCKET_NAME": "forma", "S3_ENDPOINT_URL": "http://localhost:9000", "S3_FORCE_PATH_STYLE": "1",
	} {
		t.Setenv(k, v)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.S3
	if !s.Configured() || !s.ForcePathStyle || s.EndpointURL.Host != "localhost:9000" || s.Region != "us-east-1" || s.AccessKey != "devrustfs-service" || s.SecretKey != "devrustfs-service123" {
		t.Fatalf("S3 = %+v", s)
	}

	t.Setenv("S3_ENDPOINT_URL", "https://gateway.storjshare.io/")
	if _, err := Load(); err != nil {
		t.Fatalf("Load rejected an endpoint with a bare trailing slash: %v", err)
	}
}

func TestTelemetryDefaultsAreOff(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	tc := cfg.Telemetry
	if tc.OTelEnabled() || tc.SentryDSN != "" || tc.PostHogKey != "" || tc.UsageReportEnabled {
		t.Errorf("telemetry = %+v, want everything off by default", tc)
	}
	if tc.ServiceName != "forma" || tc.PrometheusPort != 9464 || tc.TracesSampler != "always_on" || tc.DeploymentEnvironment() != "production" {
		t.Errorf("telemetry defaults = %+v (environment %s)", tc, tc.DeploymentEnvironment())
	}
}

func TestTelemetryConfig(t *testing.T) {
	setRequired(t)
	t.Setenv("PROMETHEUS_ENABLED", "1")
	t.Setenv("NODE_ENV", "staging")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Telemetry.OTelEnabled() || cfg.Telemetry.DeploymentEnvironment() != "staging" {
		t.Errorf("telemetry = %+v", cfg.Telemetry)
	}
	t.Setenv("ENVIRONMENT", "eu-prod")
	if cfg, _ := Load(); cfg.Telemetry.DeploymentEnvironment() != "eu-prod" {
		t.Errorf("ENVIRONMENT did not win over NODE_ENV: %s", cfg.Telemetry.DeploymentEnvironment())
	}
	for name, value := range map[string]string{
		"PROMETHEUS_ENABLED":          "true",
		"PROMETHEUS_EXPORTER_PORT":    "0",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "collector:4318",
		"SENTRY_DSN":                  "not a dsn",
		"TELEMETRY_ENABLED":           "yes",
	} {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(name, value)
			if _, err := Load(); err == nil {
				t.Errorf("%s=%q was accepted", name, value)
			}
		})
	}
}

// LOG_LEVEL takes forma2's level names: fatal keeps only what is above error, which is audit events alone, and audit keeps audit events only.
func TestLoadLogLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"fatal": LevelFatal,
		"audit": LevelAudit,
		"WARN":  slog.LevelWarn,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			setRequired(t)
			t.Setenv("LOG_LEVEL", value)
			for name, load := range map[string]func() (Config, error){"Load": Load, "LoadForMigrate": LoadForMigrate, "LoadForAuthz": LoadForAuthz} {
				cfg, err := load()
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if cfg.LogLevel.Level() != want {
					t.Fatalf("%s: LogLevel = %v, want %v", name, cfg.LogLevel.Level(), want)
				}
			}
		})
	}
	if LevelFatal <= slog.LevelError || LevelAudit <= LevelFatal {
		t.Fatalf("levels out of order: error %d, fatal %d, audit %d", slog.LevelError, LevelFatal, LevelAudit)
	}
	setRequired(t)
	t.Setenv("LOG_LEVEL", "verbose")
	for name, load := range map[string]func() (Config, error){"LoadForMigrate": LoadForMigrate, "LoadForAuthz": LoadForAuthz} {
		if _, err := load(); err == nil {
			t.Errorf("%s accepted LOG_LEVEL=verbose", name)
		}
	}
}

// The variables renamed from forma2's names are still read under the old ones, behind the current name, with one deprecation warning per old name set; an error names the variable the operator actually set.
func TestLoadRenamedVariables(t *testing.T) {
	const tokenSecret, pinSecret = "token-secret-0123456789abcdefghijklmnop", "pin-secret-0123456789abcdefghijklmnopqrs"
	renamed := []string{"AUTH_URL", "BETTER_AUTH_URL", "NEXTAUTH_URL", "LINK_SURVEY_TOKEN_SECRET", "NEXTAUTH_SECRET", "LINK_SURVEY_PIN_TOKEN_SECRET", "BETTER_AUTH_SECRET", "SAML_ENABLED", "SAML_DATABASE_URL", "WORKER_ENABLED", "BULLMQ_WORKER_ENABLED"}
	cases := []struct {
		name     string
		env      map[string]string
		check    func(Config) bool
		warnings []string
	}{
		{"current names", map[string]string{"AUTH_URL": "https://auth.example.com", "LINK_SURVEY_TOKEN_SECRET": tokenSecret, "LINK_SURVEY_PIN_TOKEN_SECRET": pinSecret, "SAML_ENABLED": "1"},
			func(c Config) bool {
				return c.SSO.AuthURL.Host == "auth.example.com" && c.LinkSurveyEmailSecret() == tokenSecret && c.LinkSurveyPinSecret() == pinSecret && c.SSO.SAMLEnabled()
			}, nil},
		{"better auth url", map[string]string{"BETTER_AUTH_URL": "https://better.example.com"},
			func(c Config) bool { return c.SSO.AuthURL.Host == "better.example.com" },
			[]string{"BETTER_AUTH_URL is deprecated; rename it to AUTH_URL"}},
		{"nextauth url", map[string]string{"NEXTAUTH_URL": "https://next.example.com"},
			func(c Config) bool { return c.SSO.AuthURL.Host == "next.example.com" },
			[]string{"NEXTAUTH_URL is deprecated; rename it to AUTH_URL"}},
		{"better auth url before nextauth url", map[string]string{"BETTER_AUTH_URL": "https://better.example.com", "NEXTAUTH_URL": "https://next.example.com"},
			func(c Config) bool { return c.SSO.AuthURL.Host == "better.example.com" },
			[]string{"BETTER_AUTH_URL is deprecated; rename it to AUTH_URL", "NEXTAUTH_URL is deprecated and ignored because BETTER_AUTH_URL is set; remove it"}},
		{"auth url wins", map[string]string{"AUTH_URL": "https://auth.example.com", "NEXTAUTH_URL": "not a url"},
			func(c Config) bool { return c.SSO.AuthURL.Host == "auth.example.com" },
			[]string{"NEXTAUTH_URL is deprecated and ignored because AUTH_URL is set; remove it"}},
		{"forma2 secrets", map[string]string{"NEXTAUTH_SECRET": tokenSecret, "BETTER_AUTH_SECRET": pinSecret},
			func(c Config) bool {
				return c.LinkSurveyEmailSecret() == tokenSecret && c.LinkSurveyPinSecret() == pinSecret
			},
			[]string{"NEXTAUTH_SECRET is deprecated; rename it to LINK_SURVEY_TOKEN_SECRET", "BETTER_AUTH_SECRET is deprecated; rename it to LINK_SURVEY_PIN_TOKEN_SECRET"}},
		// Each old name stands for its own current name: forma2's PIN secret stays the PIN secret next to a renamed email secret.
		{"mixed secrets", map[string]string{"LINK_SURVEY_TOKEN_SECRET": tokenSecret, "BETTER_AUTH_SECRET": pinSecret},
			func(c Config) bool {
				return c.LinkSurveyEmailSecret() == tokenSecret && c.LinkSurveyPinSecret() == pinSecret
			},
			[]string{"BETTER_AUTH_SECRET is deprecated; rename it to LINK_SURVEY_PIN_TOKEN_SECRET"}},
		{"nextauth secret behind the current one", map[string]string{"LINK_SURVEY_TOKEN_SECRET": tokenSecret, "NEXTAUTH_SECRET": pinSecret},
			func(c Config) bool {
				return c.LinkSurveyEmailSecret() == tokenSecret && c.LinkSurveyPinSecret() == tokenSecret
			},
			[]string{"NEXTAUTH_SECRET is deprecated and ignored because LINK_SURVEY_TOKEN_SECRET is set; remove it"}},
		{"saml database url", map[string]string{"SAML_DATABASE_URL": "postgres://forma:secret@db:5432/forma-saml"},
			func(c Config) bool { return c.SSO.SAMLEnabled() },
			[]string{"SAML_DATABASE_URL is deprecated; rename it to SAML_ENABLED"}},
		{"saml switched off", map[string]string{"SAML_ENABLED": "0", "SAML_DATABASE_URL": "postgres://forma:secret@db:5432/forma-saml"},
			func(c Config) bool { return !c.SSO.SAMLEnabled() },
			[]string{"SAML_DATABASE_URL is deprecated and ignored because SAML_ENABLED is set; remove it"}},
		// `forma serve` and `forma worker` do not read the switch, but the process that does (`forma start`) learns of the deprecation through Load.
		{"bullmq worker enabled", map[string]string{"BULLMQ_WORKER_ENABLED": "0"},
			func(Config) bool { return true },
			[]string{"BULLMQ_WORKER_ENABLED is deprecated; rename it to WORKER_ENABLED"}},
		{"empty old names are unset", map[string]string{"NEXTAUTH_SECRET": "", "NEXTAUTH_URL": ""},
			func(c Config) bool { return c.SSO.AuthURL == nil && c.LinkSurveyTokenSecret == "" }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			for _, k := range renamed {
				unsetEnv(t, k)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !tc.check(cfg) {
				t.Errorf("config does not hold the values of %v", tc.env)
			}
			if !slices.Equal(cfg.Warnings, tc.warnings) {
				t.Errorf("warnings %q, want %q", cfg.Warnings, tc.warnings)
			}
		})
	}

	t.Run("errors name the variable set", func(t *testing.T) {
		for name, value := range map[string]string{"NEXTAUTH_URL": "/api/auth", "BETTER_AUTH_SECRET": "too-short", "NEXTAUTH_SECRET": "too-short"} {
			setRequired(t)
			for _, k := range renamed {
				unsetEnv(t, k)
			}
			t.Setenv(name, value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), name+" must") {
				t.Errorf("%s=%q: %v", name, value, err)
			}
		}
	})

	t.Run("startup switch", func(t *testing.T) {
		for _, tc := range []struct {
			current, old string
			want         bool
		}{{"", "0", false}, {"", "1", true}, {"1", "0", true}, {"0", "1", false}} {
			unsetEnv(t, "WORKER_ENABLED")
			unsetEnv(t, "BULLMQ_WORKER_ENABLED")
			if tc.current != "" {
				t.Setenv("WORKER_ENABLED", tc.current)
			}
			t.Setenv("BULLMQ_WORKER_ENABLED", tc.old)
			s, err := LoadStartup()
			if err != nil || s.WorkerEnabled != tc.want {
				t.Errorf("WORKER_ENABLED=%q BULLMQ_WORKER_ENABLED=%q: worker %v, %v; want %v", tc.current, tc.old, s.WorkerEnabled, err, tc.want)
			}
		}
	})
}

// forma2 ran BULLMQ_WORKER_COUNT workers of BULLMQ_WORKER_CONCURRENCY jobs each; an instance that still sets them gets the same parallelism and a deprecation warning, unless WORKER_CONCURRENCY says otherwise.
func TestLoadWorkerConcurrencyFromBullMQ(t *testing.T) {
	cases := []struct {
		name        string
		env         map[string]string
		want        int
		wantWarning string
	}{
		{"neither style", map[string]string{}, 0, ""},
		{"worker concurrency", map[string]string{"WORKER_CONCURRENCY": "6"}, 6, ""},
		{"both bullmq variables", map[string]string{"BULLMQ_WORKER_CONCURRENCY": "3", "BULLMQ_WORKER_COUNT": "2"}, 6, "set WORKER_CONCURRENCY=6 instead"},
		{"bullmq concurrency alone", map[string]string{"BULLMQ_WORKER_CONCURRENCY": "5"}, 5, "set WORKER_CONCURRENCY=5 instead"},
		{"bullmq count alone", map[string]string{"BULLMQ_WORKER_COUNT": "3"}, 3 * DefaultWorkerConcurrency(), "deprecated"},
		{"worker concurrency wins", map[string]string{"WORKER_CONCURRENCY": "2", "BULLMQ_WORKER_CONCURRENCY": "8", "BULLMQ_WORKER_COUNT": "4"}, 2, "ignored because WORKER_CONCURRENCY is set"},
		{"empty bullmq values are unset", map[string]string{"BULLMQ_WORKER_CONCURRENCY": "", "BULLMQ_WORKER_COUNT": ""}, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			for _, k := range []string{"WORKER_CONCURRENCY", "BULLMQ_WORKER_CONCURRENCY", "BULLMQ_WORKER_COUNT"} {
				unsetEnv(t, k)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.WorkerConcurrency != tc.want {
				t.Errorf("WorkerConcurrency = %d, want %d", cfg.WorkerConcurrency, tc.want)
			}
			switch {
			case tc.wantWarning == "" && len(cfg.Warnings) != 0:
				t.Errorf("warnings %q, want none", cfg.Warnings)
			case tc.wantWarning != "" && (len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], tc.wantWarning)):
				t.Errorf("warnings %q, want one containing %q", cfg.Warnings, tc.wantWarning)
			}
		})
	}
}

// forma2's connection URLs carried Prisma's ?schema=public, which PostgreSQL would reject as a run-time parameter.
func TestDatabaseURLDropsPrismaSchemaPublic(t *testing.T) {
	cases := []struct{ in, want string }{
		{"postgresql://postgres:secret@postgres:5432/forma?schema=public", "postgresql://postgres:secret@postgres:5432/forma"},
		{"postgres://u:p%40ss@db/forma?sslmode=require&schema=public&connect_timeout=5", "postgres://u:p%40ss@db/forma?sslmode=require&connect_timeout=5"},
		{"postgres://db/forma?SCHEMA=public&sslmode=disable", "postgres://db/forma?sslmode=disable"},
		{"POSTGRESQL://db/forma?Schema=public", "POSTGRESQL://db/forma"},
		{"postgres://db/forma?sslmode=disable", "postgres://db/forma?sslmode=disable"},
		{"postgres://db/forma", "postgres://db/forma"},
		{"postgres://db/forma?search_path=public&application_name=forma", "postgres://db/forma?search_path=public&application_name=forma"},
		{"host=db dbname=forma schema=other", "host=db dbname=forma schema=other"},
	}
	for _, tc := range cases {
		got, err := databaseURL("DATABASE_URL", tc.in, true)
		if err != nil || got != tc.want {
			t.Errorf("databaseURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"postgres://db/forma?schema=forma", "postgres://db/forma?schema=Public", "postgres://db/forma?sslmode=disable&schema="} {
		_, err := databaseURL("MIGRATE_DATABASE_URL", in, false)
		if err == nil || !strings.Contains(err.Error(), "MIGRATE_DATABASE_URL") || !strings.Contains(err.Error(), "Prisma's schema parameter") {
			t.Errorf("databaseURL(%q) error = %v, want one naming the variable and Prisma's schema parameter", in, err)
		}
	}
}

func TestLoadersDropPrismaSchemaPublic(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_URL", "postgresql://postgres:secret@postgres:5432/forma?schema=public")
	t.Setenv("AUTHZED_ENDPOINT", "spicedb:50051")
	want := "postgresql://postgres:secret@postgres:5432/forma"
	for name, load := range map[string]func() (Config, error){"Load": Load, "LoadForMigrate": LoadForMigrate, "LoadForAuthz": LoadForAuthz} {
		cfg, err := load()
		if err != nil || cfg.DatabaseURL != want {
			t.Errorf("%s: DatabaseURL %q, err %v; want %q", name, cfg.DatabaseURL, err, want)
		}
	}

	t.Setenv("MIGRATE_DATABASE_URL", "postgresql://owner@postgres/forma?sslmode=disable&schema=public")
	if cfg, err := LoadForMigrate(); err != nil || cfg.DatabaseURL != "postgresql://owner@postgres/forma?sslmode=disable" {
		t.Errorf("LoadForMigrate: DatabaseURL %q, err %v", cfg.DatabaseURL, err)
	}
	t.Setenv("MIGRATE_DATABASE_URL", "postgresql://owner@postgres/forma?schema=tenant")
	if _, err := LoadForMigrate(); err == nil || !strings.Contains(err.Error(), "MIGRATE_DATABASE_URL") {
		t.Errorf("LoadForMigrate accepted another schema: %v", err)
	}
}

// Production's forma2 URLs, credentials replaced: the app's pooled URL through PgBouncer and the migration hook's direct one, each with the pool settings forma2's Prisma adapter read.
const (
	prodPooledURL = "postgresql://forma:pw@pgbouncer.postgres.svc.cluster.local:5432/forma?schema=public&pgbouncer=true&connection_limit=10"
	prodDirectURL = "postgresql://forma:pw@pg-rw.postgres.svc.cluster.local:5432/forma?schema=public&connection_limit=4"
)

// forma2's Prisma adapter consumed these keys itself; pgx would send them to the server as run-time settings, which PostgreSQL and PgBouncer reject.
func TestDatabaseURLDropsPrismaConnectionParameters(t *testing.T) {
	pooled := "postgresql://forma:pw@pgbouncer.postgres.svc.cluster.local:5432/forma"
	direct := "postgresql://forma:pw@pg-rw.postgres.svc.cluster.local:5432/forma"
	cases := []struct {
		in, app, migration string
	}{
		{prodPooledURL, pooled, pooled},
		{prodDirectURL, direct, direct},
		{pooled + "?pgbouncer=true&connection_limit=10&pool_timeout=20&schema=public", pooled, pooled},
		{pooled + "?connection_limit=10&pgbouncer=true&pool_timeout=0", pooled, pooled},
		{direct + "?connection_limit=4&pool_timeout=10", direct, direct},
		{direct + "?Connection_Limit=4&PGBOUNCER=true", direct, direct},
		{direct + "?max_connection_lifetime=0&max_idle_connection_lifetime=300&socket_timeout=30&statement_cache_size=0&query_timeout=15", direct, direct},
		// libpq's and pgx's own parameters stay, in their order and spelling; connect_timeout means seconds to both.
		{direct + "?sslmode=require&connection_limit=4&connect_timeout=5&application_name=forma&pool_max_conns=8", direct + "?sslmode=require&connect_timeout=5&application_name=forma&pool_max_conns=8", direct + "?sslmode=require&connect_timeout=5&application_name=forma&pool_max_conns=8"},
		{direct + "?target_session_attrs=read-write&connection_limit=4", direct + "?target_session_attrs=read-write", direct + "?target_session_attrs=read-write"},
		// forma2 read these two in seconds, PostgreSQL reads a bare number in milliseconds; migrations ran without them.
		{direct + "?statement_timeout=30&connection_limit=4&idle_in_transaction_session_timeout=60", direct + "?statement_timeout=30s&idle_in_transaction_session_timeout=60s", direct},
		{direct + "?statement_timeout=500ms&idle_in_transaction_session_timeout=0", direct + "?statement_timeout=500ms&idle_in_transaction_session_timeout=0s", direct},
	}
	for _, tc := range cases {
		for _, c := range []struct {
			appTimeouts bool
			want        string
		}{{true, tc.app}, {false, tc.migration}} {
			got, err := databaseURL("DATABASE_URL", tc.in, c.appTimeouts)
			if err != nil || got != c.want {
				t.Errorf("databaseURL(%q, appTimeouts=%v) = %q, %v; want %q", tc.in, c.appTimeouts, got, err, c.want)
				continue
			}
			cfg, err := pgx.ParseConfig(got)
			if err != nil {
				t.Errorf("pgx.ParseConfig(%q): %v", got, err)
				continue
			}
			for k := range cfg.RuntimeParams {
				if prismaOnlyParams[strings.ToLower(k)] || strings.EqualFold(k, "schema") {
					t.Errorf("pgx would send %s from %q to the server", k, got)
				}
			}
		}
	}
	if cfg, err := pgx.ParseConfig(cases[len(cases)-2].app); err != nil || cfg.RuntimeParams["statement_timeout"] != "30s" || cfg.RuntimeParams["idle_in_transaction_session_timeout"] != "60s" {
		t.Errorf("statement timeouts reach the server as %v, %v", cfg.RuntimeParams, err)
	}

	for in, want := range map[string]string{
		direct + "?sslaccept=accept_invalid_certs":       "sslaccept",
		direct + "?sslmode=require&sslaccept=strict":     "sslaccept",
		direct + "?sslidentity=client.p12&sslpassword=x": "sslidentity",
	} {
		_, err := databaseURL("MIGRATE_DATABASE_URL", in, false)
		if err == nil || !strings.Contains(err.Error(), "MIGRATE_DATABASE_URL") || !strings.Contains(err.Error(), want) {
			t.Errorf("databaseURL(%q) error = %v, want one naming the variable and %s", in, err, want)
		}
	}
}

// Every process that reads forma2's DATABASE_URL (serve, worker and start through Load; migrate and wait-for-database through LoadForMigrate; the authz commands through LoadForAuthz) gets it without Prisma's parameters.
func TestLoadersAcceptProductionForma2URLs(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_URL", prodPooledURL)
	t.Setenv("AUTHZED_ENDPOINT", "spicedb:50051")
	want := "postgresql://forma:pw@pgbouncer.postgres.svc.cluster.local:5432/forma"
	for name, load := range map[string]func() (Config, error){"Load": Load, "LoadForMigrate": LoadForMigrate, "LoadForAuthz": LoadForAuthz} {
		cfg, err := load()
		if err != nil || cfg.DatabaseURL != want {
			t.Errorf("%s: DatabaseURL %q, err %v; want %q", name, cfg.DatabaseURL, err, want)
		}
	}

	// The migration hook overrides DATABASE_URL with the direct URL.
	t.Setenv("DATABASE_URL", prodDirectURL+"&statement_timeout=30")
	want = "postgresql://forma:pw@pg-rw.postgres.svc.cluster.local:5432/forma"
	if cfg, err := LoadForMigrate(); err != nil || cfg.DatabaseURL != want {
		t.Errorf("LoadForMigrate: DatabaseURL %q, err %v; want %q", cfg.DatabaseURL, err, want)
	}
	t.Setenv("MIGRATE_DATABASE_URL", prodDirectURL)
	if cfg, err := LoadForMigrate(); err != nil || cfg.DatabaseURL != want {
		t.Errorf("LoadForMigrate: DatabaseURL %q, err %v; want %q", cfg.DatabaseURL, err, want)
	}
}

// The admin pages live under /app on WEBAPP_URL, whatever path, query or trailing slash WEBAPP_URL was written with.
func TestAppURL(t *testing.T) {
	cases := []struct {
		webApp, app, page string
	}{
		{"https://forma.example.com", "https://forma.example.com/app", "https://forma.example.com/app/auth/login"},
		{"https://forma.example.com/", "https://forma.example.com/app", "https://forma.example.com/app/auth/login"},
		{"http://localhost:3000/?x=1#y", "http://localhost:3000/app", "http://localhost:3000/app/auth/login"},
		{"https://example.com/forma/", "https://example.com/forma/app", "https://example.com/forma/app/auth/login"},
	}
	for _, tc := range cases {
		t.Run(tc.webApp, func(t *testing.T) {
			u, err := url.Parse(tc.webApp)
			if err != nil {
				t.Fatal(err)
			}
			cfg := Config{WebAppURL: u}
			if got := cfg.AppURL().String(); got != tc.app {
				t.Errorf("AppURL = %s, want %s", got, tc.app)
			}
			if got := cfg.AppPageURL("/auth/login").String(); got != tc.page {
				t.Errorf("AppPageURL = %s, want %s", got, tc.page)
			}
			if cfg.WebAppURL.String() != tc.webApp {
				t.Errorf("WEBAPP_URL changed to %s", cfg.WebAppURL)
			}
		})
	}
}

func TestCustomScripts(t *testing.T) {
	parse := func(raw string) *url.URL {
		if raw == "" {
			return nil
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	cases := []struct {
		name           string
		webApp, public string
		cloud          bool
		want           CustomScripts
	}{
		{"no PUBLIC_URL: the survey pages share the admin origin", "https://forma.example.com", "", false, CustomScriptsNeedPublicURL},
		{"PUBLIC_URL on the admin host", "https://forma.example.com", "https://FORMA.example.com/", false, CustomScriptsNeedPublicURL},
		{"PUBLIC_URL on its own host", "https://forma.example.com", "https://surveys.example.com", false, CustomScriptsAvailable},
		{"PUBLIC_URL on another port", "http://localhost:3000", "http://localhost:3001", false, CustomScriptsAvailable},
		{"Forma Cloud, even with its own survey host", "https://forma.example.com", "https://surveys.example.com", true, CustomScriptsFormaCloud},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{WebAppURL: parse(tc.webApp), PublicURL: parse(tc.public), IsFormaCloud: tc.cloud}
			if got := cfg.CustomScripts(); got != tc.want {
				t.Errorf("CustomScripts = %q, want %q", got, tc.want)
			}
			if refusal := cfg.CustomScriptsRefusal(); (refusal == "") != (tc.want == CustomScriptsAvailable) {
				t.Errorf("CustomScriptsRefusal = %q for %q", refusal, tc.want)
			}
		})
	}
	if msg := (Config{WebAppURL: parse("https://forma.example.com")}).CustomScriptsRefusal(); !strings.Contains(msg, "PUBLIC_URL") {
		t.Errorf("the refusal without PUBLIC_URL does not tell the operator what to set: %q", msg)
	}
}
