package response

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The response views' operations, the cases of forma2's lib/response/service.test.ts (getResponses, getResponseCountBySurveyId, deleteResponse), where-clause tests in lib/response/utils.test.ts, the analysis actions' authorization, and tagOnResponse.

type listed struct {
	ID       uuid.UUID `json:"id"`
	Finished bool      `json:"finished"`
	Tags     []struct {
		Name string `json:"name"`
	} `json:"tags"`
	Quotas  []QuotaRef `json:"quotas"`
	Contact *Contact   `json:"contact"`
}

func decodeList(t *testing.T, body []byte) []listed {
	t.Helper()
	var out []listed
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

func listedIDs(list []listed) []uuid.UUID {
	out := make([]uuid.UUID, len(list))
	for i, l := range list {
		out[i] = l.ID
	}
	return out
}

func (e *env) tag(ws uuid.UUID, name string, responses ...uuid.UUID) uuid.UUID {
	t := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, ?) RETURNING id`, ws, name)
	for _, r := range responses {
		e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, r, t)
	}
	return t
}

func TestListAndCountFilterTheSurveysResponses(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})
	ctx := context.Background()
	q := e.quotaOn(sv, "Q", "a", 10, actionContinueSurvey, nil, true)
	e.contact(ws, "u1", "plan", "gold")
	e.field(ws, sv, "score", "computed", "number", false)

	mk := func(data string, in CreateInput) uuid.UUID {
		res, err := e.submit(ctx, ws, sv, data, in)
		if err != nil {
			t.Fatal(err)
		}
		return res.ID
	}
	r1 := mk(`{"q1":"a","q2":"One"}`, CreateInput{Finished: true, Language: ptr("de"), UserID: ptr("u1"), Variables: map[string]any{"score": 5.0}, Meta: ClientMeta{URL: ptr("https://shop.example.com/p")}, Request: RequestInfo{UserAgent: UserAgent{Browser: "Chrome"}, Country: "DE"}, TTC: map[string]float64{"q1": 2400}})
	r2 := mk(`{"q1":"b","q2":"something else"}`, CreateInput{Finished: false, Language: ptr("fr"), Variables: map[string]any{"score": 1.0}, Request: RequestInfo{UserAgent: UserAgent{Browser: "Firefox"}}})
	r3 := mk(`{"q1":"c","f1":["https://files/1"]}`, CreateInput{Finished: true})
	e.tag(ws, "vip", r1, r3)
	e.tag(ws, "spam", r3)

	cases := []struct {
		name   string
		filter string
		want   []uuid.UUID
	}{
		{"no filter", `null`, []uuid.UUID{r3, r2, r1}},
		{"finished", `{"finished":true}`, []uuid.UUID{r3, r1}},
		{"data equals", `{"data":{"q1":{"op":"equals","value":"b"}}}`, []uuid.UUID{r2}},
		{"data notEquals counts the absent answer", `{"data":{"q2":{"op":"notEquals","value":"One"}}}`, []uuid.UUID{r3, r2}},
		{"data submitted", `{"data":{"f1":{"op":"submitted"}}}`, []uuid.UUID{r3}},
		{"data includesAll", `{"data":{"f1":{"op":"includesAll","value":["https://files/1"]}}}`, []uuid.UUID{r3}},
		{"data includesOne an element of a list answer", `{"data":{"f1":{"op":"includesOne","value":["https://files/1","nope"]}}}`, []uuid.UUID{r3}},
		{"data skipped", `{"data":{"q2":{"op":"skipped"}}}`, []uuid.UUID{r3}},
		{"includesOne Other is any answer no label covers", `{"data":{"q2":{"op":"includesOne","value":["Other"]}}}`, []uuid.UUID{r2}},
		{"includesOne a label", `{"data":{"q2":{"op":"includesOne","value":["One"]}}}`, []uuid.UUID{r1}},
		{"tags applied and not applied", `{"tags":{"applied":["vip"],"notApplied":["spam"]}}`, []uuid.UUID{r1}},
		{"contact attribute", `{"contactAttributes":{"plan":{"op":"equals","value":"gold"}}}`, []uuid.UUID{r1}},
		{"contact attribute notEquals counts responses without a contact", `{"contactAttributes":{"plan":{"op":"notEquals","value":"gold"}}}`, []uuid.UUID{r3, r2}},
		{"meta browser", `{"meta":{"browser":{"op":"equals","value":"Firefox"}}}`, []uuid.UUID{r2}},
		{"meta url contains", `{"meta":{"url":{"op":"contains","value":"shop."}}}`, []uuid.UUID{r1}},
		{"others language", `{"others":{"Language":{"op":"equals","value":"de-DE"}}}`, []uuid.UUID{r1}},
		{"reserved country", `{"reserved":{"country":{"op":"equals","value":"DE"}}}`, []uuid.UUID{r1}},
		{"reserved country isNotSet", `{"reserved":{"country":{"op":"isNotSet"}}}`, []uuid.UUID{r3, r2}},
		{"reserved durationSeconds rounds like the read seam", `{"reserved":{"durationSeconds":{"op":"equals","value":2}}}`, []uuid.UUID{r1}},
		{"reserved names a survey element shadows filter nothing", `{"reserved":{"q1":{"op":"equals","value":"zzz"}}}`, []uuid.UUID{r3, r2, r1}},
		{"variables compare as their type", `{"variables":{"score":{"op":"greaterThan","value":3}}}`, []uuid.UUID{r1}},
		{"variables isNotSet", `{"variables":{"score":{"op":"isNotSet"}}}`, []uuid.UUID{r3}},
		{"a variable the survey does not compute filters nothing", `{"variables":{"nope":{"op":"isSet"}}}`, []uuid.UUID{r3, r2, r1}},
		{"a text operator on a number variable matches nothing", `{"variables":{"score":{"op":"contains","value":"5"}}}`, []uuid.UUID{}},
		{"quota screenedIn", `{"quotas":{"` + q.String() + `":{"op":"screenedIn"}}}`, []uuid.UUID{r1}},
		{"quota screenedOutNotInQuota", `{"quotas":{"` + q.String() + `":{"op":"screenedOutNotInQuota"}}}`, []uuid.UUID{r3, r2}},
		{"created before", `{"createdAt":{"max":"2000-01-01T00:00:00Z"}}`, []uuid.UUID{}},
		{"response ids", `{"responseIds":["` + r2.String() + `"]}`, []uuid.UUID{r2}},
	}
	for _, c := range cases {
		var filter any
		if err := json.Unmarshal([]byte(c.filter), &filter); err != nil {
			t.Fatal(err)
		}
		rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"filterCriteria": filter})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
		if got := listedIDs(decodeList(t, rec.Body.Bytes())); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		rec = e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/count", map[string]any{"filterCriteria": filter})
		var count countResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &count); err != nil || count.Count != int64(len(c.want)) {
			t.Errorf("%s: count %s, want %d", c.name, rec.Body, len(c.want))
		}
	}

	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"limit": 1, "offset": 1})
	if got := listedIDs(decodeList(t, rec.Body.Bytes())); !reflect.DeepEqual(got, []uuid.UUID{r2}) {
		t.Errorf("page: %v", got)
	}
	var createdAt time.Time
	if err := e.db.Raw(`SELECT created_at FROM responses WHERE id = ?`, r2).Row().Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	rec = e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"cursor": map[string]any{"createdAt": createdAt, "id": r2}})
	if got := listedIDs(decodeList(t, rec.Body.Bytes())); !reflect.DeepEqual(got, []uuid.UUID{r1}) {
		t.Errorf("after the cursor: %v", got)
	}
	list := decodeList(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{}).Body.Bytes())
	first := list[2]
	if !reflect.DeepEqual(first.Quotas, []QuotaRef{{ID: q, Name: "Q"}}) || len(first.Tags) != 1 || first.Tags[0].Name != "vip" || first.Contact == nil || *first.Contact.UserID != "u1" {
		t.Errorf("listed response %+v", first)
	}

	rec = e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/search", map[string]any{"filterCriteria": map[string]any{"data": map[string]any{"q1": map[string]any{"op": "nope"}}}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown operator: %d %s", rec.Code, rec.Body)
	}
}

func TestResponseViewsAuthorizeEveryCall(t *testing.T) {
	e := newEnv(t)
	owner, reader, outsider := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.exec(`INSERT INTO memberships (user_id, organization_id, role, accepted) VALUES (?, ?, 'member', true)`, reader, org)
	e.grant(org, ws, reader, "read")
	sv := e.survey(ws, surveyOpts{})
	res, err := e.submit(context.Background(), ws, sv, `{}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	tagID := e.tag(ws, "vip")

	for _, c := range []struct {
		user   uuid.UUID
		method string
		path   string
		body   any
		want   int
	}{
		{reader, http.MethodPost, "/api/app/surveys/" + sv.String() + "/responses/search", map[string]any{}, http.StatusOK},
		{reader, http.MethodGet, "/api/app/responses/" + res.ID.String(), nil, http.StatusOK},
		{outsider, http.MethodPost, "/api/app/surveys/" + sv.String() + "/responses/search", map[string]any{}, http.StatusForbidden},
		{outsider, http.MethodPost, "/api/app/surveys/" + sv.String() + "/responses/count", map[string]any{}, http.StatusForbidden},
		{outsider, http.MethodGet, "/api/app/responses/" + res.ID.String(), nil, http.StatusForbidden},
		{owner, http.MethodGet, "/api/app/responses/" + uuid.NewString(), nil, http.StatusForbidden},
		{reader, http.MethodDelete, "/api/app/responses/" + res.ID.String(), nil, http.StatusForbidden},
		{reader, http.MethodPost, "/api/app/responses/" + res.ID.String() + "/tags", map[string]any{"tagId": tagID}, http.StatusForbidden},
		{outsider, http.MethodDelete, "/api/app/responses/" + res.ID.String() + "/tags/" + tagID.String(), nil, http.StatusForbidden},
	} {
		if rec := e.request(c.user, c.method, c.path, c.body); rec.Code != c.want {
			t.Errorf("%s %s: %d %s, want %d", c.method, c.path, rec.Code, rec.Body, c.want)
		}
	}
	if n := e.count(`SELECT count(*) FROM responses`) + e.count(`SELECT count(*) FROM response_tags`); n != 1 {
		t.Fatalf("a refused call changed something: %d rows", n)
	}
}

