package contact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// A segment's filters are forma2's TBaseFilters (packages/types/segment.ts): a list of nodes, each a filter or a nested group, joined by "and"/"or" connectors.

// Bounds on a filter tree, checked on the raw document before anything walks it recursively (forma2's MAX_SEGMENT_FILTERS_PER_TREE, MAX_SEGMENT_FILTER_DEPTH and MAX_SEGMENT_SURVEY_INTERACTION_IDS_PER_TREE).
const (
	MaxFiltersPerTree              = 1000
	MaxFilterDepth                 = 50
	MaxSurveyInteractionIDsPerTree = 1000
)

// surveyWorkspaceLookupBatchSize is forma2's SURVEY_WORKSPACE_LOOKUP_BATCH_SIZE: survey ids are checked against a workspace this many at a time.
const surveyWorkspaceLookupBatchSize = 200

// The messages of forma2's filter validation, which its actions reported as InvalidInputError.
const (
	codeInvalidFilters           = "invalid_filters"
	filtersSchema                = "segment.filters"
	invalidFiltersMessage        = "Invalid filters"
	invalidFiltersAppliedMessage = "Invalid filters applied"
	filterValueTypeMessage       = "Value must be a string for string operators, a number for arithmetic operators, and an object for relative date operators"
	filterValueMessage           = "Invalid value for filters: please check your filter values"
	selectSurveyMessage          = "Select at least one survey"
	atLeastOneFilterMessage      = "At least one filter is required"
)

// Filter roots, connectors and the operators the evaluation treats specially.
const (
	filterRootAttribute            = "attribute"
	filterRootPerson               = "person"
	filterRootSegment              = "segment"
	filterRootDevice               = "device"
	filterRootSurveyInteraction    = "surveyInteraction"
	connectorAnd                   = "and"
	connectorOr                    = "or"
	personIdentifierUserID         = "userId"
	surveyInteractionScopeSpecific = "specific"
	operatorIsSet                  = "isSet"
	operatorIsNotSet               = "isNotSet"
	operatorUserIsIn               = "userIsIn"
	operatorUserIsNotIn            = "userIsNotIn"
	operatorIsOlderThan            = "isOlderThan"
	operatorIsNewerThan            = "isNewerThan"
	operatorIsBefore               = "isBefore"
	operatorIsAfter                = "isAfter"
	operatorIsBetween              = "isBetween"
	operatorIsSameDay              = "isSameDay"
	operatorEquals                 = "equals"
	operatorNotEquals              = "notEquals"
	unitDays                       = "days"
	unitWeeks                      = "weeks"
	unitMonths                     = "months"
	unitYears                      = "years"
)

var (
	arithmeticOperators = []string{"lessThan", "lessEqual", "greaterThan", "greaterEqual"}
	stringOperators     = []string{"contains", "doesNotContain", "startsWith", "endsWith"}
	dateOperators       = []string{operatorIsOlderThan, operatorIsNewerThan, operatorIsBefore, operatorIsAfter, operatorIsBetween, operatorIsSameDay, operatorIsSet, operatorIsNotSet}
	timeUnits           = []string{unitDays, unitWeeks, unitMonths, unitYears}
)

// Filters is a filter tree.
type Filters []FilterNode

// FilterNode is one entry of a tree: exactly one of Leaf and Group is set.
type FilterNode struct {
	ID        string
	Connector *string
	Leaf      *Filter
	Group     Filters
}

func (n *FilterNode) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID        string          `json:"id"`
		Connector *string         `json:"connector"`
		Resource  json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	n.ID, n.Connector = raw.ID, raw.Connector
	if r := bytes.TrimSpace(raw.Resource); len(r) > 0 && r[0] == '[' {
		n.Group = Filters{}
		return json.Unmarshal(r, &n.Group)
	}
	n.Leaf = &Filter{}
	return json.Unmarshal(raw.Resource, n.Leaf)
}

func (n FilterNode) MarshalJSON() ([]byte, error) {
	var resource any = n.Leaf
	if n.Leaf == nil {
		resource = n.Group
	}
	return json.Marshal(struct {
		ID        string  `json:"id"`
		Connector *string `json:"connector"`
		Resource  any     `json:"resource"`
	}{n.ID, n.Connector, resource})
}

