package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// Lifetimes: an authorization request has ten minutes to come back (Better Auth's state expiry); an SSO recovery link lives a day, as forma2's did.
const (
	oauthStateTTL  = 10 * time.Minute
	ssoRecoveryTTL = 24 * time.Hour
)

// Pages of the SPA single sign-on sends the browser to.
const (
	signupPath = "/auth/signup"
	// ssoRecoveryPath is where the emailed recovery link lands; the page posts the token to complete the link.
	ssoRecoveryPath = "/auth/sso-recovery"
	// securitySettingsPath is where a password and two-factor authentication are set up again.
	securitySettingsPath = "/account/settings/profile"
)

// Codes the callback reports in the error parameter of the sign-in page. The OAuth ones are Better Auth's, which forma2's sign-in page classified (account_not_linked, signup_not_allowed, misconfigured, anything else generic); account_inactive is the credential module's.
const (
	callbackStateMismatch      = "state_mismatch"
	callbackRestart            = "please_restart_the_process"
	callbackNoCode             = "no_code"
	callbackIssuerMismatch     = "issuer_mismatch"
	callbackInvalidCode        = "invalid_code"
	callbackNoUserInfo         = "unable_to_get_user_info"
	callbackEmailNotFound      = "email_not_found"
	callbackCreateFailed       = "unable_to_create_user"
	callbackProviderNotFound   = "oauth_provider_not_found"
	callbackTooManyRequests    = "too_many_requests"
	callbackOAuthProviderError = "oauth_provider_error"
)

// CodeAccountNotLinked is forma2's OAuthAccountNotLinked: an identity that could not be linked to the account with its address, reported by the callback when recovery cannot start and by a recovery link that can no longer link.
const CodeAccountNotLinked = "account_not_linked"

func errProviderNotFound() error {
	return &apperr.Error{Status: http.StatusNotFound, Code: callbackProviderNotFound, Message: "Provider not found"}
}

func errAccountNotLinked() error {
	return apperr.InvalidInput(CodeAccountNotLinked, "The sign-in method could not be linked to the account.")
}

// SSO signs users in through Google, GitHub, Azure AD, a generic OpenID Connect provider and SAML with forma2's rules (apps/web/modules/sso and the Better Auth providers it configured): the authorization code flow with state, PKCE and, for OpenID Connect, a nonce, or a SAML AuthnRequest bound to the same state (saml.go); identities matched by provider and subject; an address that already has an account linked only after its owner proves the mailbox; and new users created on first sign-in.
type SSO struct {
	cfg       config.Config
	svc       *Service
	providers map[string]*ssoProvider
	client    *http.Client
	log       *slog.Logger
}

func NewSSO(cfg config.Config, svc *Service, log *slog.Logger) *SSO {
	return &SSO{
		cfg:       cfg,
		svc:       svc,
		providers: newSSOProviders(cfg, log),
		client:    &http.Client{Timeout: ssoHTTPTimeout},
		log:       log,
	}
}

