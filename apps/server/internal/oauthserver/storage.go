package oauthserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Lifetimes, forma2's mcp-oauth-provider-options.ts: access tokens live 15 minutes (the write scopes' step-up expiry is the same 15 minutes), refresh tokens 30 days from their last use. An authorization request, and the code issued for it, lives 10 minutes.
const (
	AccessTokenLifetime  = 15 * time.Minute
	RefreshTokenLifetime = 30 * 24 * time.Hour
	authRequestLifetime  = 10 * time.Minute
)

// Prefixes of the opaque values, forma2's: refresh tokens and client secrets.
const (
	refreshTokenPrefix = "fbor_"
	clientSecretPrefix = "fbocs_"
)

// AccessTokenType is the JWT type of an access token (RFC 9068); /api/mcp refuses any other.
const AccessTokenType = "at+jwt"

// AccessTokenClaims are the claims of an access token: the RFC 9068 set /api/mcp reads, and the user's email and name, as forma2's customAccessTokenClaims added them.
type AccessTokenClaims struct {
	Issuer          string   `json:"iss"`
	Subject         string   `json:"sub"`
	Audience        Audience `json:"aud"`
	Expiry          int64    `json:"exp"`
	IssuedAt        int64    `json:"iat"`
	JWTID           string   `json:"jti"`
	ClientID        string   `json:"client_id"`
	AuthorizedParty string   `json:"azp"`
	Scope           string   `json:"scope"`
	Email           string   `json:"email,omitempty"`
	Name            string   `json:"name,omitempty"`
}

// Audience is the aud claim, which may be a single string or an array (RFC 7519 §4.1.3).
type Audience []string

func (a *Audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = Audience{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("aud must be a string or an array of strings: %w", err)
	}
	*a = list
	return nil
}

// storage is op's Storage over the tables of migration 00006.
type storage struct {
	repo   *repository
	keys   *Keys
	format *tokenFormat
	urls   URLs
	log    *slog.Logger
	now    func() time.Time
}

var (
	_ op.Storage                   = (*storage)(nil)
	_ op.CanSetUserinfoFromRequest = (*storage)(nil)
)

// formKey carries the parameters of the authorization or token request op is handling; op parses only the ones it knows, and the resource indicator (RFC 8707) is not among them.
type formKey struct{}

func withForm(ctx context.Context, form url.Values) context.Context {
	return context.WithValue(ctx, formKey{}, form)
}

func formFrom(ctx context.Context) url.Values {
	form, _ := ctx.Value(formKey{}).(url.Values)
	return form
}

// loginPath is where op sends the browser once it accepted an authorization request.
const loginPath = "/oauth2/continue"

func (s *storage) loginURL(requestID string) string {
	return s.urls.Issuer + loginPath + "?id=" + url.QueryEscape(requestID)
}

func (s *storage) Health(context.Context) error { return nil }

// ---- clients ----

