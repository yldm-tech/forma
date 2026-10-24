// Package workflow is forma2's Workflows (packages/workflows and apps/web/modules/workflows): the definition document a workflow stores and the rules that make it persistable or executable, the /api/v3/workflows management API with its lifecycle (draft, enabled, disabled, archived) and immutable published versions, and the runner that turns a finished response into queued runs, executes their send_email steps at most once each, and reconciles runs that were lost or abandoned.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// SchemaVersion is WORKFLOW_SCHEMA_VERSION, the newest definition schema this server understands.
const SchemaVersion = 1

// Node and trigger vocabulary, forma2's ZWorkflowNodeType, WORKFLOW_TRIGGERS and WORKFLOW_ACTIONS.
const (
	NodeAction  = "action"
	NodeIfElse  = "if_else"
	NodeTrigger = "trigger"

	TriggerResponseCompleted = "response.completed"
	ActionSendEmail          = "send_email"

	branchThen = "then"
	branchElse = "else"
)

// Bounds of a send_email action, forma2's MAX_SUBJECT_LENGTH (RFC 5322's line limit) and MAX_BODY_LENGTH (where Gmail clips a message).
const (
	maxSubjectLength = 998
	maxBodyLength    = 100_000
)

// Position is a node's place on the builder canvas.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// UI is builder-only metadata that never affects execution: a position, whether the node is collapsed, and any other key the builder stored, which zod's catchall kept.
type UI map[string]json.RawMessage

// TriggerConfig is the response.completed trigger's configuration. An empty EndingCardIDs means every ending, including endings added later.
type TriggerConfig struct {
	SurveyID      string   `json:"surveyId"`
	EndingCardIDs []string `json:"endingCardIds"`
}

// TriggerNode is the workflow's trigger, the only node of type "trigger".
type TriggerNode struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Label       *string       `json:"label,omitempty"`
	UI          UI            `json:"ui,omitempty"`
	TriggerType string        `json:"triggerType"`
	Config      TriggerConfig `json:"config"`
}

// SendEmailConfig is the send_email action's configuration, field for field the follow-up email's. To is an address or the id of the survey element or hidden field whose answer holds one; From is vestigial and only names the Message-ID's domain.
type SendEmailConfig struct {
	To                  string   `json:"to"`
	From                string   `json:"from"`
	ReplyTo             []string `json:"replyTo"`
	Subject             string   `json:"subject"`
	Body                string   `json:"body"`
	AttachResponseData  bool     `json:"attachResponseData"`
	IncludeVariables    *bool    `json:"includeVariables,omitempty"`
	IncludeHiddenFields *bool    `json:"includeHiddenFields,omitempty"`
}

// DataRef points at a value of the run's context, with an optional fallback.
type DataRef struct {
	Path     string  `json:"path"`
	Fallback *string `json:"fallback,omitempty"`
}

// Condition compares a referenced value with a right-hand value; presence operators have none. Right holds the value as JSON, so a stored null stays distinct from an absent value.
type Condition struct {
	ID       string          `json:"id"`
	Left     DataRef         `json:"left"`
	Operator string          `json:"operator"`
	Right    json.RawMessage `json:"right,omitempty"`
}

// ConditionItem is one entry of a condition group: a condition or a nested group.
type ConditionItem struct {
	Condition *Condition
	Group     *ConditionGroup
}

func (c ConditionItem) MarshalJSON() ([]byte, error) {
	if c.Group != nil {
		return json.Marshal(c.Group)
	}
	return json.Marshal(c.Condition)
}

// ConditionGroup joins its conditions with "and" or "or".
type ConditionGroup struct {
	ID         string          `json:"id"`
	Connector  string          `json:"connector"`
	Conditions []ConditionItem `json:"conditions"`
}

// Node is a node after the trigger: a send_email action (Email set) or an if_else branch (Condition set), which can be stored but not executed.
type Node struct {
	ID         string
	Type       string
	ActionType string
	Label      *string
	UI         UI
	Email      *SendEmailConfig
	Condition  *ConditionGroup
}

