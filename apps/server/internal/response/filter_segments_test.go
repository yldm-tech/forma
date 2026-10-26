package response

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// segment stores a segment of ws with the given filter tree.
func (e *env) segment(ws uuid.UUID, title string, isPrivate bool, filters any) uuid.UUID {
	e.t.Helper()
	raw, err := json.Marshal(filters)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, ?, ?::jsonb) RETURNING id`, ws, title, isPrivate, string(raw))
}

// segmentNode is one node of a segment's filter tree.
func segmentNode(connector any, resource map[string]any) map[string]any {
	return map[string]any{"id": uuid.NewString(), "connector": connector, "resource": resource}
}

func attributeEquals(key, value string) map[string]any {
	return map[string]any{"id": uuid.NewString(), "root": map[string]any{"type": "attribute", "contactAttributeKey": key}, "value": value, "qualifier": map[string]any{"operator": "equals"}}
}

func nestedSegment(id uuid.UUID, operator string) map[string]any {
	return map[string]any{"id": uuid.NewString(), "root": map[string]any{"type": "segment", "segmentId": id.String()}, "value": id.String(), "qualifier": map[string]any{"operator": operator}}
}

func phoneOnly() map[string]any {
	return map[string]any{"id": uuid.NewString(), "root": map[string]any{"type": "device", "deviceType": "phone"}, "value": "phone", "qualifier": map[string]any{"operator": "equals"}}
}

// segmentFilter is a filter on one segment.
func segmentFilter(id string, op string) string {
	return `{"segments":{"` + id + `":{"op":"` + op + `"}}}`
}

// Responses filter by the segments their contact is in now: a segment's members as its contact list shows them, nested segments included and device conditions left out. "Not in" counts a response without a contact; a segment of another workspace, one since deleted, or one nesting a deleted segment holds nobody.
func TestSegmentFilterKeepsTheResponsesOfTheSegmentsContacts(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})

	pro := e.contact(ws, "pro-user", "plan", "pro")
	free := e.contact(ws, "free-user", "plan", "free")
	r := map[string]uuid.UUID{}
	for name, c := range map[string]*uuid.UUID{"pro": &pro, "free": &free, "anonymous": nil} {
		r[name] = e.insert(`INSERT INTO responses (survey_id, finished, data, contact_id) VALUES (?, true, '{}'::jsonb, ?) RETURNING id`, sv, c)
	}
	// The response keeps the attributes the contact had when answering; the filter reads the contact as it is now.
	r["unfinished pro"] = e.insert(`INSERT INTO responses (survey_id, finished, data, contact_id, contact_attributes) VALUES (?, false, '{}'::jsonb, ?, '{"plan":"free"}'::jsonb) RETURNING id`, sv, pro)

	proSegment := e.segment(ws, "Pro customers", false, []any{segmentNode(nil, attributeEquals("plan", "pro"))})
	everyone := e.segment(ws, "Everyone", false, []any{})
	phones := e.segment(ws, "Phones", false, []any{segmentNode(nil, phoneOnly())})
	notPro := e.segment(ws, "Not pro", false, []any{segmentNode(nil, nestedSegment(proSegment, "userIsNotIn"))})
	private := e.segment(ws, uuid.NewString(), true, []any{segmentNode(nil, attributeEquals("plan", "free"))})
	broken := e.segment(ws, "Broken", false, []any{segmentNode(nil, nestedSegment(uuid.New(), "userIsIn"))})
	otherWorkspace := e.workspace(org)
	foreign := e.segment(otherWorkspace, "Pro elsewhere", false, []any{segmentNode(nil, attributeEquals("plan", "pro"))})

	all := []string{"pro", "free", "anonymous", "unfinished pro"}
	expectFiltered(t, e, owner, sv, r, []struct {
		name, filter string
		want         []string
	}{
		{"in a segment", segmentFilter(proSegment.String(), "userIsIn"), []string{"pro", "unfinished pro"}},
		{"not in a segment counts responses without a contact", segmentFilter(proSegment.String(), "userIsNotIn"), []string{"free", "anonymous"}},
		{"a segment without filters holds every contact", segmentFilter(everyone.String(), "userIsIn"), []string{"pro", "free", "unfinished pro"}},
		{"nobody with a contact is outside a segment without filters", segmentFilter(everyone.String(), "userIsNotIn"), []string{"anonymous"}},
		{"a device condition constrains nothing", segmentFilter(phones.String(), "userIsIn"), []string{"pro", "free", "unfinished pro"}},
		{"a nested segment", segmentFilter(notPro.String(), "userIsIn"), []string{"free"}},
		{"a private segment of the workspace", segmentFilter(private.String(), "userIsIn"), []string{"free"}},
		{"a segment nesting a deleted segment holds nobody", segmentFilter(broken.String(), "userIsIn"), nil},
		{"another workspace's segment holds nobody", segmentFilter(foreign.String(), "userIsIn"), nil},
		{"everyone is outside another workspace's segment", segmentFilter(foreign.String(), "userIsNotIn"), all},
		{"a deleted segment holds nobody", segmentFilter(uuid.NewString(), "userIsIn"), nil},
		{"a key that is no row id names no segment", segmentFilter("clxbivtla014iye2vfrn436xd", "userIsIn"), nil},
		{"with another filter", `{"finished":true,"segments":{"` + proSegment.String() + `":{"op":"userIsIn"}}}`, []string{"pro"}},
		{"two segments", `{"segments":{"` + everyone.String() + `":{"op":"userIsIn"},"` + notPro.String() + `":{"op":"userIsNotIn"}}}`, []string{"pro", "unfinished pro"}},
		{"in and not in one segment", `{"segments":{"` + proSegment.String() + `":[{"op":"userIsIn"},{"op":"userIsNotIn"}]}}`, nil},
	})

	for name, filter := range map[string]any{
		"an unknown operator": map[string]any{"segments": map[string]any{proSegment.String(): map[string]any{"op": "equals"}}},
		"an empty array":      map[string]any{"segments": map[string]any{proSegment.String(): []any{}}},
	} {
		if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/count", map[string]any{"filterCriteria": filter}); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}