func (s *storage) client(ctx context.Context, clientID string) (*Client, error) {
	c, err := s.repo.client(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if c == nil || c.disabled {
		return nil, errors.New("client not found")
	}
	c.loginURL = s.loginURL
	return c, nil
}

func (s *storage) GetClientByClientID(ctx context.Context, clientID string) (op.Client, error) {
	return s.client(ctx, clientID)
}

func (s *storage) AuthorizeClientIDSecret(ctx context.Context, clientID, clientSecret string) error {
	c, err := s.client(ctx, clientID)
	if err != nil {
		return err
	}
	if c.secretHash == nil || subtle.ConstantTimeCompare(c.secretHash, secret.HashToken(clientSecret)) != 1 {
		return errors.New("invalid client secret")
	}
	return nil
}

// ---- authorization requests ----

// CreateAuthRequest records an authorization request op accepted, after the checks op does not make: the redirect URI must be registered exactly (op lets a native client's loopback redirect use any port), every scope must be one the client registered, PKCE must use S256, and the resource indicator, when given, must name a resource the client may be issued tokens for. Without one, the MCP resource is meant.
func (s *storage) CreateAuthRequest(ctx context.Context, req *oidc.AuthRequest, _ string) (op.AuthRequest, error) {
	c, err := s.client(ctx, req.ClientID)
	if err != nil {
		return nil, oidc.ErrInvalidRequestRedirectURI().WithDescription("unknown client").WithParent(err)
	}
	if !slices.Contains(c.redirectURIs, req.RedirectURI) {
		return nil, oidc.ErrInvalidRequestRedirectURI().WithDescription("The requested redirect_uri is missing in the client configuration.")
	}
	for _, scope := range req.Scopes {
		if !slices.Contains(Scopes, scope) || !slices.Contains(c.scopes, scope) {
			return nil, oidc.ErrInvalidScope().WithDescription("The requested scope is invalid, unknown, or malformed.")
		}
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != oidc.CodeChallengeMethodS256 {
		return nil, oidc.ErrInvalidRequest().WithDescription("PKCE with code_challenge_method S256 is required.")
	}
	resource, ok := requestedResource(formFrom(ctx), c, s.urls.Resource)
	if !ok {
		return nil, oidc.ErrInvalidTarget().WithDescription("The requested resource is invalid, unknown, or not allowed for this client.")
	}
	a := &AuthRequest{
		clientID: c.id, redirectURI: req.RedirectURI, scopes: slices.Compact(slices.Clone(req.Scopes)), resource: resource, state: req.State, nonce: req.Nonce,
		responseMode: string(req.ResponseMode), prompt: append([]string{}, req.Prompt...), codeChallenge: req.CodeChallenge,
	}
	if err := s.repo.insertAuthRequest(ctx, a, s.now().Add(authRequestLifetime)); err != nil {
		return nil, err
	}
	return a, nil
}

// requestedResource is the one resource the request's resource indicators name, the default when there are none. It is false when they name a resource the client may not use, or more than one.
func requestedResource(form url.Values, c *Client, fallback string) (string, bool) {
	values := slices.Compact(slices.Sorted(slices.Values(form["resource"])))
	switch len(values) {
	case 0:
		return fallback, c.allowsResource(fallback)
	case 1:
		return values[0], c.allowsResource(values[0])
	default:
		return "", false
	}
}

func parseID(id string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, errors.New("authorization request not found")
	}
	return parsed, nil
}

func (s *storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	parsed, err := parseID(id)
	if err != nil {
		return nil, err
	}
	a, err := s.repo.authRequest(ctx, parsed)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("authorization request not found")
	}
	return a, nil
}

func (s *storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	a, err := s.repo.authRequestByCode(ctx, secret.HashToken(code))
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("authorization code not found")
	}
	return a, nil
}

func (s *storage) SaveAuthCode(ctx context.Context, id, code string) error {
	parsed, err := parseID(id)
	if err != nil {
		return err
	}
	return s.repo.saveCode(ctx, parsed, secret.HashToken(code))
}

func (s *storage) DeleteAuthRequest(ctx context.Context, id string) error {
	parsed, err := parseID(id)
	if err != nil {
		return err
	}
	return s.repo.deleteAuthRequest(ctx, parsed)
}

// ---- tokens ----

// grantOf is what a token request carries: an approved authorization request redeemed by its code, or a refresh token.
func grantOf(request op.TokenRequest) (grant, *storedToken, error) {
	switch r := request.(type) {
	case *AuthRequest:
		if r.userID == nil {
			return grant{}, nil, oidc.ErrInvalidGrant()
		}
		return grant{ClientID: r.clientID, UserID: *r.userID, Scopes: r.scopes, Resources: []string{r.resource}, AuthTime: r.GetAuthTime()}, nil, nil
	case *refreshRequest:
		return grant{ClientID: r.token.ClientID, UserID: r.token.UserID, Scopes: r.scopes, Resources: r.token.Resources, AuthTime: r.token.AuthTime}, r.token, nil
	default:
		return grant{}, nil, oidc.ErrUnsupportedGrantType()
	}
}

func (s *storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	g, _, err := grantOf(request)
	if err != nil {
		return "", time.Time{}, err
	}
	u, err := s.activeUser(ctx, g.UserID)
	if err != nil {
		return "", time.Time{}, err
	}
	var id string
	var exp time.Time
	err = s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, exp, err = s.issueAccessToken(ctx, tx, g, nil, u)
		return err
	})
	return id, exp, err
}

// CreateAccessAndRefreshTokens issues an access token with a refresh token: a new family for an authorization code, the next token of the family for a refresh token, which is spent in the same transaction. A refresh token spent twice is a replay; it revokes the whole family.
func (s *storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, _ string) (string, string, time.Time, error) {
	g, current, err := grantOf(request)
	if err != nil {
		return "", "", time.Time{}, err
	}
	u, err := s.activeUser(ctx, g.UserID)
	if err != nil {
		return "", "", time.Time{}, err
	}
	family := uuid.New()
	if current != nil {
		family = current.FamilyID
	}
	raw, err := secret.NewToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	refresh := refreshTokenPrefix + raw
	replayed := false
	var id string
	var exp time.Time
	err = s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if current != nil {
			spent, err := s.repo.spendRefreshToken(ctx, tx, current.ID)
			if err != nil {
				return err
			}
			if !spent {
				replayed = true
				return oidc.ErrInvalidGrant().WithDescription("The refresh token was already used.")
			}
		}
		if err := s.repo.insertRefreshToken(ctx, tx, g, family, secret.HashToken(refresh), s.now().Add(RefreshTokenLifetime)); err != nil {
			return err
		}
		id, exp, err = s.issueAccessToken(ctx, tx, g, &family, u)
		return err
	})
	if replayed {
		s.revokeReplayedFamily(ctx, family)
	}
	if err != nil {
		return "", "", time.Time{}, err
	}
	return id, refresh, exp, nil
}

