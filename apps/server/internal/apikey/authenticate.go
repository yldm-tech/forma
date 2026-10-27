package apikey

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// usageWriteInterval throttles last_used_at: a use is recorded when the previous one is at least this old, as forma2 did.
const usageWriteInterval = 30 * time.Second

// HeaderAPIKey carries an API key in any format. It takes precedence over Authorization.
const HeaderAPIKey = "X-Api-Key" //nolint:gosec // a header name, not a credential

// Principal is an authenticated API key, forma2's TAuthenticationApiKey.
type Principal struct {
	APIKeyID       uuid.UUID
	OrganizationID uuid.UUID
	// OrganizationAccess is read strictly: only an exact true grants.
	OrganizationAccess OrganizationAccess
	// WorkspacePermissions holds only grants on workspaces of the key's own organization.
	WorkspacePermissions []WorkspaceGrant
}

// Actor is the principal as an authorization subject.
func (p *Principal) Actor() authz.Actor {
	return authz.APIKey(p.APIKeyID)
}

// Options are a route's rules for which keys it accepts.
type Options struct {
	// AllowOrganizationOnly accepts a key without any workspace grant in its own organization. forma2's routes opted in one by one (v1 management surveys, v2 me, roles, teams, users, workspace-teams); v3 and MCP never did.
	AllowOrganizationOnly bool
}

// Authenticator resolves the API key a request carries.
type Authenticator struct {
	repo *Repository
	log  *slog.Logger
	now  func() time.Time
}

func NewAuthenticator(db *gorm.DB, log *slog.Logger) *Authenticator {
	return &Authenticator{repo: NewRepository(db), log: log, now: time.Now}
}

// KeyFromHeaders returns the API key a request carries, or "": the trimmed x-api-key header in any format, otherwise a Bearer token that is a current-format key. Other Bearer tokens (OAuth access tokens, JWTs) are left to their own authentication, so a legacy key only works through x-api-key.
func KeyFromHeaders(h http.Header) string {
	if key := strings.TrimSpace(h.Get(HeaderAPIKey)); key != "" {
		return key
	}
	token := BearerToken(h)
	if _, ok := parseV2(token); !ok {
		return ""
	}
	return token
}

// BearerToken returns the trimmed token of an Authorization header whose scheme is Bearer in any case, or "".
func BearerToken(h http.Header) string {
	value := strings.TrimSpace(h.Get("Authorization"))
	const prefix = "bearer "
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(value[len(prefix):])
}

