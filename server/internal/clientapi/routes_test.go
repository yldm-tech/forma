package clientapi

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// forma2's display creates: v1 attaches the display to the contact with the userId, created when there is none; v2 only to an existing contact of the workspace.
func TestCreateDisplay(t *testing.T) {
	e := newEnv(t)
	ws, other := e.workspace(), e.workspace()
	sv := e.survey(ws, "type", "app")
	d := e.post(t, "/api/v1/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "userId": "new-user"}).data(t)
	if d["contactId"] == nil || d["surveyId"] != sv.String() {
		t.Fatalf("display %v", d)
	}
	if e.attribute(d["contactId"], "userId") != "new-user" {
		t.Fatal("v1 did not create the contact")
	}
	theirs := stateData(t, e.identify(t, other, map[string]any{"userId": "theirs"}))["contactId"]
	d = e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "contactId": theirs}).data(t)
	if d["contactId"] != nil {
		t.Fatalf("v2 attached another workspace's contact: %v", d)
	}
	details := e.post(t, "/api/v1/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "userId": strings.Repeat("u", 256)}).expectError(t, 400, "bad_request", msgInvalidFields)
	if details["userId"] != "User ID cannot exceed 255 characters" {
		t.Fatalf("details %v", details)
	}
	details = e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": "Not-An-Id", "contactId": 7}).expectError(t, 400, "bad_request", msgInvalidFields)
	if details["surveyId"] != "Invalid cuid2" || details["contactId"] != "Invalid input: expected string, received number" {
		t.Fatalf("details %v", details)
	}
	paused := e.survey(ws, "status", "paused")
	details = e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": paused.String()}).expectError(t, 403, "forbidden", "Survey is not accepting submissions")
	if details["surveyId"] != paused.String() {
		t.Fatalf("details %v", details)
	}
	foreign := e.survey(other)
	e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": foreign.String()}).expectError(t, 404, "not_found", "Survey not found")
	if n := e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws); n != 1 {
		t.Fatalf("contacts %d", n)
	}

	// The display-response lookup is scoped to the workspace, and an id that is no id is 400.
	display := e.insert(`INSERT INTO displays (survey_id) VALUES (?) RETURNING id`, foreign)
	e.clock.nextWindow()
	e.do("GET", "/api/v1/client/"+ws.String()+"/displays/"+display.String()+"/response", nil).expectError(t, 404, "not_found", "Display not found")
	e.do("GET", "/api/v1/client/"+ws.String()+"/displays/NOT_AN_ID/response", nil).expectError(t, 400, "bad_request", "Validation failed: Invalid cuid2")
}