type nodeJSON struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Label      *string `json:"label,omitempty"`
	UI         UI      `json:"ui,omitempty"`
	ActionType string  `json:"actionType,omitempty"`
	Config     any     `json:"config"`
}

func (n Node) MarshalJSON() ([]byte, error) {
	out := nodeJSON{ID: n.ID, Type: n.Type, Label: n.Label, UI: n.UI, ActionType: n.ActionType}
	if n.Email != nil {
		out.Config = n.Email
	} else {
		out.Config = map[string]any{"condition": n.Condition}
	}
	return json.Marshal(out)
}

// Edge connects two nodes; an if_else's edges carry the branch as their source handle.
type Edge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	SourceHandle *string `json:"sourceHandle,omitempty"`
	TargetHandle *string `json:"targetHandle,omitempty"`
}

// Definition is the workflow graph as stored: forma2's ZWorkflowDefinition output, with defaults applied and keys zod would strip left out. A draft may have no trigger yet; EntryNodeID then is nil.
type Definition struct {
	SchemaVersion int          `json:"schemaVersion"`
	Trigger       *TriggerNode `json:"trigger"`
	Nodes         []Node       `json:"nodes"`
	Edges         []Edge       `json:"edges"`
	EntryNodeID   *string      `json:"entryNodeId"`
}

// Issue is one rule a document breaks, as zod reported it: the path to the offending value and a message.
type Issue struct {
	Path    []any
	Message string
}

// Name is the issue's path joined with dots, forma2's invalid_params name.
func (i Issue) Name() string {
	parts := make([]string, len(i.Path))
	for k, p := range i.Path {
		parts[k] = fmt.Sprint(p)
	}
	return strings.Join(parts, ".")
}

// ParseDefinition is ZWorkflowDefinition.safeParse: the document every draft must satisfy to be stored. It returns the normalized definition, or the issues.
func ParseDefinition(raw json.RawMessage) (*Definition, []Issue) {
	return parseDefinition(raw, false)
}

// ParseExecutable is ZWorkflowExecutableDefinition.safeParse: the subset the runner can execute, which enable publishes and every run re-checks. It needs a trigger with exactly one outgoing edge, a graph that is acyclic with every node reachable, no if_else nodes, and a to, subject and body in every send_email action.
func ParseExecutable(raw json.RawMessage) (*Definition, []Issue) {
	return parseDefinition(raw, true)
}

func parseDefinition(raw json.RawMessage, executable bool) (*Definition, []Issue) {
	value, err := decodeJSON(raw)
	if err != nil {
		return nil, []Issue{{Message: "Invalid input: expected object, received undefined"}}
	}
	d, issues, _ := parseDefinitionValue(value, nil, executable)
	return d, issues
}

// parseDefinitionValue parses a decoded definition found at path at, reporting whether a type-level issue stopped the refinements.
func parseDefinitionValue(value any, at []any, executable bool) (*Definition, []Issue, bool) {
	p := &parser{}
	d := p.definition(value, at, executable)
	if len(p.issues) > 0 && p.aborted {
		return nil, p.issues, true
	}
	// zod runs a refinement only when the parse produced no type-level issue; failed checks (lengths, formats) do not stop it.
	p.graph(d, at)
	if executable {
		p.executableGraph(d)
		for i, n := range d.Nodes {
			if n.Type == NodeIfElse {
				p.add(path(nil, "nodes", i, "type"), "if_else nodes are not executable in this version of workflows")
			}
			if n.Type == NodeAction {
				for _, field := range BlankContentFields(*n.Email) {
					p.add(path(nil, "nodes", i, "config", field), fmt.Sprintf("send_email node %s is missing %s", n.ID, field))
				}
			}
		}
	}
	if len(p.issues) > 0 {
		return nil, p.issues, false
	}
	return d, nil, false
}

