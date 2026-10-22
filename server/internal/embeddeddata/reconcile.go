package embeddeddata

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// current is one field a survey has now: its link, and the definition the link points at.
type current struct {
	LinkID     uuid.UUID
	StorageKey string
	// Position is on the link: one shared definition sits at a different position in each survey.
	Position int
	FieldID  uuid.UUID
	// SurveyID owns a local definition and is nil for a shared one.
	SurveyID *uuid.UUID
	Field    Field
}

// plan is what a reconcile changes.
type plan struct {
	create []positioned
	update []Field
	// updateIDs are the definitions update rewrites, index for index.
	updateIDs []uuid.UUID
	reorder   []positionedLink
	unlink    []unlinked
}

type positioned struct {
	Field
	Position int
}

type positionedLink struct {
	LinkID   uuid.UUID
	Position int
}

// unlinked is a link to drop; DeleteField is nil when the definition outlives the link.
type unlinked struct {
	LinkID      uuid.UUID
	DeleteField *uuid.UUID
}

// planReconcile is forma2's planEmbeddedDataReconcile. A current link whose storage key is no longer desired, or is desired under another source (a different field at the same address), is unlinked, and its definition deleted only when this survey owns it. A kept link moves when its position changed, shared or not. Only a definition this survey owns is rewritten when its name, type or default changed; a shared definition is never edited by a survey save. A desired field with no link of its source is created at its index in the full desired list.
func planReconcile(surveyID uuid.UUID, cur []current, desired []Field) plan {
	type want struct {
		field    Field
		position int
	}
	byKey := make(map[string]want, len(desired))
	for i, f := range desired {
		byKey[f.StorageKey] = want{field: f, position: i}
	}
	curByKey := make(map[string]current, len(cur))
	var p plan
	for _, c := range cur {
		curByKey[c.StorageKey] = c
		w, ok := byKey[c.StorageKey]
		owned := c.SurveyID != nil && *c.SurveyID == surveyID
		if !ok || w.field.Source != c.Field.Source {
			u := unlinked{LinkID: c.LinkID}
			if owned {
				id := c.FieldID
				u.DeleteField = &id
			}
			p.unlink = append(p.unlink, u)
			continue
		}
		if c.Position != w.position {
			p.reorder = append(p.reorder, positionedLink{LinkID: c.LinkID, Position: w.position})
		}
		if owned && definitionDiffers(w.field, c.Field) {
			p.update = append(p.update, w.field)
			p.updateIDs = append(p.updateIDs, c.FieldID)
		}
	}
	for i, f := range desired {
		if c, ok := curByKey[f.StorageKey]; !ok || c.Field.Source != f.Source {
			p.create = append(p.create, positioned{Field: f, Position: i})
		}
	}
	return p
}

// definitionDiffers compares what a definition stores, forma2's strict equality on name, type and default.
func definitionDiffers(want, have Field) bool {
	return want.Name != have.Name || want.DataType != have.DataType || want.DefaultValue != have.DefaultValue
}

// defaultJSON is a default value as jsonb, or SQL NULL for none.
func defaultJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode embedded data default: %w", err)
	}
	return string(raw), nil
}

// readDefault decodes a stored default value; numbers become float64, as a survey document's do.
func readDefault(raw []byte) (any, error) {
	if raw == nil {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode embedded data default: %w", err)
	}
	return v, nil
}

// load reads the survey's links with their definitions in stored order, the storage key breaking ties so the order is total.
func load(ctx context.Context, tx *gorm.DB, surveyID, workspaceID uuid.UUID) ([]current, error) {
	var rows []struct {
		LinkID       uuid.UUID
		StorageKey   string
		Position     int
		FieldID      uuid.UUID
		SurveyID     *uuid.UUID
		Name         string
		Source       string
		DataType     string
		DefaultValue []byte
	}
	err := tx.WithContext(ctx).Raw(
		`SELECT l.id AS link_id, l.storage_key, l.position, e.id AS field_id, e.survey_id, e.name, e.source, e.data_type, e.default_value
		FROM survey_embedded_data l JOIN embedded_data e ON e.id = l.embedded_data_id AND e.workspace_id = l.workspace_id
		WHERE l.survey_id = ? AND l.workspace_id = ? ORDER BY l.position, l.storage_key`, surveyID, workspaceID,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load embedded data links: %w", err)
	}
	out := make([]current, len(rows))
	for i, r := range rows {
		def, err := readDefault(r.DefaultValue)
		if err != nil {
			return nil, err
		}
		out[i] = current{LinkID: r.LinkID, StorageKey: r.StorageKey, Position: r.Position, FieldID: r.FieldID, SurveyID: r.SurveyID,
			Field: Field{StorageKey: r.StorageKey, Name: r.Name, Source: r.Source, DataType: r.DataType, DefaultValue: def}}
	}
	return out, nil
}