func decodePolicy(t *testing.T, fields map[string]any) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(fields["Policy"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func sizeRange(t *testing.T, policy map[string]any) []any {
	t.Helper()
	for _, c := range policy["conditions"].([]any) {
		if l, ok := c.([]any); ok && l[0] == "content-length-range" {
			return l
		}
	}
	t.Fatalf("no size range in %v", policy)
	return nil
}

// forma2's client storage route: the element must take uploads of the file's type, the signed size is the element's, and a workspace takes at most 100 uploads a minute.
func TestStorageUpload(t *testing.T) {
	e := newEnv(t)
	ws, other := e.workspace(), e.workspace()
	sv := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[
		{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":false,"inputType":"text"},
		{"id":"small","type":"fileUpload","headline":{"default":"F"},"required":false,"allowedFileExtensions":["pdf"],"maxSizeInMB":2.5},
		{"id":"any","type":"fileUpload","headline":{"default":"F"},"required":false}]}]`)
	path := "/api/v1/client/" + ws.String() + "/storage"
	upload := func(element, name, typ string) result {
		e.clock.nextWindow()
		return e.do("POST", path, map[string]any{"fileName": name, "fileType": typ, "surveyId": sv.String(), "elementId": element})
	}
	d := upload("small", "report.pdf", "application/pdf").data(t)
	if got := sizeRange(t, decodePolicy(t, d["presignedFields"].(map[string]any))); got[2] != float64(2621440) {
		t.Fatalf("size range %v", got)
	}
	if !strings.HasPrefix(d["fileUrl"].(string), "/storage/"+ws.String()+"/private/surveys/"+sv.String()+"/elements/small/report--fid--") {
		t.Fatalf("file URL %v", d["fileUrl"])
	}
	d = upload("any", "photo.png", "image/png").data(t)
	if got := sizeRange(t, decodePolicy(t, d["presignedFields"].(map[string]any))); got[2] != float64(1<<30) {
		t.Fatalf("size range without an advertised size %v", got)
	}
	upload("small", "photo.png", "image/png").expectError(t, 400, "bad_request", "File extension is not allowed for this element")
	upload("q1", "report.pdf", "application/pdf").expectError(t, 400, "bad_request", "Element does not allow file uploads")
	details := upload("small", "report.pdf", "image/png").expectError(t, 400, "bad_request", msgInvalidFields)
	if details["fileType"] != "File type doesn't match the file extension" {
		t.Fatalf("details %v", details)
	}
	e.clock.nextWindow()
	details = e.do("POST", path, map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "small", "allowedFileExtensions": []string{"exe"}}).expectError(t, 400, "bad_request", msgInvalidFields)
	if !strings.HasPrefix(details["allowedFileExtensions.0"].(string), `Invalid option: expected one of "heic"|"png"`) {
		t.Fatalf("details %v", details)
	}
	foreign := e.survey(other)
	e.clock.nextWindow()
	details = e.do("POST", path, map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": foreign.String(), "elementId": "small"}).expectError(t, 400, "bad_request", "Survey does not belong to the workspace")
	if details["surveyId"] != foreign.String() || details["workspaceId"] != ws.String() {
		t.Fatalf("details %v", details)
	}

	e.clock.nextWindow()
	for i := range 100 {
		if r := e.do("POST", path, map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "small"}); r.Status != 200 {
			t.Fatalf("upload %d: %d %s", i, r.Status, r.Body)
		}
		clearIPLimit(e)
	}
	e.do("POST", path, map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "small"}).
		expectError(t, 429, "too_many_requests", "Maximum number of requests reached. Please try again later.")
}

// clearIPLimit forgets the per-address counters, so a test can reach a per-workspace limit from one address.
func clearIPLimit(e *env) {
	keys, _ := e.cache.Keys(e.t.Context(), "fb:rate_limit:storage:upload:*").Result()
	for _, k := range keys {
		if !strings.HasPrefix(k, "fb:rate_limit:storage:upload:workspace:") {
			e.cache.Del(e.t.Context(), k)
		}
	}
}

// Every route but OPTIONS is charged to the client's address: api:client (100 a minute) everywhere except storage, which has its own storage:upload (5 a minute). A 429 carries no Retry-After.
func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	env := "/api/v1/client/" + ws.String() + "/environment"
	e.clock.nextWindow()
	for range 100 {
		if r := e.do("GET", env, nil); r.Status != 200 {
			t.Fatalf("%d %s", r.Status, r.Body)
		}
	}
	r := e.do("GET", env, nil)
	r.expectError(t, 429, "too_many_requests", "Maximum number of requests reached. Please try again later.")
	if r.Header.Get("Retry-After") != "" || r.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers %v", r.Header)
	}
	for _, p := range []string{"/environment", "/displays", "/responses", "/user", "/storage", "/responses/" + uuid.NewString()} {
		if r := e.do("OPTIONS", "/api/v2/client/"+ws.String()+p, nil); r.Status != 200 {
			t.Fatalf("OPTIONS %s: %d", p, r.Status)
		}
	}
	// The storage route is not charged to api:client.
	if r := e.do("POST", "/api/v1/client/"+ws.String()+"/storage", map[string]any{}); r.Status != 400 {
		t.Fatalf("storage under an exhausted api:client window: %d %s", r.Status, r.Body)
	}
}