// decodeJSON reads one JSON value with numbers kept as written.
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// htmlTag is forma2's HTML_TAG_PATTERN: an opening or closing tag that starts with a letter and contains no angle bracket, so visible text such as "<3" is not taken for markup.
var htmlTag = regexp.MustCompile(`</?[a-zA-Z][^<>]*>`)

// htmlWhitespaceEntity is the whitespace the rich-text editor writes as entities.
var htmlWhitespaceEntity = regexp.MustCompile(`(?i)&nbsp;|&#160;|&#xa0;`)

// IsBlankRichText is forma2's isBlankWorkflowRichText: whether an email body holds nothing a recipient would see once its tags and whitespace entities are gone. The editor stores an emptied body as `<p class="…"><br></p>`, never as "".
func IsBlankRichText(value string) bool {
	return jsvalue.Trim(htmlWhitespaceEntity.ReplaceAllString(htmlTag.ReplaceAllString(value, ""), " ")) == ""
}

// BlankContentFields is forma2's getBlankSendEmailContentFields: which of to, subject and body are still blank, in form order.
func BlankContentFields(c SendEmailConfig) []string {
	var out []string
	if jsvalue.Trim(c.To) == "" {
		out = append(out, "to")
	}
	if jsvalue.Trim(c.Subject) == "" {
		out = append(out, "subject")
	}
	if IsBlankRichText(c.Body) {
		out = append(out, "body")
	}
	return out
}

// IsLiteralRecipient is forma2's isLiteralEmailRecipient: a `to` that is itself an address, as opposed to the id of the element whose answer holds the respondent's. Literal recipients must be able to access the workspace.
func IsLiteralRecipient(to string) bool {
	return userinput.ValidEmailFormat(to)
}

// parser walks a decoded document the way the zod schemas did, collecting issues with their paths. aborted records a type-level issue, after which zod skipped refinements.
type parser struct {
	issues  []Issue
	aborted bool
}

func path(base []any, parts ...any) []any {
	out := make([]any, 0, len(base)+len(parts))
	return append(append(out, base...), parts...)
}

func (p *parser) add(at []any, message string) {
	p.issues = append(p.issues, Issue{Path: at, Message: message})
}

// fail records a type-level issue.
func (p *parser) fail(at []any, message string) {
	p.aborted = true
	p.add(at, message)
}

// received names a value's type in zod's words; missing is a key that is absent.
func received(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func (p *parser) expected(at []any, kind string, v any, present bool) {
	p.fail(at, fmt.Sprintf("Invalid input: expected %s, received %s", kind, received(v, present)))
}

func (p *parser) object(v any, present bool, at []any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		p.expected(at, "object", v, present)
	}
	return m, ok
}

// jsLength is a string's length as JavaScript counts it, in UTF-16 code units.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// lengthChecks applies z.string().min().max(); a negative bound is no bound.
func (p *parser) lengthChecks(s string, at []any, minimum, maximum int) {
	n := jsLength(s)
	if minimum >= 0 && n < minimum {
		p.add(at, fmt.Sprintf("Too small: expected string to have >=%d characters", minimum))
	}
	if maximum >= 0 && n > maximum {
		p.add(at, fmt.Sprintf("Too big: expected string to have <=%d characters", maximum))
	}
}

// str reads a required string member.
func (p *parser) str(m map[string]any, key string, at []any, minimum, maximum int) string {
	v, present := m[key]
	s, ok := v.(string)
	if !ok {
		p.expected(path(at, key), "string", v, present)
		return ""
	}
	p.lengthChecks(s, path(at, key), minimum, maximum)
	return s
}

// optStr reads an optional string member; nil when absent.
func (p *parser) optStr(m map[string]any, key string, at []any, minimum, maximum int) *string {
	v, present := m[key]
	if !present {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		p.expected(path(at, key), "string", v, present)
		return nil
	}
	p.lengthChecks(s, path(at, key), minimum, maximum)
	return &s
}

