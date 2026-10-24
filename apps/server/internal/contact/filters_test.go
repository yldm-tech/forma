package contact

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Ported from forma2's segment-schema.test.ts and the ZSegmentFilter refinements in packages/types/segment.ts.

func attributeLeaf(key, operator string, value any) map[string]any {
	return map[string]any{"id": "leaf" + key, "root": map[string]any{"type": "attribute", "contactAttributeKey": key}, "value": value, "qualifier": map[string]any{"operator": operator}}
}

func interactionLeaf(operator, scope string, ids []string, amount int, unit string) map[string]any {
	if ids == nil {
		ids = []string{}
	}
	return map[string]any{"id": "interaction", "root": map[string]any{"type": "surveyInteraction"}, "value": map[string]any{"surveyScope": scope, "surveyIds": ids, "within": map[string]any{"amount": amount, "unit": unit}}, "qualifier": map[string]any{"operator": operator}}
}

// nodeIDs numbers tree entries across the package's parallel tests.
var nodeIDs atomic.Int64

// node is a tree entry with a fresh id, which must be a cuid2-shaped string.
func node(connector any, resource any) map[string]any {
	return map[string]any{"id": fmt.Sprintf("node%d", nodeIDs.Add(1)), "connector": connector, "resource": resource}
}

func tree(nodes ...map[string]any) []map[string]any {
	return append([]map[string]any{}, nodes...)
}

func parse(t *testing.T, doc any) error {
	t.Helper()
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	_, err = ParseFilters(schemas, raw)
	return err
}

func TestParseFilters(t *testing.T) {
	const surveyID = "01a16183-b01c-7c25-8029-3eb49de07277"
	cases := []struct {
		name    string
		doc     any
		message string
	}{
		{"an empty tree", tree(), ""},
		{"a string filter", tree(node(nil, attributeLeaf("plan", "equals", "pro"))), ""},
		{"a number filter", tree(node(nil, attributeLeaf("age", "greaterThan", 18))), ""},
		{"isSet ignores the value", tree(node(nil, attributeLeaf("plan", "isSet", ""))), ""},
		{"relative date", tree(node(nil, attributeLeaf("signup", "isOlderThan", map[string]any{"amount": 3, "unit": "months"}))), ""},
		{"between dates", tree(node(nil, attributeLeaf("signup", "isBetween", []string{"2024-01-01", "2024-12-31"}))), ""},
		{"and/or with a group", tree(node(nil, attributeLeaf("plan", "equals", "pro")), node("or", tree(node(nil, attributeLeaf("age", "lessThan", 3)), node("and", attributeLeaf("x", "contains", "y"))))), ""},
		{"a group first may carry a connector", tree(node("and", tree(node(nil, attributeLeaf("plan", "equals", "pro"))))), ""},
		{"any-survey interaction", tree(node(nil, interactionLeaf("haveSeen", "any", nil, 1, "months"))), ""},
		{"specific-survey interaction", tree(node(nil, interactionLeaf("haveCompleted", "specific", []string{surveyID}, 999, "days"))), ""},

		{"not a list", "everyone", invalidFiltersMessage},
		{"an unknown operator", tree(node(nil, attributeLeaf("plan", "matches", "pro"))), invalidFiltersMessage},
		{"a person filter with an arithmetic operator", tree(node(nil, map[string]any{"id": "p", "root": map[string]any{"type": "person", "personIdentifier": "userId"}, "value": 1, "qualifier": map[string]any{"operator": "lessThan"}})), invalidFiltersMessage},
		{"a first filter with a connector", tree(node("and", attributeLeaf("plan", "equals", "pro"))), invalidFiltersAppliedMessage},
		{"a nested first filter with a connector", tree(node(nil, tree(node("or", attributeLeaf("plan", "equals", "pro"))))), invalidFiltersAppliedMessage},
		{"arithmetic on a string", tree(node(nil, attributeLeaf("age", "greaterThan", "18"))), filterValueTypeMessage},
		{"contains on a number", tree(node(nil, attributeLeaf("plan", "contains", 1))), filterValueTypeMessage},
		{"relative date without a unit", tree(node(nil, attributeLeaf("signup", "isNewerThan", "3 days"))), filterValueTypeMessage},
		{"isBefore with a number", tree(node(nil, attributeLeaf("signup", "isBefore", 5))), filterValueTypeMessage},
		{"isBetween with a string", tree(node(nil, attributeLeaf("signup", "isBetween", "2024"))), filterValueTypeMessage},
		{"an empty string", tree(node(nil, attributeLeaf("plan", "equals", ""))), filterValueMessage},
		{"a zero relative amount", tree(node(nil, attributeLeaf("signup", "isOlderThan", map[string]any{"amount": 0, "unit": "days"}))), filterValueMessage},
		{"an empty isBetween bound", tree(node(nil, attributeLeaf("signup", "isBetween", []string{"2024-01-01", ""}))), filterValueMessage},
		{"an empty isSameDay date", tree(node(nil, attributeLeaf("signup", "isSameDay", ""))), filterValueMessage},
		{"the first failing filter wins", tree(node(nil, attributeLeaf("a", "equals", "")), node("and", attributeLeaf("b", "greaterThan", "x"))), filterValueMessage},
		{"specific scope without surveys", tree(node(nil, interactionLeaf("haveSeen", "specific", nil, 1, "days"))), selectSurveyMessage},
		{"an interaction window over 999", tree(node(nil, interactionLeaf("haveSeen", "any", nil, 1000, "days"))), invalidFiltersMessage},
		{"an interaction window in years", tree(node(nil, interactionLeaf("haveSeen", "any", nil, 1, "years"))), invalidFiltersMessage},
		{"an interaction survey that is no id", tree(node(nil, interactionLeaf("haveSeen", "specific", []string{"Survey 1"}, 1, "days"))), invalidFiltersMessage},
		{"an unknown field", tree(map[string]any{"id": "n", "connector": nil, "resource": attributeLeaf("plan", "equals", "pro"), "extra": true}), invalidFiltersMessage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := parse(t, c.doc)
			if c.message == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			e, ok := apperr.As(err)
			if !ok || e.Message != c.message || e.Code != codeInvalidFilters {
				t.Fatalf("got %v, want %q", err, c.message)
			}
		})
	}
}