func (s *storage) revokeReplayedFamily(ctx context.Context, family uuid.UUID) {
	if err := s.repo.revokeFamily(ctx, family); err != nil {
		s.log.ErrorContext(ctx, "Failed to revoke a replayed refresh token family", "err", err)
		return
	}
	s.log.WarnContext(ctx, "OAuth refresh token replayed; its family was revoked")
}

func (s *storage) activeUser(ctx context.Context, id uuid.UUID) (*user, error) {
	u, err := s.repo.user(ctx, id)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive {
		return nil, oidc.ErrInvalidGrant().WithDescription("The user is not active.")
	}
	return u, nil
}

// audience is an access token's aud: the approved resources, and the userinfo endpoint when openid was granted, as Better Auth did.
func (s *storage) audience(g grant) []string {
	aud := slices.Clone(g.Resources)
	if slices.Contains(g.Scopes, oidc.ScopeOpenID) {
		aud = append(aud, s.urls.UserInfo)
	}
	return aud
}

// issueAccessToken records an access token and signs it; tokenFormat hands the signed token to op.
func (s *storage) issueAccessToken(ctx context.Context, tx *gorm.DB, g grant, family *uuid.UUID, u *user) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(AccessTokenLifetime)
	id, err := s.repo.insertAccessToken(ctx, tx, g, family, exp)
	if err != nil {
		return "", time.Time{}, err
	}
	claims := AccessTokenClaims{
		Issuer: s.urls.Issuer, Subject: g.UserID.String(), Audience: s.audience(g), Expiry: exp.Unix(), IssuedAt: now.Unix(), JWTID: id.String(),
		ClientID: g.ClientID, AuthorizedParty: g.ClientID, Scope: strings.Join(g.Scopes, " "), Email: u.Email, Name: u.Name,
	}
	signed, err := s.sign(ctx, claims)
	if err != nil {
		return "", time.Time{}, err
	}
	s.format.stage(id.String(), signed)
	return id.String(), exp, nil
}

func (s *storage) sign(ctx context.Context, claims AccessTokenClaims) (string, error) {
	key, err := s.keys.signing(ctx)
	if err != nil {
		return "", err
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: jose.JSONWebKey{Key: key.private, KeyID: key.ID()}}, (&jose.SignerOptions{}).WithType(AccessTokenType))
	if err != nil {
		return "", fmt.Errorf("create access token signer: %w", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode access token: %w", err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return jws.CompactSerialize()
}

// TokenRequestByRefreshToken finds a refresh token being redeemed. A revoked one is refused, and when it was spent by rotation, presenting it again revokes its family.
func (s *storage) TokenRequestByRefreshToken(ctx context.Context, token string) (op.RefreshTokenRequest, error) {
	t, err := s.repo.refreshToken(ctx, secret.HashToken(token))
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, op.ErrInvalidRefreshToken
	}
	if t.RevokedAt != nil {
		s.revokeReplayedFamily(ctx, t.FamilyID)
		return nil, op.ErrInvalidRefreshToken
	}
	if !t.ExpiresAt.After(s.now()) {
		return nil, op.ErrInvalidRefreshToken
	}
	return &refreshRequest{token: t, scopes: t.Scopes}, nil
}