func (p *parser) literal(m map[string]any, key string, at []any, want string) string {
	v := m[key]
	if s, ok := v.(string); ok && s == want {
		return s
	}
	p.fail(path(at, key), fmt.Sprintf("Invalid input: expected %q", want))
	return want
}

func (p *parser) boolean(m map[string]any, key string, at []any) bool {
	v, present := m[key]
	b, ok := v.(bool)
	if !ok {
		p.expected(path(at, key), "boolean", v, present)
	}
	return b
}

func (p *parser) optBool(m map[string]any, key string, at []any) *bool {
	v, present := m[key]
	if !present {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		p.expected(path(at, key), "boolean", v, present)
		return nil
	}
	return &b
}

func (p *parser) number(v any, present bool, at []any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		p.expected(at, "number", v, present)
		return 0, false
	}
	f, err := n.Float64()
	if err != nil {
		p.expected(at, "number", v, present)
		return 0, false
	}
	return f, true
}

func (p *parser) email(s string, at []any) {
	if !userinput.ValidEmailFormat(s) {
		p.add(at, "Invalid email address")
	}
}

// uuidString checks the survey id format; the rewrite's ids are UUIDs where forma2's were cuid2.
func (p *parser) uuidString(s string, at []any) {
	if _, err := uuid.Parse(s); err != nil || len(s) != 36 {
		p.add(at, "Invalid UUID")
	}
}

// triggerSurveyID checks the trigger's survey id and returns it as the survey's id: a UUID, the rewrite's id format, as it is, or forma2's cuid2 id, which a workflow migrated from forma2 still names, as the id the survey was migrated to (legacyid), so a migrated workflow runs, tests and enables, and is stored with the UUID the next time it is saved.
func (p *parser) triggerSurveyID(s string, at []any) string {
	if surveydoc.IsCUID2(s) {
		return legacyid.ID(s).String()
	}
	p.uuidString(s, at)
	return s
}

func (p *parser) definition(v any, at []any, executable bool) *Definition {
	d := &Definition{SchemaVersion: SchemaVersion, Nodes: []Node{}, Edges: []Edge{}}
	m, ok := p.object(v, true, at)
	if !ok {
		return d
	}
	if sv, present := m["schemaVersion"]; present {
		if f, ok := p.number(sv, present, path(at, "schemaVersion")); ok {
			switch {
			case f != float64(int64(f)):
				p.add(path(at, "schemaVersion"), "Invalid input: expected int, received number")
			case f <= 0:
				p.add(path(at, "schemaVersion"), "Too small: expected number to be >0")
			case f > SchemaVersion:
				p.add(path(at, "schemaVersion"), fmt.Sprintf("Too big: expected number to be <=%d", SchemaVersion))
			default:
				d.SchemaVersion = int(f)
			}
		}
	}
	trigger, present := m["trigger"]
	switch {
	case trigger == nil && present && !executable:
	case trigger == nil:
		p.expected(path(at, "trigger"), "object", trigger, present)
	default:
		d.Trigger = p.trigger(trigger, path(at, "trigger"))
	}
	if raw, present := m["nodes"]; present {
		if list, ok := raw.([]any); ok {
			for i, item := range list {
				if n := p.node(item, path(at, "nodes", i)); n != nil {
					d.Nodes = append(d.Nodes, *n)
				}
			}
		} else {
			p.expected(path(at, "nodes"), "array", raw, present)
		}
	}
	if raw, present := m["edges"]; present {
		if list, ok := raw.([]any); ok {
			for i, item := range list {
				d.Edges = append(d.Edges, p.edge(item, path(at, "edges", i)))
			}
		} else {
			p.expected(path(at, "edges"), "array", raw, present)
		}
	}
	entry, present := m["entryNodeId"]
	switch s, isString := entry.(string); {
	case isString:
		p.lengthChecks(s, path(at, "entryNodeId"), 1, -1)
		d.EntryNodeID = &s
	case entry == nil && present && !executable:
	default:
		p.expected(path(at, "entryNodeId"), "string", entry, present)
	}
	return d
}

