package contact

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func deviceLeaf(deviceType string) map[string]any {
	return map[string]any{"id": "device", "root": map[string]any{"type": "device", "deviceType": deviceType}, "value": deviceType, "qualifier": map[string]any{"operator": "equals"}}
}

func segmentLeaf(id uuid.UUID, operator string) map[string]any {
	return map[string]any{"id": "segref", "root": map[string]any{"type": "segment", "segmentId": id.String()}, "value": id.String(), "qualifier": map[string]any{"operator": operator}}
}

func (e *env) preview(user, ws uuid.UUID, filters any) PreviewCount {
	e.t.Helper()
	var out PreviewCount
	e.ok(e.do(user, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": filters}), http.StatusOK, &out)
	return out
}

func TestPreviewSegmentCountAuthorization(t *testing.T) {
	e := newEnv(t)
	owner, reader, member, stranger := e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	// A member of the organization whose teams give no access to the workspace.
	e.member(org, member, "member")
	e.contact(ws, map[string]string{"plan": "pro"})

	if got := e.preview(reader, ws, planIsPro); got != (PreviewCount{Count: 1, Total: 1}) {
		t.Fatalf("a reader of the workspace counts: %+v", got)
	}
	e.fails(e.do(member, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": planIsPro}), http.StatusForbidden, "forbidden", "")
	e.fails(e.do(stranger, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": planIsPro}), http.StatusForbidden, "forbidden", "")
	e.fails(e.request(nil, http.MethodPost, wsPath(ws, "/segments/preview-count"), nil, "application/json", nil), http.StatusUnauthorized, "not_authenticated", "")
}

func TestPreviewSegmentCount(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	a := e.contact(ws, map[string]string{"plan": "pro", "email": "a@example.com"})
	e.contact(ws, map[string]string{"plan": "free", "email": "b@example.com"})
	e.contact(ws, map[string]string{"email": "c@example.com"})
	e.contact(ws, nil)
	// Another workspace's contacts are neither matched nor part of the total.
	e.contact(e.workspace(org), map[string]string{"plan": "pro"})
	survey := e.survey(ws, "inProgress")
	// Only the pro contact has seen the survey, and nobody has completed one.
	e.exec(`INSERT INTO displays (survey_id, contact_id) VALUES (?, ?)`, survey, a)
	interaction := interactionLeaf("haveSeen", "specific", []string{survey.String()}, 1, "days")
	completedAny := interactionLeaf("haveCompleted", "any", nil, 30, "days")
	onlyInteraction := e.segmentOf(ws, leaf(interaction))
	onlyDevice := e.segmentOf(ws, leaf(deviceLeaf("phone")))
	pro := e.segmentOf(ws, planIsPro)

	for _, tc := range []struct {
		name    string
		filters any
		want    PreviewCount
	}{
		{"an empty tree matches everyone", tree(), PreviewCount{Count: 4, Total: 4}},
		{"one filter", planIsPro, PreviewCount{Count: 1, Total: 4}},
		{"and", tree(node(nil, attributeLeaf("plan", "isSet", "")), node("and", attributeLeaf("email", "contains", "b"))), PreviewCount{Count: 1, Total: 4}},
		{"or", tree(node(nil, attributeLeaf("plan", "equals", "pro")), node("or", attributeLeaf("plan", "equals", "free"))), PreviewCount{Count: 2, Total: 4}},
		{"and binds tighter than or", tree(node(nil, attributeLeaf("plan", "equals", "pro")), node("and", attributeLeaf("email", "contains", "b")), node("or", attributeLeaf("plan", "isNotSet", ""))), PreviewCount{Count: 2, Total: 4}},
		{"a nested group", tree(node(nil, tree(node(nil, attributeLeaf("plan", "equals", "free")), node("or", attributeLeaf("plan", "equals", "pro")))), node("and", attributeLeaf("email", "startsWith", "a"))), PreviewCount{Count: 1, Total: 4}},
		{"a saved segment", leaf(segmentLeaf(pro, "userIsIn")), PreviewCount{Count: 1, Total: 4}},
		{"outside a saved segment", leaf(segmentLeaf(pro, "userIsNotIn")), PreviewCount{Count: 3, Total: 4}},
		{"a device filter is left out", tree(node(nil, deviceLeaf("phone")), node("and", attributeLeaf("plan", "equals", "free"))), PreviewCount{Count: 1, Total: 4, Approximate: true}},
		// Survey interactions are a property of the contact, so they are counted exactly, as the segment's contact list evaluates them.
		{"a survey-interaction filter is evaluated", leaf(interaction), PreviewCount{Count: 1, Total: 4}},
		{"an interaction nobody has performed matches no one", leaf(completedAny), PreviewCount{Count: 0, Total: 4}},
		{"an interaction and an attribute", tree(node(nil, interaction), node("and", attributeLeaf("plan", "equals", "free"))), PreviewCount{Count: 0, Total: 4}},
		{"an interaction inside a nested group", tree(node(nil, tree(node(nil, interaction), node("or", attributeLeaf("plan", "equals", "free"))))), PreviewCount{Count: 2, Total: 4}},
		{"inside a saved segment of interactions", leaf(segmentLeaf(onlyInteraction, "userIsIn")), PreviewCount{Count: 1, Total: 4}},
		{"outside a saved segment of interactions", leaf(segmentLeaf(onlyInteraction, "userIsNotIn")), PreviewCount{Count: 3, Total: 4}},
		// A device filter drops out of its segment, as it does when a segment's contacts are listed without a device.
		{"inside a saved segment of device filters", leaf(segmentLeaf(onlyDevice, "userIsIn")), PreviewCount{Count: 4, Total: 4, Approximate: true}},
		{"outside a saved segment of device filters", leaf(segmentLeaf(onlyDevice, "userIsNotIn")), PreviewCount{Count: 4, Total: 4, Approximate: true}},
	} {
		if got := e.preview(owner, ws, tc.filters); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}

	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": tree(node(nil, attributeLeaf("age", "greaterThan", "x")))}), http.StatusBadRequest, codeInvalidFilters, filterValueTypeMessage)
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{}), http.StatusBadRequest, "validation_error", "")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": leaf(segmentLeaf(uuid.New(), "userIsIn"))}), http.StatusNotFound, "not_found", "")
}

func TestSegmentContactCounts(t *testing.T) {
	e := newEnv(t)
	owner, reader, member := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	e.member(org, member, "member")
	first := e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": "free"})
	survey := e.survey(ws, "inProgress")
	e.exec(`INSERT INTO responses (survey_id, contact_id, finished) VALUES (?, ?, true)`, survey, first)

	pro := e.segmentOf(ws, planIsPro)
	notPro := e.segmentOf(ws, leaf(segmentLeaf(pro, "userIsNotIn")))
	phones := e.segmentOf(ws, tree(node(nil, deviceLeaf("phone")), node("and", attributeLeaf("plan", "equals", "free"))))
	completed := e.segmentOf(ws, leaf(interactionLeaf("haveCompleted", "any", nil, 30, "days")))
	everyone := e.segmentOf(ws, tree())
	private := e.segmentRow(ws, survey.String(), true, `[]`)
	broken := e.segmentOf(ws, leaf(segmentLeaf(uuid.New(), "userIsIn")))

	var counts []SegmentContactCount
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/segments/contact-counts"), nil), http.StatusOK, &counts)
	got := map[uuid.UUID]SegmentContactCount{}
	for _, c := range counts {
		got[c.SegmentID] = c
	}
	for _, tc := range []struct {
		name        string
		id          uuid.UUID
		count       int64
		approximate bool
	}{
		{"pro", pro, 2, false},
		{"nested and negated", notPro, 1, false},
		{"with a device filter", phones, 1, true},
		{"with a survey interaction", completed, 1, false},
		{"without filters", everyone, 3, false},
	} {
		c, ok := got[tc.id]
		if !ok || c.Count != tc.count || c.Approximate != tc.approximate {
			t.Errorf("%s: %+v (listed %v), want %d %v", tc.name, c, ok, tc.count, tc.approximate)
		}
	}
	if _, ok := got[private]; ok {
		t.Error("a private segment is not counted")
	}
	if _, ok := got[broken]; ok {
		t.Error("a segment nesting a missing segment is not counted")
	}
	if len(counts) != 5 {
		t.Errorf("%d counts, want 5: %+v", len(counts), counts)
	}

	e.fails(e.do(member, http.MethodGet, wsPath(ws, "/segments/contact-counts"), nil), http.StatusForbidden, "forbidden", "")
	e.fails(e.request(nil, http.MethodGet, wsPath(ws, "/segments/contact-counts"), nil, "", nil), http.StatusUnauthorized, "not_authenticated", "")
}

// The segment list, which the survey editor's loader and other pages wait on, does not count contacts.
func TestSegmentListDoesNotCountContacts(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	e.contact(ws, map[string]string{"plan": "pro"})
	e.segmentOf(ws, planIsPro)

	var segs []map[string]json.RawMessage
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/segments"), nil), http.StatusOK, &segs)
	if len(segs) != 1 {
		t.Fatalf("%d segments, want 1", len(segs))
	}
	for _, field := range []string{"contactCount", "contactCountApproximate"} {
		if _, ok := segs[0][field]; ok {
			t.Errorf("the segment list carries %s", field)
		}
	}
}
