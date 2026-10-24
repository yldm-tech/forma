package contact

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
)

// Ported from forma2's prisma-query.test.ts, survey-interaction.test.ts and survey-interaction.parity.test.ts, evaluated by PostgreSQL instead of compared as Prisma objects.

// segmentOf stores a segment with the given tree.
func (e *env) segmentOf(ws uuid.UUID, filters any) uuid.UUID {
	e.t.Helper()
	raw, err := json.Marshal(filters)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.segmentRow(ws, "Segment "+uuid.NewString(), false, string(raw))
}

// members lists which of contacts are in the segment.
func (e *env) members(seg uuid.UUID, device string, contacts ...uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var in []uuid.UUID
	for _, c := range contacts {
		ok, err := e.evaluator.IsMember(e.t.Context(), seg, c, device)
		if err != nil {
			e.t.Fatalf("evaluate: %v", err)
		}
		if ok {
			in = append(in, c)
		}
	}
	return in
}

func (e *env) expectMembers(name string, filters any, device string, all []uuid.UUID, want ...uuid.UUID) {
	e.t.Helper()
	ws := e.workspaceOf(all[0])
	got := e.members(e.segmentOf(ws, filters), device, all...)
	if !slices.Equal(got, want) {
		e.t.Errorf("%s: members %v, want %v", name, got, want)
	}
}

func (e *env) workspaceOf(contact uuid.UUID) uuid.UUID {
	return e.insert(`SELECT workspace_id FROM contacts WHERE id = ?`, contact)
}

func leaf(f map[string]any) []map[string]any { return tree(node(nil, f)) }

func TestEvaluateAttributeFilters(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	a := e.contact(ws, map[string]string{"plan": "Pro", "email": "a@example.com"})
	b := e.contact(ws, map[string]string{"plan": "free", "email": "b@example.com"})
	c := e.contact(ws, map[string]string{"email": "c@example.com"})
	d := e.contact(ws, map[string]string{"plan": "", "email": "d@example.com"})
	all := []uuid.UUID{a, b, c, d}

	e.expectMembers("equals ignores case", leaf(attributeLeaf("plan", "equals", "pro")), "", all, a)
	e.expectMembers("notEquals needs the value", leaf(attributeLeaf("plan", "notEquals", "pro")), "", all, b, d)
	e.expectMembers("contains", leaf(attributeLeaf("plan", "contains", "RO")), "", all, a)
	e.expectMembers("doesNotContain needs the value", leaf(attributeLeaf("plan", "doesNotContain", "ro")), "", all, b, d)
	e.expectMembers("startsWith", leaf(attributeLeaf("plan", "startsWith", "p")), "", all, a)
	e.expectMembers("endsWith", leaf(attributeLeaf("plan", "endsWith", "EE")), "", all, b)
	e.expectMembers("isSet counts an empty value", leaf(attributeLeaf("plan", "isSet", "")), "", all, a, b, d)
	e.expectMembers("isNotSet", leaf(attributeLeaf("plan", "isNotSet", "")), "", all, c)
	e.expectMembers("LIKE wildcards are literal", leaf(attributeLeaf("plan", "contains", "_")), "", all)
	e.expectMembers("an unknown key", leaf(attributeLeaf("nope", "equals", "x")), "", all)

	e.setText(ws, a, "userId", "u1")
	e.expectMembers("person userId", leaf(map[string]any{"id": "p", "root": map[string]any{"type": "person", "personIdentifier": "userId"}, "value": "U1", "qualifier": map[string]any{"operator": "equals"}}), "", all, a)
	e.expectMembers("another person identifier constrains nothing", leaf(map[string]any{"id": "p", "root": map[string]any{"type": "person", "personIdentifier": "email"}, "value": "x", "qualifier": map[string]any{"operator": "equals"}}), "", all, a, b, c, d)
}