func (p *parser) base(m map[string]any, at []any) (id string, label *string, ui UI) {
	id = p.str(m, "id", at, 1, -1)
	label = p.optStr(m, "label", at, 1, 120)
	if raw, present := m["ui"]; present {
		ui = p.ui(raw, path(at, "ui"))
	}
	return id, label, ui
}

func (p *parser) ui(v any, at []any) UI {
	m, ok := p.object(v, true, at)
	if !ok {
		return nil
	}
	out := UI{}
	for k, val := range m {
		switch k {
		case "position":
			pm, ok := p.object(val, true, path(at, k))
			if !ok {
				continue
			}
			x, xok := p.number(pm["x"], hasKey(pm, "x"), path(at, k, "x"))
			y, yok := p.number(pm["y"], hasKey(pm, "y"), path(at, k, "y"))
			if xok && yok {
				out[k] = mustJSON(Position{X: x, Y: y})
			}
		case "collapsed":
			if b, ok := val.(bool); ok {
				out[k] = mustJSON(b)
			} else {
				p.expected(path(at, k), "boolean", val, true)
			}
		default:
			out[k] = mustJSON(val)
		}
	}
	return out
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("encode decoded JSON: %v", err))
	}
	return raw
}

func (p *parser) trigger(v any, at []any) *TriggerNode {
	m, ok := p.object(v, true, at)
	if !ok {
		return nil
	}
	if tt, _ := m["triggerType"].(string); tt != TriggerResponseCompleted {
		p.fail(path(at, "triggerType"), "Invalid input")
		return nil
	}
	t := &TriggerNode{TriggerType: TriggerResponseCompleted, Config: TriggerConfig{EndingCardIDs: []string{}}}
	t.ID, t.Label, t.UI = p.base(m, at)
	t.Type = p.literal(m, "type", at, NodeTrigger)
	cfg, present := m["config"]
	c, ok := p.object(cfg, present, path(at, "config"))
	if !ok {
		return t
	}
	cat := path(at, "config")
	t.Config.SurveyID = p.str(c, "surveyId", cat, -1, -1)
	if _, isString := c["surveyId"].(string); isString {
		t.Config.SurveyID = p.triggerSurveyID(t.Config.SurveyID, path(cat, "surveyId"))
	}
	if raw, present := c["endingCardIds"]; present {
		list, ok := raw.([]any)
		if !ok {
			p.expected(path(cat, "endingCardIds"), "array", raw, present)
			return t
		}
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				p.expected(path(cat, "endingCardIds", i), "string", item, true)
				continue
			}
			if !surveydoc.IsCUID2(s) {
				p.add(path(cat, "endingCardIds", i), "Invalid cuid2")
			}
			t.Config.EndingCardIDs = append(t.Config.EndingCardIDs, s)
		}
	}
	return t
}

func (p *parser) node(v any, at []any) *Node {
	m, ok := p.object(v, true, at)
	if !ok {
		return nil
	}
	switch m["type"] {
	case NodeAction:
		if m["actionType"] != ActionSendEmail {
			p.fail(path(at, "actionType"), "Invalid input")
			return nil
		}
		n := &Node{Type: NodeAction, ActionType: ActionSendEmail}
		n.ID, n.Label, n.UI = p.base(m, at)
		cfg, present := m["config"]
		if c, ok := p.object(cfg, present, path(at, "config")); ok {
			n.Email = p.sendEmail(c, path(at, "config"))
		} else {
			n.Email = &SendEmailConfig{ReplyTo: []string{}}
		}
		return n
	case NodeIfElse:
		n := &Node{Type: NodeIfElse}
		n.ID, n.Label, n.UI = p.base(m, at)
		cfg, present := m["config"]
		if c, ok := p.object(cfg, present, path(at, "config")); ok {
			cond, present := c["condition"]
			n.Condition = p.group(cond, present, path(at, "config", "condition"))
		}
		return n
	}
	p.fail(at, "Invalid input")
	return nil
}

