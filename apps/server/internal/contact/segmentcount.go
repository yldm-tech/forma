package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The contact counts the admin UI shows for segments: under the filter tree while it is edited, and beside each public segment on the segments page. Both evaluate a tree the way SegmentContacts does, over the contacts of the workspace. A device filter is a property of the request, not of the contact, so without a device it constrains nothing, and a count that left one out is approximate.

// Bounds on one counting query of the segments' contact counts: PostgreSQL takes at most 65535 bind parameters and 1664 result columns, so a list whose conditions need more is counted in several queries.
const (
	maxCountArgs    = 30000
	maxCountColumns = 500
)

// PreviewCount is how many of a workspace's contacts a filter tree matches, of how many in all.
type PreviewCount struct {
	Count       int64 `json:"count"`
	Total       int64 `json:"total"`
	Approximate bool  `json:"approximate"`
}

// PreviewSegmentCount counts the contacts an unsaved filter tree matches, for a reader of the workspace. The tree is validated as a saved one is, but may be empty, which matches every contact.
func (s *Service) PreviewSegmentCount(ctx context.Context, userID, workspaceID uuid.UUID, raw json.RawMessage) (*PreviewCount, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	tree, err := ParseFilters(s.schemas, raw)
	if err != nil {
		return nil, err
	}
	b := &builder{db: s.repo.db, log: s.log, workspaceID: workspaceID, now: s.now(), preview: true}
	cond, err := b.tree(ctx, tree, nil)
	if err != nil {
		return nil, notFound(err)
	}
	matched := "count(*)"
	var args []any
	if !cond.empty() {
		matched = "count(*) FILTER (WHERE " + cond.sql + ")"
		args = append(args, cond.args...)
	}
	var out struct {
		Matched int64
		Total   int64
	}
	if err := s.withoutJIT(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT `+matched+` AS matched, count(*) AS total FROM contacts c WHERE c.workspace_id = ?`, append(args, workspaceID)...).Scan(&out).Error
	}); err != nil {
		return nil, fmt.Errorf("count segment preview: %w", err)
	}
	return &PreviewCount{Count: out.Matched, Total: out.Total, Approximate: b.approximate}, nil
}

// withoutJIT runs a counting query in a transaction with JIT compilation off. The planner's cost estimate of a filter tree evaluated over every contact passes jit_above_cost even in a small workspace, and compiling the plan then takes far longer than running it.
func (s *Service) withoutJIT(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return s.repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL jit = off`).Error; err != nil {
			return fmt.Errorf("turn off jit: %w", err)
		}
		return fn(tx)
	})
}

// SegmentContactCount is how many of the workspace's contacts a public segment matches, as the segments page shows it beside the segment. Approximate says the count left out device filters.
type SegmentContactCount struct {
	SegmentID   uuid.UUID `json:"segmentId"`
	Count       int64     `json:"count"`
	Approximate bool      `json:"approximate"`
}

// countedSegment is a public segment whose condition was built, waiting for its count.
type countedSegment struct {
	id          uuid.UUID
	cond        condition
	approximate bool
}

// SegmentContactCounts counts the contacts of each public segment of the workspace, for a reader of it. Only the segments page asks for them, apart from the segment list every page reads, since counting evaluates every segment over every contact. Nested segments are resolved from the workspace's own segments, read once. A segment that cannot be counted, because its tree cannot be read, it nests a deleted segment or its counting query failed, is logged and left out, so one bad segment does not hide the others' counts.
func (s *Service) SegmentContactCounts(ctx context.Context, userID, workspaceID uuid.UUID) ([]SegmentContactCount, error) {
	if err := s.read(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	var rows []struct {
		ID          uuid.UUID
		IsPrivate   bool
		Filters     json.RawMessage
		WorkspaceID uuid.UUID
	}
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT id, is_private, filters, workspace_id FROM segments WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read segments: %w", err)
	}
	stored := make(map[string]storedSegment, len(rows))
	trees := make([]Filters, len(rows))
	readable := make([]bool, len(rows))
	for i, row := range rows {
		if err := json.Unmarshal(row.Filters, &trees[i]); err != nil {
			s.log.WarnContext(ctx, "Failed to read segment filters, not counting its contacts", "segmentId", row.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		readable[i] = true
		stored[row.ID.String()] = storedSegment{filters: trees[i], workspaceID: row.WorkspaceID}
	}
	now := s.now()
	var pending []countedSegment
	for i, row := range rows {
		if row.IsPrivate || !readable[i] {
			continue
		}
		b := &builder{db: s.repo.db, log: s.log, workspaceID: workspaceID, now: now, preview: true, stored: stored}
		cond, err := b.tree(ctx, trees[i], []string{row.ID.String()})
		if err != nil {
			s.log.WarnContext(ctx, "Failed to build the condition of a segment, not counting its contacts", "segmentId", row.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		pending = append(pending, countedSegment{id: row.ID, cond: cond, approximate: b.approximate})
	}
	out := make([]SegmentContactCount, 0, len(pending))
	for len(pending) > 0 {
		batch, args := 0, 0
		for batch < len(pending) && batch < maxCountColumns {
			n := len(pending[batch].cond.args)
			if batch > 0 && args+n > maxCountArgs {
				break
			}
			args += n
			batch++
		}
		counts, err := s.countBatch(ctx, workspaceID, pending[:batch])
		if err != nil {
			s.log.ErrorContext(ctx, "Failed to count the contacts of segments, leaving them uncounted", "workspaceId", workspaceID.String(), "segments", batch, "err", err)
		} else {
			for i, p := range pending[:batch] {
				out = append(out, SegmentContactCount{SegmentID: p.id, Count: counts[i], Approximate: p.approximate})
			}
		}
		pending = pending[batch:]
	}
	return out, nil
}

// countBatch counts the contacts of several segments in one pass over the workspace's contacts, one aggregate per segment.
func (s *Service) countBatch(ctx context.Context, workspaceID uuid.UUID, batch []countedSegment) ([]int64, error) {
	columns := make([]string, len(batch))
	var args []any
	for i, p := range batch {
		if p.cond.empty() {
			columns[i] = "count(*)"
			continue
		}
		columns[i] = "count(*) FILTER (WHERE " + p.cond.sql + ")"
		args = append(args, p.cond.args...)
	}
	counts := make([]int64, len(batch))
	err := s.withoutJIT(ctx, func(tx *gorm.DB) error {
		rows, err := tx.Raw(`SELECT `+strings.Join(columns, ", ")+` FROM contacts c WHERE c.workspace_id = ?`, append(args, workspaceID)...).Rows()
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fmt.Errorf("no row")
		}
		dest := make([]any, len(batch))
		for i := range counts {
			dest[i] = &counts[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("count segment contacts: %w", err)
	}
	return counts, nil
}