func TestEvaluateNumberFilters(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	age := e.key(ws, "age", TypeNumber)
	thirty := e.contact(ws, nil)
	e.setNumber(ws, thirty, "age", 30)
	// Text a number key kept without a parsed number, as rows carried over from forma2 can be.
	legacy := e.contact(ws, nil)
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, '25')`, legacy, age)
	garbage := e.contact(ws, nil)
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, 'abc')`, garbage, age)
	none := e.contact(ws, nil)
	all := []uuid.UUID{thirty, legacy, garbage, none}

	e.expectMembers("greaterThan", leaf(attributeLeaf("age", "greaterThan", 26)), "", all, thirty)
	e.expectMembers("lessThan falls back to numeric text", leaf(attributeLeaf("age", "lessThan", 28)), "", all, legacy)
	e.expectMembers("lessEqual", leaf(attributeLeaf("age", "lessEqual", 30)), "", all, thirty, legacy)
	e.expectMembers("greaterEqual", leaf(attributeLeaf("age", "greaterEqual", 25.5)), "", all, thirty)
	e.expectMembers("equals compares the text", leaf(attributeLeaf("age", "equals", 30)), "", all, thirty)
	e.expectMembers("equals on degraded text", leaf(attributeLeaf("age", "equals", "ABC")), "", all, garbage)
}