// secretEqual compares two secrets in constant time.
func secretEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// validatedCallbackURL is forma2's getValidatedCallbackUrl: a root-relative path or an absolute http(s) URL on WEBAPP_URL's origin, without credentials and without a path starting with "//". It returns the absolute URL, or "" when the input is not acceptable.
func validatedCallbackURL(webApp *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	if !absoluteURLPrefix.MatchString(raw) && !strings.HasPrefix(raw, "/") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() {
		base, _ := url.Parse(URLOrigin(webApp))
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || URLOrigin(u) != URLOrigin(webApp) || u.User != nil || strings.HasPrefix(u.Path, "//") {
		return ""
	}
	return u.String()
}

var absoluteURLPrefix = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+\-.]*:`)

// enabledSSOProviders lists the providers whose credentials are configured, in the order the sign-in page shows them.
func enabledSSOProviders(cfg config.SSOConfig) []string {
	ids := []string{}
	for _, p := range []struct {
		id      string
		enabled bool
	}{
		{providerGoogle, cfg.GoogleEnabled()}, {providerGitHub, cfg.GitHubEnabled()}, {providerAzureAD, cfg.AzureADEnabled()}, {providerOpenID, cfg.OIDCEnabled()}, {providerSAML, cfg.SAMLEnabled()},
	} {
		if p.enabled {
			ids = append(ids, p.id)
		}
	}
	return ids
}

// oauthState is what an authorization request in flight remembers. The browser holds only an opaque cookie naming it. A SAML flow has neither verifier nor nonce: its state is the RelayState.
type oauthState struct {
	Provider     string `json:"provider"`
	State        string `json:"state"`
	CodeVerifier string `json:"codeVerifier"`
	Nonce        string `json:"nonce"`
	CallbackURL  string `json:"callbackUrl"`
}

// offered reports whether a normalized provider id without an OAuth provider is SAML with SAML enabled.
func (s *SSO) offered(provider string) bool {
	return provider == providerSAML && s.samlEnabled()
}

// clientContext makes the OAuth and OpenID Connect libraries use the SSO HTTP client.
func (s *SSO) clientContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, s.client)
}

// start begins an authorization code flow and returns the provider's authorization URL and the state cookie that binds the flow to this browser. callbackURL is where a successful sign-in lands; empty means the app's root.
func (s *SSO) start(ctx context.Context, providerID, callbackURL string) (string, issuedToken, error) {
	provider := normalizeProvider(providerID)
	p := s.providers[provider]
	if p == nil && !s.offered(provider) {
		return "", issuedToken{}, errProviderNotFound()
	}
	target := s.svc.pageURL("/")
	if callbackURL != "" {
		if target = validatedCallbackURL(s.cfg.WebAppURL, callbackURL); target == "" {
			var errs fieldErrors
			errs.add("callbackUrl", "url", "")
			return "", issuedToken{}, errs.err()
		}
	}
	if p == nil {
		return s.startSAML(ctx, target)
	}
	conf, _, err := p.config(s.clientContext(ctx), s.client)
	if err != nil {
		return "", issuedToken{}, err
	}
	state, err := secret.NewToken()
	if err != nil {
		return "", issuedToken{}, err
	}
	nonce, err := secret.NewToken()
	if err != nil {
		return "", issuedToken{}, err
	}
	cookie, err := secret.NewToken()
	if err != nil {
		return "", issuedToken{}, err
	}
	payload := oauthState{Provider: p.id, State: state, CodeVerifier: oauth2.GenerateVerifier(), Nonce: nonce, CallbackURL: target}
	expires := s.svc.now().Add(oauthStateTTL)
	if err := s.svc.repo.insertPayloadVerification(ctx, purposeOAuthState, uuid.Nil, "", secret.HashToken(cookie), expires, payload); err != nil {
		return "", issuedToken{}, err
	}
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(payload.CodeVerifier)}
	if p.source != sourceGitHub {
		opts = append(opts, oauth2.SetAuthURLParam("nonce", nonce))
	}
	return conf.AuthCodeURL(state, opts...), issuedToken{Value: cookie, ExpiresAt: expires, MaxAge: oauthStateTTL}, nil
}

// callbackResult is where the callback sends the browser, and the session to set when it signed the browser in.
type callbackResult struct {
	redirect string
	session  *issuedToken
}

// failure sends the browser to the sign-in page with an error code, forma2's errorCallbackURL.
func (s *SSO) failure(code string) callbackResult {
	return callbackResult{redirect: s.pageWithQuery(loginPath, url.Values{"error": {code}})}
}

func (s *SSO) pageWithQuery(path string, query url.Values) string {
	u := *s.cfg.WebAppURL
	u.Path = path
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String()
}

// providerErrorCode is the identity provider's own error code (RFC 6749 §4.1.2.1), passed through as Better Auth did when it looks like one; anything else is reported generically.
var providerErrorCode = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// callback finishes an authorization code flow. It never fails: every outcome is a redirect, and a cause worth knowing is logged without the code, tokens or state.
func (s *SSO) callback(ctx context.Context, providerID string, query url.Values, stateCookie, acceptLanguage string, meta clientMeta) callbackResult {
	provider := normalizeProvider(providerID)
	p := s.providers[provider]
	if p == nil && !s.offered(provider) {
		return s.failure(callbackProviderNotFound)
	}
	if stateCookie == "" {
		return s.failure(callbackRestart)
	}
	row, err := s.svc.repo.consumePayloadVerification(ctx, purposeOAuthState, secret.HashToken(stateCookie), s.svc.now())
	if err != nil {
		s.log.ErrorContext(ctx, "SSO callback failed to load its state", "err", err, "provider", provider)
		return s.failure(callbackRestart)
	}
	if row == nil {
		return s.failure(callbackRestart)
	}
	var state oauthState
	if err := json.Unmarshal(row.Payload, &state); err != nil {
		s.log.ErrorContext(ctx, "SSO callback state is unreadable", "err", err, "provider", provider)
		return s.failure(callbackRestart)
	}
	if state.Provider != provider || !secretEqual(state.State, query.Get("state")) {
		return s.failure(callbackStateMismatch)
	}
	if e := query.Get("error"); e != "" {
		if !providerErrorCode.MatchString(e) {
			e = callbackOAuthProviderError
		}
		return s.failure(e)
	}
	code := query.Get("code")
	if code == "" {
		return s.failure(callbackNoCode)
	}
	if p == nil {
		return s.finishSAML(ctx, state, code, acceptLanguage, meta)
	}
	ctx = s.clientContext(ctx)
	conf, discovered, err := p.config(ctx, s.client)
	if err != nil {
		s.log.ErrorContext(ctx, "SSO provider discovery failed", "err", err, "provider", provider)
		return s.failure(callbackNoUserInfo)
	}
	// RFC 9207: a provider that names itself on the authorization response must name the issuer it was configured with, which defeats mix-up attacks.
	if iss := query.Get("iss"); iss != "" {
		if expected := p.expectedIssuer(discovered); expected != "" && iss != expected {
			return s.failure(callbackIssuerMismatch)
		}
	}
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(state.CodeVerifier))
	if err != nil {
		s.log.WarnContext(ctx, "SSO code exchange failed", "err", err, "provider", provider)
		return s.failure(callbackInvalidCode)
	}
	identity, err := p.resolveIdentity(ctx, s.client, discovered, token, state.Nonce)
	if err != nil {
		s.log.WarnContext(ctx, "SSO identity could not be established", "err", err, "provider", provider)
		return s.failure(callbackNoUserInfo)
	}
	if identity.Email == "" {
		return s.failure(callbackEmailNotFound)
	}
	res, err := s.signIn(ctx, provider, identity, state.CallbackURL, acceptLanguage, meta)
	if err != nil {
		s.log.ErrorContext(ctx, "SSO sign-in failed", "err", err, "provider", provider)
		return s.failure(callbackCreateFailed)
	}
	return res
}

// signIn is forma2's callback decision: a linked identity signs its user in; an unlinked one whose address has an account starts SSO recovery; otherwise a new user is created.
func (s *SSO) signIn(ctx context.Context, provider string, id ssoIdentity, callbackURL, acceptLanguage string, meta clientMeta) (callbackResult, error) {
	owner, err := s.svc.repo.identityOwner(ctx, provider, id.Subject)
	if err != nil {
		return callbackResult{}, err
	}
	if owner != uuid.Nil {
		return s.signInLinked(ctx, owner, id, callbackURL, meta)
	}
	acc, err := s.svc.repo.accountByEmail(ctx, id.Email)
	if err != nil {
		return callbackResult{}, err
	}
	if acc != nil {
		return s.startRecovery(ctx, acc, provider, id, callbackURL), nil
	}
	return s.provision(ctx, provider, id, callbackURL, acceptLanguage, meta)
}

// signInLinked signs in the user an identity is linked to. The provider is authoritative for the display name, re-read on every sign-in; it never rewrites the address, which changes only through the verified email change. An unverified account whose provider now vouches for its address becomes verified.
func (s *SSO) signInLinked(ctx context.Context, userID uuid.UUID, id ssoIdentity, callbackURL string, meta clientMeta) (callbackResult, error) {
	acc, err := s.svc.repo.accountByID(ctx, userID)
	if err != nil {
		return callbackResult{}, err
	}
	if acc == nil {
		return s.failure(callbackNoUserInfo), nil
	}
	if !acc.IsActive {
		return s.failure(CodeAccountInactive), nil
	}
	now := s.svc.now()
	if name := normalizeUserName(id.Name); name != "" {
		if err := s.svc.repo.syncName(ctx, acc.ID, name, now); err != nil {
			return callbackResult{}, err
		}
	}
	if id.EmailVerified && !acc.EmailVerified && normalizeEmail(id.Email) == normalizeEmail(acc.Email) {
		if err := s.svc.repo.markEmailVerified(ctx, acc.ID, now); err != nil {
			return callbackResult{}, err
		}
	}
	sess, err := s.svc.sessions.create(ctx, s.svc.repo.db, acc.ID, meta)
	if err != nil {
		return callbackResult{}, err
	}
	s.svc.finalizeSignIn(ctx, acc.ID, signInMethodSSO)
	return callbackResult{redirect: callbackURL, session: &sess}, nil
}

// recoveryPayload is what an SSO recovery link links once redeemed.
type recoveryPayload struct {
	Provider    string `json:"provider"`
	Subject     string `json:"subject"`
	CallbackURL string `json:"callbackUrl"`
}

// startRecovery is forma2's verify-before-link: an identity provider asserting an address that already has an account proves nothing about who owns the account, so nothing is linked until a link mailed to the address on record is used. The browser that started the flow learns only that a mail was sent; the link itself travels by mail alone and works once, so it cannot be handed to the account's owner to click, which forma2's intent in the redirect URL allowed.
func (s *SSO) startRecovery(ctx context.Context, acc *account, provider string, id ssoIdentity, callbackURL string) callbackResult {
	details := map[string]any{"email": acc.Email, "provider": provider, "callbackUrl": callbackURL}
	fail := func(reason string, err error) callbackResult {
		s.log.ErrorContext(ctx, "Failed to start SSO recovery", "err", err, "userId", acc.ID.String(), "provider", provider)
		s.auditRecovery(ctx, audit.ActionSSORecoveryFailed, audit.StatusFailure, acc.ID.String(), withReason(details, reason))
		return s.failure(CodeAccountNotLinked)
	}
	if err := s.svc.limiter.ApplyIP(ctx, ratelimit.BASSORecovery); err != nil {
		return s.failure(callbackTooManyRequests)
	}
	token, err := secret.NewToken()
	if err != nil {
		return fail("token_generation_failed", err)
	}
	payload := recoveryPayload{Provider: provider, Subject: id.Subject, CallbackURL: callbackURL}
	if err := s.svc.repo.insertPayloadVerification(ctx, purposeSSORecovery, acc.ID, normalizeEmail(acc.Email), secret.HashToken(token), s.svc.now().Add(ssoRecoveryTTL), payload); err != nil {
		return fail("token_store_failed", err)
	}
	// forma2 sent its verification mail for this; the recovery link is the one it carries.
	sent, err := s.svc.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.Verification{
		VerifyLink:              s.svc.link(ssoRecoveryPath, token),
		VerificationRequestLink: s.svc.pageURL(verificationRequestedPath),
	})
	if err == nil && !sent {
		err = errMailNotSent
	}
	if err != nil {
		// forma2 ignored a mailer without SMTP and told the user to check a mailbox no mail would reach.
		return fail("email_not_sent", err)
	}
	s.log.InfoContext(ctx, "SSO recovery started", "userId", acc.ID.String(), "provider", provider)
	s.auditRecovery(ctx, audit.ActionSSORecoveryStarted, audit.StatusSuccess, acc.ID.String(), details)
	return callbackResult{redirect: s.pageWithQuery(verificationRequestedPath, url.Values{"purpose": {purposeSSORecovery}})}
}

func withReason(details map[string]any, reason string) map[string]any {
	out := make(map[string]any, len(details)+1)
	for k, v := range details {
		out[k] = v
	}
	out["failureReason"] = reason
	return out
}

func (s *SSO) auditRecovery(ctx context.Context, action audit.Action, status audit.Status, userID string, details map[string]any) {
	s.svc.logEvent(ctx, audit.Event{Action: action, TargetType: audit.TargetUser, TargetID: userID, ActorID: userID, Status: status, NewObject: details})
}

// provision creates a user on their first SSO sign-in (forma2's just-in-time provisioning). Public sign-up being closed does not stop it: on a self-hosted instance the identity provider decides who may sign in. On Forma Cloud a personal mail domain is refused, as at sign-up, unless the sign-in is headed for a valid invite to the address and SIGNUP_DOMAIN_CHECK_ON_INVITES is off. With AUTH_SSO_DEFAULT_ORGANIZATION_ID set the user joins that organization; otherwise they start without one, as forma2's always-on multi-organization mode did.
func (s *SSO) provision(ctx context.Context, provider string, id ssoIdentity, callbackURL, acceptLanguage string, meta clientMeta) (callbackResult, error) {
	email := normalizeEmail(id.Email)
	if s.cfg.IsFormaCloud && isBlockedEmailDomain(email) {
		invited := false
		if !s.cfg.SignupDomainCheckOnInvites {
			var err error
			if invited, err = s.callbackInvited(ctx, callbackURL, email); err != nil {
				return callbackResult{}, err
			}
		}
		if !invited {
			return callbackResult{redirect: s.pageWithQuery(signupPath, url.Values{"error": {CodeEmailDomainNotAllowed}})}, nil
		}
	}
	name := normalizeUserName(id.Name)
	if name == "" {
		local, _, _ := strings.Cut(email, "@")
		name = normalizeUserName(emailLocalSeparators.ReplaceAllString(local, " "))
	}
	if name == "" {
		name = "User"
	}
	locale := mailer.MatchLocale(acceptLanguage)
	var userID uuid.UUID
	var sess issuedToken
	err := s.svc.repo.transaction(ctx, func(tx *Repository) error {
		var created bool
		var err error
		if userID, created, err = tx.insertUser(ctx, name, email, locale); err != nil {
			return err
		}
		if !created {
			return errors.New("an account with the address was created concurrently")
		}
		if err := tx.setEmailVerified(ctx, userID, emailVerifiedOnCreate(provider, id)); err != nil {
			return err
		}
		if err := tx.insertIdentity(ctx, userID, provider, id.Subject); err != nil {
			return err
		}
		sess, err = s.svc.sessions.create(ctx, tx.db, userID, meta)
		return err
	})
	if err != nil {
		return callbackResult{}, err
	}
	orgForAudit := audit.Unknown
	defaultOrg, hasDefaultOrg := s.cfg.SSO.DefaultOrganization()
	if hasDefaultOrg {
		orgForAudit = defaultOrg.String()
	}
	if !emailVerifiedOnCreate(provider, id) {
		// The log is what a self-hoster sees; the audit event is the security trail. Neither carries the address.
		s.log.WarnContext(ctx, "SSO sign-up created an account the identity provider did not report as verified",
			"source", "sso-signup", "ssoProvider", provider, "userId", userID.String(), "emailVerified", false)
		s.svc.logEvent(ctx, audit.Event{
			Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: userID.String(), ActorID: userID.String(), OrganizationID: orgForAudit,
			Status: audit.StatusSuccess, NewObject: map[string]any{"ssoUnverifiedSignupMarker": true, "provider": provider, "emailVerified": false},
		})
	}
	source := "direct"
	if strings.Contains(callbackURL, "token=") {
		source = "invite"
	}
	var joined *uuid.UUID
	if hasDefaultOrg {
		s.joinDefaultOrganization(ctx, defaultOrg, userID, name)
		joined = &defaultOrg
	}
	// The sign-in below consumes the attribution; the sign-up reads it first, as forma2 carried it from the create hook to the after hook.
	captureSignedUp(userID, provider, email, source, joined, attributionProperties(ctx))
	s.svc.finalizeSignIn(ctx, userID, signInMethodSSO)
	return callbackResult{redirect: callbackURL, session: &sess}, nil
}

// emailVerifiedOnCreate is forma2's resolveSsoEmailVerifiedForCreate. SAML is its never-attests provider: an assertion cannot carry a verification claim, so there is nothing to honour and the user it creates is verified, as it always was. A linked SAML sign-in reads id.EmailVerified, which SAML never sets, so it never verifies an existing account's address.
func emailVerifiedOnCreate(provider string, id ssoIdentity) bool {
	return provider == providerSAML || id.EmailVerified
}

// callbackInvited is forma2's validateSsoInviteToken as gateSsoProvisioning used it for the domain exemption: the callback URL (already validated, absolute) carries an invite link in its token parameter, as the invite page's sign-in does, and the link is live and issued to this address.
func (s *SSO) callbackInvited(ctx context.Context, callbackURL, email string) (bool, error) {
	token := ""
	// An unparsable URL carries no invite.
	if u, err := url.Parse(callbackURL); err == nil {
		token = u.Query().Get("token")
	}
	if token == "" {
		return false, nil
	}
	return s.svc.invites.MatchForSignup(ctx, token, email)
}

// emailLocalSeparators are the characters of an address's local part that read as word breaks in a name derived from it.
var emailLocalSeparators = regexp.MustCompile(`[._+]+`)

// defaultMembershipAttempts is how often the membership write is tried before it is given up and logged (forma2's MAX_ATTEMPTS).
const defaultMembershipAttempts = 2

// joinDefaultOrganization is forma2's ensureDefaultOrganization and membership write. The sign-up that creates the organization makes its user the owner, so someone can administer it; later sign-ups get AUTH_SSO_DEFAULT_ORGANIZATION_ROLE, manager when unset. The user already exists, so nothing here fails the sign-in: a failure leaves the user without the organization and is logged for an operator to reconcile.
func (s *SSO) joinDefaultOrganization(ctx context.Context, orgID, userID uuid.UUID, userName string) {
	role, err := s.resolveDefaultOrganization(ctx, orgID, userName)
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to resolve AUTH_SSO_DEFAULT_ORGANIZATION_ID for a new SSO user; the user was created without an organization", "err", err, "organizationId", orgID.String(), "userId", userID.String())
		return
	}
	for attempt := 1; attempt <= defaultMembershipAttempts; attempt++ {
		err = s.svc.repo.transaction(ctx, func(tx *Repository) error {
			if err := tx.addMembership(ctx, userID, orgID, role); err != nil {
				return err
			}
			return tx.unsubscribeOrganizationAlerts(ctx, userID, orgID)
		})
		if err == nil {
			return
		}
	}
	s.log.ErrorContext(ctx, "SSO provisioning: failed to assign new SSO user to its organization", "err", err, "organizationId", orgID.String(), "userId", userID.String())
}

// resolveDefaultOrganization finds or creates the default organization and returns the role its new member gets. The organization and its workspace are created in one transaction, so a failure leaves neither behind; a concurrent first sign-up that wins the race is read back as an existing organization.
func (s *SSO) resolveDefaultOrganization(ctx context.Context, orgID uuid.UUID, userName string) (string, error) {
	existingRole := s.cfg.SSO.DefaultOrganizationRole
	if existingRole == "" {
		existingRole = "manager"
	}
	exists, err := s.svc.repo.organizationExists(ctx, orgID)
	if err != nil || exists {
		return existingRole, err
	}
	created := false
	err = s.svc.repo.transaction(ctx, func(tx *Repository) error {
		var err error
		if created, err = tx.createOrganizationWithID(ctx, orgID, userName+"'s Organization"); err != nil || !created {
			return err
		}
		return tx.createDefaultWorkspace(ctx, orgID)
	})
	if err != nil {
		return "", err
	}
	if created {
		s.svc.billing.OrganizationCreated(ctx, orgID)
		return "owner", nil
	}
	return existingRole, nil
}

// completeRecovery redeems an emailed recovery link: it proves the clicker reads the account's mailbox, so the identity is linked and the browser signed in. An account whose address was never proven first loses the password, two-factor authentication and other single sign-on identities set on it, since whoever registered it is held out by nothing once the address is verified; every other session of the account ends, and the owner is told what was removed. Returns where the browser goes next.
func (s *SSO) completeRecovery(ctx context.Context, token string, meta clientMeta) (string, issuedToken, error) {
	if err := s.svc.limiter.ApplyIP(ctx, ratelimit.BASSORecovery); err != nil {
		return "", issuedToken{}, err
	}
	failed := func(userID string, details map[string]any, reason string, err error) (string, issuedToken, error) {
		s.log.ErrorContext(ctx, "SSO recovery failed", "userId", userID, "failureReason", reason)
		s.auditRecovery(ctx, audit.ActionSSORecoveryFailed, audit.StatusFailure, userID, withReason(details, reason))
		return "", issuedToken{}, err
	}
	v, err := s.svc.repo.findPayloadVerification(ctx, purposeSSORecovery, secret.HashToken(token))
	if err != nil {
		return "", issuedToken{}, err
	}
	if v == nil || v.ConsumedAt.Valid || !v.UserID.Valid {
		return failed(audit.Unknown, map[string]any{"provider": "unknown"}, "invalid_or_expired_intent", errInvalidToken())
	}
	userID := v.UserID.UUID
	var payload recoveryPayload
	if err := json.Unmarshal(v.Payload, &payload); err != nil {
		return "", issuedToken{}, err
	}
	details := map[string]any{"email": v.Email, "provider": payload.Provider, "callbackUrl": payload.CallbackURL}
	if !v.ExpiresAt.After(s.svc.now()) {
		return failed(userID.String(), details, "invalid_or_expired_intent", errTokenExpired())
	}
	provider := normalizeProvider(payload.Provider)
	if provider == "" {
		return failed(userID.String(), details, "invalid_provider", errAccountNotLinked())
	}

	var sess issuedToken
	var acc *account
	var reclaimed *reclaimOutcome
	var sessionsRevoked int64
	reason := ""
	err = s.svc.repo.transaction(ctx, func(tx *Repository) error {
		claimed, err := tx.consumePayloadVerification(ctx, purposeSSORecovery, secret.HashToken(token), s.svc.now())
		if err != nil {
			return err
		}
		if claimed == nil {
			reason = "invalid_or_expired_intent"
			return errInvalidToken()
		}
		if acc, err = tx.lockAccount(ctx, userID); err != nil {
			return err
		}
		// The link was mailed to the address the account had then; one that has moved since proves nothing about the current one.
		if acc == nil || normalizeEmail(acc.Email) != v.Email {
			reason = "user_mismatch"
			return errAccountNotLinked()
		}
		if !acc.IsActive {
			reason = "inactive_user"
			return errAccountInactive()
		}
		owner, err := tx.identityOwner(ctx, provider, payload.Subject)
		if err != nil {
			return err
		}
		if owner != uuid.Nil && owner != acc.ID {
			reason = "identity_linked_to_another_user"
			return errAccountNotLinked()
		}
		if !acc.EmailVerified {
			outcome, err := tx.reclaimUnprovenAccount(ctx, acc.ID, provider, payload.Subject, s.svc.now())
			if err != nil {
				return err
			}
			reclaimed = &outcome
			// Whoever held the account before its address was proven is signed out with it.
			if sessionsRevoked, err = s.svc.sessions.RevokeAll(ctx, tx.db, acc.ID, uuid.Nil); err != nil {
				return err
			}
		}
		if owner == uuid.Nil {
			if err := tx.insertIdentity(ctx, acc.ID, provider, payload.Subject); err != nil {
				return err
			}
		}
		sess, err = s.svc.sessions.create(ctx, tx.db, acc.ID, meta)
		return err
	})
	if err != nil {
		if reason != "" {
			return failed(userID.String(), details, reason, err)
		}
		return "", issuedToken{}, err
	}

	completed := details
	if reclaimed != nil {
		completed = map[string]any{
			"email": v.Email, "provider": provider, "callbackUrl": payload.CallbackURL,
			"credentialPasswordsCleared": reclaimed.PasswordsCleared, "twoFactorRowsRemoved": reclaimed.TwoFactorsRemoved,
			"externalIdentitiesRemoved": reclaimed.IdentitiesRemoved, "sessionsRevoked": sessionsRevoked,
		}
		s.notifyFactorsRemoved(ctx, acc, *reclaimed)
	}
	s.svc.finalizeSignIn(ctx, acc.ID, signInMethodSSO)
	s.log.InfoContext(ctx, "SSO recovery completed", "userId", acc.ID.String(), "provider", provider)
	s.auditRecovery(ctx, audit.ActionSSORecoveryCompleted, audit.StatusSuccess, acc.ID.String(), completed)
	redirect := validatedCallbackURL(s.cfg.WebAppURL, payload.CallbackURL)
	if redirect == "" {
		redirect = s.svc.pageURL("/")
	}
	return redirect, sess, nil
}

// notifyFactorsRemoved tells the owner what recovery took away (forma2's ENG-2633): correct for someone who registered on another's address, but a silent downgrade for an owner who simply never verified, and nothing distinguishes the two. Best effort: the recovery has already committed.
func (s *SSO) notifyFactorsRemoved(ctx context.Context, acc *account, removed reclaimOutcome) {
	content := mailer.SSORecoveryFactorsRemoved{
		PasswordRemoved:      removed.PasswordsCleared > 0,
		TwoFactorRemoved:     removed.TwoFactorsRemoved > 0,
		SingleSignOnRemoved:  removed.IdentitiesRemoved > 0,
		SecuritySettingsLink: s.svc.pageURL(securitySettingsPath),
	}
	if !content.PasswordRemoved && !content.TwoFactorRemoved && !content.SingleSignOnRemoved {
		return
	}
	sent, err := s.svc.mailer.SendTemplate(ctx, acc.Email, acc.Locale, content)
	switch {
	case err != nil:
		s.log.ErrorContext(ctx, "Failed to notify the user that SSO recovery removed their local sign-in factors", "err", err, "userId", acc.ID.String())
	case !sent:
		s.log.ErrorContext(ctx, "SSO recovery removed local sign-in factors but the notification email was not sent",
			"userId", acc.ID.String(), "passwordRemoved", content.PasswordRemoved, "twoFactorRemoved", content.TwoFactorRemoved, "singleSignOnRemoved", content.SingleSignOnRemoved)
	}
}