func (s *storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (string, string, error) {
	t, err := s.repo.refreshToken(ctx, secret.HashToken(token))
	if err != nil {
		return "", "", err
	}
	if t == nil || t.ClientID != clientID {
		return "", "", op.ErrInvalidRefreshToken
	}
	return t.UserID.String(), t.ID.String(), nil
}

// RevokeToken revokes an access token by its id, or a refresh token by its id or value together with its family (RFC 7009). A token of another client is refused; an unknown token is not an error.
func (s *storage) RevokeToken(ctx context.Context, tokenOrID, _, clientID string) *oidc.Error {
	notYours := oidc.ErrUnauthorizedClient().WithDescription("The token was not issued to this client.")
	var t *storedToken
	var err error
	access := false
	if id, parseErr := uuid.Parse(tokenOrID); parseErr == nil {
		if t, err = s.repo.accessToken(ctx, id); err == nil && t != nil {
			access = true
		} else if err == nil {
			t, err = s.repo.refreshTokenByID(ctx, id)
		}
	} else {
		t, err = s.repo.refreshToken(ctx, secret.HashToken(tokenOrID))
	}
	if err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	if t == nil {
		return nil
	}
	if t.ClientID != clientID {
		return notYours
	}
	if access {
		err = s.repo.revokeAccessToken(ctx, t.ID)
	} else {
		err = s.repo.revokeFamily(ctx, t.FamilyID)
	}
	if err != nil {
		return oidc.ErrServerError().WithParent(err)
	}
	return nil
}

func (s *storage) TerminateSession(context.Context, string, string) error { return nil }

// ---- keys ----

func (s *storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	return s.keys.signing(ctx)
}

func (s *storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.EdDSA}, nil
}

func (s *storage) KeySet(ctx context.Context) ([]op.Key, error) {
	keys, err := s.keys.published(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]op.Key, len(keys))
	for i, k := range keys {
		out[i] = publicKey{key: k}
	}
	return out, nil
}

// ---- userinfo and introspection ----

// liveAccessToken is the recorded access token behind a verified JWT, if it was neither revoked nor has expired, and its user is still active.
func (s *storage) liveAccessToken(ctx context.Context, tokenID, subject string) (*storedToken, *user, error) {
	id, err := uuid.Parse(tokenID)
	if err != nil {
		return nil, nil, oidc.ErrAccessDenied()
	}
	t, err := s.repo.accessToken(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if t == nil || t.RevokedAt != nil || !t.ExpiresAt.After(s.now()) || t.UserID.String() != subject {
		return nil, nil, oidc.ErrAccessDenied().WithDescription("The access token is not active.")
	}
	u, err := s.repo.user(ctx, t.UserID)
	if err != nil {
		return nil, nil, err
	}
	if u == nil || !u.IsActive {
		return nil, nil, oidc.ErrAccessDenied().WithDescription("The access token is not active.")
	}
	return t, u, nil
}

func setUserinfo(info *oidc.UserInfo, u *user, scopes []string) {
	info.Subject = u.ID.String()
	if slices.Contains(scopes, oidc.ScopeEmail) {
		info.Email = u.Email
		info.EmailVerified = oidc.Bool(u.EmailVerified)
	}
	if slices.Contains(scopes, oidc.ScopeProfile) {
		info.Name = u.Name
	}
}

func (s *storage) SetUserinfoFromToken(ctx context.Context, info *oidc.UserInfo, tokenID, subject, _ string) error {
	t, u, err := s.liveAccessToken(ctx, tokenID, subject)
	if err != nil {
		return err
	}
	if !slices.Contains(t.Scopes, oidc.ScopeOpenID) {
		return oidc.ErrAccessDenied().WithDescription("The access token was not granted openid.")
	}
	setUserinfo(info, u, t.Scopes)
	return nil
}

func (s *storage) SetUserinfoFromRequest(ctx context.Context, info *oidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	id, err := uuid.Parse(request.GetSubject())
	if err != nil {
		return err
	}
	u, err := s.activeUser(ctx, id)
	if err != nil {
		return err
	}
	setUserinfo(info, u, scopes)
	return nil
}

func (s *storage) SetUserinfoFromScopes(context.Context, *oidc.UserInfo, string, string, []string) error {
	return nil
}

// SetIntrospectionFromToken describes an active access token to the client it was issued to; to any other client it is inactive.
func (s *storage) SetIntrospectionFromToken(ctx context.Context, resp *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	t, u, err := s.liveAccessToken(ctx, tokenID, subject)
	if err != nil {
		return err
	}
	if t.ClientID != clientID {
		return oidc.ErrAccessDenied()
	}
	resp.Scope = t.Scopes
	resp.ClientID = t.ClientID
	resp.TokenType = oidc.BearerToken
	resp.Expiration = oidc.FromTime(t.ExpiresAt)
	resp.IssuedAt = oidc.FromTime(t.CreatedAt)
	resp.Subject = u.ID.String()
	resp.Audience = s.audience(t.grant)
	resp.Issuer = s.urls.Issuer
	resp.JWTID = t.ID.String()
	resp.Username = u.Email
	return nil
}

func (s *storage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}

func (s *storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errors.New("client assertions are not supported")
}

func (s *storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errors.New("the JWT profile grant is not supported")
}
