// Package tag is the response tag module: a workspace's tags with their usage counts, created from the response view and renamed, deleted and merged from the tags settings page through /api/v3/tags, with forma2's rules (apps/web/lib/tag, apps/web/modules/workspaces/settings/lib/tag.ts, apps/web/app/api/v3/tags).
package tag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Tag is forma2's TTag.
type Tag struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Name        string    `json:"name"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// Counted is a tag with the number of responses carrying it.
type Counted struct {
	Tag
	Count int64
}

// errNameTaken is a name already used by another tag of the workspace.
var errNameTaken = errors.New("tag name already exists")

// Repository reads and writes tags. Every write names the workspace the tag belongs to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

const columns = `id, created_at, updated_at, name, workspace_id`

func isNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "tags_workspace_id_name_key"
}

// byID returns nil when the tag does not exist.
func (r *Repository) byID(ctx context.Context, id uuid.UUID) (*Tag, error) {
	var rows []Tag
	if err := r.db.WithContext(ctx).Raw(`SELECT `+columns+` FROM tags WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load tag: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// organizationOf is the organization of a workspace, or nil when it does not exist.
func (r *Repository) organizationOf(ctx context.Context, workspaceID uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID
	if err := r.db.WithContext(ctx).Raw(`SELECT organization_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("resolve workspace organization: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

// ofWorkspace lists the workspace's tags with their response counts, oldest first. forma2 left the order to the database; a stable one keeps the settings table from reshuffling between loads.
func (r *Repository) ofWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]Counted, error) {
	var rows []struct {
		Tag
		Count int64
	}
	err := r.db.WithContext(ctx).Raw(
		`SELECT t.id, t.created_at, t.updated_at, t.name, t.workspace_id, (SELECT count(*) FROM response_tags rt WHERE rt.tag_id = t.id) AS count
		FROM tags t WHERE t.workspace_id = ? ORDER BY t.created_at, t.id`, workspaceID,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	out := make([]Counted, len(rows))
	for i, row := range rows {
		out[i] = Counted{Tag: row.Tag, Count: row.Count}
	}
	return out, nil
}

// count is the number of responses carrying one tag.
func (r *Repository) count(ctx context.Context, id uuid.UUID) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM response_tags WHERE tag_id = ?`, id).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count tag responses: %w", err)
	}
	return n, nil
}

func (r *Repository) create(ctx context.Context, workspaceID uuid.UUID, name string) (*Tag, error) {
	var rows []Tag
	err := r.db.WithContext(ctx).Raw(`INSERT INTO tags (workspace_id, name) VALUES (?, ?) RETURNING `+columns, workspaceID, name).Scan(&rows).Error
	if isNameTaken(err) {
		return nil, errNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}
	return &rows[0], nil
}

// rename returns nil when the tag no longer exists in the workspace.
func (r *Repository) rename(ctx context.Context, id, workspaceID uuid.UUID, name string) (*Tag, error) {
	var rows []Tag
	err := r.db.WithContext(ctx).Raw(`UPDATE tags SET name = ?, updated_at = now() WHERE id = ? AND workspace_id = ? RETURNING `+columns, name, id, workspaceID).Scan(&rows).Error
	if isNameTaken(err) {
		return nil, errNameTaken
	}
	if err != nil {
		return nil, fmt.Errorf("rename tag: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// delete removes a tag of the workspace and, by cascade, its response links. It returns false when there was none.
func (r *Repository) delete(ctx context.Context, id, workspaceID uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM tags WHERE id = ? AND workspace_id = ?`, id, workspaceID)
	if res.Error != nil {
		return false, fmt.Errorf("delete tag: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// merge is forma2's mergeTags in one transaction: every response carrying the source tag carries the target instead (a response that already had both keeps one link), then the source tag is deleted. Copying the links with ON CONFLICT DO NOTHING rather than forma2's find-then-update also holds when the target is applied to a response concurrently. It returns false when the source tag no longer exists.
func (r *Repository) merge(ctx context.Context, sourceID, targetID, workspaceID uuid.UUID) (bool, error) {
	var merged bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			`INSERT INTO response_tags (response_id, tag_id) SELECT response_id, ? FROM response_tags WHERE tag_id = ? ON CONFLICT DO NOTHING`,
			targetID, sourceID,
		).Error; err != nil {
			return fmt.Errorf("move tag links: %w", err)
		}
		res := tx.Exec(`DELETE FROM tags WHERE id = ? AND workspace_id = ?`, sourceID, workspaceID)
		if res.Error != nil {
			return fmt.Errorf("delete merged tag: %w", res.Error)
		}
		merged = res.RowsAffected > 0
		if !merged {
			// Nothing to merge any more; undo the copied links.
			return errSourceGone
		}
		return nil
	})
	if errors.Is(err, errSourceGone) {
		return false, nil
	}
	return merged, err
}

var errSourceGone = errors.New("merged tag no longer exists")
