package workflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// RunSummary is forma2's ZWorkflowRunSummary, the run shape a workflow embeds as its lastRun.
type RunSummary struct {
	ID                uuid.UUID  `json:"id"`
	WorkflowID        uuid.UUID  `json:"workflowId"`
	WorkspaceID       uuid.UUID  `json:"workspaceId"`
	WorkflowVersionID *uuid.UUID `json:"workflowVersionId"`
	Status            string     `json:"status"`
	IsDryRun          bool       `json:"isDryRun"`
	TriggerType       string     `json:"triggerType"`
	SurveyID          *uuid.UUID `json:"surveyId"`
	ResponseID        *uuid.UUID `json:"responseId"`
	Error             *string    `json:"error"`
	Attempt           int        `json:"attempt"`
	CreatedAt         string     `json:"createdAt"`
	UpdatedAt         string     `json:"updatedAt"`
	StartedAt         *string    `json:"startedAt"`
	FinishedAt        *string    `json:"finishedAt"`
}

// RunListItem is a row of the runs list: the summary and the workflow's name.
type RunListItem struct {
	RunSummary
	WorkflowName string `json:"workflowName"`
}

// RunLogResource is one step of a run with every field explicit.
type RunLogResource struct {
	ID         uuid.UUID       `json:"id"`
	RunID      uuid.UUID       `json:"runId"`
	Sequence   int             `json:"sequence"`
	StepID     string          `json:"stepId"`
	StepType   string          `json:"stepType"`
	Status     string          `json:"status"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
	Error      *string         `json:"error"`
	StartedAt  *string         `json:"startedAt"`
	FinishedAt *string         `json:"finishedAt"`
}

// RunResource is a run's detail: the summary, its trigger payload, its data without the copy of the payload, and its steps in sequence order.
type RunResource struct {
	RunSummary
	TriggerPayload json.RawMessage  `json:"triggerPayload"`
	Data           json.RawMessage  `json:"data"`
	Logs           []RunLogResource `json:"logs"`
	IdempotencyKey *string          `json:"idempotencyKey"`
	NextAttemptAt  *string          `json:"nextAttemptAt"`
	LastErrorAt    *string          `json:"lastErrorAt"`
}

// Creator is the name of the user who created a workflow.
type Creator struct {
	Name string `json:"name"`
}

// ListItem is forma2's ZWorkflowListItem: a workflow without its definition, with the trigger's type and survey read from it.
type ListItem struct {
	ID          uuid.UUID   `json:"id"`
	WorkspaceID uuid.UUID   `json:"workspaceId"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Status      string      `json:"status"`
	TriggerType *string     `json:"triggerType"`
	SurveyID    *string     `json:"surveyId"`
	CreatedBy   *uuid.UUID  `json:"createdBy"`
	Creator     *Creator    `json:"creator"`
	CreatedAt   string      `json:"createdAt"`
	UpdatedAt   string      `json:"updatedAt"`
	LastRun     *RunSummary `json:"lastRun"`
	RunCount    int         `json:"runCount"`
}

// Resource is forma2's ZWorkflowResource: the list item and the definition.
type Resource struct {
	ListItem
	Definition json.RawMessage `json:"definition"`
}

// TestProblem is one reason a dry run found the workflow not ready.
type TestProblem struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// TestResult is forma2's ZWorkflowTestResult.
type TestResult struct {
	WorkflowID uuid.UUID     `json:"workflowId"`
	OK         bool          `json:"ok"`
	Problems   []TestProblem `json:"problems"`
}

func iso(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := jsvalue.ISOString(*t)
	return &s
}

func toRunSummary(r *Run) RunSummary {
	return RunSummary{
		ID: r.ID, WorkflowID: r.WorkflowID, WorkspaceID: r.WorkspaceID, WorkflowVersionID: r.WorkflowVersionID, Status: r.Status, IsDryRun: r.IsDryRun,
		TriggerType: r.TriggerType, SurveyID: r.SurveyID, ResponseID: r.ResponseID, Error: r.Error, Attempt: r.Attempt,
		CreatedAt: jsvalue.ISOString(r.CreatedAt), UpdatedAt: jsvalue.ISOString(r.UpdatedAt), StartedAt: iso(r.StartedAt), FinishedAt: iso(r.FinishedAt),
	}
}

func toRunListItem(r *Run) RunListItem {
	return RunListItem{RunSummary: toRunSummary(r), WorkflowName: r.WorkflowName}
}

func jsonObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

// runData is a run's data as the detail shows it: data.trigger dropped, since it repeats the trigger payload, and steps defaulted to [].
func runData(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(jsonObject(raw), &m); err != nil || m == nil {
		return json.RawMessage(`{"steps":[]}`)
	}
	delete(m, "trigger")
	if _, ok := m["steps"]; !ok {
		m["steps"] = json.RawMessage("[]")
	}
	return mustJSON(m)
}

func toRunResource(r *Run) RunResource {
	logs := make([]RunLogResource, len(r.Logs))
	for i, l := range r.Logs {
		logs[i] = RunLogResource{
			ID: l.ID, RunID: l.RunID, Sequence: l.Sequence, StepID: l.StepID, StepType: l.StepType, Status: l.Status,
			Input: jsonObject(l.Input), Output: jsonObject(l.Output), Error: l.Error, StartedAt: iso(l.StartedAt), FinishedAt: iso(l.FinishedAt),
		}
	}
	return RunResource{
		RunSummary: toRunSummary(r), TriggerPayload: jsonObject(r.TriggerPayload), Data: runData(r.Data), Logs: logs,
		IdempotencyKey: r.IdempotencyKey, NextAttemptAt: iso(r.NextAttemptAt), LastErrorAt: iso(r.LastErrorAt),
	}
}

