package clientapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
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
	// The display names its contact by the token an identify answers, not by id.
	created := e.contactOf(t, ws, d)
	if e.attribute(created, "userId") != "new-user" {
		t.Fatal("v1 did not create the contact")
	}
	d = e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "contactId": d["contactId"]}).data(t)
	if e.contactOf(t, ws, d) != created {
		t.Fatalf("v2 did not attach the contact its token names: %v", d)
	}
	theirs := stateData(t, e.identify(t, other, map[string]any{"userId": "theirs"}))
	for _, ref := range []any{theirs["contactId"], e.contactOf(t, other, theirs).String()} {
		d = e.post(t, "/api/v2/client/"+ws.String()+"/displays", map[string]any{"surveyId": sv.String(), "contactId": ref}).data(t)
		if d["contactId"] != nil {
			t.Fatalf("v2 attached another workspace's contact by %v: %v", ref, d)
		}
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
	// The file is stored with its extension's type, not the type sent: "text/plain;,text/html" passes the type check, which reads what comes before the first semicolon, and browsers would render it as HTML.
	d = upload("any", "note.txt", "text/plain;,text/html").data(t)
	fields := d["presignedFields"].(map[string]any)
	if fields["Content-Type"] != "text/plain" || !slices.ContainsFunc(decodePolicy(t, fields)["conditions"].([]any), func(c any) bool {
		l, ok := c.([]any)
		return ok && len(l) == 3 && l[0] == "eq" && l[1] == "$Content-Type" && l[2] == "text/plain"
	}) {
		t.Fatalf("crafted type signed as %v, policy %v", fields["Content-Type"], decodePolicy(t, fields))
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

// The element id becomes a directory of the stored file's key, so ZUploadPrivateFileRequest allows only letters, digits, hyphens and underscores in it; the extension list must be a list; and a name with nothing left once sanitized is the storage's invalid input.
func TestStorageUploadInput(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[{"id":"any","type":"fileUpload","headline":{"default":"F"},"required":false}]}]`)
	path := "/api/v1/client/" + ws.String() + "/storage"
	upload := func(body map[string]any) result {
		e.clock.nextWindow()
		return e.do("POST", path, body)
	}
	const charRule = "Element id must contain only alphanumeric characters, hyphens, or underscores"
	for elementID, want := range map[string]string{
		"../any":   charRule,
		"any/../x": charRule,
		"an y":     charRule,
		"any%2F":   charRule,
		// Trimmed to nothing, it fails the length and the character rule; forma2's details keep the last issue of a path.
		"   ": charRule,
	} {
		details := upload(map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": elementID}).expectError(t, 400, "bad_request", msgInvalidFields)
		if details["elementId"] != want {
			t.Errorf("elementId %q: details %v", elementID, details)
		}
	}
	details := upload(map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "any", "allowedFileExtensions": "pdf"}).expectError(t, 400, "bad_request", msgInvalidFields)
	if details["allowedFileExtensions"] != "Invalid input: expected array, received string" {
		t.Errorf("details %v", details)
	}
	details = upload(map[string]any{"fileName": "#?*.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "any"}).expectError(t, 400, "bad_request", "Invalid input")
	if details["fileName"] != "#?*.pdf" {
		t.Errorf("details %v", details)
	}
}

// Without a bucket the route answers the storage_error_code the survey runtime reads to tell the respondent uploads are unavailable.
func TestStorageUploadWithoutStorage(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.S3 = config.S3Config{} })
	ws := e.workspace()
	sv := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[{"id":"any","type":"fileUpload","headline":{"default":"F"},"required":false}]}]`)
	e.clock.nextWindow()
	details := e.do("POST", "/api/v1/client/"+ws.String()+"/storage", map[string]any{"fileName": "a.pdf", "fileType": "application/pdf", "surveyId": sv.String(), "elementId": "any"}).
		expectError(t, 500, "internal_server_error", "File storage is not configured correctly. Please check your file upload settings.")
	if details["storage_error_code"] != "s3_client_error" {
		t.Fatalf("details %v", details)
	}
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

// spend uses up rule's current window for the client address ip, as that many earlier requests would have.
func (e *env) spend(rule ratelimit.Rule, ip string) {
	e.t.Helper()
	sum := sha256.Sum256([]byte(ip))
	if res, err := e.h.limiter.Check(e.t.Context(), rule, hex.EncodeToString(sum[:]), rule.Limit); err != nil || !res.Allowed {
		e.t.Fatalf("spend %s: %+v, %v", rule.Namespace, res, err)
	}
}

// GATEWAY_RATE_LIMITING=1 declares a gateway enforcing forma2's policy set: the client API routes it covers are no longer charged here, the others still are. Without it the server limits them itself.
func TestGatewayRateLimiting(t *testing.T) {
	const ip = "203.0.113.9"
	for _, gateway := range []bool{false, true} {
		t.Run(map[bool]string{false: "no gateway", true: "gateway"}[gateway], func(t *testing.T) {
			e := newEnv(t, func(cfg *config.Config) { cfg.GatewayRateLimiting = gateway })
			ws := e.workspace().String()
			e.clock.nextWindow()
			e.spend(ratelimit.APIClient, ip)
			e.spend(ratelimit.StorageUpload, ip)

			covered := e.do("GET", "/api/v1/client/"+ws+"/environment", nil, "X-Forwarded-For", ip)
			upload := e.do("POST", "/api/v1/client/"+ws+"/storage", map[string]any{}, "X-Forwarded-For", ip)
			uncovered := e.do("GET", "/api/v2/client/"+ws+"/environment", nil, "X-Forwarded-For", ip)
			if !gateway {
				covered.expectError(t, 429, "too_many_requests", "Maximum number of requests reached. Please try again later.")
				upload.expectError(t, 429, "too_many_requests", "Maximum number of requests reached. Please try again later.")
			} else {
				if covered.Status != 200 {
					t.Fatalf("covered route behind a gateway: %d %s", covered.Status, covered.Body)
				}
				if upload.Status != 400 {
					t.Fatalf("covered upload behind a gateway: %d %s, want the body refused rather than the rate limit", upload.Status, upload.Body)
				}
			}
			uncovered.expectError(t, 429, "too_many_requests", "Maximum number of requests reached. Please try again later.")
		})
	}
}
