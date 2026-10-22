package survey

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Filter is forma2's TSurveyFilterCriteria as the list reads it.
type Filter struct {
	// Name is a case-insensitive substring of the survey name.
	Name string
	// Statuses narrows to these statuses; IncludeArchived adds archived surveys (alone, only archived ones).
	Statuses        []string
	IncludeArchived bool
	Types           []string
}

// where builds the list's WHERE clause after the workspace condition, forma2's buildWhereClause: archived surveys are hidden unless asked for.
func (f Filter) where() (string, []any) {
	var clauses []string
	var args []any
	if f.Name != "" {
		clauses = append(clauses, `s.name ILIKE ? ESCAPE '\'`)
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Name)+"%")
	}
	switch {
	case f.IncludeArchived && len(f.Statuses) > 0:
		clauses = append(clauses, `((s.status = ANY(?::text[]) AND s.archived_at IS NULL) OR s.archived_at IS NOT NULL)`)
		args = append(args, textArray(f.Statuses))
	case f.IncludeArchived:
		clauses = append(clauses, `s.archived_at IS NOT NULL`)
	default:
		clauses = append(clauses, `s.archived_at IS NULL`)
		if len(f.Statuses) > 0 {
			clauses = append(clauses, `s.status = ANY(?::text[])`)
			args = append(args, textArray(f.Statuses))
		}
	}
	if len(f.Types) > 0 {
		clauses = append(clauses, `s.type = ANY(?::text[])`)
		args = append(args, textArray(f.Types))
	}
	return strings.Join(clauses, " AND "), args
}

type listRow struct {
	ID          uuid.UUID
	Name        string
	WorkspaceID uuid.UUID
	Type        string
	Status      string
	PublishOn   *time.Time
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatorName *string
}

// listQuery selects one page of list rows: the filter, an extra condition (the cursor or the relevance bucket), the order and the page size plus one.
func (r *Repository) listRows(ctx context.Context, workspaceID uuid.UUID, f Filter, extra string, extraArgs []any, order string, limit int) ([]listRow, error) {
	where, args := f.where()
	query := `SELECT s.id, s.name, s.workspace_id, s.type, s.status, s.publish_on, s.archived_at, s.created_at, s.updated_at, u.name AS creator_name
		FROM surveys s LEFT JOIN users u ON u.id = s.created_by WHERE s.workspace_id = ? AND ` + where
	all := append([]any{workspaceID}, args...)
	if extra != "" {
		query += " AND " + extra
		all = append(all, extraArgs...)
	}
	query += " ORDER BY " + order + " LIMIT ?"
	all = append(all, limit)
	var rows []listRow
	if err := r.db.WithContext(ctx).Raw(query, all...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list surveys: %w", err)
	}
	return rows, nil
}

// listItems adds the response counts to list rows.
func (r *Repository) listItems(ctx context.Context, rows []listRow) ([]ListItem, error) {
	items := make([]ListItem, len(rows))
	if len(rows) == 0 {
		return items, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	var counts []struct {
		SurveyID  uuid.UUID
		Total     int
		Completed int
	}
	if err := r.db.WithContext(ctx).Raw(`SELECT survey_id, count(*) AS total, count(*) FILTER (WHERE finished) AS completed
		FROM responses WHERE survey_id = ANY(?::uuid[]) GROUP BY survey_id`, uuidArray(ids)).Scan(&counts).Error; err != nil {
		return nil, fmt.Errorf("count survey responses: %w", err)
	}
	byID := map[uuid.UUID][2]int{}
	for _, c := range counts {
		byID[c.SurveyID] = [2]int{c.Total, c.Completed}
	}
	for i, row := range rows {
		items[i] = ListItem{
			ID: row.ID, Name: row.Name, WorkspaceID: row.WorkspaceID, Type: row.Type, Status: row.Status, PublishOn: row.PublishOn,
			ArchivedAt: row.ArchivedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			ResponseCount: byID[row.ID][0], CompletedResponseCount: byID[row.ID][1],
		}
		if row.CreatorName != nil {
			items[i].Creator = &Creator{Name: *row.CreatorName}
		}
	}
	return items, nil
}

// count is the number of surveys the filter matches.
func (r *Repository) count(ctx context.Context, workspaceID uuid.UUID, f Filter) (int, error) {
	where, args := f.where()
	var n int
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM surveys s WHERE s.workspace_id = ? AND `+where, append([]any{workspaceID}, args...)...).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count surveys: %w", err)
	}
	return n, nil
}

// workspaceCount is the number of surveys in the workspace, archived ones included.
func (r *Repository) workspaceCount(ctx context.Context, workspaceID uuid.UUID) (int, error) {
	var n int
	if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM surveys WHERE workspace_id = ?`, workspaceID).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count workspace surveys: %w", err)
	}
	return n, nil
}