func flatTree(n int) []map[string]any {
	nodes := make([]map[string]any, n)
	for i := range nodes {
		var connector any
		if i > 0 {
			connector = "and"
		}
		nodes[i] = node(connector, attributeLeaf("plan", "equals", "pro"))
	}
	return nodes
}

// nested wraps a filter in depth-1 groups, so the tree is depth levels deep.
func nested(depth int) []map[string]any {
	inner := tree(node(nil, attributeLeaf("plan", "equals", "pro")))
	for range depth - 1 {
		inner = tree(node(nil, inner))
	}
	return inner
}

func TestFilterTreeBounds(t *testing.T) {
	if err := parse(t, flatTree(MaxFiltersPerTree)); err != nil {
		t.Fatalf("a flat tree at the node cap: %v", err)
	}
	if err := parse(t, flatTree(MaxFiltersPerTree+1)); err == nil || !strings.Contains(err.Error(), "Too many filters: a segment supports at most 1000 filters in total") {
		t.Fatalf("over the node cap: %v", err)
	}
	// Small groups at every level still count toward the total.
	groups := make([]map[string]any, 0, 501)
	for i := range 501 {
		var connector any
		if i > 0 {
			connector = "or"
		}
		groups = append(groups, node(connector, tree(node(nil, attributeLeaf("plan", "equals", "pro")))))
	}
	if err := parse(t, groups); err == nil || !strings.Contains(err.Error(), "Too many filters") {
		t.Fatalf("an over-cap tree hidden behind nesting: %v", err)
	}
	if err := parse(t, nested(MaxFilterDepth)); err != nil {
		t.Fatalf("a tree at the depth limit: %v", err)
	}
	if err := parse(t, nested(MaxFilterDepth+1)); err == nil || !strings.Contains(err.Error(), "Segment filters are nested too deeply: at most 50 levels are supported") {
		t.Fatalf("one level too deep: %v", err)
	}
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("01a16183-b01c-7c25-8029-%012d", i)
		}
		return out
	}
	interactions := func(total int) []map[string]any {
		var nodes []map[string]any
		for i := 0; i < total; i += 100 {
			var connector any
			if i > 0 {
				connector = "and"
			}
			nodes = append(nodes, node(connector, interactionLeaf("haveSeen", "specific", ids(min(100, total-i)), 1, "days")))
		}
		return nodes
	}
	if err := parse(t, interactions(MaxSurveyInteractionIDsPerTree)); err != nil {
		t.Fatalf("survey ids at the tree-wide cap: %v", err)
	}
	if err := parse(t, interactions(MaxSurveyInteractionIDsPerTree+1)); err == nil || !strings.Contains(err.Error(), "Too many surveys referenced: survey-interaction filters may reference at most 1000 surveys in total") {
		t.Fatalf("one id over the cap: %v", err)
	}
}

func TestInteractionSurveyIDs(t *testing.T) {
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(tree(
		node(nil, interactionLeaf("haveSeen", "specific", []string{"01a16183-b01c-7c25-8029-000000000001"}, 1, "days")),
		node("and", tree(node(nil, interactionLeaf("haveSeen", "any", nil, 1, "days")), node("or", interactionLeaf("haveCompleted", "specific", []string{"01a16183-b01c-7c25-8029-000000000002"}, 1, "days")))),
	))
	parsed, err := ParseFilters(schemas, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(interactionSurveyIDs(parsed), ","); got != "01a16183-b01c-7c25-8029-000000000001,01a16183-b01c-7c25-8029-000000000002" {
		t.Fatalf("ids %s", got)
	}
	again, _ := json.Marshal(parsed)
	var a, b any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(again, &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("a parsed tree marshals back unchanged:\n%s\n%s", raw, again)
	}
}