func (p *parser) sendEmail(c map[string]any, at []any) *SendEmailConfig {
	e := &SendEmailConfig{ReplyTo: []string{}}
	e.To = p.str(c, "to", at, -1, -1)
	e.From = p.str(c, "from", at, -1, -1)
	if _, ok := c["from"].(string); ok {
		p.email(e.From, path(at, "from"))
	}
	raw, present := c["replyTo"]
	if list, ok := raw.([]any); ok {
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				p.expected(path(at, "replyTo", i), "string", item, true)
				continue
			}
			p.email(s, path(at, "replyTo", i))
			e.ReplyTo = append(e.ReplyTo, s)
		}
	} else {
		p.expected(path(at, "replyTo"), "array", raw, present)
	}
	e.Subject = p.str(c, "subject", at, -1, maxSubjectLength)
	e.Body = p.str(c, "body", at, -1, maxBodyLength)
	e.AttachResponseData = p.boolean(c, "attachResponseData", at)
	e.IncludeVariables = p.optBool(c, "includeVariables", at)
	e.IncludeHiddenFields = p.optBool(c, "includeHiddenFields", at)
	return e
}

var conditionOperators = []string{"equals", "notEquals", "lessThan", "lessEqual", "greaterThan", "greaterEqual", "contains", "notContains", "exists", "notExists"}

func oneOf(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = strconv.Quote(v)
	}
	return "Invalid option: expected one of " + strings.Join(quoted, "|")
}

func (p *parser) group(v any, present bool, at []any) *ConditionGroup {
	m, ok := p.object(v, present, at)
	if !ok {
		return nil
	}
	g := &ConditionGroup{Conditions: []ConditionItem{}}
	g.ID = p.str(m, "id", at, 1, -1)
	switch c := m["connector"]; c {
	case "and", "or":
		g.Connector = c.(string)
	default:
		p.fail(path(at, "connector"), oneOf([]string{"and", "or"}))
	}
	raw, present := m["conditions"]
	list, ok := raw.([]any)
	if !ok {
		p.expected(path(at, "conditions"), "array", raw, present)
		return g
	}
	if len(list) == 0 {
		p.add(path(at, "conditions"), "Too small: expected array to have >=1 items")
	}
	for i, item := range list {
		im, _ := item.(map[string]any)
		if im != nil && (hasKey(im, "conditions") || hasKey(im, "connector")) {
			if sub := p.group(item, true, path(at, "conditions", i)); sub != nil {
				g.Conditions = append(g.Conditions, ConditionItem{Group: sub})
			}
			continue
		}
		if c := p.condition(item, path(at, "conditions", i)); c != nil {
			g.Conditions = append(g.Conditions, ConditionItem{Condition: c})
		}
	}
	return g
}

func (p *parser) dataRef(v any, present bool, at []any) DataRef {
	var r DataRef
	m, ok := p.object(v, present, at)
	if !ok {
		return r
	}
	r.Path = p.str(m, "path", at, 1, -1)
	r.Fallback = p.optStr(m, "fallback", at, -1, -1)
	return r
}

func (p *parser) condition(v any, at []any) *Condition {
	m, ok := p.object(v, true, at)
	if !ok {
		return nil
	}
	c := &Condition{}
	c.ID = p.str(m, "id", at, 1, -1)
	left, present := m["left"]
	c.Left = p.dataRef(left, present, path(at, "left"))
	op, _ := m["operator"].(string)
	known := false
	for _, o := range conditionOperators {
		known = known || o == op
	}
	if !known {
		p.fail(path(at, "operator"), oneOf(conditionOperators))
	}
	right, hasRight := m["right"]
	if hasRight {
		switch r := right.(type) {
		case nil, string, bool:
			c.Right = mustJSON(r)
		case json.Number:
			c.Right = json.RawMessage(r.String())
		case map[string]any:
			c.Right = mustJSON(p.dataRef(r, true, path(at, "right")))
		default:
			p.fail(path(at, "right"), "Invalid input")
		}
	}
	if !known {
		return c
	}
	presence := op == "exists" || op == "notExists"
	if presence && hasRight {
		p.add(path(at, "right"), fmt.Sprintf("Operator %s must not have a right-hand value", op))
	}
	if !presence && !hasRight {
		p.add(path(at, "right"), fmt.Sprintf("Operator %s requires a right-hand value", op))
	}
	c.Operator = op
	return c
}

