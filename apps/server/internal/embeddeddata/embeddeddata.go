// Package embeddeddata is a survey's Embedded Data: the field definitions in embedded_data (local to one survey, or shared in the workspace library) and the per-survey links in survey_embedded_data that address them, kept in step with the survey's variables and hidden fields on every save, with forma2's rules (packages/types/embedded-data*.ts, apps/web/lib/embedded-data).
//
// forma2 has no library manager yet (ENG-1851): every row a survey save writes is local to that survey, and shared rows are only ever linked, reordered and unlinked, never edited or deleted, by a survey save.
package embeddeddata

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Sources of a stored field. "reserved" fields are a static catalog in code and never stored.
const (
	SourceComputed = "computed"
	SourceIngested = "ingested"
)

// Data types of a field.
const (
	TypeString  = "string"
	TypeNumber  = "number"
	TypeBoolean = "boolean"
	TypeDate    = "date"
)

// Variable is a survey variable (ZSurveyVariable): a number or text value the survey's logic computes. Value is a float64 or a string, as decoded from JSON.
type Variable struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Field is one Embedded Data field a survey should have, forma2's TDesiredEmbeddedField: the definition and its link flattened, without the ids the database assigns. DefaultValue is nil, a string, a float64 or a bool.
type Field struct {
	StorageKey   string
	Name         string
	Source       string
	DataType     string
	DefaultValue any
}

// DesiredFields is forma2's toDesiredEmbeddedFields: a variable is addressed by its id and computed, typed number for a number variable and string otherwise, with its value as the default; a hidden field is addressed and named by its id, ingested, untyped (string) and without a default. Computed fields come first. Duplicates are kept, for the caller to reject.
func DesiredFields(variables []Variable, hiddenFieldIDs []string) []Field {
	out := make([]Field, 0, len(variables)+len(hiddenFieldIDs))
	for _, v := range variables {
		dataType := TypeString
		if v.Type == TypeNumber {
			dataType = TypeNumber
		}
		out = append(out, Field{StorageKey: v.ID, Name: v.Name, Source: SourceComputed, DataType: dataType, DefaultValue: v.Value})
	}
	for _, id := range hiddenFieldIDs {
		out = append(out, Field{StorageKey: id, Name: id, Source: SourceIngested, DataType: TypeString})
	}
	return out
}

// Patch is what a survey save carries of its Embedded Data. A nil group was not part of the save and keeps its stored fields; a non-nil empty group clears them. A save whose hidden fields are null carries an empty group.
type Patch struct {
	Variables      *[]Variable
	HiddenFieldIDs *[]string
}

// resolveDesired is forma2's resolveDesiredEmbeddedFields: each group the save carried replaces that group, and a group it did not carry keeps the current fields of its source in their stored order. The list's order is the stored position of each field, computed before ingested.
func resolveDesired(current []Field, patch Patch) []Field {
	carried := func(source string) []Field {
		var out []Field
		for _, f := range current {
			if f.Source == source {
				out = append(out, f)
			}
		}
		return out
	}
	var out []Field
	if patch.Variables != nil {
		out = append(out, DesiredFields(*patch.Variables, nil)...)
	} else {
		out = append(out, carried(SourceComputed)...)
	}
	if patch.HiddenFieldIDs != nil {
		out = append(out, DesiredFields(nil, *patch.HiddenFieldIDs)...)
	} else {
		out = append(out, carried(SourceIngested)...)
	}
	return out
}

// legacyIDCharset is isLegacyIdCharset, the storage key rule (ZSurveyEmbeddedData): the address a hidden field is filled from by URL parameter, so a padded or punctuated key could never receive a value.
var legacyIDCharset = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func invalid(message string) *apperr.Error {
	return apperr.InvalidInput(apperr.CodeValidation, message)
}

// validateDesired rejects what the tables would store but forma2's row schemas refuse: a repeated storage key (forma2's InvalidInputError "Duplicate embedded data field: <key>"), a storage key outside the legacy id charset, a blank name, and a default that disagrees with the field's type.
func validateDesired(fields []Field) error {
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if seen[f.StorageKey] {
			return invalid("Duplicate embedded data field: " + f.StorageKey)
		}
		seen[f.StorageKey] = true
		if !legacyIDCharset.MatchString(f.StorageKey) {
			return invalid(fmt.Sprintf("Invalid embedded data field %q: the key must contain only letters, digits, hyphens and underscores", f.StorageKey))
		}
		if strings.TrimSpace(f.Name) == "" {
			return invalid(fmt.Sprintf("Invalid embedded data field %q: the name must not be blank", f.StorageKey))
		}
		if !defaultMatchesType(f.DefaultValue, f.DataType) {
			return invalid(fmt.Sprintf("Invalid embedded data field %q: the default value does not match its %s type", f.StorageKey, f.DataType))
		}
	}
	return nil
}

// isoDateTime is the datetime half of packages/types' ZIsoDateOrDateTime, z.iso.datetime() or z.iso.datetime({ precision: -1 }): a UTC date and time with optional seconds and fraction.
var isoDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d+)?)?Z$`)

// defaultMatchesType is ZEmbeddedData's default value rule: a default has its field's type, and a date default is an ISO 8601 date or UTC datetime string.
func defaultMatchesType(v any, dataType string) bool {
	switch d := v.(type) {
	case nil:
		return true
	case float64:
		return dataType == TypeNumber
	case bool:
		return dataType == TypeBoolean
	case string:
		switch dataType {
		case TypeString:
			return true
		case TypeDate:
			if _, err := time.Parse(time.DateOnly, d); err == nil {
				return true
			}
			if !isoDateTime.MatchString(d) {
				return false
			}
			_, err := time.Parse(time.DateOnly, d[:10])
			return err == nil
		}
	}
	return false
}
