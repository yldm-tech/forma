package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// SSO provider ids. They are forma2's provider ids, the user_identities.provider values and the last segment of the callback paths.
const (
	providerGoogle  = "google"
	providerGitHub  = "github"
	providerAzureAD = "azuread"
	providerOpenID  = "openid"
)

// normalizeProvider is forma2's normalizeSsoProvider: provider ids are matched without regard to case, and "azure-ad", the NextAuth-era id, still names Azure AD. SAML is recognized by forma2 but has no provider here. It returns "" for anything else.
func normalizeProvider(id string) string {
	switch strings.ToLower(id) {
	case providerGoogle:
		return providerGoogle
	case providerGitHub:
		return providerGitHub
	case "azure-ad", providerAzureAD:
		return providerAzureAD
	case providerOpenID:
		return providerOpenID
	}
	return ""
}

// Endpoints of the hosted identity providers. Tests point a provider at a local fake instead.
const (
	googleDiscoveryURL     = "https://accounts.google.com/.well-known/openid-configuration"
	githubAuthorizeURL     = "https://github.com/login/oauth/authorize"
	githubTokenURL         = "https://github.com/login/oauth/access_token" //nolint:gosec // an endpoint, not a credential
	githubAPIURL           = "https://api.github.com"
	microsoftLoginURL      = "https://login.microsoftonline.com"
	microsoftGraphUserInfo = "https://graph.microsoft.com/oidc/userinfo"
)

// ssoHTTPTimeout bounds every call to an identity provider: discovery, keys, the token exchange and the profile reads.
const ssoHTTPTimeout = 10 * time.Second

// ssoResponseLimit bounds what is read from an identity provider's JSON endpoints.
const ssoResponseLimit = 1 << 20

// identitySource is where a provider's signed-in identity comes from.
type identitySource int

const (
	// sourceIDToken verifies the id_token against the issuer and keys of the provider's discovery document, as Better Auth did whenever discovery yielded both.
	sourceIDToken identitySource = iota
	// sourceMicrosoftGraph reads Microsoft Graph's userinfo with the access token. It serves Microsoft's multi-tenant authorities, whose discovery documents advertise a placeholder issuer no id_token can match (ENG-2750); the id_token is never read on that path, verified or not.
	sourceMicrosoftGraph
	// sourceGitHub reads GitHub's user and email APIs; GitHub is plain OAuth 2.0 without an id_token.
	sourceGitHub
)

// ssoProvider is one configured identity provider.
type ssoProvider struct {
	id     string
	source identitySource
	// oauth carries the client credentials, scopes and redirect URI; for sourceIDToken its endpoint comes from discovery.
	oauth oauth2.Config
	// discoveryURL is read once, on first use, for sourceIDToken.
	discoveryURL string
	// userInfoURL is Graph's userinfo for sourceMicrosoftGraph and the API root for sourceGitHub.
	userInfoURL string

	mu         sync.Mutex
	discovered *discoveredProvider
}

