package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/platform/analytics"
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

// lifecycleEvents are forma2's WORKFLOW_LIFECYCLE_EVENTS. Edits have none on purpose: the builder autosaves every few seconds, so the final shape is reported when the workflow is enabled.
var lifecycleEvents = map[string]string{
	"created":    "workflow_created",
	"duplicated": "workflow_duplicated",
	"enabled":    "workflow_enabled",
	"disabled":   "workflow_disabled",
	"archived":   "workflow_archived",
	"unarchived": "workflow_unarchived",
	"deleted":    "workflow_deleted",
	"tested":     "workflow_tested",
}

// lifecycleEvent is forma2's WorkflowAnalyticsDetail: ids, the status transition, timestamps and the definition's shape, never the definition itself.
type lifecycleEvent struct {
	operation string
	// row is the workflow after the operation; for deleted and tested, as it was.
	row              *Workflow
	previousStatus   string
	sourceWorkflowID *uuid.UUID
	testOK           *bool
}

// captureTransition records a status change that succeeded.
func (s *Service) captureTransition(ctx context.Context, actor authz.Actor, operation string, before, after *Workflow, err error) {
	if err == nil && after != nil {
		s.capture(ctx, actor, lifecycleEvent{operation: operation, row: after, previousStatus: before.Status})
	}
}

// capture is forma2's buildRecordAnalytics: the event goes to the acting user, or to the organization for an API key, which has no person of its own, and says which surface acted (the builder, the API, or an MCP tool).
func (s *Service) capture(ctx context.Context, actor authz.Actor, e lifecycleEvent) {
	if !analytics.Enabled() {
		return
	}
	org := s.organizationOf(ctx, e.row.WorkspaceID)
	via := "ui"
	switch {
	case authz.SurfaceOf(ctx) == authz.SurfaceMCP:
		via = "mcp"
	case actor.Type == authz.ActorAPIKey:
		via = "api"
	}
	distinctID := org
	if actor.Type == authz.ActorUser {
		distinctID = actor.ID.String()
	}
	summary := SummarizeDefinition(e.row.Definition)
	options := SummarizeOptions(e.row.Definition)
	props := analytics.Properties{
		"workflow_id":           e.row.ID.String(),
		"workspace_id":          e.row.WorkspaceID.String(),
		"organization_id":       org,
		"via":                   via,
		"status":                e.row.Status,
		"hours_since_created":   math.Round(time.Since(e.row.CreatedAt).Hours()*10) / 10,
		"trigger_type":          nil,
		"action_types":          summary.ActionTypes,
		"action_types_joined":   strings.Join(summary.ActionTypes, ","),
		"action_count":          summary.ActionCount,
		"node_count":            summary.NodeCount,
		"ending_scope":          options.EndingScope,
		"email_recipient_kind":  options.EmailRecipientKind,
		"attach_response_data":  options.AttachResponseData,
		"include_variables":     options.IncludeVariables,
		"include_hidden_fields": options.IncludeHiddenFields,
	}
	if summary.TriggerType != "" {
		props["trigger_type"] = summary.TriggerType
	}
	if e.previousStatus != "" {
		props["previous_status"] = e.previousStatus
	}
	if e.sourceWorkflowID != nil {
		props["source_workflow_id"] = e.sourceWorkflowID.String()
	}
	if e.testOK != nil {
		props["test_ok"] = *e.testOK
	}
	analytics.Capture(distinctID, lifecycleEvents[e.operation], props, analytics.Groups{OrganizationID: org, WorkspaceID: e.row.WorkspaceID.String()})
}

// runFailure is forma2's WorkflowRunFailureFacts: a coarse failure class instead of the error message, which can name a recipient.
type runFailure struct {
	runID, workflowID, workspaceID uuid.UUID
	triggerType                    string
	// failedStepType is the step that failed, empty when the run failed outside a step.
	failedStepType string
	errorKind      string
	attempt        int
}

// captureRunFailed is forma2's workflow_run_failed, once per run that ends failed, from every writer of that terminal status: the runner's last attempt and both reconcilers, so the event agrees with the snapshot's failed runs.
func captureRunFailed(ctx context.Context, db *gorm.DB, log *slog.Logger, f runFailure) {
	if !analytics.Enabled() {
		return
	}
	var orgs []string
	if err := db.WithContext(ctx).Raw(`SELECT organization_id::text FROM workspaces WHERE id = ?`, f.workspaceID).Scan(&orgs).Error; err != nil {
		log.WarnContext(ctx, "Failed to capture workflow run failure analytics", "err", err)
		return
	}
	distinctID, org := f.workspaceID.String(), any(nil)
	groups := analytics.Groups{WorkspaceID: f.workspaceID.String()}
	if len(orgs) == 1 {
		distinctID, org, groups.OrganizationID = orgs[0], orgs[0], orgs[0]
	}
	var step any
	if f.failedStepType != "" {
		step = f.failedStepType
	}
	analytics.Capture(distinctID, "workflow_run_failed", analytics.Properties{
		"workflow_id":      f.workflowID.String(),
		"workspace_id":     f.workspaceID.String(),
		"organization_id":  org,
		"run_id":           f.runID.String(),
		"trigger_type":     f.triggerType,
		"failed_step_type": step,
		"error_kind":       f.errorKind,
		"attempt":          f.attempt,
	}, groups)
}

// classifyRunError is forma2's classifyRunError.
func classifyRunError(err error) string {
	var notExecutable *notExecutableError
	var stepFailed *stepFailedError
	switch {
	case errors.As(err, &notExecutable):
		return "not_executable"
	case errors.As(err, &stepFailed):
		return "step_failed"
	default:
		return "unknown"
	}
}

// failedStepType is the type of the last failed step in a run's data.
func failedStepType(data json.RawMessage) string {
	var doc struct {
		Steps []StepResult `json:"steps"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	for i := len(doc.Steps) - 1; i >= 0; i-- {
		if doc.Steps[i].Status == StepFailed {
			return doc.Steps[i].StepType
		}
	}
	return ""
}
