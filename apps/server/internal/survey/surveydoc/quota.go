package surveydoc

import (
	"encoding/json"
	"fmt"
)

// QuotaLogic is a quota's inclusion criteria: a flat list of single conditions, unlike block logic, which nests groups.
type QuotaLogic struct {
	Connector  string      `json:"connector"`
	Conditions []Condition `json:"conditions"`
}

// Quota is forma2's ZSurveyQuotaInput.
type Quota struct {
	Name                    string          `json:"name"`
	Limit                   float64         `json:"limit"`
	Logic                   json.RawMessage `json:"logic"`
	Action                  string          `json:"action"`
	EndingCardID            *string         `json:"endingCardId"`
	CountPartialSubmissions bool            `json:"countPartialSubmissions"`
}

// QuotaIssues validates a quota definition: the logic's structure against the exported schema, then forma2's rules.
func (v *Validator) QuotaIssues(q Quota) ([]Issue, error) {
	var is issues
	if q.Name == "" {
		is.add(path("name"), "Quota name is required")
	}
	if q.Limit < 1 {
		is.add(path("limit"), "Limit must be greater than 0")
	}
	if q.Limit != float64(int64(q.Limit)) || q.Limit > 2147483647 {
		is.add(path("limit"), "Invalid input: expected int, received number")
	}
	if q.Action != "endSurvey" && q.Action != "continueSurvey" {
		is.add(path("action"), `Invalid option: expected one of "endSurvey"|"continueSurvey"`)
	}
	if q.EndingCardID != nil && *q.EndingCardID != "" && !IsCUID2(*q.EndingCardID) {
		is.add(path("endingCardId"), "Invalid cuid2")
	}
	if isAbsent(q.Logic) {
		is.add(path("logic"), "Invalid input: expected object, received undefined")
	} else {
		structure, err := v.schemaIssues("survey_quota.logic", q.Logic, "logic")
		if err != nil {
			return nil, err
		}
		if len(structure) > 0 {
			is = append(is, structure...)
		} else {
			var logic QuotaLogic
			if err := json.Unmarshal(q.Logic, &logic); err != nil {
				return nil, fmt.Errorf("decode quota logic: %w", err)
			}
			for i := range logic.Conditions {
				is.singleConditionFields(path("logic", "conditions", i), &logic.Conditions[i])
			}
		}
	}
	if q.Action == "endSurvey" && (q.EndingCardID == nil || *q.EndingCardID == "") {
		is.add(path("action"), "endingCardId is required when action is endSurvey")
	}
	return is, nil
}
