package survey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// desiredField is a survey's embedded-data field as its variables and hidden fields declare it, forma2's TDesiredEmbeddedField.
type desiredField struct {
	StorageKey   string
	Name         string
	Source       string
	DataType     string
	DefaultValue json.RawMessage
}

// desiredFromVariables is forma2's mapping of variables: computed fields under the variable id, numbers or strings, defaulting to the variable's value.
func desiredFromVariables(variables []surveydoc.Variable) []desiredField {
	out := make([]desiredField, 0, len(variables))
	for _, v := range variables {
		dataType := "string"
		if v.Type == "number" {
			dataType = "number"
		}
		value := v.Value
		if len(value) == 0 {
			value = json.RawMessage(`""`)
			if v.Type == "number" {
				value = json.RawMessage(`0`)
			}
		}
		out = append(out, desiredField{StorageKey: v.ID, Name: v.Name, Source: "computed", DataType: dataType, DefaultValue: value})
	}
	return out
}

// desiredFromHiddenFields is forma2's mapping of hidden fields: ingested strings named and keyed by the field id, without a default.
func desiredFromHiddenFields(h surveydoc.HiddenFields) []desiredField {
	out := make([]desiredField, 0, len(h.FieldIDs))
	for _, id := range h.FieldIDs {
		out = append(out, desiredField{StorageKey: id, Name: id, Source: "ingested", DataType: "string"})
	}
	return out
}

type currentField struct {
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

// embeddedPatch says which groups a save replaces; a nil group keeps the stored rows.
type embeddedPatch struct {
	Variables    *[]surveydoc.Variable
	HiddenFields *surveydoc.HiddenFields
}

func sameJSON(a, b []byte) bool {
	if isNullJSON(a) || isNullJSON(b) {
		return isNullJSON(a) && isNullJSON(b)
	}
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	xs, _ := json.Marshal(x)
	ys, _ := json.Marshal(y)
	return bytes.Equal(xs, ys)
}

// reconcileEmbeddedData is forma2's reconcileEmbeddedData, run in the transaction of the survey save: the survey's embedded-data links are made to match its variables (computed) and hidden fields (ingested). A link whose key now names something else is removed, with its definition when the survey owns it; owned definitions take the new name, type and default; positions follow the declared order; new keys get a new survey-local definition. Shared definitions are never edited.
func (r *Repository) reconcileEmbeddedData(ctx context.Context, surveyID, workspaceID uuid.UUID, patch embeddedPatch) error {
	var current []currentField
	err := r.db.WithContext(ctx).Raw(`SELECT l.id AS link_id, l.storage_key, l.position, e.id AS field_id, e.survey_id, e.name, e.source, e.data_type, e.default_value
		FROM survey_embedded_data l JOIN embedded_data e ON e.id = l.embedded_data_id WHERE l.survey_id = ? ORDER BY l.position, l.storage_key`, surveyID).Scan(&current).Error
	if err != nil {
		return fmt.Errorf("load embedded data: %w", err)
	}
	var desired []desiredField
	carried := func(source string) {
		for _, c := range current {
			if c.Source == source {
				desired = append(desired, desiredField{StorageKey: c.StorageKey, Name: c.Name, Source: c.Source, DataType: c.DataType, DefaultValue: c.DefaultValue})
			}
		}
	}
	if patch.Variables != nil {
		desired = append(desired, desiredFromVariables(*patch.Variables)...)
	} else {
		carried("computed")
	}
	if patch.HiddenFields != nil {
		desired = append(desired, desiredFromHiddenFields(*patch.HiddenFields)...)
	} else {
		carried("ingested")
	}
	type wanted struct {
		field    desiredField
		position int
	}
	desiredByKey := map[string]wanted{}
	for i, d := range desired {
		if _, dup := desiredByKey[d.StorageKey]; dup {
			return apperr.InvalidInput(apperr.CodeBadRequest, "Duplicate embedded data field: "+d.StorageKey)
		}
		desiredByKey[d.StorageKey] = wanted{d, i}
	}
	currentByKey := map[string]currentField{}
	for _, c := range current {
		currentByKey[c.StorageKey] = c
	}

	for _, c := range current {
		w, ok := desiredByKey[c.StorageKey]
		owned := c.SurveyID != nil && *c.SurveyID == surveyID
		if !ok || w.field.Source != c.Source {
			if _, err := r.exec(ctx, `DELETE FROM survey_embedded_data WHERE id = ?`, c.LinkID); err != nil {
				return fmt.Errorf("unlink embedded data: %w", err)
			}
			if owned {
				if _, err := r.exec(ctx, `DELETE FROM embedded_data e WHERE e.id = ? AND e.survey_id = ? AND NOT EXISTS (SELECT 1 FROM survey_embedded_data l WHERE l.embedded_data_id = e.id)`, c.FieldID, surveyID); err != nil {
					return fmt.Errorf("delete embedded data: %w", err)
				}
			}
			continue
		}
		if c.Position != w.position {
			if _, err := r.exec(ctx, `UPDATE survey_embedded_data SET position = ? WHERE id = ? AND survey_id = ?`, w.position, c.LinkID, surveyID); err != nil {
				return fmt.Errorf("reorder embedded data: %w", err)
			}
		}
		if owned && (w.field.Name != c.Name || w.field.DataType != c.DataType || !sameJSON(w.field.DefaultValue, c.DefaultValue)) {
			if _, err := r.exec(ctx, `UPDATE embedded_data SET name = ?, data_type = ?, default_value = ?::jsonb, updated_at = now() WHERE id = ? AND survey_id = ?`,
				w.field.Name, w.field.DataType, jsonParam(w.field.DefaultValue), c.FieldID, surveyID); err != nil {
				return fmt.Errorf("update embedded data: %w", err)
			}
		}
	}

	for i, d := range desired {
		if c, ok := currentByKey[d.StorageKey]; ok && c.Source == d.Source {
			continue
		}
		var fieldID uuid.UUID
		if err := r.db.WithContext(ctx).Raw(`INSERT INTO embedded_data (workspace_id, survey_id, name, source, data_type, default_value) VALUES (?, ?, ?, ?, ?, ?::jsonb) RETURNING id`,
			workspaceID, surveyID, d.Name, d.Source, d.DataType, jsonParam(d.DefaultValue)).Row().Scan(&fieldID); err != nil {
			return fmt.Errorf("create embedded data: %w", err)
		}
		if _, err := r.exec(ctx, `INSERT INTO survey_embedded_data (workspace_id, survey_id, embedded_data_id, storage_key, position) VALUES (?, ?, ?, ?, ?)`,
			workspaceID, surveyID, fieldID, d.StorageKey, i); err != nil {
			return fmt.Errorf("link embedded data: %w", err)
		}
	}
	return nil
}
