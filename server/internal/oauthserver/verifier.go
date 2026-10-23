package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// TokenVerifier checks access tokens for /api/mcp, the resource server half of forma2's oauthProviderResourceClient.verifyBearerToken: the signature against the authorization server's keys, the at+jwt type, the issuer, the expiry, and an audience naming the MCP resource. It never reads the token tables, so a revoked token stays valid until it expires.
type TokenVerifier struct {
	urls URLs
	keys oidc.KeySet
	now  func() time.Time
}

// NewTokenVerifier verifies against the keys in the database, or against the JWKS at MCP_OAUTH_JWKS_URL when it is set.
func NewTokenVerifier(cfg config.Config, keys *Keys) *TokenVerifier {
	v := &TokenVerifier{urls: NewURLs(cfg), keys: keys, now: time.Now}
	if cfg.MCPOAuthJWKSURL != nil {
		v.keys = oidc.NewRemoteKeySet(context.Background(), cfg.MCPOAuthJWKSURL.String())
	}
	return v
}

// ErrKeysUnavailable is a verification that failed because the signing keys could not be fetched, forma2's failureSource "jwks_fetch".
var ErrKeysUnavailable = errors.New("signing keys are unavailable")

// Verify returns the claims of a valid access token for the MCP resource.
func (v *TokenVerifier) Verify(ctx context.Context, token string) (*AccessTokenClaims, error) {
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("access token must carry exactly one signature")
	}
	typ, _ := jws.Signatures[0].Protected.ExtraHeaders[jose.HeaderType].(string)
	if !strings.EqualFold(typ, AccessTokenType) && !strings.EqualFold(typ, "application/"+AccessTokenType) {
		return nil, errors.New("token is not an access token")
	}
	payload, err := v.keys.VerifySignature(ctx, token)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
		}
		return nil, fmt.Errorf("verify access token signature: %w", err)
	}
	var claims AccessTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("decode access token: %w", err)
	}
	if claims.Issuer != v.urls.Issuer {
		return nil, errors.New("access token has another issuer")
	}
	if claims.Expiry == 0 || !time.Unix(claims.Expiry, 0).After(v.now()) {
		return nil, errors.New("access token has expired")
	}
	if !slices.Contains(claims.Audience, v.urls.Resource) {
		return nil, errors.New("access token was not issued for the MCP resource")
	}
	return &claims, nil
}