// Authenticate returns the principal of the API key in h, or nil when there is none or it is not valid for a route with opts. Only a database failure is an error.
func (a *Authenticator) Authenticate(ctx context.Context, h http.Header, opts Options) (*Principal, error) {
	raw := KeyFromHeaders(h)
	if raw == "" {
		return nil, nil
	}
	key, err := a.verify(ctx, raw)
	if err != nil || key == nil {
		return nil, err
	}
	// A disabled key is refused after its secret was verified, so it costs what any other key does, and the warning names only a key whose holder proved they have it. Its use is not recorded: Last used stays the last request it was allowed.
	if key.DisabledAt.Valid {
		a.log.WarnContext(ctx, "A disabled API key was used", "apiKeyId", key.ID.String())
		return nil, nil
	}
	// A key whose creator the organization suspended is refused the same way while the suspension lasts, and works again once it is restored: a suspension is meant to be undone, and the key is the creator's way into the organization as much as their session is.
	if key.CreatorSuspended {
		a.log.WarnContext(ctx, "An API key of a suspended member was used", "apiKeyId", key.ID.String())
		return nil, nil
	}
	// So is a key whose creator's account the instance's operator deactivated (forma user deactivate), until it is reactivated: a deactivated user signs in nowhere, and their keys are their way in as much as a session.
	if key.CreatorDeactivated {
		a.log.WarnContext(ctx, "An API key of a deactivated user was used", "apiKeyId", key.ID.String())
		return nil, nil
	}
	// An expired key is refused the same way, from the instant it expires.
	if key.ExpiresAt.Valid && !a.now().Before(key.ExpiresAt.Time) {
		a.log.WarnContext(ctx, "An expired API key was used", "apiKeyId", key.ID.String())
		return nil, nil
	}
	if !key.LastUsedAt.Valid || !key.LastUsedAt.Time.After(a.now().Add(-usageWriteInterval)) {
		now := a.now()
		// A failed usage write never fails the request it records.
		if err := a.repo.touch(ctx, key.ID, now, now.Add(-usageWriteInterval)); err != nil {
			a.log.ErrorContext(ctx, "Failed to update API key usage", "err", err)
		}
	}
	grants, err := a.repo.grantsOf(ctx, key.ID)
	if err != nil {
		return nil, err
	}
	p := &Principal{APIKeyID: key.ID, OrganizationID: key.OrganizationID, OrganizationAccess: storedAccess(key.OrganizationAccess), WorkspacePermissions: []WorkspaceGrant{}}
	dropped := 0
	for _, g := range grants {
		// A grant on another organization's workspace can only come from a defect or an exploit; it is never honoured.
		if g.OrganizationID != key.OrganizationID {
			dropped++
			continue
		}
		p.WorkspacePermissions = append(p.WorkspacePermissions, WorkspaceGrant{WorkspaceID: g.WorkspaceID, WorkspaceName: g.WorkspaceName, Permission: g.Permission})
	}
	if dropped > 0 {
		a.log.WarnContext(ctx, "Cross-organization API-key workspace grants were filtered", "component", "authorization", "crossOrganizationGrantCount", dropped)
	}
	if !opts.AllowOrganizationOnly && len(p.WorkspacePermissions) == 0 {
		return nil, nil
	}
	return p, nil
}

// verify finds and checks the stored key for raw, or returns nil.
//
// A current-format key is found by the SHA-256 of its secret and then always costs one bcrypt comparison, against the stored hash or, when no row matched, against a dummy hash of the same cost, so an unknown key and a wrong one take the same time. A legacy key costs one indexed lookup and one constant-time comparison whether it exists or not.
func (a *Authenticator) verify(ctx context.Context, raw string) (*storedKey, error) {
	if s, ok := parseV2(raw); ok {
		key, err := a.repo.byLookupHash(ctx, sha256Hex(s))
		if err != nil {
			return nil, err
		}
		hash := ""
		if key != nil {
			hash = key.HashedKey
		}
		valid, err := secret.CheckPassword(hash, s)
		if err != nil {
			// A stored hash bcrypt cannot read (a legacy row found through a current-format key) is a mismatch.
			a.log.WarnContext(ctx, "Secret verification failed due to invalid hash format")
			valid = false
		}
		if key == nil || !valid {
			if key != nil {
				a.log.WarnContext(ctx, "API key bcrypt verification failed", "apiKeyId", key.ID.String())
			}
			return nil, nil
		}
		return key, nil
	}
	digest := sha256Hex(raw)
	key, err := a.repo.byLookupHash(ctx, digest)
	if err != nil || key == nil {
		return nil, err
	}
	if !legacyMatch(key.HashedKey, digest) {
		return nil, nil
	}
	return key, nil
}

type principalKey struct{}

// PrincipalFrom returns the API key a request authenticated with, or false.
func PrincipalFrom(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok
}

// Require is the API key middleware: it answers 401 not_authenticated unless the request carries a key valid for opts, and otherwise records the principal for PrincipalFrom. Like Sessions.Require it reports through httpapi.Fail, so the route group installs httpapi.ErrorHandler.
func (a *Authenticator) Require(opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, err := a.Authenticate(c.Request.Context(), c.Request.Header, opts)
		if err != nil {
			httpapi.Fail(c, err)
			return
		}
		if p == nil {
			httpapi.Fail(c, apperr.NotAuthenticated("Not authenticated"))
			return
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), principalKey{}, p))
		c.Next()
	}
}
