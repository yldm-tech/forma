package actionclass

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Repository reads and writes action classes. Every query names the workspace or the action class it is scoped to.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

const columns = `id, created_at, updated_at, name, description, type, key, no_code_config, workspace_id`

type row struct {
	ID           uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string
	Description  sql.NullString
	Type         string
	Key          sql.NullString
	NoCodeConfig []byte
	WorkspaceID  uuid.UUID
}

func (r row) actionClass() *ActionClass {
	a := &ActionClass{ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Name: r.Name, Type: r.Type, NoCodeConfig: r.NoCodeConfig, WorkspaceID: r.WorkspaceID}
	if r.Description.Valid {
		a.Description = &r.Description.String
	}
	if r.Key.Valid {
		a.Key = &r.Key.String
	}
	if a.NoCodeConfig == nil {
		a.NoCodeConfig = []byte("null")
	}
	return a
}

// ofWorkspace lists the workspace's action classes oldest first, forma2's getActionClasses.
func (r *Repository) ofWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]*ActionClass, error) {
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`SELECT `+columns+` FROM action_classes WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list action classes: %w", err)
	}
	out := make([]*ActionClass, len(rows))
	for i, r := range rows {
		out[i] = r.actionClass()
	}
	return out, nil
}

// byID returns nil when the action class does not exist.
func (r *Repository) byID(ctx context.Context, id uuid.UUID) (*ActionClass, error) {
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`SELECT `+columns+` FROM action_classes WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load action class: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].actionClass(), nil
}

// organizationOf is the organization of a workspace, or nil when the workspace does not exist.
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

// nullableJSON passes a document as jsonb, or SQL NULL for none.
func nullableJSON(doc []byte) any {
	if doc == nil {
		return nil
	}
	return string(doc)
}

func (r *Repository) create(ctx context.Context, workspaceID uuid.UUID, in Input) (*ActionClass, error) {
	var description *string
	if in.Description != nil {
		description = in.Description.Value
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(
		`INSERT INTO action_classes (workspace_id, name, description, type, key, no_code_config) VALUES (?, ?, ?, ?, ?, ?::jsonb) RETURNING `+columns,
		workspaceID, in.Name, description, in.Type, in.Key, nullableJSON(in.NoCodeConfig),
	).Scan(&rows).Error
	if err != nil {
		return nil, uniqueError(err, in)
	}
	return rows[0].actionClass(), nil
}

// update writes an action class of the given workspace. A switch of type clears the other type's field, so a no-code action keeps no code key that would block the key's reuse (forma2 left it); a no-code update without a configuration keeps the stored one, as forma2 did.
func (r *Repository) update(ctx context.Context, id, workspaceID uuid.UUID, in Input) (*ActionClass, error) {
	sets := `name = ?, type = ?, updated_at = now()`
	args := []any{in.Name, in.Type}
	if in.Description != nil {
		sets += `, description = ?`
		args = append(args, in.Description.Value)
	}
	if in.Type == TypeCode {
		sets += `, key = ?, no_code_config = NULL`
		args = append(args, in.Key)
	} else {
		sets += `, key = NULL`
		if in.NoCodeConfig != nil {
			sets += `, no_code_config = ?::jsonb`
			args = append(args, string(in.NoCodeConfig))
		}
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`UPDATE action_classes SET `+sets+` WHERE id = ? AND workspace_id = ? RETURNING `+columns, append(args, id, workspaceID)...).Scan(&rows).Error
	if err != nil {
		return nil, uniqueError(err, in)
	}
	if len(rows) == 0 {
		return nil, apperr.NotFound("ActionClass", id.String())
	}
	return rows[0].actionClass(), nil
}

// uniqueError is forma2's UniqueConstraintError, "Action with <field> <value> already exists", for the name or code key already used in the workspace.
func uniqueError(err error, in Input) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "action_classes_workspace_id_name_key":
			return apperr.Conflict("Action with name " + in.Name + " already exists")
		case "action_classes_workspace_id_key_key":
			key := ""
			if in.Key != nil {
				key = *in.Key
			}
			return apperr.Conflict("Action with key " + key + " already exists")
		}
	}
	return fmt.Errorf("write action class: %w", err)
}

// delete removes an action class of the given workspace; its survey triggers cascade. It returns false when there was none.
func (r *Repository) delete(ctx context.Context, id, workspaceID uuid.UUID) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`DELETE FROM action_classes WHERE id = ? AND workspace_id = ?`, id, workspaceID)
	if res.Error != nil {
		return false, fmt.Errorf("delete action class: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

type triggeredSurvey struct {
	Name   string
	Status string
}

// surveysTriggeredBy lists the surveys of the workspace an action class triggers, oldest first.
func (r *Repository) surveysTriggeredBy(ctx context.Context, id, workspaceID uuid.UUID, exclude *uuid.UUID) ([]triggeredSurvey, error) {
	query := `SELECT s.name, s.status FROM survey_triggers t JOIN surveys s ON s.id = t.survey_id WHERE t.action_class_id = ? AND s.workspace_id = ?`
	args := []any{id, workspaceID}
	if exclude != nil {
		query += ` AND s.id <> ?`
		args = append(args, *exclude)
	}
	var out []triggeredSurvey
	if err := r.db.WithContext(ctx).Raw(query+` ORDER BY s.created_at, s.id`, args...).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("list surveys triggered by action class: %w", err)
	}
	return out, nil
}