// discoveredProvider is what a discovery document gave: the endpoints and an id_token verifier bound to the advertised issuer and keys.
type discoveredProvider struct {
	issuer   string
	endpoint oauth2.Endpoint
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// authIssuerURL is forma2's getAuthIssuerUrl: BETTER_AUTH_URL, else NEXTAUTH_URL, else WEBAPP_URL, without query, fragment or trailing slashes, with /api/auth appended unless the path already ends with it.
func authIssuerURL(cfg config.Config) string {
	base := cfg.WebAppURL
	switch {
	case cfg.SSO.BetterAuthURL != nil:
		base = cfg.SSO.BetterAuthURL
	case cfg.SSO.NextAuthURL != nil:
		base = cfg.SSO.NextAuthURL
	}
	path := strings.TrimRight(base.Path, "/")
	if !strings.HasSuffix(path, "/api/auth") {
		path += "/api/auth"
	}
	return urlOrigin(base) + path
}

// urlOrigin is a URL's origin as a browser serializes it: lowercase scheme and host, default port omitted, no credentials.
func urlOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port == "" || (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

// microsoftPlaceholderAuthorities are the Microsoft authorities whose discovery documents advertise the literal "{tenantid}" issuer. consumers is not among them: its issuer is the real personal-account tenant, so it keeps full id_token verification.
var microsoftPlaceholderAuthorities = map[string]bool{"common": true, "organizations": true}

// microsoftPlaceholderAuthority returns the multi-tenant authority an issuer URL names on login.microsoftonline.com, matched on the parsed host so a lookalike host is not mistaken for Microsoft, or "".
func microsoftPlaceholderAuthority(issuer string) string {
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil || strings.ToLower(u.Host) != "login.microsoftonline.com" {
		return ""
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "" {
			continue
		}
		if authority := strings.ToLower(segment); microsoftPlaceholderAuthorities[authority] {
			return authority
		}
		return ""
	}
	return ""
}

// useMicrosoftGraph configures explicit endpoints for a placeholder-issuer authority and takes identity from Graph, skipping discovery.
func (p *ssoProvider) useMicrosoftGraph(authority string) {
	p.source = sourceMicrosoftGraph
	p.oauth.Endpoint = oauth2.Endpoint{
		AuthURL:  microsoftLoginURL + "/" + authority + "/oauth2/v2.0/authorize",
		TokenURL: microsoftLoginURL + "/" + authority + "/oauth2/v2.0/token",
	}
	p.userInfoURL = microsoftGraphUserInfo
}

// newSSOProviders builds the providers whose credentials are configured. Like forma2 it warns at start-up when Azure AD or the generic provider is pointed at a placeholder-issuer authority, since id_tokens cannot be verified there.
func newSSOProviders(cfg config.Config, log *slog.Logger) map[string]*ssoProvider {
	issuer := authIssuerURL(cfg)
	sso := cfg.SSO
	oidcScopes := []string{"openid", "email", "profile"}
	providers := map[string]*ssoProvider{}
	if sso.GoogleEnabled() {
		providers[providerGoogle] = &ssoProvider{
			id: providerGoogle, source: sourceIDToken, discoveryURL: googleDiscoveryURL,
			oauth: oauth2.Config{ClientID: sso.GoogleClientID, ClientSecret: sso.GoogleClientSecret, RedirectURL: issuer + "/callback/google", Scopes: oidcScopes},
		}
	}
	if sso.GitHubEnabled() {
		providers[providerGitHub] = &ssoProvider{
			id: providerGitHub, source: sourceGitHub, userInfoURL: githubAPIURL,
			oauth: oauth2.Config{
				ClientID: sso.GitHubID, ClientSecret: sso.GitHubSecret, RedirectURL: issuer + "/callback/github", Scopes: []string{"read:user", "user:email"},
				Endpoint: oauth2.Endpoint{AuthURL: githubAuthorizeURL, TokenURL: githubTokenURL},
			},
		}
	}
	if sso.AzureADEnabled() {
		p := &ssoProvider{
			id:    providerAzureAD,
			oauth: oauth2.Config{ClientID: sso.AzureADClientID, ClientSecret: sso.AzureADClientSecret, RedirectURL: issuer + "/oauth2/callback/azuread", Scopes: oidcScopes},
		}
		tenant := strings.TrimSpace(sso.AzureADTenantID)
		if tenant == "" {
			tenant = "common"
		}
		if microsoftPlaceholderAuthorities[strings.ToLower(tenant)] {
			p.useMicrosoftGraph(strings.ToLower(tenant))
			if strings.TrimSpace(sso.AzureADTenantID) != "" {
				log.Warn(fmt.Sprintf(`AZUREAD_TENANT_ID="%s" names a Microsoft multi-tenant authority whose discovery document advertises a placeholder issuer, so id_tokens cannot be verified against it. Skipping discovery for this provider and taking identity from the userinfo endpoint; the authority you configured still applies at sign-in. Set a Directory (tenant) ID for full id_token verification.`, tenant))
			}
		} else {
			p.source = sourceIDToken
			p.discoveryURL = microsoftLoginURL + "/" + tenant + "/v2.0/.well-known/openid-configuration"
		}
		providers[providerAzureAD] = p
	}
	if sso.OIDCEnabled() {
		p := &ssoProvider{
			id:    providerOpenID,
			oauth: oauth2.Config{ClientID: sso.OIDCClientID, ClientSecret: sso.OIDCClientSecret, RedirectURL: issuer + "/oauth2/callback/openid", Scopes: oidcScopes},
		}
		if authority := microsoftPlaceholderAuthority(sso.OIDCIssuer); authority != "" {
			p.useMicrosoftGraph(authority)
			log.Warn(fmt.Sprintf(`OIDC_ISSUER points at Microsoft's "%s" authority, whose discovery document advertises a placeholder issuer, so id_tokens cannot be verified against it. Skipping discovery for this provider and taking identity from the userinfo endpoint; the authority you configured still applies at sign-in. Prefer the dedicated AZUREAD_CLIENT_ID / AZUREAD_CLIENT_SECRET provider for Microsoft Entra ID.`, authority))
		} else {
			p.source = sourceIDToken
			// Appended as forma2 did, without normalizing a trailing slash on the issuer.
			p.discoveryURL = sso.OIDCIssuer + "/.well-known/openid-configuration"
		}
		providers[providerOpenID] = p
	}
	return providers
}

// errUnverifiedIdentity marks an identity the provider's answer did not establish: a failed verification, a missing subject, an unreadable profile. The cause is logged; the browser learns only unable_to_get_user_info.
var errUnverifiedIdentity = errors.New("identity provider did not establish an identity")

// discover returns the provider's discovery result, fetching and caching it on first use. A failure is not cached, so a provider that was unreachable is retried on the next sign-in.
func (p *ssoProvider) discover(ctx context.Context, client *http.Client) (*discoveredProvider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovered != nil {
		return p.discovered, nil
	}
	var doc oidc.ProviderConfig
	if err := getJSON(ctx, client, p.discoveryURL, "", &doc); err != nil {
		return nil, fmt.Errorf("read %s discovery document: %w", p.id, err)
	}
	if doc.IssuerURL == "" || doc.AuthURL == "" || doc.TokenURL == "" || doc.JWKSURL == "" {
		return nil, fmt.Errorf("%s discovery document lacks issuer, authorization, token or jwks endpoint", p.id)
	}
	provider := doc.NewProvider(oidc.ClientContext(context.Background(), client))
	p.discovered = &discoveredProvider{
		issuer:   doc.IssuerURL,
		endpoint: provider.Endpoint(),
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: p.oauth.ClientID}),
	}
	return p.discovered, nil
}