func (p *parser) edge(v any, at []any) Edge {
	var e Edge
	m, ok := p.object(v, true, at)
	if !ok {
		return e
	}
	e.ID = p.str(m, "id", at, 1, -1)
	e.Source = p.str(m, "source", at, 1, -1)
	e.Target = p.str(m, "target", at, 1, -1)
	e.SourceHandle = p.optStr(m, "sourceHandle", at, 1, -1)
	e.TargetHandle = p.optStr(m, "targetHandle", at, 1, -1)
	return e
}

func isBranch(handle *string) bool {
	return handle != nil && (*handle == branchThen || *handle == branchElse)
}

// graph is forma2's validateWorkflowGraph, the refinement every stored definition passes: unique node ids, an entry point that is the trigger, edges between existing nodes, then/else handles on exactly the if_else nodes' edges, at most one edge out of the trigger, and one then and one else edge per if_else.
func (p *parser) graph(d *Definition, at []any) {
	types := map[string]string{}
	if d.Trigger != nil {
		types[d.Trigger.ID] = NodeTrigger
	}
	for _, n := range d.Nodes {
		if _, dup := types[n.ID]; dup {
			p.add(path(at, "nodes"), "Duplicate workflow node id: "+n.ID)
			continue
		}
		types[n.ID] = n.Type
	}
	switch {
	case d.Trigger != nil && (d.EntryNodeID == nil || *d.EntryNodeID != d.Trigger.ID):
		p.add(path(at, "entryNodeId"), "entryNodeId must reference the workflow trigger")
	case d.Trigger == nil && d.EntryNodeID != nil:
		p.add(path(at, "entryNodeId"), "entryNodeId must be null while the workflow has no trigger")
	}
	for i, e := range d.Edges {
		if _, ok := types[e.Source]; !ok {
			p.add(path(at, "edges", i, "source"), "Edge source does not reference a workflow node: "+e.Source)
		}
		if _, ok := types[e.Target]; !ok {
			p.add(path(at, "edges", i, "target"), "Edge target does not reference a workflow node: "+e.Target)
		}
		sourceType, known := types[e.Source]
		if sourceType == NodeIfElse && !isBranch(e.SourceHandle) {
			p.add(path(at, "edges", i, "sourceHandle"), "if_else edges must use then or else sourceHandle")
		}
		if known && sourceType != NodeIfElse && isBranch(e.SourceHandle) {
			p.add(path(at, "edges", i, "sourceHandle"), "Only if_else nodes can use then or else sourceHandle")
		}
	}
	if d.Trigger != nil && countEdges(d.Edges, d.Trigger.ID, "") > 1 {
		p.add(path(at, "edges"), "A workflow can have at most one outgoing trigger edge")
	}
	for _, n := range d.Nodes {
		if n.Type != NodeIfElse {
			continue
		}
		if countEdges(d.Edges, n.ID, branchThen) != 1 {
			p.add(path(at, "edges"), fmt.Sprintf("if_else node %s must have exactly one then edge", n.ID))
		}
		if countEdges(d.Edges, n.ID, branchElse) != 1 {
			p.add(path(at, "edges"), fmt.Sprintf("if_else node %s must have exactly one else edge", n.ID))
		}
	}
}

// countEdges counts the edges out of source, only those with the handle when one is given.
func countEdges(edges []Edge, source, handle string) int {
	n := 0
	for _, e := range edges {
		if e.Source == source && (handle == "" || (e.SourceHandle != nil && *e.SourceHandle == handle)) {
			n++
		}
	}
	return n
}