// Reconcile is forma2's reconcileEmbeddedData: it brings a survey's definitions and links in step with what the save carried, inside the save's transaction (tx), so a survey never commits without its fields. workspaceID is the survey's workspace; both foreign keys include it, so a wrong one fails rather than linking across tenants. Responses are never read or written: they store values under the same storage keys, which a reconcile never changes for a kept field.
//
// A field whose source changed keeps its storage key, so links are dropped before any are created.
func Reconcile(ctx context.Context, tx *gorm.DB, surveyID, workspaceID uuid.UUID, patch Patch) error {
	cur, err := load(ctx, tx, surveyID, workspaceID)
	if err != nil {
		return err
	}
	have := make([]Field, len(cur))
	for i, c := range cur {
		have[i] = c.Field
	}
	desired := resolveDesired(have, patch)
	if err := validateDesired(desired); err != nil {
		return err
	}
	p := planReconcile(surveyID, cur, desired)
	db := tx.WithContext(ctx)

	if len(p.unlink) > 0 {
		links := make([]uuid.UUID, len(p.unlink))
		var fields []uuid.UUID
		for i, u := range p.unlink {
			links[i] = u.LinkID
			if u.DeleteField != nil {
				fields = append(fields, *u.DeleteField)
			}
		}
		if err := db.Exec(`DELETE FROM survey_embedded_data WHERE id IN ? AND survey_id = ?`, links, surveyID).Error; err != nil {
			return fmt.Errorf("unlink embedded data: %w", err)
		}
		// A definition another survey still links is left behind as an orphan rather than deleted, which would cascade that survey's link away. Nothing creates such a link today; this guards the invariant.
		if len(fields) > 0 {
			if err := db.Exec(
				`DELETE FROM embedded_data e WHERE e.id IN ? AND e.survey_id = ? AND NOT EXISTS (SELECT 1 FROM survey_embedded_data l WHERE l.embedded_data_id = e.id)`, fields, surveyID,
			).Error; err != nil {
				return fmt.Errorf("delete embedded data: %w", err)
			}
		}
	}
	for i, f := range p.update {
		def, err := defaultJSON(f.DefaultValue)
		if err != nil {
			return err
		}
		if err := db.Exec(
			`UPDATE embedded_data SET name = ?, data_type = ?, default_value = ?::jsonb, updated_at = now() WHERE id = ? AND survey_id = ?`,
			f.Name, f.DataType, def, p.updateIDs[i], surveyID,
		).Error; err != nil {
			return fmt.Errorf("update embedded data: %w", err)
		}
	}
	// Only links that moved: a rename on a field-heavy survey stays one write.
	for _, r := range p.reorder {
		if err := db.Exec(`UPDATE survey_embedded_data SET position = ? WHERE id = ? AND survey_id = ?`, r.Position, r.LinkID, surveyID).Error; err != nil {
			return fmt.Errorf("reorder embedded data: %w", err)
		}
	}
	for _, c := range p.create {
		def, err := defaultJSON(c.DefaultValue)
		if err != nil {
			return err
		}
		var id uuid.UUID
		// Local: owned by this survey and absent from the library, so no library key.
		if err := db.Raw(
			`INSERT INTO embedded_data (workspace_id, survey_id, key, name, source, data_type, default_value) VALUES (?, ?, NULL, ?, ?, ?, ?::jsonb) RETURNING id`,
			workspaceID, surveyID, c.Name, c.Source, c.DataType, def,
		).Row().Scan(&id); err != nil {
			return fmt.Errorf("create embedded data: %w", err)
		}
		if err := db.Exec(
			`INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`,
			workspaceID, surveyID, id, c.StorageKey, c.Position,
		).Error; err != nil {
			return fmt.Errorf("link embedded data: %w", err)
		}
	}
	return nil
}

// LinkedField is a field as a survey's readers consume it, forma2's TLinkedEmbeddedField: the definition's public columns and the link's storage key, nothing that identifies or owns the rows (this shape reaches public survey payloads).
type LinkedField struct {
	Field LinkedDefinition `json:"field"`
	Link  Link             `json:"link"`
}

type LinkedDefinition struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	DataType     string `json:"dataType"`
	DefaultValue any    `json:"defaultValue"`
	Locked       bool   `json:"locked"`
}

type Link struct {
	StorageKey string `json:"storageKey"`
}

// SurveyFields is forma2's read seam (selectSurveyEmbeddedDataLinks and inlineSurveyEmbeddedFields): a survey's fields in stored order, the storage key breaking ties.
func SurveyFields(ctx context.Context, db *gorm.DB, surveyID uuid.UUID) ([]LinkedField, error) {
	var rows []struct {
		StorageKey   string
		Name         string
		Source       string
		DataType     string
		DefaultValue []byte
		Locked       bool
	}
	err := db.WithContext(ctx).Raw(
		`SELECT l.storage_key, e.name, e.source, e.data_type, e.default_value, e.locked
		FROM survey_embedded_data l JOIN embedded_data e ON e.id = l.embedded_data_id AND e.workspace_id = l.workspace_id
		WHERE l.survey_id = ? ORDER BY l.position, l.storage_key`, surveyID,
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("load survey embedded data: %w", err)
	}
	out := make([]LinkedField, len(rows))
	for i, r := range rows {
		def, err := readDefault(r.DefaultValue)
		if err != nil {
			return nil, err
		}
		out[i] = LinkedField{Field: LinkedDefinition{Name: r.Name, Source: r.Source, DataType: r.DataType, DefaultValue: def, Locked: r.Locked}, Link: Link{StorageKey: r.StorageKey}}
	}
	return out, nil
}