// looseTrigger is the part of a stored definition the list item derives from, read without trusting the document.
type looseTrigger struct {
	Trigger *struct {
		TriggerType *string `json:"triggerType"`
		Config      *struct {
			SurveyID *string `json:"surveyId"`
		} `json:"config"`
	} `json:"trigger"`
}

func toListItem(w *Workflow) ListItem {
	item := ListItem{
		ID: w.ID, WorkspaceID: w.WorkspaceID, Name: w.Name, Description: w.Description, Status: w.Status, CreatedBy: w.CreatedBy,
		CreatedAt: jsvalue.ISOString(w.CreatedAt), UpdatedAt: jsvalue.ISOString(w.UpdatedAt), RunCount: w.RunCount,
	}
	if w.CreatorName != nil {
		item.Creator = &Creator{Name: *w.CreatorName}
	}
	var t looseTrigger
	if json.Unmarshal(w.Definition, &t) == nil && t.Trigger != nil {
		item.TriggerType = t.Trigger.TriggerType
		if t.Trigger.Config != nil {
			item.SurveyID = t.Trigger.Config.SurveyID
			if item.SurveyID != nil && surveydoc.IsCUID2(*item.SurveyID) {
				id := legacyid.ID(*item.SurveyID).String()
				item.SurveyID = &id
			}
		}
	}
	if w.LastRun != nil {
		s := toRunSummary(w.LastRun)
		item.LastRun = &s
	}
	return item
}

func toResource(w *Workflow) Resource {
	return Resource{ListItem: toListItem(w), Definition: canonicalTriggerSurvey(w.Definition)}
}

// canonicalTriggerSurvey is a stored definition with its trigger's survey named by the survey's id. A workflow migrated from forma2 keeps forma2's id there until it is next saved; the builder finds the trigger's survey by its id, so it gets the id migrated from it, which its save then stores. Anything else, a document that does not parse included, is returned as stored.
func canonicalTriggerSurvey(raw json.RawMessage) json.RawMessage {
	var doc, trigger, config map[string]json.RawMessage
	var surveyID string
	if json.Unmarshal(raw, &doc) != nil || json.Unmarshal(doc["trigger"], &trigger) != nil || json.Unmarshal(trigger["config"], &config) != nil ||
		json.Unmarshal(config["surveyId"], &surveyID) != nil || !surveydoc.IsCUID2(surveyID) {
		return raw
	}
	config["surveyId"], _ = json.Marshal(legacyid.ID(surveyID).String())
	trigger["config"], _ = json.Marshal(config)
	doc["trigger"], _ = json.Marshal(trigger)
	out, _ := json.Marshal(doc)
	return out
}

// sendEmailPII are the send_email fields that can carry personal data.
var sendEmailPII = []string{"to", "from", "replyTo", "subject", "body"}

// redactValue is forma2's audit marker: the first 12 hex digits of an HMAC-SHA256 under the instance's ENCRYPTION_KEY, so an unchanged value makes no diff and a changed one does, without the value being guessable from the log.
func redactValue(value, key string) string {
	var sum []byte
	if key != "" {
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(value))
		sum = mac.Sum(nil)
	} else {
		s := sha256.Sum256([]byte(value))
		sum = s[:]
	}
	return "[redacted:" + hex.EncodeToString(sum)[:12] + "]"
}

// redactDefinition is forma2's redactWorkflowDefinitionPII: every send_email action's addresses, subject and body replaced by markers, each replyTo entry separately.
func redactDefinition(definition any, key string) any {
	d, ok := definition.(map[string]any)
	if !ok {
		return definition
	}
	nodes, ok := d["nodes"].([]any)
	if !ok {
		return definition
	}
	out := make([]any, len(nodes))
	for i, n := range nodes {
		out[i] = n
		node, ok := n.(map[string]any)
		if !ok || node["type"] != NodeAction || node["actionType"] != ActionSendEmail {
			continue
		}
		config, ok := node["config"].(map[string]any)
		if !ok {
			continue
		}
		redacted := map[string]any{}
		for k, v := range config {
			redacted[k] = v
		}
		for _, field := range sendEmailPII {
			v, present := redacted[field]
			if !present {
				continue
			}
			if list, ok := v.([]any); ok {
				items := make([]any, len(list))
				for j, item := range list {
					items[j] = redactValue(jsvalue.String(item), key)
				}
				redacted[field] = items
				continue
			}
			redacted[field] = redactValue(jsvalue.String(v), key)
		}
		copied := map[string]any{}
		for k, v := range node {
			copied[k] = v
		}
		copied["config"] = redacted
		out[i] = copied
	}
	copied := map[string]any{}
	for k, v := range d {
		copied[k] = v
	}
	copied["nodes"] = out
	return copied
}

// auditSnapshot is the resource as an audit event records it, with the definition's personal data redacted.
func auditSnapshot(w *Workflow, key string) map[string]any {
	var snapshot map[string]any
	if err := json.Unmarshal(mustJSON(toResource(w)), &snapshot); err != nil {
		return nil
	}
	snapshot["definition"] = redactDefinition(snapshot["definition"], key)
	return snapshot
}