func TestDeleteTakesTheDisplayAndQueuesTheFiles(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws, surveyOpts{})
	q := e.quotaOn(sv, "Q", "a", 5, actionContinueSurvey, nil, true)
	ctx := context.Background()
	display := e.display(sv, nil)
	res, err := e.submit(ctx, ws, sv, `{"q1":"a","f1":["https://files.example.com/storage/x/private/a.pdf","https://files.example.com/storage/x/private/b.pdf"]}`, CreateInput{DisplayID: ptr(display.String())})
	if err != nil {
		t.Fatal(err)
	}

	rec := e.request(owner, http.MethodDelete, "/api/app/responses/"+res.ID.String()+"?decrementQuotas=true", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	var deleted Response
	if err := json.Unmarshal(rec.Body.Bytes(), &deleted); err != nil || deleted.ID != res.ID {
		t.Fatalf("deleted %s", rec.Body)
	}
	if e.count(`SELECT count(*) FROM responses`)+e.count(`SELECT count(*) FROM displays`) != 0 {
		t.Fatal("the response or its display survived")
	}
	if n := e.count(`SELECT "limit" FROM survey_quotas WHERE id = ?`, q); n != 4 {
		t.Fatalf("quota limit %d, want 4", n)
	}
	files := e.jsonOf(`SELECT args FROM river_job WHERE kind = 'response.delete-files'`)
	want := map[string]any{"workspaceId": ws.String(), "fileUrls": []any{"https://files.example.com/storage/x/private/a.pdf", "https://files.example.com/storage/x/private/b.pdf"}}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("file cleanup job %v, want %v", files, want)
	}
	var deletion string
	for line := range strings.SplitSeq(e.logs.String(), "\n") {
		if contains(line, `"msg":"audit"`) && contains(line, `"action":"deleted"`) {
			deletion = line
		}
	}
	if deletion == "" {
		t.Fatal("no audit event for the deletion")
	}
	// The event names the answered questions but keeps none of the answers.
	if contains(deletion, "private/a.pdf") || !contains(deletion, `"data":{`) || !contains(deletion, `"q1":"********"`) || !contains(deletion, `"f1":"********"`) {
		t.Fatalf("deletion audit event %s, want the answers' keys without their values", deletion)
	}
}

func TestTagging(t *testing.T) {
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
	path := "/api/app/responses/" + res.ID.String() + "/tags"

	for range 2 {
		if rec := e.request(owner, http.MethodPost, path, map[string]any{"tagId": vip}); rec.Code != http.StatusOK {
			t.Fatalf("add: %d %s", rec.Code, rec.Body)
		}
	}
	if n := e.count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, res.ID); n != 1 {
		t.Fatalf("%d tags after adding one twice", n)
	}
	if rec := e.request(owner, http.MethodPost, path, map[string]any{"tagId": foreign}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a tag of another workspace: %d %s", rec.Code, rec.Body)
	}
	if rec := e.request(owner, http.MethodPost, path, map[string]any{"tagId": uuid.New()}); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown tag: %d %s", rec.Code, rec.Body)
	}
	if rec := e.request(owner, http.MethodDelete, path+"/"+vip.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
	if rec := e.request(owner, http.MethodDelete, path+"/"+vip.String(), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("remove again: %d %s", rec.Code, rec.Body)
	}
	if !contains(e.logs.String(), `"action":"addedToResponse"`) || !contains(e.logs.String(), `"action":"removedFromResponse"`) {
		t.Fatal("tag changes were not audited")
	}
}
