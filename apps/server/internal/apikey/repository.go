package apikey

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
)

// Repository reads and writes api_keys and api_key_workspaces. Every admin query is scoped by organization as well as by id, so tenant isolation never rests on the authorization check alone. The authz triggers enqueue the SpiceDB projection of every insert and delete in the same transaction.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// workspacesOf returns the names of those of ids that are workspaces of organizationID.
func (r *Repository) workspacesOf(ctx context.Context, organizationID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	var found []struct {
		ID   uuid.UUID
		Name string
	}
	err := r.db.WithContext(ctx).Raw(`SELECT id, name FROM workspaces WHERE organization_id = ? AND id IN ?`, organizationID, ids).Scan(&found).Error
	if err != nil {
		return nil, fmt.Errorf("load organization workspaces: %w", err)
	}
	names := make(map[uuid.UUID]string, len(found))
	for _, w := range found {
		names[w.ID] = w.Name
	}
	return names, nil
}

// newKeyRow is what create stores.
type newKeyRow struct {
	OrganizationID uuid.UUID
	CreatedBy      uuid.UUID
	Label          string
	HashedKey      string
	LookupHash     string
	// KeyHint is the key's last characters (Hint); "" stores none.
	KeyHint            string
	OrganizationAccess []byte
	// ExpiresAt is when the key stops working; nil for never.
	ExpiresAt *time.Time
	Grants    []grantInput
}

type grantInput struct {
	WorkspaceID uuid.UUID
	Permission  string
}

// createdKey is what create returns of the new row.
type createdKey struct {
	ID          uuid.UUID
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	CreatorName string
}

// create inserts a key and its workspace grants and returns the new row. It refuses with 403 when the creator is no longer an owner or manager of the organization, or is suspended there: the service has authorized them through SpiceDB, which follows a removal or demotion only once the outbox delivers it, and the check runs in the same Serializable transaction as the insert, so a key cannot be created after, and slip past, the disable.CreatedBy of a concurrent removal or demotion (which run Serializable too).
func (r *Repository) create(ctx context.Context, row newKeyRow) (createdKey, error) {
	var created createdKey
	err := database.Serializable(ctx, r.db, func(tx *gorm.DB) error {
		var roles []string
		if err := tx.Raw(`SELECT role FROM memberships WHERE user_id = ? AND organization_id = ? AND suspended_at IS NULL`, row.CreatedBy, row.OrganizationID).Scan(&roles).Error; err != nil {
			return fmt.Errorf("load creator role: %w", err)
		}
		if len(roles) == 0 || !disable.ManagesKeys(roles[0]) {
			return apperr.Forbidden("Not authorized")
		}
		err := tx.Raw(
			`WITH k AS (INSERT INTO api_keys (organization_id, created_by, label, hashed_key, lookup_hash, organization_access, key_hint, expires_at) VALUES (?, ?, ?, ?, ?, ?::jsonb, NULLIF(?, ''), ?) RETURNING id, created_at, expires_at, created_by)
			SELECT k.id, k.created_at, k.expires_at, u.name AS creator_name FROM k JOIN users u ON u.id = k.created_by`,
			row.OrganizationID, row.CreatedBy, row.Label, row.HashedKey, row.LookupHash, string(row.OrganizationAccess), row.KeyHint, row.ExpiresAt,
		).Scan(&created).Error
		if err != nil {
			return fmt.Errorf("insert api key: %w", err)
		}
		for _, g := range row.Grants {
			if err := tx.Exec(`INSERT INTO api_key_workspaces (api_key_id, workspace_id, permission) VALUES (?, ?, ?)`, created.ID, g.WorkspaceID, g.Permission).Error; err != nil {
				return fmt.Errorf("insert api key workspace: %w", err)
			}
		}
		return nil
	})
	return created, err
}

type keyRow struct {
	ID                 uuid.UUID
	Label              string
	CreatedAt          time.Time
	OrganizationAccess []byte
	LastUsedAt         *time.Time
	DisabledAt         *time.Time
	DisabledReason     *string
	KeyHint            *string
	ExpiresAt          *time.Time
	CreatorID          *uuid.UUID
	CreatorName        *string
	CreatorIsMember    bool
	CreatorSuspended   bool
}

// creator is the key's Creator, or nil when its creator's account is gone.
func (k keyRow) creator() *Creator {
	if k.CreatorID == nil {
		return nil
	}
	c := &Creator{ID: *k.CreatorID, FormerMember: !k.CreatorIsMember, Suspended: k.CreatorSuspended}
	if k.CreatorName != nil {
		c.Name = *k.CreatorName
	}
	return c
}

type grantRow struct {
	APIKeyID      uuid.UUID
	WorkspaceID   uuid.UUID
	WorkspaceName string
	Permission    string
}

// list returns the organization's keys, oldest first. forma2 returned them unordered.
func (r *Repository) list(ctx context.Context, organizationID uuid.UUID) ([]View, error) {
	return views(ctx, r.db, organizationID, nil)
}

// byID returns one key of the organization, or nil when the organization has no such key.
func (r *Repository) byID(ctx context.Context, organizationID, id uuid.UUID) (*View, error) {
	return oneView(ctx, r.db, organizationID, id)
}

