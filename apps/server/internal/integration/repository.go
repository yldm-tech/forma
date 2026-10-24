package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Repository reads and writes integrations and their sealed credentials.
type Repository struct {
	db     *gorm.DB
	cipher *secret.Cipher
	log    *slog.Logger
}

func NewRepository(db *gorm.DB, cipher *secret.Cipher, log *slog.Logger) *Repository {
	return &Repository{db: db, cipher: cipher, log: log}
}

// credentialsContext is the context integration credentials are sealed for: the integration's own row, so a ciphertext copied to another integration does not open there.
func credentialsContext(integrationID uuid.UUID) []byte {
	return []byte("integration_credentials:" + integrationID.String())
}

// SealCredentials returns what integration_credentials stores for an integration's credentials. The data migration writes the credentials it carries over from forma2 with it.
func SealCredentials(c *secret.Cipher, integrationID uuid.UUID, creds Credentials) ([]byte, error) {
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("encode integration credentials: %w", err)
	}
	return c.Seal(plaintext, credentialsContext(integrationID))
}

// OpenCredentials is the inverse of SealCredentials. The data migration compares what a row holds with what it would write, so a re-run leaves unchanged credentials as they are.
func OpenCredentials(c *secret.Cipher, integrationID uuid.UUID, ciphertext []byte) (Credentials, error) {
	plaintext, err := c.Open(ciphertext, credentialsContext(integrationID))
	if err != nil {
		return Credentials{}, err
	}
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return Credentials{}, fmt.Errorf("decode integration credentials: %w", err)
	}
	return creds, nil
}

const selectIntegration = `SELECT i.id, i.workspace_id, i.type, i.config, i.created_at, i.updated_at, i.last_error_at, i.last_error_message, i.consecutive_failures, c.ciphertext
	FROM integrations i LEFT JOIN integration_credentials c ON c.integration_id = i.id`

type integrationRow struct {
	ID                  uuid.UUID
	WorkspaceID         uuid.UUID
	Type                string
	Config              []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
	LastErrorAt         *time.Time
	LastErrorMessage    *string
	ConsecutiveFailures int
	Ciphertext          []byte
}

func (r *Repository) find(ctx context.Context, query string, args ...any) ([]*Integration, error) {
	var rows []integrationRow
	if err := r.db.WithContext(ctx).Raw(selectIntegration+" "+query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load integrations: %w", err)
	}
	out := make([]*Integration, len(rows))
	for i, x := range rows {
		in := &Integration{ID: x.ID, WorkspaceID: x.WorkspaceID, Type: x.Type, Config: x.Config, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
			LastErrorAt: x.LastErrorAt, LastErrorMessage: x.LastErrorMessage, ConsecutiveFailures: x.ConsecutiveFailures}
		if len(x.Ciphertext) > 0 {
			in.Credentials = r.open(ctx, x.ID, x.Ciphertext)
		}
		out[i] = in
	}
	return out, nil
}

// open decrypts an integration's credentials. One that does not open is logged and left out rather than failing the read: every other integration of the workspace travels with it, and the way out is the same as for a revoked token, reconnecting.
func (r *Repository) open(ctx context.Context, id uuid.UUID, ciphertext []byte) *Credentials {
	plaintext, err := r.cipher.Open(ciphertext, credentialsContext(id))
	if err != nil {
		r.log.WarnContext(ctx, "Could not decrypt a stored integration credential; leaving it as stored", "integration_id", id, "err", err)
		return nil
	}
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		r.log.WarnContext(ctx, "Stored integration credential is not a token set", "integration_id", id, "err", err)
		return nil
	}
	return &creds
}

// OfWorkspace is forma2's getIntegrations: every integration of the workspace, in the order they were connected.
func (r *Repository) OfWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*Integration, error) {
	return r.find(ctx, `WHERE i.workspace_id = ? ORDER BY i.created_at, i.id`, workspaceID)
}

// ByType is forma2's getIntegrationByType, or nil.
func (r *Repository) ByType(ctx context.Context, workspaceID uuid.UUID, typ string) (*Integration, error) {
	found, err := r.find(ctx, `WHERE i.workspace_id = ? AND i.type = ?`, workspaceID, typ)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// ByID is the integration with id, or nil.
func (r *Repository) ByID(ctx context.Context, id uuid.UUID) (*Integration, error) {
	found, err := r.find(ctx, `WHERE i.id = ?`, id)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// Connect stores the credentials an OAuth callback obtained, creating the workspace's integration of the type with no links if there is none and keeping the links of one that exists, as forma2's callbacks did on reconnect.
func (r *Repository) Connect(ctx context.Context, workspaceID uuid.UUID, typ string, creds Credentials) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`INSERT INTO integrations (workspace_id, type, config) VALUES (?, ?, ?)
			ON CONFLICT (workspace_id, type) DO UPDATE SET updated_at = now() RETURNING id`, workspaceID, typ, []byte(emptyConfig)).Row().Scan(&id); err != nil {
			return fmt.Errorf("upsert integration: %w", err)
		}
		return r.saveCredentials(ctx, tx, id, creds)
	})
	return id, err
}

// SaveCredentials replaces an integration's credentials, after a token refresh.
func (r *Repository) SaveCredentials(ctx context.Context, id uuid.UUID, creds Credentials) error {
	return r.saveCredentials(ctx, r.db.WithContext(ctx), id, creds)
}

func (r *Repository) saveCredentials(ctx context.Context, db *gorm.DB, id uuid.UUID, creds Credentials) error {
	sealed, err := SealCredentials(r.cipher, id, creds)
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`INSERT INTO integration_credentials (integration_id, ciphertext) VALUES (?, ?)
		ON CONFLICT (integration_id) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = now()`, id, sealed).Error; err != nil {
		return fmt.Errorf("save integration credentials: %w", err)
	}
	return nil
}

// SaveConfig replaces an integration's links.
func (r *Repository) SaveConfig(ctx context.Context, id uuid.UUID, config json.RawMessage) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE integrations SET config = ?, updated_at = now() WHERE id = ?`, []byte(config), id).Error; err != nil {
		return fmt.Errorf("save integration config: %w", err)
	}
	return nil
}

// Delete removes an integration with its links and credentials; false when there was none.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM integrations WHERE id = ?`, id)
	if res.Error != nil {
		return false, fmt.Errorf("delete integration: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// organizationOf is the organization of a workspace, or nil.
func (r *Repository) organizationOf(ctx context.Context, workspaceID uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load workspace organization: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

// foreignSurveys are the ids among ids that are not surveys of the workspace.
func (r *Repository) foreignSurveys(ctx context.Context, workspaceID uuid.UUID, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var own []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE workspace_id = ? AND id IN ?`, workspaceID, ids).Scan(&own).Error; err != nil {
		return nil, fmt.Errorf("load link surveys: %w", err)
	}
	var out []uuid.UUID
	for _, id := range ids {
		if !slices.Contains(own, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// linkableSurveys are the surveys the integrations pages offer to link, forma2's integrations getSurveys: not completed, not archived, most recently updated first.
func (r *Repository) linkableSurveys(ctx context.Context, workspaceID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT id FROM surveys WHERE workspace_id = ? AND status <> 'completed' AND archived_at IS NULL
		ORDER BY updated_at DESC, id DESC`, workspaceID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("load linkable surveys: %w", err)
	}
	return ids, nil
}
