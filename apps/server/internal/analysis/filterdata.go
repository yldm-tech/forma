package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
	"github.com/yldm-tech/forma/apps/server/internal/tag"
)

// filterSampleSize is forma2's RESPONSE_FILTER_SAMPLE_SIZE: the filter's options come from the newest responses only.
const filterSampleSize = 10_000

// maxFilterOptions is forma2's MAX_FILTER_VALUE_OPTIONS, the distinct values a reserved or variable field offers at most.
const maxFilterOptions = 50

// FilterData is forma2's getSurveyFilterDataAction result: what the response filter offers for the survey.
type FilterData struct {
	EnvironmentTags []tag.Tag           `json:"environmentTags"`
	Attributes      map[string][]string `json:"attributes"`
	Meta            map[string][]string `json:"meta"`
	HiddenFields    map[string][]string `json:"hiddenFields"`
	ReservedValues  map[string][]string `json:"reservedValues"`
	VariableValues  map[string][]string `json:"variableValues"`
	Quotas          []survey.Quota      `json:"quotas"`
	// AttributeTypes are the data types (string, number, date) of the attribute keys Attributes lists, by key, which decide the operators the filter offers; a key the workspace no longer has is missing.
	AttributeTypes map[string]string `json:"attributeTypes"`
	// Segments are the workspace's public segments, by title, which the filter offers as "in segment" and "not in segment". A survey's own private segment is its targeting, not something to filter by.
	Segments []FilterSegment `json:"segments"`
}

// FilterSegment is a segment the response filter offers.
type FilterSegment struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

// valueSet collects distinct values per key in the order they are first seen, up to an optional bound per key.
type valueSet struct {
	limit  int
	values map[string][]string
	seen   map[string]map[string]bool
}

func newValueSet(limit int) *valueSet {
	return &valueSet{limit: limit, values: map[string][]string{}, seen: map[string]map[string]bool{}}
}

// open makes key present even without values.
func (v *valueSet) open(key string) {
	if _, ok := v.values[key]; !ok {
		v.values[key] = []string{}
		v.seen[key] = map[string]bool{}
	}
}

func (v *valueSet) add(key, value string) {
	v.open(key)
	if v.seen[key][value] || (v.limit > 0 && len(v.values[key]) >= v.limit) {
		return
	}
	v.seen[key][value] = true
	v.values[key] = append(v.values[key], value)
}

// reservedFilterEntries is forma2's getReservedFilterEntries: the reserved fields the response table shows, and durationSeconds, minus what the survey's own names shadow, what an anonymized survey no longer captures, and the IP address unless the survey captures it.
func reservedFilterEntries(sv *surveyDoc) []embeddeddata.ReservedField {
	var out []embeddeddata.ReservedField
	for _, e := range embeddeddata.DropShadowed(embeddeddata.ReservedCatalog, embeddeddata.ShadowingNames(sv.Fields, sv.elementIDs())) {
		if e.Display == embeddeddata.DisplayNone && e.Name != "durationSeconds" {
			continue
		}
		if sv.IsAnonymizeResponsesEnabled && e.Privacy == embeddeddata.PrivacyDrop {
			continue
		}
		if e.Name == "ipAddress" && !sv.IsCaptureIPEnabled {
			continue
		}
		out = append(out, e)
	}
	return out
}