// config returns the OAuth client for one request, with the discovered endpoint for sourceIDToken.
func (p *ssoProvider) config(ctx context.Context, client *http.Client) (*oauth2.Config, *discoveredProvider, error) {
	conf := p.oauth
	if p.source != sourceIDToken {
		return &conf, nil, nil
	}
	d, err := p.discover(ctx, client)
	if err != nil {
		return nil, nil, err
	}
	conf.Endpoint = d.endpoint
	return &conf, d, nil
}

// expectedIssuer is the RFC 9207 iss the authorization response must carry when it carries one. Providers without discovery declare none, so the check is skipped for them, as Better Auth skipped it.
func (p *ssoProvider) expectedIssuer(d *discoveredProvider) string {
	if d == nil {
		return ""
	}
	return d.issuer
}

// ssoIdentity is who the provider says signed in.
type ssoIdentity struct {
	Subject string
	Email   string
	// Name is the provider's display name, unnormalized; it may be empty.
	Name string
	// EmailVerified is the provider's answer after forma2's trust policy for this provider.
	EmailVerified bool
}

// resolveIdentity turns a token response into the signed-in identity, failing closed on anything the provider did not establish.
func (p *ssoProvider) resolveIdentity(ctx context.Context, client *http.Client, d *discoveredProvider, token *oauth2.Token, nonce string) (ssoIdentity, error) {
	var claims map[string]any
	var err error
	switch p.source {
	case sourceIDToken:
		claims, err = d.claims(ctx, client, token, nonce)
	case sourceMicrosoftGraph:
		claims, err = bearerJSON(ctx, client, p.userInfoURL, token.AccessToken)
	case sourceGitHub:
		return githubIdentity(ctx, client, p.userInfoURL, token.AccessToken)
	}
	if err != nil {
		return ssoIdentity{}, err
	}
	return identityFromClaims(p.id, claims)
}

