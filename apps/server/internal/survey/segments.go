package survey

import (
	"encoding/json"
	"fmt"

	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// Bounds on a segment filter tree, forma2's MAX_SEGMENT_FILTERS_PER_TREE, MAX_SEGMENT_FILTER_DEPTH and MAX_SEGMENT_SURVEY_INTERACTION_IDS_PER_TREE.
const (
	maxSegmentFilters               = 1000
	maxSegmentFilterDepth           = 50
	maxSegmentSurveyInteractionIDs  = 1000
	segmentFiltersInvalidConnection = "Invalid filters applied"
)

// segmentFilterBoundsViolation is forma2's getSegmentFilterTreeBoundsViolation: the tree's node count, nesting depth and survey-interaction ids are bounded before anything walks it recursively. Shapes it does not recognise are left to the schema.
func segmentFilterBoundsViolation(raw json.RawMessage) string {
	if isNullJSON(raw) {
		return ""
	}
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	nodes, depth, interactionIDs := 0, 0, 0
	type pending struct {
		group []any
		level int
	}
	var stack []pending
	if group, ok := root.([]any); ok {
		stack = append(stack, pending{group, 1})
	}
	for len(stack) > 0 {
		entry := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		depth = max(depth, entry.level)
		for _, node := range entry.group {
			nodes++
			obj, _ := node.(map[string]any)
			resource := obj["resource"]
			if group, ok := resource.([]any); ok {
				stack = append(stack, pending{group, entry.level + 1})
				continue
			}
			interactionIDs += surveyInteractionIDCount(resource)
		}
	}
	switch {
	case depth > maxSegmentFilterDepth:
		return fmt.Sprintf("Segment filters are nested too deeply: at most %d levels are supported", maxSegmentFilterDepth)
	case nodes > maxSegmentFilters:
		return fmt.Sprintf("Too many filters: a segment supports at most %d filters in total", maxSegmentFilters)
	case interactionIDs > maxSegmentSurveyInteractionIDs:
		return fmt.Sprintf("Too many surveys referenced: survey-interaction filters may reference at most %d surveys in total", maxSegmentSurveyInteractionIDs)
	}
	return ""
}

func surveyInteractionIDCount(resource any) int {
	obj, _ := resource.(map[string]any)
	root, _ := obj["root"].(map[string]any)
	if root == nil || root["type"] != "surveyInteraction" {
		return 0
	}
	value, _ := obj["value"].(map[string]any)
	ids, _ := value["surveyIds"].([]any)
	return len(ids)
}

// segmentFilterNode is a node of a filter tree as the connector rule reads it.
type segmentFilterNode struct {
	Connector *string         `json:"connector"`
	Resource  json.RawMessage `json:"resource"`
}

// firstConnectorsAreNull is forma2's refineFilters: the first node of every group has no connector.
func firstConnectorsAreNull(raw json.RawMessage) bool {
	var nodes []segmentFilterNode
	if json.Unmarshal(raw, &nodes) != nil {
		return false
	}
	for i, n := range nodes {
		if len(n.Resource) > 0 && n.Resource[0] == '[' {
			if !firstConnectorsAreNull(n.Resource) {
				return false
			}
			continue
		}
		if i == 0 && n.Connector != nil {
			return false
		}
	}
	return true
}

// segmentFilterIssues is forma2's ZSegmentFilters on a survey save: the bounds, the exported schema, then the connector rule.
func (s *Service) segmentFilterIssues(raw json.RawMessage, prefix ...any) ([]surveydoc.Issue, error) {
	if m := segmentFilterBoundsViolation(raw); m != "" {
		return []surveydoc.Issue{{Path: prefix, Message: m}}, nil
	}
	issues, err := s.docs.SchemaIssues("segment.filters", raw, prefix...)
	if err != nil || len(issues) > 0 {
		return issues, err
	}
	if !firstConnectorsAreNull(raw) {
		return []surveydoc.Issue{{Path: prefix, Message: segmentFiltersInvalidConnection}}, nil
	}
	return nil, nil
}
