package survey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Sort orders of the survey list, forma2's ZSurveyFilters sortBy.
const (
	SortCreatedAt = "createdAt"
	SortUpdatedAt = "updatedAt"
	SortName      = "name"
	SortRelevance = "relevance"
)

const (
	cursorVersion    = 1
	bucketInProgress = "inProgress"
	bucketOther      = "other"
)

// cursorTime is how a cursor carries an instant: microseconds, the precision PostgreSQL stores, so two surveys a millisecond apart never collapse into one position.
const cursorTime = "2006-01-02T15:04:05.000000Z07:00"

// Cursor is a decoded list position, forma2's TSurveyListPageCursor.
type Cursor struct {
	SortBy string
	// Value is the sort key of the last row: its name, or its timestamp for the date orders.
	Value  string
	Bucket string
	At     time.Time
	ID     uuid.UUID
}

type dateOrNameCursor struct {
	Version int    `json:"version"`
	SortBy  string `json:"sortBy"`
	Value   string `json:"value"`
	ID      string `json:"id"`
}

type relevanceCursor struct {
	Version   int    `json:"version"`
	SortBy    string `json:"sortBy"`
	Bucket    string `json:"bucket"`
	UpdatedAt string `json:"updatedAt"`
	ID        string `json:"id"`
}

