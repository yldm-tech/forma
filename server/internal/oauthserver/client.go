package oauthserver

import (
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// idTokenLifetime is how long an ID token is valid.
const idTokenLifetime = time.Hour

// Client is a registered OAuth client, as op reads it (op.Client).
type Client struct {
	id              string
	secretHash      []byte
	name            string
	clientURI       *string
	logoURI         *string
	redirectURIs    []string
	applicationType string
	authMethod      string
	grantTypes      []string
	responseTypes   []string
	scopes          []string
	resources       []string
	disabled        bool
	createdAt       time.Time

	// loginURL is where op sends the browser after it accepted an authorization request: the step that signs the user in and asks for consent.
	loginURL func(requestID string) string
}

func (c *Client) GetID() string                       { return c.id }
func (c *Client) RedirectURIs() []string              { return c.redirectURIs }
func (c *Client) PostLogoutRedirectURIs() []string    { return nil }
func (c *Client) LoginURL(requestID string) string    { return c.loginURL(requestID) }
func (c *Client) AccessTokenType() op.AccessTokenType { return op.AccessTokenTypeBearer }
func (c *Client) IDTokenLifetime() time.Duration      { return idTokenLifetime }
func (c *Client) DevMode() bool                       { return false }
func (c *Client) ClockSkew() time.Duration            { return 0 }

// IsScopeAllowed lets every scope through op's filter, which would otherwise drop an unknown scope silently; CreateAuthRequest refuses one outside the client's scopes with invalid_scope, as forma2 did.
func (c *Client) IsScopeAllowed(string) bool           { return true }
func (c *Client) IDTokenUserinfoClaimsAssertion() bool { return false }

func (c *Client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *Client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *Client) ApplicationType() op.ApplicationType {
	if c.applicationType == applicationTypeNative {
		return op.ApplicationTypeNative
	}
	return op.ApplicationTypeWeb
}

func (c *Client) AuthMethod() oidc.AuthMethod {
	switch c.authMethod {
	case authMethodNone:
		return oidc.AuthMethodNone
	case authMethodPost:
		return oidc.AuthMethodPost
	default:
		return oidc.AuthMethodBasic
	}
}

func (c *Client) ResponseTypes() []oidc.ResponseType {
	out := make([]oidc.ResponseType, len(c.responseTypes))
	for i, t := range c.responseTypes {
		out[i] = oidc.ResponseType(t)
	}
	return out
}

func (c *Client) GrantTypes() []oidc.GrantType {
	out := make([]oidc.GrantType, len(c.grantTypes))
	for i, t := range c.grantTypes {
		out[i] = oidc.GrantType(t)
	}
	return out
}

// allowsResource reports whether the client may be issued tokens for the resource.
func (c *Client) allowsResource(resource string) bool {
	return slices.Contains(c.resources, resource)
}

// AuthRequest is an authorization in progress (op.AuthRequest).
type AuthRequest struct {
	id            uuid.UUID
	createdAt     time.Time
	clientID      string
	redirectURI   string
	scopes        []string
	resource      string
	state         string
	nonce         string
	responseMode  string
	prompt        []string
	codeChallenge string
	userID        *uuid.UUID
	authTime      *time.Time
}

func (a *AuthRequest) GetID() string                      { return a.id.String() }
func (a *AuthRequest) GetACR() string                     { return "" }
func (a *AuthRequest) GetAMR() []string                   { return nil }
func (a *AuthRequest) GetClientID() string                { return a.clientID }
func (a *AuthRequest) GetNonce() string                   { return a.nonce }
func (a *AuthRequest) GetRedirectURI() string             { return a.redirectURI }
func (a *AuthRequest) GetResponseType() oidc.ResponseType { return oidc.ResponseTypeCode }
func (a *AuthRequest) GetResponseMode() oidc.ResponseMode { return oidc.ResponseMode(a.responseMode) }
func (a *AuthRequest) GetScopes() []string                { return a.scopes }
func (a *AuthRequest) GetState() string                   { return a.state }
func (a *AuthRequest) Done() bool                         { return a.userID != nil }

// GetAudience is the ID token's audience, the client. The access token's audience is the approved resource, which the token issuer reads from the request itself.
func (a *AuthRequest) GetAudience() []string { return []string{a.clientID} }

func (a *AuthRequest) GetCodeChallenge() *oidc.CodeChallenge {
	return &oidc.CodeChallenge{Challenge: a.codeChallenge, Method: oidc.CodeChallengeMethodS256}
}

func (a *AuthRequest) GetSubject() string {
	if a.userID == nil {
		return ""
	}
	return a.userID.String()
}

func (a *AuthRequest) GetAuthTime() time.Time {
	if a.authTime == nil {
		return time.Time{}
	}
	return *a.authTime
}

// refreshRequest is a refresh token being redeemed (op.RefreshTokenRequest).
type refreshRequest struct {
	token  *storedToken
	scopes []string
}

func (r *refreshRequest) GetAMR() []string            { return nil }
func (r *refreshRequest) GetAudience() []string       { return []string{r.token.ClientID} }
func (r *refreshRequest) GetAuthTime() time.Time      { return r.token.AuthTime }
func (r *refreshRequest) GetClientID() string         { return r.token.ClientID }
func (r *refreshRequest) GetScopes() []string         { return r.scopes }
func (r *refreshRequest) GetSubject() string          { return r.token.UserID.String() }
func (r *refreshRequest) SetCurrentScopes(s []string) { r.scopes = s }