// claims verifies the id_token (signature, issuer, audience, expiry and nonce) and returns its claims. Better Auth's shortcut took identity from the id_token when it carried both sub and email; otherwise, or without an id_token, the claims come from the discovered userinfo endpoint, whose subject must then equal the id_token's.
func (d *discoveredProvider) claims(ctx context.Context, client *http.Client, token *oauth2.Token, nonce string) (map[string]any, error) {
	subject := ""
	if raw, ok := token.Extra("id_token").(string); ok && raw != "" {
		idToken, err := d.verifier.Verify(oidc.ClientContext(ctx, client), raw)
		if err != nil {
			return nil, fmt.Errorf("%w: verify id_token: %w", errUnverifiedIdentity, err)
		}
		if !secretEqual(idToken.Nonce, nonce) {
			return nil, fmt.Errorf("%w: id_token nonce does not match", errUnverifiedIdentity)
		}
		var payload json.RawMessage
		if err := idToken.Claims(&payload); err != nil {
			return nil, fmt.Errorf("%w: read id_token claims: %w", errUnverifiedIdentity, err)
		}
		claims, err := decodeClaims(payload)
		if err != nil {
			return nil, fmt.Errorf("%w: read id_token claims: %w", errUnverifiedIdentity, err)
		}
		if claimString(claims["sub"]) != "" && claimString(claims["email"]) != "" {
			return claims, nil
		}
		subject = idToken.Subject
	}
	userInfoURL := d.provider.UserInfoEndpoint()
	if userInfoURL == "" {
		return nil, fmt.Errorf("%w: no id_token with sub and email, and no userinfo endpoint", errUnverifiedIdentity)
	}
	claims, err := bearerJSON(ctx, client, userInfoURL, token.AccessToken)
	if err != nil {
		return nil, err
	}
	if subject != "" && claimString(claims["sub"]) != subject {
		return nil, fmt.Errorf("%w: userinfo subject differs from the id_token's", errUnverifiedIdentity)
	}
	return claims, nil
}

// identityFromClaims maps an OpenID Connect profile the way forma2's provider mappers did.
func identityFromClaims(provider string, claims map[string]any) (ssoIdentity, error) {
	id := ssoIdentity{Subject: claimString(claims["sub"]), Email: claimText(claims["email"])}
	if id.Subject == "" {
		return ssoIdentity{}, fmt.Errorf("%w: profile has no subject", errUnverifiedIdentity)
	}
	switch provider {
	case providerGoogle:
		// Google attests the address: only an explicit true counts.
		id.Name = claimText(claims["name"])
		verified, _ := claims["email_verified"].(bool)
		id.EmailVerified = verified
	case providerAzureAD:
		// Entra sends no email_verified; xms_edov is its optional equivalent, which a tenant must enable.
		id.Name = displayName(claims)
		raw, present := claims["email_verified"]
		if !present || raw == nil {
			raw = claims["xms_edov"]
		}
		id.EmailVerified = emailVerifiedFromRawClaim(raw)
	default:
		id.Name = displayName(claims)
		id.EmailVerified = emailVerifiedFromRawClaim(claims["email_verified"])
	}
	return id, nil
}