func oneView(ctx context.Context, db *gorm.DB, organizationID, id uuid.UUID) (*View, error) {
	found, err := views(ctx, db, organizationID, &id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

func views(ctx context.Context, db *gorm.DB, organizationID uuid.UUID, id *uuid.UUID) ([]View, error) {
	// The creator is read through users, and whether they still belong to the key's organization, and whether it suspended them, through memberships, so a key whose creator left reads as a former member's and one whose creator is suspended says why it does not authenticate.
	query := `SELECT k.id, k.label, k.created_at, k.organization_access, k.last_used_at, k.disabled_at, k.disabled_reason, k.key_hint, k.expires_at, u.id AS creator_id, u.name AS creator_name,
		EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = k.created_by AND m.organization_id = k.organization_id) AS creator_is_member,
		EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = k.created_by AND m.organization_id = k.organization_id AND m.suspended_at IS NOT NULL) AS creator_suspended
		FROM api_keys k LEFT JOIN users u ON u.id = k.created_by WHERE k.organization_id = ?`
	args := []any{organizationID}
	if id != nil {
		query += ` AND k.id = ?`
		args = append(args, *id)
	}
	var keys []keyRow
	if err := db.WithContext(ctx).Raw(query+` ORDER BY k.created_at, k.id`, args...).Scan(&keys).Error; err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	out := make([]View, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	index := make(map[uuid.UUID]int, len(keys))
	ids := make([]uuid.UUID, len(keys))
	for i, k := range keys {
		out[i] = View{
			ID: k.ID, Label: k.Label, CreatedAt: k.CreatedAt, OrganizationAccess: storedAccess(k.OrganizationAccess), WorkspacePermissions: []WorkspaceGrant{},
			LastUsedAt: k.LastUsedAt, CreatedBy: k.creator(), DisabledAt: k.DisabledAt, DisabledReason: k.DisabledReason, KeyHint: k.KeyHint, ExpiresAt: k.ExpiresAt,
		}
		index[k.ID] = i
		ids[i] = k.ID
	}
	var grants []grantRow
	err := db.WithContext(ctx).Raw(
		`SELECT g.api_key_id, g.workspace_id, w.name AS workspace_name, g.permission FROM api_key_workspaces g JOIN workspaces w ON w.id = g.workspace_id WHERE g.api_key_id IN ? ORDER BY w.name, g.workspace_id`, ids,
	).Scan(&grants).Error
	if err != nil {
		return nil, fmt.Errorf("list api key workspaces: %w", err)
	}
	for _, g := range grants {
		v := &out[index[g.APIKeyID]]
		v.WorkspacePermissions = append(v.WorkspacePermissions, WorkspaceGrant{WorkspaceID: g.WorkspaceID, WorkspaceName: g.WorkspaceName, Permission: g.Permission})
	}
	return out, nil
}

// updateLabel relabels one key of the organization; a key of another organization is left alone.
func (r *Repository) updateLabel(ctx context.Context, organizationID, id uuid.UUID, label string) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE api_keys SET label = ? WHERE id = ? AND organization_id = ?`, label, id, organizationID).Error; err != nil {
		return fmt.Errorf("update api key label: %w", err)
	}
	return nil
}

// delete removes one key of the organization, its grants with it, and returns what it was; nil when the organization has no such key.
func (r *Repository) delete(ctx context.Context, organizationID, id uuid.UUID) (*View, error) {
	var deleted *View
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := oneView(ctx, tx, organizationID, id)
		if err != nil || v == nil {
			return err
		}
		res := tx.Exec(`DELETE FROM api_keys WHERE id = ? AND organization_id = ?`, id, organizationID)
		if res.Error != nil {
			return fmt.Errorf("delete api key: %w", res.Error)
		}
		if res.RowsAffected > 0 {
			deleted = v
		}
		return nil
	})
	return deleted, err
}

// storedKey is what authentication reads of a key.
type storedKey struct {
	ID                 uuid.UUID
	OrganizationID     uuid.UUID
	HashedKey          string
	OrganizationAccess []byte
	LastUsedAt         sql.NullTime
	// DisabledAt is set once the key was disabled; such a key never authenticates.
	DisabledAt sql.NullTime
	// ExpiresAt is set for a key that stops working then; from that time on it never authenticates.
	ExpiresAt sql.NullTime
	// CreatorSuspended is set while the organization suspends the access of the member who created the key; the key does not authenticate until it is restored.
	CreatorSuspended bool
}

// byLookupHash finds the key whose lookup_hash is hash, or nil.
func (r *Repository) byLookupHash(ctx context.Context, hash string) (*storedKey, error) {
	var rows []storedKey
	err := r.db.WithContext(ctx).Raw(`SELECT k.id, k.organization_id, k.hashed_key, k.organization_access, k.last_used_at, k.disabled_at, k.expires_at,
		EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = k.created_by AND m.organization_id = k.organization_id AND m.suspended_at IS NOT NULL) AS creator_suspended
		FROM api_keys k WHERE k.lookup_hash = ?`, hash).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("look up api key: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// authGrant is a key's grant with the workspace's organization, so authentication can drop grants outside the key's organization.
type authGrant struct {
	WorkspaceID    uuid.UUID
	WorkspaceName  string
	OrganizationID uuid.UUID
	Permission     string
}

func (r *Repository) grantsOf(ctx context.Context, keyID uuid.UUID) ([]authGrant, error) {
	var grants []authGrant
	err := r.db.WithContext(ctx).Raw(
		`SELECT g.workspace_id, w.name AS workspace_name, w.organization_id, g.permission FROM api_key_workspaces g JOIN workspaces w ON w.id = g.workspace_id WHERE g.api_key_id = ? ORDER BY w.name, g.workspace_id`, keyID,
	).Scan(&grants).Error
	if err != nil {
		return nil, fmt.Errorf("load api key workspaces: %w", err)
	}
	return grants, nil
}

// touch records a use at now unless one was recorded after staleBefore. The condition is repeated in SQL so concurrent requests write once.
func (r *Repository) touch(ctx context.Context, id uuid.UUID, now, staleBefore time.Time) error {
	err := r.db.WithContext(ctx).Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`, now, id, staleBefore).Error
	if err != nil {
		return fmt.Errorf("update api key usage: %w", err)
	}
	return nil
}