// executableGraph is forma2's validateExecutableGraph: exactly one edge out of the trigger, every node reachable from it, and no cycle.
func (p *parser) executableGraph(d *Definition) {
	if countEdges(d.Edges, d.Trigger.ID, "") != 1 {
		p.add([]any{"edges"}, "An executable workflow must have exactly one outgoing trigger edge")
	}
	ids := []string{d.Trigger.ID}
	known := map[string]bool{d.Trigger.ID: true}
	for _, n := range d.Nodes {
		if !known[n.ID] {
			ids = append(ids, n.ID)
		}
		known[n.ID] = true
	}
	out := map[string][]string{}
	for _, e := range d.Edges {
		if known[e.Source] && known[e.Target] {
			out[e.Source] = append(out[e.Source], e.Target)
		}
	}
	reachable := map[string]bool{}
	stack := []string{d.Trigger.ID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reachable[id] {
			continue
		}
		reachable[id] = true
		stack = append(stack, out[id]...)
	}
	var unreachable []string
	for _, n := range d.Nodes {
		if !reachable[n.ID] {
			unreachable = append(unreachable, n.ID)
		}
	}
	if len(unreachable) > 0 {
		p.add([]any{"nodes"}, "Executable workflow contains unreachable node ids: "+strings.Join(unreachable, ", "))
	}
	visited, active := map[string]bool{}, map[string]bool{}
	var cyclic func(id string) bool
	cyclic = func(id string) bool {
		if active[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visited[id], active[id] = true, true
		for _, next := range out[id] {
			if cyclic(next) {
				return true
			}
		}
		delete(active, id)
		return false
	}
	for _, id := range ids {
		if cyclic(id) {
			p.add([]any{"edges"}, "Executable workflow graph must be acyclic")
			break
		}
	}
}

// Step is one executable step in run order. Only send_email actions are executable.
type Step struct {
	ID     string
	Type   string
	Config SendEmailConfig
}

// PlanError is a graph the runner cannot walk, forma2's WorkflowPlanError.
type PlanError struct {
	Message string
}

func (e *PlanError) Error() string { return e.Message }

// Plan is forma2's planExecutableSteps: the chain of send_email steps from the trigger along the single edge out of each node.
func Plan(d *Definition) ([]Step, error) {
	nodes := map[string]Node{}
	for _, n := range d.Nodes {
		nodes[n.ID] = n
	}
	targets := map[string][]string{}
	for _, e := range d.Edges {
		targets[e.Source] = append(targets[e.Source], e.Target)
	}
	steps := []Step{}
	visited := map[string]bool{}
	id := d.Trigger.ID
	for {
		if visited[id] {
			return nil, &PlanError{Message: "Workflow graph must be acyclic"}
		}
		visited[id] = true
		next := targets[id]
		if len(next) == 0 {
			return steps, nil
		}
		if len(next) > 1 {
			return nil, &PlanError{Message: fmt.Sprintf("Workflow node %s has more than one outgoing edge", id)}
		}
		n, ok := nodes[next[0]]
		if !ok {
			return nil, &PlanError{Message: "Workflow edge targets a missing node: " + next[0]}
		}
		if n.Type != NodeAction || n.ActionType != ActionSendEmail || n.Email == nil {
			return nil, &PlanError{Message: fmt.Sprintf("Workflow node %s is not an executable send_email action", n.ID)}
		}
		steps = append(steps, Step{ID: n.ID, Type: ActionSendEmail, Config: *n.Email})
		id = n.ID
	}
}

// LiteralRecipients are the distinct literal addresses the definition's send_email actions write to, in order.
func LiteralRecipients(d *Definition) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range d.Nodes {
		if n.Type == NodeAction && n.Email != nil && IsLiteralRecipient(n.Email.To) && !seen[n.Email.To] {
			seen[n.Email.To] = true
			out = append(out, n.Email.To)
		}
	}
	return out
}