// Filter is one condition.
type Filter struct {
	ID        string          `json:"id"`
	Root      FilterRoot      `json:"root"`
	Value     json.RawMessage `json:"value"`
	Qualifier struct {
		Operator string `json:"operator"`
	} `json:"qualifier"`
}

// FilterRoot says what a filter tests; which field is set depends on Type.
type FilterRoot struct {
	Type                string `json:"type"`
	ContactAttributeKey string `json:"contactAttributeKey,omitempty"`
	PersonIdentifier    string `json:"personIdentifier,omitempty"`
	SegmentID           string `json:"segmentId,omitempty"`
	DeviceType          string `json:"deviceType,omitempty"`
}

// relativeDate is the value of isOlderThan and isNewerThan.
type relativeDate struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// interactionValue is the value of a survey-interaction filter.
type interactionValue struct {
	SurveyScope string   `json:"surveyScope"`
	SurveyIDs   []string `json:"surveyIds"`
	Within      struct {
		Amount int    `json:"amount"`
		Unit   string `json:"unit"`
	} `json:"within"`
}

// filterValue is a decoded filter value: a string, a number, an object or a list.
type filterValue struct {
	raw    json.RawMessage
	str    *string
	num    *float64
	object map[string]json.RawMessage
	list   []json.RawMessage
}

func decodeValue(raw json.RawMessage) filterValue {
	v := filterValue{raw: raw}
	r := bytes.TrimSpace(raw)
	if len(r) == 0 {
		return v
	}
	switch r[0] {
	case '"':
		var s string
		if json.Unmarshal(r, &s) == nil {
			v.str = &s
		}
	case '{':
		_ = json.Unmarshal(r, &v.object)
	case '[':
		_ = json.Unmarshal(r, &v.list)
	default:
		var f float64
		if json.Unmarshal(r, &f) == nil {
			v.num = &f
		}
	}
	return v
}

func (v filterValue) relative() (relativeDate, bool) {
	if v.object == nil {
		return relativeDate{}, false
	}
	_, hasAmount := v.object["amount"]
	_, hasUnit := v.object["unit"]
	if !hasAmount || !hasUnit {
		return relativeDate{}, false
	}
	var r relativeDate
	if json.Unmarshal(v.raw, &r) != nil {
		return relativeDate{}, false
	}
	return r, true
}

func (v filterValue) pair() ([2]string, bool) {
	var out [2]string
	if len(v.list) != 2 {
		return out, false
	}
	for i := range 2 {
		if json.Unmarshal(v.list[i], &out[i]) != nil {
			return out, false
		}
	}
	return out, true
}

func invalidFilters(message string) error {
	return apperr.InvalidInput(codeInvalidFilters, message)
}

// ParseFilters is forma2's ZSegmentFilters as the segment actions applied it: the tree-wide bounds on the raw document, the exported JSON Schema for its structure, then the zod refinements the schema cannot carry. A failure is an InvalidInputError with the first refinement's message, or "Invalid filters" for a structural one, as forma2's actions reported it.
func ParseFilters(schemas *jsonschema.Validator, raw json.RawMessage) (Filters, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, invalidFilters(invalidFiltersMessage)
	}
	if msg := boundsViolation(doc); msg != "" {
		return nil, invalidFilters(msg)
	}
	if err := schemas.Validate(filtersSchema, raw); err != nil {
		return nil, invalidFilters(invalidFiltersMessage)
	}
	var tree Filters
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, invalidFilters(invalidFiltersMessage)
	}
	if msg := refineTree(tree); msg != "" {
		return nil, invalidFilters(msg)
	}
	return tree, nil
}

// boundsViolation is forma2's getSegmentFilterTreeBoundsViolation: one iterative pass over the untrusted document counting nodes, depth and survey-interaction survey ids. Shapes it does not recognise are not counted; they are the schema's to reject.
func boundsViolation(doc any) string {
	type pending struct {
		group []any
		level int
	}
	nodes, depth, ids := 0, 0, 0
	var stack []pending
	if group, ok := doc.([]any); ok {
		stack = append(stack, pending{group, 1})
	}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		depth = max(depth, p.level)
		for _, node := range p.group {
			nodes++
			obj, _ := node.(map[string]any)
			resource := obj["resource"]
			if group, ok := resource.([]any); ok {
				stack = append(stack, pending{group, p.level + 1})
				continue
			}
			ids += interactionIDCount(resource)
		}
	}
	switch {
	case depth > MaxFilterDepth:
		return fmt.Sprintf("Segment filters are nested too deeply: at most %d levels are supported", MaxFilterDepth)
	case nodes > MaxFiltersPerTree:
		return fmt.Sprintf("Too many filters: a segment supports at most %d filters in total", MaxFiltersPerTree)
	case ids > MaxSurveyInteractionIDsPerTree:
		return fmt.Sprintf("Too many surveys referenced: survey-interaction filters may reference at most %d surveys in total", MaxSurveyInteractionIDsPerTree)
	}
	return ""
}

