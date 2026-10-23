package workflow

import (
	"encoding/json"
	"slices"
)

// DefinitionSummary is forma2's shape summary of a definition for product analytics (packages/workflows/src/analytics/definition-summary.ts). It never names a node type of its own: each node reports its concrete type, so a new trigger or step kind shows up in analytics the day someone uses it. A malformed definition summarizes to zeros.
type DefinitionSummary struct {
	// TriggerType is the trigger's type, empty while a draft has none.
	TriggerType string
	// ActionTypes are the distinct, sorted types of the other nodes: an action's actionType, any other node's own type.
	ActionTypes []string
	// ActionCount counts the other nodes, duplicates included; NodeCount adds the trigger.
	ActionCount int
	NodeCount   int
}

type nodeLike struct {
	Type        any `json:"type"`
	TriggerType any `json:"triggerType"`
	ActionType  any `json:"actionType"`
	Config      any `json:"config"`
}

type definitionLike struct {
	Trigger json.RawMessage `json:"trigger"`
	Nodes   json.RawMessage `json:"nodes"`
}

// concreteType is a node's analytics type: its triggerType, else its actionType, else its type; empty for anything that is not a typed node.
func (n *nodeLike) concreteType() string {
	for _, v := range []any{n.TriggerType, n.ActionType, n.Type} {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func parseNodes(raw json.RawMessage) (*nodeLike, []nodeLike) {
	var d definitionLike
	if json.Unmarshal(raw, &d) != nil {
		return nil, nil
	}
	var trigger *nodeLike
	if t := new(nodeLike); json.Unmarshal(d.Trigger, t) == nil {
		trigger = t
	}
	var items []json.RawMessage
	if json.Unmarshal(d.Nodes, &items) != nil {
		return trigger, nil
	}
	nodes := make([]nodeLike, 0, len(items))
	for _, item := range items {
		var n nodeLike
		if json.Unmarshal(item, &n) == nil {
			nodes = append(nodes, n)
		}
	}
	return trigger, nodes
}

// SummarizeDefinition is forma2's summarizeWorkflowDefinition.
func SummarizeDefinition(raw json.RawMessage) DefinitionSummary {
	trigger, nodes := parseNodes(raw)
	s := DefinitionSummary{ActionTypes: []string{}}
	if trigger != nil {
		s.TriggerType = trigger.concreteType()
	}
	for i := range nodes {
		if t := nodes[i].concreteType(); t != "" {
			s.ActionCount++
			if !slices.Contains(s.ActionTypes, t) {
				s.ActionTypes = append(s.ActionTypes, t)
			}
		}
	}
	slices.Sort(s.ActionTypes)
	s.NodeCount = s.ActionCount
	if s.TriggerType != "" {
		s.NodeCount++
	}
	return s
}

// DefinitionOptions are forma2's PII-free configuration facts of the node types that exist: booleans and small enums only, never a recipient, subject or body. Nil means the definition has nothing the fact applies to.
type DefinitionOptions struct {
	// EndingScope is all or specific for a response.completed trigger.
	EndingScope *string
	// EmailRecipientKind is literal, element or mixed across the send_email actions.
	EmailRecipientKind  *string
	AttachResponseData  *bool
	IncludeVariables    *bool
	IncludeHiddenFields *bool
}

// SummarizeOptions is forma2's summarizeWorkflowDefinitionOptions.
func SummarizeOptions(raw json.RawMessage) DefinitionOptions {
	trigger, nodes := parseNodes(raw)
	var o DefinitionOptions
	if trigger != nil && trigger.TriggerType == TriggerResponseCompleted {
		if config, ok := trigger.Config.(map[string]any); ok {
			scope := "all"
			if ids, ok := config["endingCardIds"].([]any); ok && len(ids) > 0 {
				scope = "specific"
			}
			o.EndingScope = &scope
		}
	}
	var emailConfigs []map[string]any
	for i := range nodes {
		if nodes[i].ActionType != "send_email" {
			continue
		}
		if config, ok := nodes[i].Config.(map[string]any); ok {
			emailConfigs = append(emailConfigs, config)
		}
	}
	kinds := map[string]bool{}
	for _, config := range emailConfigs {
		if to, ok := config["to"].(string); ok && to != "" {
			if IsLiteralRecipient(to) {
				kinds["literal"] = true
			} else {
				kinds["element"] = true
			}
		}
	}
	switch {
	case len(kinds) == 1:
		for k := range kinds {
			o.EmailRecipientKind = &k
		}
	case len(kinds) > 1:
		mixed := "mixed"
		o.EmailRecipientKind = &mixed
	}
	anyFlag := func(key string) *bool {
		if len(emailConfigs) == 0 {
			return nil
		}
		on := slices.ContainsFunc(emailConfigs, func(c map[string]any) bool { return c[key] == true })
		return &on
	}
	o.AttachResponseData = anyFlag("attachResponseData")
	o.IncludeVariables = anyFlag("includeVariables")
	o.IncludeHiddenFields = anyFlag("includeHiddenFields")
	return o
}