// displayName is forma2's toDisplayName: name, else given and family name, else preferred_username.
func displayName(claims map[string]any) string {
	if name := claimText(claims["name"]); name != "" {
		return name
	}
	var parts []string
	for _, key := range []string{"given_name", "family_name"} {
		if v := claimText(claims[key]); v != "" {
			parts = append(parts, v)
		}
	}
	if joined := strings.Join(parts, " "); joined != "" {
		return joined
	}
	return claimText(claims["preferred_username"])
}

// emailVerifiedFromRawClaim is forma2's resolveEmailVerifiedFromRawClaim for providers whose raw claim is trusted: only an explicit denial (false, 0, or "false"/"0" in any case and spacing) is unverified. An absent claim counts as verified, deliberately: most self-hosted identity providers never send it, and treating silence as a denial would mark every new user unverified. The cost is that Entra, which sends no email_verified, always resolves verified unless the tenant enables xms_edov, and its email is a mutable directory attribute.
func emailVerifiedFromRawClaim(raw any) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case json.Number:
		f, err := v.Float64()
		return err != nil || f != 0
	case float64:
		return v != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s != "false" && s != "0"
	}
	return true
}

// claimString reads a subject-like claim: a string as is, a number in its shortest decimal form, anything else as missing. Like forma2's toAccountSubject, a missing value never becomes a string such as "undefined".
func claimString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return ""
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// claimText reads a claim that must be a string.
func claimText(v any) string {
	s, _ := v.(string)
	return s
}

// decodeClaims decodes a JSON object keeping numbers exact.
func decodeClaims(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var claims map[string]any
	if err := dec.Decode(&claims); err != nil {
		return nil, err
	}
	if claims == nil {
		return nil, errors.New("not a JSON object")
	}
	return claims, nil
}

// bearerJSON reads a JSON object from an endpoint authenticated with the access token. Anything but a 200 with a JSON object fails closed, as forma2's microsoftGraphUserInfo did.
func bearerJSON(ctx context.Context, client *http.Client, endpoint, accessToken string) (map[string]any, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("%w: no access token", errUnverifiedIdentity)
	}
	var raw json.RawMessage
	if err := getJSON(ctx, client, endpoint, accessToken, &raw); err != nil {
		return nil, fmt.Errorf("%w: %w", errUnverifiedIdentity, err)
	}
	claims, err := decodeClaims(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: read profile: %w", errUnverifiedIdentity, err)
	}
	return claims, nil
}

// getJSON GETs a JSON document, with the access token as a bearer credential when one is given.
func getJSON(ctx context.Context, client *http.Client, endpoint, accessToken string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, ssoHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	// GitHub's API refuses requests without a User-Agent.
	req.Header.Set("User-Agent", "Forma")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", req.URL.Host, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, ssoResponseLimit)).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", req.URL.Host, err)
	}
	return nil
}

// githubIdentity is Better Auth's GitHub profile: the numeric user id as the subject, the display name or else the login, the public address or else the primary (or first) one from /user/emails. GitHub attests the address through /user/emails; a failed lookup leaves it unverified rather than verified.
func githubIdentity(ctx context.Context, client *http.Client, apiURL, accessToken string) (ssoIdentity, error) {
	profile, err := bearerJSON(ctx, client, apiURL+"/user", accessToken)
	if err != nil {
		return ssoIdentity{}, err
	}
	id := ssoIdentity{Subject: claimString(profile["id"]), Email: claimText(profile["email"]), Name: claimText(profile["name"])}
	if id.Subject == "" {
		return ssoIdentity{}, fmt.Errorf("%w: GitHub profile has no id", errUnverifiedIdentity)
	}
	if id.Name == "" {
		id.Name = claimText(profile["login"])
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, apiURL+"/user/emails", accessToken, &emails); err != nil {
		emails = nil
	}
	if id.Email == "" && len(emails) > 0 {
		id.Email = emails[0].Email
		for _, e := range emails {
			if e.Primary {
				id.Email = e.Email
				break
			}
		}
	}
	for _, e := range emails {
		if e.Email == id.Email {
			id.EmailVerified = e.Verified
			break
		}
	}
	return id, nil
}
