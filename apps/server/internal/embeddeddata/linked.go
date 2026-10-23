package embeddeddata

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LoadLinked reads a survey's fields in their stored order, forma2's embeddedFields as the survey payload carries them: every link with the definition it points at.
func LoadLinked(ctx context.Context, db *gorm.DB, surveyID uuid.UUID) ([]Linked, error) {
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
		return nil, fmt.Errorf("load embedded data fields: %w", err)
	}
	out := make([]Linked, len(rows))
	for i, r := range rows {
		def, err := readDefault(r.DefaultValue)
		if err != nil {
			return nil, err
		}
		out[i] = Linked{Field: Field{StorageKey: r.StorageKey, Name: r.Name, Source: r.Source, DataType: r.DataType, DefaultValue: def}, Locked: r.Locked}
	}
	return out, nil
}
