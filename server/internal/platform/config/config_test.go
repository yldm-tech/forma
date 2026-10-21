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
	if cfg.InviteDisabled || cfg.InviteRateLimitPer24Hours != 50 || cfg.SignupDomainCheckOnInvites {
		t.Errorf("invites disabled %v, limit %d, domain check on invites %v; want enabled, 50, false", cfg.InviteDisabled, cfg.InviteRateLimitPer24Hours, cfg.SignupDomainCheckOnInvites)
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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.RateLimitingDisabled || cfg.SMTPAuthenticated || !cfg.SMTPConfigured() {
		t.Fatalf("cfg = %+v, want rate limiting disabled, SMTP unauthenticated and configured", cfg)
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
		"default org not uuid": {"AUTH_SSO_DEFAULT_ORGANIZATION_ID", "clxyz123abc"},
		"default org role":     {"AUTH_SSO_DEFAULT_ORGANIZATION_ROLE", "admin"},
		"relative auth url":    {"BETTER_AUTH_URL", "/api/auth"},
		"nextauth url scheme":  {"NEXTAUTH_URL", "ftp://auth.example.com"},
		"zero invite limit":    {"INVITE_RATE_LIMIT_PER_24_HOURS", "0"},
		"invite limit text":    {"INVITE_RATE_LIMIT_PER_24_HOURS", "many"},
		"invite flag spelled":  {"INVITE_DISABLED", "yes"},
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

func TestLoadRejectsInvalidAuthz(t *testing.T) {
	cases := map[string]map[string]string{
		"enabled without endpoint":  {"AUTHZED_ENABLED": "1", "AUTHZED_TOKEN": "secret"},
		"enabled without token":     {"AUTHZED_ENABLED": "1", "AUTHZED_ENDPOINT": "localhost:50051"},
		"enabled spelled true":      {"AUTHZED_ENABLED": "true"},
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
	if cfg.SSO.GoogleEnabled() || cfg.SSO.GitHubEnabled() || cfg.SSO.AzureADEnabled() || cfg.SSO.OIDCEnabled() || cfg.SSO.BetterAuthURL != nil {
		t.Fatalf("SSO = %+v, want every provider off", cfg.SSO)
	}

	for k, v := range map[string]string{
		"GOOGLE_CLIENT_ID": "g", "GOOGLE_CLIENT_SECRET": "gs", "GITHUB_ID": "h", "GITHUB_SECRET": "hs",
		"AZUREAD_CLIENT_ID": "a", "AZUREAD_CLIENT_SECRET": "as", "AZUREAD_TENANT_ID": "common",
		"OIDC_CLIENT_ID": "o", "OIDC_CLIENT_SECRET": "os", "OIDC_ISSUER": "https://idp.example.com/realms/forma", "OIDC_DISPLAY_NAME": "Okta",
		"AUTH_SSO_DEFAULT_ORGANIZATION_ID": "0199e0a1-7b1c-7a8e-9d1e-0f5c3a7b2d10", "AUTH_SSO_DEFAULT_ORGANIZATION_ROLE": "member",
		"BETTER_AUTH_URL": "https://auth.example.com/api/auth",
	} {
		t.Setenv(k, v)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.SSO
	if !s.GoogleEnabled() || !s.GitHubEnabled() || !s.AzureADEnabled() || !s.OIDCEnabled() || s.AzureADTenantID != "common" || s.OIDCDisplayName != "Okta" ||
		s.DefaultOrganizationRole != "member" || s.BetterAuthURL.Host != "auth.example.com" {
		t.Fatalf("SSO = %+v", s)
	}

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
