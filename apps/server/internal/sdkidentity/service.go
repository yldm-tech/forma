package sdkidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Stable codes of the settings failures.
const (
	// CodeSecretMissing refuses to require verified identities while the workspace has no secret to verify them with.
	CodeSecretMissing = "sdk_identity_secret_missing" //nolint:gosec // an error code, not a credential
	// CodeKeyNotProtectable refuses to protect an attribute key the workspace does not have, or userId or email, which have rules of their own.
	CodeKeyNotProtectable = "sdk_identity_key_not_protectable"
)

// unprotectable are the keys with rules of their own: userId names the contact being identified, and email is written only on a verified identify.
var unprotectable = []string{"userId", "email"}

// Service holds the settings page's rules for a workspace's SDK identity. Reading them takes workspace.read, changing them workspace.manage and the state-mutation limit.
type Service struct {
	db      *gorm.DB
	cipher  *secret.Cipher
	authz   *authz.Authorizer
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
}

func NewService(db *gorm.DB, cipher *secret.Cipher, authorizer *authz.Authorizer, limiter *ratelimit.Limiter, emitter *audit.Emitter) *Service {
	return &Service{db: db, cipher: cipher, authz: authorizer, limiter: limiter, audit: emitter}
}

// View is a workspace's SDK identity settings as the settings page shows them. The secret itself is never part of it.
type View struct {
	HasSecret              bool     `json:"hasSecret"`
	Required               bool     `json:"required"`
	ProtectedAttributeKeys []string `json:"protectedAttributeKeys"`
}

// SecretView is the answer to a generated secret, the only time it is shown.
type SecretView struct {
	View
	Secret string `json:"secret"`
}

// Update is a settings change; a nil field is left as it is.
type Update struct {
	Required               *bool
	ProtectedAttributeKeys []string
	// SetProtectedAttributeKeys says ProtectedAttributeKeys is part of the change, an empty list included.
	SetProtectedAttributeKeys bool
}

func (r *row) view() *View {
	keys := r.ProtectedKeys
	if keys == nil {
		keys = []string{}
	}
	return &View{HasSecret: r.Secret != nil, Required: r.Required, ProtectedAttributeKeys: keys}
}

// auditObject is a view as the audit event records it.
func (v *View) auditObject() map[string]any {
	return map[string]any{"sdkIdentityHasSecret": v.HasSecret, "sdkIdentityRequired": v.Required, "sdkProtectedAttributeKeys": v.ProtectedAttributeKeys}
}

// Get is the settings of a workspace the caller may read.
func (s *Service) Get(ctx context.Context, userID, workspaceID uuid.UUID) (*View, error) {
	if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceRead, authz.Workspace(workspaceID)); err != nil {
		return nil, err
	}
	r, err := load(ctx, s.db, workspaceID, false)
	if errors.Is(err, errNoWorkspace) {
		return nil, apperr.NotFound("Workspace", "")
	}
	if err != nil {
		return nil, err
	}
	return r.view(), nil
}

// change runs a settings change: workspace.manage, the state-mutation limit, then apply on the workspace's locked row inside a transaction, recorded as an update of the workspace whether it succeeds or not. apply returns the columns to set; marks are recorded on both sides of the event to say what changed that the view cannot show.
func (s *Service) change(ctx context.Context, userID, workspaceID uuid.UUID, apply func(tx *gorm.DB, r *row) (map[string]any, error), marks ...string) (*View, error) {
	ev := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetWorkspace, TargetID: workspaceID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	var before, after *View
	err := func() error {
		if err := s.authz.Require(ctx, authz.User(userID), authz.WorkspaceManage, authz.Workspace(workspaceID)); err != nil {
			return err
		}
		if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, workspaceID.String()); err != nil {
			return err
		}
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			r, err := load(ctx, tx, workspaceID, true)
			if errors.Is(err, errNoWorkspace) {
				return apperr.NotFound("Workspace", "")
			}
			if err != nil {
				return err
			}
			ev.OrganizationID = r.OrganizationID.String()
			before = r.view()
			columns, err := apply(tx, r)
			if err != nil {
				return err
			}
			columns["updated_at"] = gorm.Expr("now()")
			if err := tx.Table("workspaces").Where("id = ?", workspaceID).Updates(columns).Error; err != nil {
				return fmt.Errorf("update workspace identity settings: %w", err)
			}
			updated, err := load(ctx, tx, workspaceID, false)
			if err != nil {
				return err
			}
			after = updated.view()
			return nil
		})
	}()
	if err != nil {
		ev.Status = audit.StatusFailure
	} else {
		oldObject, newObject := before.auditObject(), after.auditObject()
		for _, m := range marks {
			oldObject[m], newObject[m] = false, true
		}
		ev.OldObject, ev.NewObject = oldObject, newObject
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, ev))
	return after, err
}