// FilterData is forma2's getSurveyFilterDataAction: the workspace's tags and public segments, the survey's quotas, and the values the newest responses hold for contact attributes, meta, hidden fields, reserved fields and computed variables.
func (s *Service) FilterData(ctx context.Context, userID, surveyID uuid.UUID) (*FilterData, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseRead)
	if err != nil {
		return nil, err
	}
	out := &FilterData{EnvironmentTags: []tag.Tag{}, Quotas: []survey.Quota{}, Segments: []FilterSegment{}}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, name, workspace_id FROM tags WHERE workspace_id = ? ORDER BY created_at, id`, sv.WorkspaceID).
		Scan(&out.EnvironmentTags).Error; err != nil {
		return nil, fmt.Errorf("load tags: %w", err)
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, title FROM segments WHERE workspace_id = ? AND NOT is_private ORDER BY title, id`, sv.WorkspaceID).
		Scan(&out.Segments).Error; err != nil {
		return nil, fmt.Errorf("load segments: %w", err)
	}
	var quotas []struct {
		ID                      uuid.UUID
		CreatedAt               time.Time
		UpdatedAt               time.Time
		SurveyID                uuid.UUID
		Name                    string
		Limit                   int
		Logic                   []byte
		Action                  string
		EndingCardID            *string
		CountPartialSubmissions bool
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, created_at, updated_at, survey_id, name, "limit", logic, action, ending_card_id, count_partial_submissions
		FROM survey_quotas WHERE survey_id = ? ORDER BY created_at DESC, id DESC`, surveyID).Scan(&quotas).Error; err != nil {
		return nil, fmt.Errorf("load quotas: %w", err)
	}
	for _, q := range quotas {
		out.Quotas = append(out.Quotas, survey.Quota{ID: q.ID, CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt, SurveyID: q.SurveyID, Name: q.Name, Limit: q.Limit,
			Logic: q.Logic, Action: q.Action, EndingCardID: q.EndingCardID, CountPartialSubmissions: q.CountPartialSubmissions})
	}

	var rows []struct {
		ID                uuid.UUID
		SurveyID          uuid.UUID
		CreatedAt         time.Time
		UpdatedAt         time.Time
		Finished          bool
		Language          *string
		Data              []byte
		Variables         []byte
		TTC               []byte
		Meta              []byte
		ContactAttributes []byte
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT id, survey_id, created_at, updated_at, finished, language, data, variables, ttc, meta, contact_attributes
		FROM responses WHERE survey_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, surveyID, filterSampleSize).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read responses for filter values: %w", err)
	}

	attributes, meta, hidden := newValueSet(0), newValueSet(0), newValueSet(0)
	reserved, variables := newValueSet(maxFilterOptions), newValueSet(maxFilterOptions)
	ingested := embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested)
	for _, f := range ingested {
		hidden.open(f.StorageKey)
	}
	var stringReserved []embeddeddata.ReservedField
	for _, e := range reservedFilterEntries(sv) {
		if e.DataType == embeddeddata.TypeString {
			stringReserved = append(stringReserved, e)
		}
	}
	var stringVariables []embeddeddata.Linked
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceComputed) {
		if f.DataType == embeddeddata.TypeString {
			stringVariables = append(stringVariables, f)
		}
	}

	for _, r := range rows {
		vr := embeddeddata.ValueResponse{ID: r.ID.String(), SurveyID: r.SurveyID.String(), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Finished: r.Finished, Language: r.Language}
		for _, part := range []struct {
			raw []byte
			dst *map[string]any
		}{{r.Data, &vr.Data}, {r.Variables, &vr.Variables}, {r.TTC, &vr.TTC}, {r.Meta, &vr.Meta}} {
			if err := json.Unmarshal(part.raw, part.dst); err != nil {
				return nil, fmt.Errorf("decode response %s: %w", r.ID, err)
			}
		}

		// forma2's getResponseContactAttributes: every value as text.
		var contactAttributes map[string]any
		if len(r.ContactAttributes) > 0 {
			if err := json.Unmarshal(r.ContactAttributes, &contactAttributes); err != nil {
				return nil, fmt.Errorf("decode contact attributes of response %s: %w", r.ID, err)
			}
		}
		for _, k := range jsonbKeys(contactAttributes) {
			if v := contactAttributes[k]; v != nil {
				attributes.add(k, jsvalue.String(v))
			}
		}

		// forma2's getResponseMeta: text values, an object's (the user agent's) under its own keys; the URL is offered without values.
		for _, k := range jsonbKeys(vr.Meta) {
			if k == "url" {
				meta.open(k)
				continue
			}
			switch v := vr.Meta[k].(type) {
			case map[string]any:
				for _, nk := range jsonbKeys(v) {
					if s, ok := v[nk].(string); ok && s != "" {
						meta.add(nk, s)
					}
				}
			case string:
				if v != "" {
					meta.add(k, v)
				}
			}
		}

		// forma2's getResponseHiddenFields: the non-empty text values of the ingested fields.
		for _, f := range ingested {
			if s, ok := vr.Data[f.StorageKey].(string); ok && s != "" {
				hidden.add(f.StorageKey, s)
			}
		}

		// forma2's getResponseReservedFilterValues, through the projection that strips URL queries.
		projected := embeddeddata.Project(stringReserved, vr)
		for _, e := range stringReserved {
			if s, ok := projected[e.Name].(string); ok && s != "" {
				reserved.add(e.Name, s)
			}
		}

		// forma2's getResponseVariableFilterValues.
		for _, f := range stringVariables {
			if s, ok := vr.Variables[f.StorageKey].(string); ok && s != "" {
				variables.add(f.StorageKey, s)
			}
		}
	}
	out.Attributes, out.Meta, out.HiddenFields, out.ReservedValues, out.VariableValues = attributes.values, meta.values, hidden.values, reserved.values, variables.values
	if out.AttributeTypes, err = attributeTypes(ctx, s.db, sv.WorkspaceID, out.Attributes); err != nil {
		return nil, err
	}
	return out, nil
}

// attributeTypes are the data types of the workspace's attribute keys among those of attributes.
func attributeTypes(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, attributes map[string][]string) (map[string]string, error) {
	out := map[string]string{}
	if len(attributes) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	var rows []struct {
		Key      string
		DataType string
	}
	if err := db.WithContext(ctx).Raw(`SELECT key, data_type FROM contact_attribute_keys WHERE workspace_id = ? AND key IN ?`, workspaceID, keys).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load contact attribute types: %w", err)
	}
	for _, r := range rows {
		out[r.Key] = r.DataType
	}
	return out, nil
}