func TestEvaluateDateFilters(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	signup := e.key(ws, "signup", TypeDate)
	x := e.contact(ws, nil)
	e.setDate(ws, x, "signup", time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC))
	y := e.contact(ws, nil)
	e.setDate(ws, y, "signup", time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC))
	// A date that never parsed is compared as ISO text.
	z := e.contact(ws, nil)
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, '2022-05-01T00:00:00.000Z')`, z, signup)
	recent := e.contact(ws, nil)
	e.setDate(ws, recent, "signup", time.Now().Add(-24*time.Hour))
	textual := e.contact(ws, map[string]string{"joined": "2020-01-01"})
	all := []uuid.UUID{x, y, z, recent, textual}

	e.expectMembers("isBefore", leaf(attributeLeaf("signup", "isBefore", "2024-01-01")), "", all, y, z)
	e.expectMembers("isAfter", leaf(attributeLeaf("signup", "isAfter", "2024-01-01")), "", all, x, recent)
	e.expectMembers("isBetween is inclusive", leaf(attributeLeaf("signup", "isBetween", []string{"2023-06-01", "2024-01-15T10:00:00.000Z"})), "", all, x, y)
	e.expectMembers("isSameDay in UTC", leaf(attributeLeaf("signup", "isSameDay", "2024-01-15T23:00:00Z")), "", all, x)
	e.expectMembers("isNewerThan", leaf(attributeLeaf("signup", "isNewerThan", map[string]any{"amount": 7, "unit": "days"})), "", all, recent)
	e.expectMembers("isOlderThan", leaf(attributeLeaf("signup", "isOlderThan", map[string]any{"amount": 1, "unit": "weeks"})), "", all, x, y, z)
	e.expectMembers("date operators need a date key", leaf(attributeLeaf("joined", "isBefore", "2024-01-01")), "", all)
	e.expectMembers("isSet on a date key", leaf(attributeLeaf("signup", "isSet", "")), "", all, x, y, z, recent)

	_, err := e.evaluator.IsMember(t.Context(), e.segmentOf(ws, leaf(attributeLeaf("signup", "isBefore", "not a date"))), x, "")
	if err == nil {
		t.Fatal("an invalid date fails the evaluation, as Prisma refused an Invalid Date")
	}
}

func TestEvaluateDeviceSegmentAndConnectors(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	a := e.contact(ws, map[string]string{"plan": "pro", "email": "a@example.com"})
	b := e.contact(ws, map[string]string{"plan": "free", "email": "b@example.com"})
	c := e.contact(ws, map[string]string{"email": "c@example.com"})
	all := []uuid.UUID{a, b, c}
	phone := leaf(map[string]any{"id": "dev", "root": map[string]any{"type": "device", "deviceType": "phone"}, "value": "phone", "qualifier": map[string]any{"operator": "equals"}})

	e.expectMembers("device matches", phone, DevicePhone, all, a, b, c)
	e.expectMembers("device does not match", phone, DeviceDesktop, all)
	e.expectMembers("no device constrains nothing", phone, "", all, a, b, c)

	pro := e.segmentOf(ws, leaf(attributeLeaf("plan", "equals", "pro")))
	ref := func(id uuid.UUID, op string) []map[string]any {
		return leaf(map[string]any{"id": "ref", "root": map[string]any{"type": "segment", "segmentId": id.String()}, "value": id.String(), "qualifier": map[string]any{"operator": op}})
	}
	e.expectMembers("userIsIn", ref(pro, "userIsIn"), "", all, a)
	e.expectMembers("userIsNotIn", ref(pro, "userIsNotIn"), "", all, b, c)
	everyone := e.segmentOf(ws, tree())
	e.expectMembers("userIsNotIn a segment of everyone", ref(everyone, "userIsNotIn"), "", all)
	foreign := e.segmentOf(e.workspace(org), leaf(attributeLeaf("plan", "equals", "pro")))
	e.expectMembers("another workspace's segment constrains nothing", ref(foreign, "userIsIn"), "", all, a, b, c)

	// S1 references S2, which references S1: the inner reference is dropped.
	s1, s2 := e.segmentOf(ws, tree()), e.segmentOf(ws, tree())
	raw1, _ := json.Marshal(append(ref(s2, "userIsIn"), node("and", attributeLeaf("plan", "isSet", ""))))
	raw2, _ := json.Marshal(ref(s1, "userIsIn"))
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(raw1), s1)
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(raw2), s2)
	if got := e.members(s1, "", all...); !slices.Equal(got, []uuid.UUID{a, b}) {
		t.Errorf("circular reference: %v", got)
	}

	missing := e.segmentOf(ws, ref(uuid.New(), "userIsIn"))
	_, err := e.evaluator.IsMember(t.Context(), missing, a, "")
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != apperr.CodeNotFound {
		t.Fatalf("a missing nested segment fails the evaluation: %v", err)
	}

	// AND binds tighter than OR: (plan = pro AND email contains "b") OR plan is not set.
	precedence := tree(
		node(nil, attributeLeaf("plan", "equals", "pro")),
		node("and", attributeLeaf("email", "contains", "b")),
		node("or", attributeLeaf("plan", "isNotSet", "")),
	)
	e.expectMembers("AND before OR", precedence, "", all, c)
	grouped := tree(
		node(nil, tree(node(nil, attributeLeaf("plan", "equals", "free")), node("or", attributeLeaf("plan", "equals", "pro")))),
		node("and", attributeLeaf("email", "startsWith", "a")),
	)
	e.expectMembers("groups", grouped, "", all, a)
	// A device filter without a device drops out of its group without collapsing the OR into match-all.
	orDevice := tree(node(nil, attributeLeaf("plan", "equals", "pro")), node("or", phone[0]["resource"]))
	e.expectMembers("an unconstrained OR branch is dropped", orDevice, "", all, a)
	e.expectMembers("an empty tree matches everyone", tree(), "", all, a, b, c)

	outsider := e.user()
	_, other := e.organization(outsider)
	stranger := e.contact(other, map[string]string{"plan": "pro"})
	if got := e.members(pro, "", stranger); len(got) != 0 {
		t.Fatal("a contact of another workspace is never a member")
	}
}

func TestEvaluateSurveyInteractions(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	s1, s2 := e.survey(ws, "inProgress"), e.survey(ws, "inProgress")
	seen := e.contact(ws, nil)
	started := e.contact(ws, nil)
	completed := e.contact(ws, nil)
	stale := e.contact(ws, nil)
	all := []uuid.UUID{seen, started, completed, stale}
	e.exec(`INSERT INTO displays (survey_id, contact_id) VALUES (?, ?)`, s1, seen)
	e.exec(`INSERT INTO responses (survey_id, contact_id, finished) VALUES (?, ?, false)`, s1, started)
	e.exec(`INSERT INTO responses (survey_id, contact_id, finished) VALUES (?, ?, true)`, s2, completed)
	e.exec(`INSERT INTO displays (survey_id, contact_id, created_at) VALUES (?, ?, now() - interval '10 days')`, s1, stale)

	day := func(op, scope string, ids ...uuid.UUID) []map[string]any {
		var s []string
		for _, id := range ids {
			s = append(s, id.String())
		}
		return leaf(interactionLeaf(op, scope, s, 1, "days"))
	}
	e.expectMembers("haveSeen", day("haveSeen", "any"), "", all, seen)
	e.expectMembers("haveSeen in a wider window", leaf(interactionLeaf("haveSeen", "any", nil, 2, "weeks")), "", all, seen, stale)
	e.expectMembers("haveNotSeen", day("haveNotSeen", "any"), "", all, started, completed, stale)
	e.expectMembers("haveStartedRespondingTo", day("haveStartedRespondingTo", "any"), "", all, started, completed)
	e.expectMembers("haveCompleted", day("haveCompleted", "any"), "", all, completed)
	e.expectMembers("haveNotCompleted", day("haveNotCompleted", "any"), "", all, seen, started, stale)
	e.expectMembers("specific surveys", day("haveStartedRespondingTo", "specific", s2), "", all, completed)
	e.expectMembers("a survey nobody saw", day("haveNotSeen", "specific", s2), "", all, seen, started, completed, stale)
}

// SegmentIDsForContact answers interaction-only segments from the history it is given, and every other segment from the database.
func TestSegmentIDsForContact(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	survey := e.survey(ws, "inProgress")
	c := e.contact(ws, map[string]string{"plan": "pro"})
	e.exec(`INSERT INTO displays (survey_id, contact_id) VALUES (?, ?)`, survey, c)

	everyone := e.segmentOf(ws, tree())
	pro := e.segmentOf(ws, leaf(attributeLeaf("plan", "equals", "pro")))
	e.segmentOf(ws, leaf(attributeLeaf("plan", "equals", "free")))
	seenOnly := e.segmentOf(ws, leaf(interactionLeaf("haveSeen", "any", nil, 1, "days")))
	seenAndPro := e.segmentOf(ws, tree(node(nil, interactionLeaf("haveSeen", "any", nil, 1, "days")), node("and", attributeLeaf("plan", "equals", "pro"))))
	broken := e.segmentOf(ws, leaf(map[string]any{"id": "ref", "root": map[string]any{"type": "segment", "segmentId": uuid.NewString()}, "value": "x", "qualifier": map[string]any{"operator": "userIsIn"}}))
	phoneOnly := e.segmentOf(ws, leaf(map[string]any{"id": "dev", "root": map[string]any{"type": "device", "deviceType": "phone"}, "value": "phone", "qualifier": map[string]any{"operator": "equals"}}))

	history := Interactions{Displays: []DisplayRow{{SurveyID: survey, CreatedAt: time.Now()}}}
	got, err := e.evaluator.SegmentIDsForContact(t.Context(), ws, c, DeviceDesktop, history)
	if err != nil {
		t.Fatal(err)
	}
	want := []uuid.UUID{everyone, seenOnly, pro, seenAndPro}
	if !sameIDs(got, want) {
		t.Fatalf("segments %v, want %v", got, want)
	}
	if slices.Contains(got, broken) || slices.Contains(got, phoneOnly) {
		t.Fatal("a broken segment is skipped and a device mismatch excludes")
	}

	// With no history in hand the interaction-only segment is answered from it alone, without the database; the mixed one still asks the database.
	got, err = e.evaluator.SegmentIDsForContact(t.Context(), ws, c, DevicePhone, Interactions{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(got, []uuid.UUID{everyone, pro, seenAndPro, phoneOnly}) {
		t.Fatalf("segments %v", got)
	}
}

func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range b {
		if !slices.Contains(a, id) {
			return false
		}
	}
	return true
}

func TestCombine(t *testing.T) {
	and, or := connectorAnd, connectorOr
	cases := []struct {
		pairs []resultPair
		want  bool
	}{
		{nil, false},
		{[]resultPair{{result: true}}, true},
		{[]resultPair{{result: true}, {result: false, connector: &and}}, false},
		{[]resultPair{{result: true}, {result: false, connector: &and}, {result: true, connector: &or}}, true},
		{[]resultPair{{result: false}, {result: true, connector: &or}, {result: false, connector: &and}}, false},
		{[]resultPair{{result: true}, {result: true, connector: nil}}, true},
	}
	for i, c := range cases {
		if got := combine(c.pairs); got != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}

func TestSubtractTimeUnit(t *testing.T) {
	at := time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		amount float64
		unit   string
		want   time.Time
	}{
		{1, "days", time.Date(2024, 3, 30, 12, 0, 0, 0, time.UTC)},
		{2, "weeks", time.Date(2024, 3, 17, 12, 0, 0, 0, time.UTC)},
		// setMonth keeps the day and rolls over: February 31st is March 2nd in a leap year.
		{1, "months", time.Date(2024, 3, 2, 12, 0, 0, 0, time.UTC)},
		{1, "years", time.Date(2023, 3, 31, 12, 0, 0, 0, time.UTC)},
		// setDate truncates the fractional day it is given.
		{1.5, "days", time.Date(2024, 3, 29, 12, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		if got := subtractTimeUnit(at, c.amount, c.unit); !got.Equal(c.want) {
			t.Errorf("%v %s: %s, want %s", c.amount, c.unit, got, c.want)
		}
	}
}