func interactionIDCount(resource any) int {
	leaf, _ := resource.(map[string]any)
	root, _ := leaf["root"].(map[string]any)
	if root == nil || root["type"] != filterRootSurveyInteraction {
		return 0
	}
	value, _ := leaf["value"].(map[string]any)
	ids, _ := value["surveyIds"].([]any)
	return len(ids)
}

// refineTree applies the refinements depth first, children before the list that holds them, and returns the first failure's message as zod orders its issues.
func refineTree(tree Filters) string {
	for _, node := range tree {
		var msg string
		if node.Leaf != nil {
			msg = refineLeaf(node.Leaf)
		} else {
			msg = refineTree(node.Group)
		}
		if msg != "" {
			return msg
		}
	}
	// forma2's refineFilters: a list whose first entry is a filter must give it no connector. A group in first position is not checked.
	if len(tree) > 0 && tree[0].Leaf != nil && tree[0].Connector != nil {
		return invalidFiltersAppliedMessage
	}
	return ""
}

// refineLeaf is ZSegmentFilter's two refinements, after the value's own "Select at least one survey" rule.
func refineLeaf(f *Filter) string {
	v := decodeValue(f.Value)
	if v.object != nil {
		if _, ok := v.object["surveyScope"]; ok {
			var iv interactionValue
			if json.Unmarshal(f.Value, &iv) == nil && iv.SurveyScope == surveyInteractionScopeSpecific && len(iv.SurveyIDs) == 0 {
				return selectSurveyMessage
			}
		}
	}
	if f.Root.Type == filterRootSurveyInteraction {
		return ""
	}
	op := f.Qualifier.Operator
	_, isRelative := v.relative()
	switch {
	case slices.Contains(arithmeticOperators, op) && v.num == nil,
		slices.Contains(stringOperators, op) && v.str == nil,
		(op == operatorIsOlderThan || op == operatorIsNewerThan) && !isRelative,
		(op == operatorIsBefore || op == operatorIsAfter || op == operatorIsSameDay) && v.str == nil,
		op == operatorIsBetween && v.list == nil:
		return filterValueTypeMessage
	}
	if !leafValueValid(op, v) {
		return filterValueMessage
	}
	return ""
}

func leafValueValid(op string, v filterValue) bool {
	switch op {
	case operatorIsSet, operatorIsNotSet:
		return true
	case operatorIsOlderThan, operatorIsNewerThan:
		r, ok := v.relative()
		return ok && r.Amount > 0 && slices.Contains(timeUnits, r.Unit)
	case operatorIsBetween:
		p, ok := v.pair()
		return ok && p[0] != "" && p[1] != ""
	case operatorIsBefore, operatorIsAfter, operatorIsSameDay:
		return v.str != nil && *v.str != ""
	}
	if v.str != nil {
		return *v.str != ""
	}
	return true
}

// walkLeaves calls fn for every filter of the tree, depth first.
func walkLeaves(tree Filters, fn func(*Filter)) {
	for _, node := range tree {
		if node.Leaf != nil {
			fn(node.Leaf)
		} else {
			walkLeaves(node.Group, fn)
		}
	}
}

// interactionSurveyIDs is forma2's collectSurveyIdsFromSegmentFilters: the survey ids of every survey-interaction filter scoped to specific surveys, in tree order.
func interactionSurveyIDs(tree Filters) []string {
	var ids []string
	walkLeaves(tree, func(f *Filter) {
		if f.Root.Type != filterRootSurveyInteraction {
			return
		}
		var iv interactionValue
		if json.Unmarshal(f.Value, &iv) == nil && iv.SurveyScope == surveyInteractionScopeSpecific {
			ids = append(ids, iv.SurveyIDs...)
		}
	})
	return ids
}