// Update changes whether verified identities are required and which attribute keys are protected. Verification can be required only while the workspace has a secret. A protected key must be one of the workspace's attribute keys other than userId and email; the list is stored deduplicated, in the order given.
func (s *Service) Update(ctx context.Context, userID, workspaceID uuid.UUID, in Update) (*View, error) {
	return s.change(ctx, userID, workspaceID, func(tx *gorm.DB, r *row) (map[string]any, error) {
		columns := map[string]any{}
		if in.Required != nil {
			if *in.Required && r.Secret == nil {
				return nil, apperr.InvalidInput(CodeSecretMissing, "Generate an identity secret before requiring verified identities")
			}
			columns["sdk_identity_required"] = *in.Required
		}
		if in.SetProtectedAttributeKeys {
			keys, err := protectableKeys(ctx, tx, workspaceID, in.ProtectedAttributeKeys)
			if err != nil {
				return nil, err
			}
			columns["sdk_protected_attribute_keys"] = gorm.Expr("ARRAY(SELECT jsonb_array_elements_text(?::jsonb))", jsonArray(keys))
		}
		return columns, nil
	})
}

// protectableKeys checks the keys to protect against the workspace's attribute keys and drops repeats.
func protectableKeys(ctx context.Context, tx *gorm.DB, workspaceID uuid.UUID, keys []string) ([]string, error) {
	var existing []string
	if err := tx.WithContext(ctx).Raw(`SELECT key FROM contact_attribute_keys WHERE workspace_id = ?`, workspaceID).Scan(&existing).Error; err != nil {
		return nil, fmt.Errorf("load attribute keys: %w", err)
	}
	out := []string{}
	for _, k := range keys {
		if slices.Contains(unprotectable, k) || !slices.Contains(existing, k) {
			return nil, &apperr.Error{Status: http.StatusBadRequest, Code: CodeKeyNotProtectable, Message: "Only the workspace's attribute keys other than userId and email can be protected", Details: map[string]any{"key": k}}
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

// RotateSecret generates a new identity secret, replacing any the workspace had, and returns it: the only time it is shown. Every userHash computed with the old secret stops verifying at once, so a workspace that requires verification refuses its users until the customer's server uses the new one.
func (s *Service) RotateSecret(ctx context.Context, userID, workspaceID uuid.UUID) (*SecretView, error) {
	identitySecret, err := secret.NewToken()
	if err != nil {
		return nil, err
	}
	sealed, err := s.cipher.Seal([]byte(identitySecret), secretContext(workspaceID))
	if err != nil {
		return nil, fmt.Errorf("seal identity secret: %w", err)
	}
	v, err := s.change(ctx, userID, workspaceID, func(*gorm.DB, *row) (map[string]any, error) {
		return map[string]any{"sdk_identity_secret": sealed}, nil
	}, "sdkIdentitySecretRotated")
	if err != nil {
		return nil, err
	}
	return &SecretView{View: *v, Secret: identitySecret}, nil
}

// RemoveSecret deletes the workspace's identity secret, which also stops requiring verified identities: nothing could be verified without it. Email is then no longer written through the SDK at all.
func (s *Service) RemoveSecret(ctx context.Context, userID, workspaceID uuid.UUID) (*View, error) {
	return s.change(ctx, userID, workspaceID, func(*gorm.DB, *row) (map[string]any, error) {
		return map[string]any{"sdk_identity_secret": nil, "sdk_identity_required": false}, nil
	}, "sdkIdentitySecretRemoved")
}

// jsonArray is keys as a JSON array, which the update expands into a text[]: the keys are free text, so they are not written as an array literal.
func jsonArray(keys []string) string {
	b, _ := json.Marshal(keys)
	return string(b)
}