func encodeCursor(v any) *string {
	raw, _ := json.Marshal(v)
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

// DecodeCursor is forma2's decodeSurveyListPageCursor: base64url JSON of version 1, bound to the sort order it was issued for.
func DecodeCursor(encoded, sortBy string) (*Cursor, error) {
	invalid := apperr.InvalidInput(apperr.CodeBadRequest, "The cursor is invalid.")
	raw, err := base64.RawURLEncoding.DecodeString(trimPadding(encoded))
	if err != nil {
		return nil, invalid
	}
	var probe struct {
		Version   *int    `json:"version"`
		SortBy    string  `json:"sortBy"`
		Value     *string `json:"value"`
		Bucket    string  `json:"bucket"`
		UpdatedAt string  `json:"updatedAt"`
		ID        string  `json:"id"`
	}
	if json.Unmarshal(raw, &probe) != nil || probe.Version == nil || *probe.Version != cursorVersion || probe.ID == "" {
		return nil, invalid
	}
	id, err := uuid.Parse(probe.ID)
	if err != nil {
		return nil, invalid
	}
	c := &Cursor{SortBy: probe.SortBy, ID: id}
	switch probe.SortBy {
	case SortCreatedAt, SortUpdatedAt:
		if probe.Value == nil {
			return nil, invalid
		}
		if c.At, err = time.Parse(time.RFC3339Nano, *probe.Value); err != nil {
			return nil, invalid
		}
	case SortName:
		if probe.Value == nil {
			return nil, invalid
		}
		c.Value = *probe.Value
	case SortRelevance:
		if probe.Bucket != bucketInProgress && probe.Bucket != bucketOther {
			return nil, invalid
		}
		if c.At, err = time.Parse(time.RFC3339Nano, probe.UpdatedAt); err != nil {
			return nil, invalid
		}
		c.Bucket = probe.Bucket
	default:
		return nil, invalid
	}
	if c.SortBy != sortBy {
		return nil, apperr.InvalidInput(apperr.CodeBadRequest, "The cursor does not match the requested sort order.")
	}
	return c, nil
}

func trimPadding(s string) string {
	for len(s) > 0 && s[len(s)-1] == '=' {
		s = s[:len(s)-1]
	}
	return s
}

// Page is one page of the survey list.
type Page struct {
	Surveys    []ListItem
	NextCursor *string
}

func standardOrder(sortBy string) (order, after string) {
	switch sortBy {
	case SortName:
		return "s.name ASC, s.id ASC", "(s.name > ? OR (s.name = ? AND s.id > ?))"
	case SortCreatedAt:
		return "s.created_at DESC, s.id DESC", "(s.created_at < ? OR (s.created_at = ? AND s.id < ?))"
	default:
		return "s.updated_at DESC, s.id DESC", "(s.updated_at < ? OR (s.updated_at = ? AND s.id < ?))"
	}
}

func split[T any](rows []T, limit int) ([]T, bool) {
	if len(rows) > limit {
		return rows[:limit], true
	}
	return rows, false
}

// page is forma2's getSurveyListPage: a standard order pages by its sort key and id; relevance lists running surveys first, then the rest, each newest-updated first.
func (r *Repository) page(ctx context.Context, workspaceID uuid.UUID, f Filter, sortBy string, c *Cursor, limit int) (*Page, error) {
	if sortBy == SortRelevance {
		return r.relevancePage(ctx, workspaceID, f, c, limit)
	}
	order, after := standardOrder(sortBy)
	var extra string
	var args []any
	if c != nil {
		extra = after
		if sortBy == SortName {
			args = []any{c.Value, c.Value, c.ID}
		} else {
			args = []any{c.At, c.At, c.ID}
		}
	}
	rows, err := r.listRows(ctx, workspaceID, f, extra, args, order, limit+1)
	if err != nil {
		return nil, err
	}
	pageRows, more := split(rows, limit)
	var next *string
	if more {
		last := pageRows[len(pageRows)-1]
		switch sortBy {
		case SortName:
			next = encodeCursor(dateOrNameCursor{cursorVersion, sortBy, last.Name, last.ID.String()})
		case SortCreatedAt:
			next = encodeCursor(dateOrNameCursor{cursorVersion, sortBy, last.CreatedAt.UTC().Format(cursorTime), last.ID.String()})
		default:
			next = encodeCursor(dateOrNameCursor{cursorVersion, SortUpdatedAt, last.UpdatedAt.UTC().Format(cursorTime), last.ID.String()})
		}
	}
	return r.buildPage(ctx, pageRows, next)
}

func (r *Repository) buildPage(ctx context.Context, rows []listRow, next *string) (*Page, error) {
	items, err := r.listItems(ctx, rows)
	if err != nil {
		return nil, err
	}
	return &Page{Surveys: items, NextCursor: next}, nil
}

func (r *Repository) bucketRows(ctx context.Context, workspaceID uuid.UUID, f Filter, bucket string, c *Cursor, take int) ([]listRow, error) {
	extra := `s.status = 'inProgress'`
	if bucket == bucketOther {
		extra = `s.status <> 'inProgress'`
	}
	var args []any
	if c != nil && c.Bucket == bucket {
		extra += ` AND (s.updated_at < ? OR (s.updated_at = ? AND s.id < ?))`
		args = []any{c.At, c.At, c.ID}
	}
	return r.listRows(ctx, workspaceID, f, extra, args, "s.updated_at DESC, s.id DESC", take)
}

func relevanceNext(rows []listRow, bucket string) *string {
	if bucket == "" || len(rows) == 0 {
		return nil
	}
	last := rows[len(rows)-1]
	return encodeCursor(relevanceCursor{cursorVersion, SortRelevance, bucket, last.UpdatedAt.UTC().Format(cursorTime), last.ID.String()})
}

func (r *Repository) relevancePage(ctx context.Context, workspaceID uuid.UUID, f Filter, c *Cursor, limit int) (*Page, error) {
	var pageRows []listRow
	remaining := limit
	readInProgress := c == nil || c.Bucket == bucketInProgress
	if readInProgress {
		rows, err := r.bucketRows(ctx, workspaceID, f, bucketInProgress, c, remaining+1)
		if err != nil {
			return nil, err
		}
		inProgress, more := split(rows, remaining)
		pageRows = append(pageRows, inProgress...)
		if more {
			return r.buildPage(ctx, pageRows, relevanceNext(pageRows, bucketInProgress))
		}
		remaining -= len(inProgress)
	}
	if remaining <= 0 {
		bucket := ""
		if len(pageRows) > 0 && readInProgress {
			others, err := r.bucketRows(ctx, workspaceID, f, bucketOther, nil, 1)
			if err != nil {
				return nil, err
			}
			if len(others) > 0 {
				bucket = bucketInProgress
			}
		}
		return r.buildPage(ctx, pageRows, relevanceNext(pageRows, bucket))
	}
	rows, err := r.bucketRows(ctx, workspaceID, f, bucketOther, c, remaining+1)
	if err != nil {
		return nil, err
	}
	others, more := split(rows, remaining)
	pageRows = append(pageRows, others...)
	bucket := ""
	if more {
		bucket = bucketOther
	}
	return r.buildPage(ctx, pageRows, relevanceNext(pageRows, bucket))
}
