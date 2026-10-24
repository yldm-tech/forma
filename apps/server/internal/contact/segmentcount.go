package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// The contact counts the admin UI shows for segments: under the filter tree while it is edited, and beside each public segment on the segments page. Both evaluate a tree the way SegmentContacts does, over the contacts of the workspace, except that a survey-interaction filter constrains nothing, as a device filter already does without a device; a count that left out a filter of either kind is approximate.

// Bounds on one counting query of the segment list: PostgreSQL takes at most 65535 bind parameters and 1664 result columns, so a list whose conditions need more is counted in several queries.
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
	if err := s.repo.db.WithContext(ctx).Raw(`SELECT `+matched+` AS matched, count(*) AS total FROM contacts c WHERE c.workspace_id = ?`, append(args, workspaceID)...).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("count segment preview: %w", err)
	}
	return &PreviewCount{Count: out.Matched, Total: out.Total, Approximate: b.approximate}, nil
}

// countedSegment is a public segment of a list whose condition was built, waiting for its count.
type countedSegment struct {
	index int
	cond  condition
}

// setContactCounts fills in ContactCount and ContactCountApproximate on the list's public segments, with one query for the whole list (more only past the bounds above). Nested segments are resolved from the list itself. A segment whose tree cannot be read or whose condition cannot be built, such as one nesting a deleted segment, is logged and left without a count, so one bad segment does not hide the others' counts.
func (s *Service) setContactCounts(ctx context.Context, workspaceID uuid.UUID, segs []Segment) error {
	stored := make(map[string]storedSegment, len(segs))
	trees := make([]Filters, len(segs))
	readable := make([]bool, len(segs))
	for i, seg := range segs {
		if err := json.Unmarshal(seg.Filters, &trees[i]); err != nil {
			s.log.WarnContext(ctx, "Failed to read segment filters, not counting its contacts", "segmentId", seg.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		readable[i] = true
		stored[seg.ID.String()] = storedSegment{filters: trees[i], workspaceID: seg.WorkspaceID}
	}
	now := s.now()
	var pending []countedSegment
	for i, seg := range segs {
		if seg.IsPrivate || !readable[i] {
			continue
		}
		b := &builder{db: s.repo.db, log: s.log, workspaceID: workspaceID, now: now, preview: true, stored: stored}
		cond, err := b.tree(ctx, trees[i], []string{seg.ID.String()})
		if err != nil {
			s.log.WarnContext(ctx, "Failed to build the condition of a segment, not counting its contacts", "segmentId", seg.ID.String(), "workspaceId", workspaceID.String(), "err", err)
			continue
		}
		segs[i].ContactCountApproximate = b.approximate
		pending = append(pending, countedSegment{index: i, cond: cond})
	}
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
		if err := s.countBatch(ctx, workspaceID, segs, pending[:batch]); err != nil {
			return err
		}
		pending = pending[batch:]
	}
	return nil
}

// countBatch counts the contacts of several segments in one pass over the workspace's contacts, one aggregate per segment.
func (s *Service) countBatch(ctx context.Context, workspaceID uuid.UUID, segs []Segment, batch []countedSegment) error {
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
	rows, err := s.repo.db.WithContext(ctx).Raw(`SELECT `+strings.Join(columns, ", ")+` FROM contacts c WHERE c.workspace_id = ?`, append(args, workspaceID)...).Rows()
	if err != nil {
		return fmt.Errorf("count segment contacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counts := make([]int64, len(batch))
	dest := make([]any, len(batch))
	for i := range counts {
		dest[i] = &counts[i]
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return fmt.Errorf("count segment contacts: %w", err)
		}
		return fmt.Errorf("count segment contacts: no row")
	}
	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("count segment contacts: %w", err)
	}
	for i, p := range batch {
		segs[p.index].ContactCount = &counts[i]
	}
	return rows.Err()
}
