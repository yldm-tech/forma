package response

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Bulk tag changes: an explicit selection (the filter's responseIds) and "all matching" (the view's filter) on POST /surveys/:id/responses/tags/{add,remove}.

func decodeBulk(t *testing.T, body []byte) BulkTagResult {
	t.Helper()
	var out BulkTagResult
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// taggedWith is the responses of the survey carrying the tag, sorted.
func (e *env) taggedWith(tag uuid.UUID) []uuid.UUID {
	e.t.Helper()
	var ids []uuid.UUID
	if err := e.db.Raw(`SELECT response_id FROM response_tags WHERE tag_id = ? ORDER BY response_id`, tag).Scan(&ids).Error; err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func TestBulkTagChangesTheSelectionOrEveryMatchingResponse(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})
	other := e.survey(ws, surveyOpts{})
	ctx := context.Background()
	mk := func(survey uuid.UUID, data string, finished bool) uuid.UUID {
		res, err := e.submit(ctx, ws, survey, data, CreateInput{Finished: finished})
		if err != nil {
			t.Fatal(err)
		}
		return res.ID
	}
	r1 := mk(sv, `{"q1":"refund please"}`, true)
	r2 := mk(sv, `{"q1":"great"}`, false)
	r3 := mk(sv, `{"q1":"refund now"}`, true)
	foreignResponse := mk(other, `{"q1":"refund"}`, true)
	vip := e.tag(ws, "vip", r1)
	add := "/api/app/surveys/" + sv.String() + "/responses/tags/add"
	remove := "/api/app/surveys/" + sv.String() + "/responses/tags/remove"

	// An explicit selection: r1 already carries the tag, and a response of another survey is not touched.
	rec := e.request(owner, http.MethodPost, add, map[string]any{"tagId": vip, "filterCriteria": map[string]any{"responseIds": []uuid.UUID{r1, r2, foreignResponse}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("add to the selection: %d %s", rec.Code, rec.Body)
	}
	if got := decodeBulk(t, rec.Body.Bytes()); got != (BulkTagResult{TagID: vip, Matched: 2, Changed: 1}) {
		t.Fatalf("add to the selection: %+v", got)
	}
	if got := e.taggedWith(vip); !reflect.DeepEqual(got, sortedIDs(r1, r2)) {
		t.Fatalf("tagged after the selection: %v", got)
	}

	// Every response matching a filter, here the keyword search and finished.
	refund := e.tag(ws, "refund")
	rec = e.request(owner, http.MethodPost, add, map[string]any{"tagId": refund, "filterCriteria": map[string]any{"search": "refund", "finished": true}})
	if got := decodeBulk(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got != (BulkTagResult{TagID: refund, Matched: 2, Changed: 2}) {
		t.Fatalf("add to the matching responses: %d %s", rec.Code, rec.Body)
	}
	if got := e.taggedWith(refund); !reflect.DeepEqual(got, sortedIDs(r1, r3)) {
		t.Fatalf("tagged after the filter: %v", got)
	}

	// The empty filter is every response of the survey.
	all := e.tag(ws, "all")
	rec = e.request(owner, http.MethodPost, add, map[string]any{"tagId": all, "filterCriteria": map[string]any{}})
	if got := decodeBulk(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got.Matched != 3 || got.Changed != 3 {
		t.Fatalf("add to every response: %d %s", rec.Code, rec.Body)
	}

	// Removing by a filter on the tag itself takes it off every response that carried it.
	rec = e.request(owner, http.MethodPost, remove, map[string]any{"tagId": vip, "filterCriteria": map[string]any{"tags": map[string]any{"applied": []string{"vip"}}}})
	if got := decodeBulk(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got != (BulkTagResult{TagID: vip, Matched: 2, Changed: 2}) {
		t.Fatalf("remove from the tagged responses: %d %s", rec.Code, rec.Body)
	}
	if got := e.taggedWith(vip); len(got) != 0 {
		t.Fatalf("still tagged after the removal: %v", got)
	}
	// Removing a tag no matching response carries changes nothing and is not an error.
	rec = e.request(owner, http.MethodPost, remove, map[string]any{"tagId": vip, "filterCriteria": map[string]any{}})
	if got := decodeBulk(t, rec.Body.Bytes()); rec.Code != http.StatusOK || got != (BulkTagResult{TagID: vip, Matched: 3, Changed: 0}) {
		t.Fatalf("remove a tag nobody carries: %d %s", rec.Code, rec.Body)
	}
	if got := e.taggedWith(all); !reflect.DeepEqual(got, sortedIDs(r1, r2, r3)) {
		t.Fatalf("another tag changed: %v", got)
	}

	var added, removed string
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		switch {
		case contains(line, `"msg":"audit"`) && contains(line, `"action":"addedToResponse"`) && contains(line, refund.String()):
			added = line
		case contains(line, `"msg":"audit"`) && contains(line, `"action":"removedFromResponse"`) && contains(line, `"responseCount":2`):
			removed = line
		}
	}
	if added == "" || removed == "" {
		t.Fatalf("bulk tag changes were not audited: added %q removed %q", added, removed)
	}
	// The event counts the responses but does not carry the filter, which can hold answer text.
	if !contains(added, `"responseCount":2`) || !contains(added, sv.String()) || contains(added, "refund") {
		t.Fatalf("bulk add audit event %s", added)
	}
}

func TestBulkTagRefusesBadInput(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})
	res, err := e.submit(context.Background(), ws, sv, `{}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	vip := e.tag(ws, "vip")
	foreign := e.tag(e.workspace(org), "foreign")
	add := "/api/app/surveys/" + sv.String() + "/responses/tags/add"
	tooMany := make([]uuid.UUID, MaxBulkTagResponseIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}

	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"a tag of another workspace", map[string]any{"tagId": foreign, "filterCriteria": map[string]any{}}, http.StatusBadRequest},
		{"an unknown tag", map[string]any{"tagId": uuid.New(), "filterCriteria": map[string]any{}}, http.StatusNotFound},
		{"no filter", map[string]any{"tagId": vip}, http.StatusBadRequest},
		{"no tag", map[string]any{"filterCriteria": map[string]any{}}, http.StatusBadRequest},
		{"too many ids", map[string]any{"tagId": vip, "filterCriteria": map[string]any{"responseIds": tooMany}}, http.StatusBadRequest},
		{"a malformed filter", map[string]any{"tagId": vip, "filterCriteria": map[string]any{"data": map[string]any{"q1": map[string]any{"op": "nope"}}}}, http.StatusBadRequest},
	} {
		if rec := e.request(owner, http.MethodPost, add, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, res.ID); n != 0 {
		t.Fatalf("a refused change tagged the response %d times", n)
	}
}

func TestBulkTagNeedsWriteAccessToTheSurvey(t *testing.T) {
	e := newEnv(t)
	owner, reader, writer, outsider := e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	for _, u := range []uuid.UUID{reader, writer} {
		e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, u, org)
	}
	e.grant(org, ws, reader, "read")
	e.grant(org, ws, writer, "readWrite")
	sv := e.survey(ws, surveyOpts{})
	if _, err := e.submit(context.Background(), ws, sv, `{}`, CreateInput{}); err != nil {
		t.Fatal(err)
	}
	vip := e.tag(ws, "vip")
	body := map[string]any{"tagId": vip, "filterCriteria": map[string]any{}}

	for _, c := range []struct {
		name string
		user uuid.UUID
		path string
		want int
	}{
		{"a reader adds", reader, "/api/app/surveys/" + sv.String() + "/responses/tags/add", http.StatusForbidden},
		{"a reader removes", reader, "/api/app/surveys/" + sv.String() + "/responses/tags/remove", http.StatusForbidden},
		{"an outsider adds", outsider, "/api/app/surveys/" + sv.String() + "/responses/tags/add", http.StatusForbidden},
		{"an unknown survey", owner, "/api/app/surveys/" + uuid.NewString() + "/responses/tags/add", http.StatusForbidden},
	} {
		if rec := e.request(c.user, http.MethodPost, c.path, body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM response_tags`); n != 0 {
		t.Fatalf("a refused call tagged %d responses", n)
	}
	if rec := e.request(writer, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/tags/add", body); rec.Code != http.StatusOK {
		t.Fatalf("a writer adds: %d %s", rec.Code, rec.Body)
	}
}
