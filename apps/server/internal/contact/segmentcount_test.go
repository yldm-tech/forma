package contact

import (
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
	e.contact(ws, map[string]string{"plan": "pro", "email": "a@example.com"})
	e.contact(ws, map[string]string{"plan": "free", "email": "b@example.com"})
	e.contact(ws, map[string]string{"email": "c@example.com"})
	e.contact(ws, nil)
	// Another workspace's contacts are neither matched nor part of the total.
	e.contact(e.workspace(org), map[string]string{"plan": "pro"})
	survey := e.survey(ws, "inProgress")
	interaction := interactionLeaf("haveSeen", "specific", []string{survey.String()}, 1, "days")
	onlyInteraction := e.segmentOf(ws, leaf(interaction))
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
		// Nobody has seen the survey, so the interaction filter alone would match no one: it is left out, not evaluated.
		{"a survey-interaction filter is left out", tree(node(nil, interaction), node("and", attributeLeaf("plan", "equals", "pro"))), PreviewCount{Count: 1, Total: 4, Approximate: true}},
		// A left-out filter drops out of its or, as a device filter without a device does when a segment's contacts are listed.
		{"inside a nested group", tree(node(nil, tree(node(nil, interaction), node("or", attributeLeaf("plan", "equals", "free"))))), PreviewCount{Count: 1, Total: 4, Approximate: true}},
		{"inside a saved segment", leaf(segmentLeaf(onlyInteraction, "userIsIn")), PreviewCount{Count: 4, Total: 4, Approximate: true}},
		{"outside a saved segment of left-out filters", leaf(segmentLeaf(onlyInteraction, "userIsNotIn")), PreviewCount{Count: 4, Total: 4, Approximate: true}},
	} {
		if got := e.preview(owner, ws, tc.filters); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}

	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": tree(node(nil, attributeLeaf("age", "greaterThan", "x")))}), http.StatusBadRequest, codeInvalidFilters, filterValueTypeMessage)
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{}), http.StatusBadRequest, "validation_error", "")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments/preview-count"), map[string]any{"filters": leaf(segmentLeaf(uuid.New(), "userIsIn"))}), http.StatusNotFound, "not_found", "")
}

func TestSegmentListContactCounts(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": "pro"})
	e.contact(ws, map[string]string{"plan": "free"})
	survey := e.survey(ws, "inProgress")

	pro := e.segmentOf(ws, planIsPro)
	notPro := e.segmentOf(ws, leaf(segmentLeaf(pro, "userIsNotIn")))
	phones := e.segmentOf(ws, tree(node(nil, deviceLeaf("phone")), node("and", attributeLeaf("plan", "equals", "free"))))
	everyone := e.segmentOf(ws, tree())
	private := e.segmentRow(ws, survey.String(), true, `[]`)
	broken := e.segmentOf(ws, leaf(segmentLeaf(uuid.New(), "userIsIn")))

	var segs []Segment
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/segments"), nil), http.StatusOK, &segs)
	got := map[uuid.UUID]Segment{}
	for _, s := range segs {
		got[s.ID] = s
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
		{"without filters", everyone, 3, false},
	} {
		seg := got[tc.id]
		if seg.ContactCount == nil || *seg.ContactCount != tc.count || seg.ContactCountApproximate != tc.approximate {
			t.Errorf("%s: count %v approximate %v, want %d %v", tc.name, seg.ContactCount, seg.ContactCountApproximate, tc.count, tc.approximate)
		}
	}
	if seg := got[private]; seg.ContactCount != nil {
		t.Errorf("a private segment is not counted: %d", *seg.ContactCount)
	}
	if seg := got[broken]; seg.ContactCount != nil {
		t.Errorf("a segment nesting a missing segment is not counted: %d", *seg.ContactCount)
	}
}
