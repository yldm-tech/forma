package contact

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
)

// InteractionRefresh is forma2's TSurveyInteractionRefresh, the per-survey gate of the SDK's segment refresh after an interaction: whether a display of the survey, a response to it, or finishing that response can change some live survey's segment membership.
type InteractionRefresh struct {
	OnDisplay  bool `json:"onDisplay"`
	OnResponse bool `json:"onResponse"`
	OnFinished bool `json:"onFinished"`
}

// interactionSource is forma2's SURVEY_INTERACTION_OPERATOR_SOURCE: the event that can flip a membership depending on each operator, negative or positive.
var interactionSource = map[string]func(*InteractionRefresh){
	"haveSeen":                func(r *InteractionRefresh) { r.OnDisplay = true },
	"haveNotSeen":             func(r *InteractionRefresh) { r.OnDisplay = true },
	"haveStartedRespondingTo": func(r *InteractionRefresh) { r.OnResponse = true },
	"haveCompleted":           func(r *InteractionRefresh) { r.OnFinished = true },
	"haveNotCompleted":        func(r *InteractionRefresh) { r.OnFinished = true },
}

// SurveyFilters is a survey the SDK is sent, with its segment's filters (nil without a segment).
type SurveyFilters struct {
	SurveyID uuid.UUID
	Filters  Filters
}

// InteractionRefreshMap is forma2's buildSurveyInteractionRefreshMap: every survey-interaction filter of the delivered surveys' segments, including those inside a segment a filter refers to, sets its event on the surveys it names (every delivered survey for "any"). Only delivered surveys carry flags: the SDK fires no event for any other. hasAny reports whether any flag is set. resolve returns a referenced segment's filters, false for one that does not exist.
func InteractionRefreshMap(surveys []SurveyFilters, resolve func(segmentID string) (Filters, bool)) (refresh map[uuid.UUID]*InteractionRefresh, hasAny bool) {
	refresh = make(map[uuid.UUID]*InteractionRefresh, len(surveys))
	for _, s := range surveys {
		refresh[s.SurveyID] = &InteractionRefresh{}
	}
	apply := func(f *Filter) {
		set, ok := interactionSource[f.Qualifier.Operator]
		if !ok {
			return
		}
		var v interactionValue
		_ = json.Unmarshal(f.Value, &v)
		if v.SurveyScope != surveyInteractionScopeSpecific {
			for _, s := range surveys {
				set(refresh[s.SurveyID])
				hasAny = true
			}
			return
		}
		for _, raw := range v.SurveyIDs {
			id, _ := legacyid.Resolve(raw)
			if r, delivered := refresh[id]; delivered {
				set(r)
				hasAny = true
			}
		}
	}
	var walk func(tree Filters, visited map[string]bool)
	walk = func(tree Filters, visited map[string]bool) {
		for _, node := range tree {
			switch {
			case node.Group != nil:
				walk(node.Group, visited)
			case node.Leaf == nil:
			case node.Leaf.Root.Type == filterRootSurveyInteraction:
				apply(node.Leaf)
			case node.Leaf.Root.Type == filterRootSegment:
				id := node.Leaf.Root.SegmentID
				if visited[id] {
					continue
				}
				visited[id] = true
				if nested, ok := resolve(id); ok {
					walk(nested, visited)
				}
			}
		}
	}
	for _, s := range surveys {
		if s.Filters != nil {
			walk(s.Filters, map[string]bool{})
		}
	}
	return refresh, hasAny
}
