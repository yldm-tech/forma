package clientapi

import (
	"encoding/json"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/linksurvey"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

func (e *env) post(t *testing.T, path string, body any) result {
	t.Helper()
	e.clock.nextWindow()
	return e.do("POST", path, body)
}

func respond(ws uuid.UUID, version string) string {
	return "/api/" + version + "/client/" + ws.String() + "/responses"
}

// emailToken is a link survey email verification token as the survey page issues it: the address encrypted as a link value.
func (e *env) emailToken(t *testing.T, surveyID uuid.UUID, email string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte(e.cfg.LinkSurveyEmailSecret())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := secret.EncryptLinkValue(email, e.cfg.EncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(map[string]any{
		"email": encrypted, "surveyId": surveyID.String(), "purpose": "link_survey_email_verification", "exp": time.Now().Add(time.Hour).Unix(),
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (e *env) responseData(t *testing.T, id any) map[string]any {
	t.Helper()
	var raw []byte
	if err := e.db.Raw(`SELECT data FROM responses WHERE id = ?`, id).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	return d
}

// forma2's v1 create checks, in order: the survey's workspace, its status, the PIN, then the verified-email gate, which stamps the address from its token.
func TestCreateResponseGates(t *testing.T) {
	e := newEnv(t)
	ws, other := e.workspace(), e.workspace()
	foreign := e.survey(other)
	details := e.post(t, respond(ws, "v1"), map[string]any{"surveyId": foreign.String(), "finished": true, "data": map[string]any{}}).expectError(t, 400, "bad_request", "Survey is part of another workspace")
	if details["workspaceId"] != ws.String() {
		t.Fatalf("details %v", details)
	}
	paused := e.survey(ws, "status", "completed")
	e.post(t, respond(ws, "v2"), map[string]any{"surveyId": paused.String(), "finished": true, "data": map[string]any{}}).expectError(t, 403, "forbidden", "Survey is not accepting submissions")

	pinned := e.survey(ws, "pin", "1234")
	for _, v := range []string{"v1", "v2"} {
		details = e.post(t, respond(ws, v), map[string]any{"surveyId": pinned.String(), "finished": true, "data": map[string]any{}, "pinAuthToken": "forged"}).expectError(t, 403, "forbidden", "Survey is protected by a PIN")
		if details["surveyId"] != pinned.String() {
			t.Fatalf("details %v", details)
		}
	}
	token, err := linksurvey.NewPinToken(e.cfg, pinned, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": pinned.String(), "finished": true, "data": map[string]any{}, "pinAuthToken": token}); r.Status != 200 {
		t.Fatalf("with the PIN token: %d %s", r.Status, r.Body)
	}

	verified := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true WHERE id = ?`, verified)
	for _, v := range []string{"v1", "v2"} {
		e.post(t, respond(ws, v), map[string]any{"surveyId": verified.String(), "finished": true, "data": map[string]any{"verifiedEmail": "forged@example.com"}}).
			expectError(t, 403, "forbidden", "Survey requires email verification")
		url := webAppURL + "/s/" + verified.String() + "?verify=" + e.emailToken(t, verified, "real@example.com")
		r := e.post(t, respond(ws, v), map[string]any{"surveyId": verified.String(), "finished": true, "data": map[string]any{"verifiedEmail": "forged@example.com"}, "meta": map[string]any{"url": url}})
		if r.Status != 200 {
			t.Fatalf("%s with a token: %d %s", v, r.Status, r.Body)
		}
		if got := e.responseData(t, r.data(t)["id"])["verifiedEmail"]; got != "real@example.com" {
			t.Fatalf("%s stored verifiedEmail %v", v, got)
		}
	}
	other2 := e.survey(ws)
	url := webAppURL + "/s/" + verified.String() + "?verify=" + e.emailToken(t, other2, "real@example.com")
	e.post(t, respond(ws, "v1"), map[string]any{"surveyId": verified.String(), "finished": true, "data": map[string]any{}, "meta": map[string]any{"url": url}}).
		expectError(t, 403, "forbidden", "Survey requires email verification")
}

// One response per verified email (D-FG03): with email verification on, only one response per address, in any letter case, finishes. Unfinished responses never lock the address out: a respondent on a second device, or back after the runtime dropped its saved progress, starts a new response and can finish it. Once one has finished, creating another or finishing another is refused with 409 and details.code response_email_already_used on both creates and the update; racing creates and racing finishing updates for one address finish one response; other addresses, other surveys and a survey without the setting are not limited.
func TestCreateResponseOneResponsePerEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true, is_single_response_per_email_enabled = true WHERE id = ?`, sv)
	create := func(v, surveyID string, email string, finished bool) result {
		url := webAppURL + "/s/" + surveyID + "?verify=" + e.emailToken(t, uuid.MustParse(surveyID), email)
		return e.post(t, respond(ws, v), map[string]any{"surveyId": surveyID, "finished": finished, "data": map[string]any{}, "meta": map[string]any{"url": url}})
	}
	finish := func(id string) result {
		return e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+id, map[string]any{"finished": true, "data": map[string]any{}})
	}
	refused := func(what string, r result) {
		t.Helper()
		if details := r.expectError(t, 409, "conflict", "A response was already submitted with this email address"); details["code"] != "response_email_already_used" {
			t.Fatalf("%s: details %v", what, details)
		}
	}
	first := create("v2", sv.String(), "Real@Example.com", false)
	if first.Status != 200 {
		t.Fatalf("first: %d %s", first.Status, first.Body)
	}
	firstID := first.data(t)["id"].(string)
	// The respondent left it two days ago, past the runtime's 24-hour progress window, or opens the link on another device: a new response for the address is taken.
	e.exec(`UPDATE responses SET created_at = now() - interval '2 days', updated_at = now() - interval '2 days' WHERE id = ?`, firstID)
	second := create("v2", sv.String(), "real@example.COM", false)
	if second.Status != 200 {
		t.Fatalf("second device: %d %s", second.Status, second.Body)
	}
	if put := finish(second.data(t)["id"].(string)); put.Status != 200 {
		t.Fatalf("finish the second: %d %s", put.Status, put.Body)
	}
	// One response of the address has finished: neither another create nor finishing the earlier response goes through.
	for _, v := range []string{"v1", "v2"} {
		refused(v+" create, finished", create(v, sv.String(), "REAL@example.com", true))
	}
	refused("create, unfinished", create("v2", sv.String(), "real@example.com", false))
	refused("finish the first", finish(firstID))
	var finished int64
	if err := e.db.Raw(`SELECT count(*) FROM responses WHERE survey_id = ? AND finished`, sv).Scan(&finished).Error; err != nil {
		t.Fatal(err)
	}
	if finished != 1 {
		t.Fatalf("%d finished responses for one address", finished)
	}
	// An update that does not finish the earlier response still goes on, and so does another address.
	if put := e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+firstID, map[string]any{"data": map[string]any{}}); put.Status != 200 {
		t.Fatalf("update the unfinished one: %d %s", put.Status, put.Body)
	}
	if r := create("v2", sv.String(), "other@example.com", true); r.Status != 200 {
		t.Fatalf("another address: %d %s", r.Status, r.Body)
	}

	// Without email verification there is no address to count, and without the setting an address answers again.
	unverified := e.survey(ws)
	e.exec(`UPDATE surveys SET is_single_response_per_email_enabled = true WHERE id = ?`, unverified)
	for range 2 {
		if r := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": unverified.String(), "finished": true, "data": map[string]any{}}); r.Status != 200 {
			t.Fatalf("verification off: %d %s", r.Status, r.Body)
		}
	}
	unlimited := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true WHERE id = ?`, unlimited)
	for range 2 {
		if r := create("v2", unlimited.String(), "real@example.com", true); r.Status != 200 {
			t.Fatalf("setting off: %d %s", r.Status, r.Body)
		}
	}

	// Creates racing for one address: exactly one gets through.
	raced := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true, is_single_response_per_email_enabled = true WHERE id = ?`, raced)
	url := webAppURL + "/s/" + raced.String() + "?verify=" + e.emailToken(t, raced, "race@example.com")
	const racers = 8
	statuses := make(chan int, racers)
	var wg sync.WaitGroup
	for range racers {
		wg.Go(func() {
			statuses <- e.do("POST", respond(ws, "v2"), map[string]any{"surveyId": raced.String(), "finished": true, "data": map[string]any{}, "meta": map[string]any{"url": url}}).Status
		})
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for s := range statuses {
		counts[s]++
	}
	var stored int64
	if err := e.db.Raw(`SELECT count(*) FROM responses WHERE survey_id = ?`, raced).Scan(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if counts[200] != 1 || counts[409] != racers-1 || stored != 1 {
		t.Fatalf("racing creates: statuses %v, %d stored", counts, stored)
	}

	// Tabs racing to finish their responses for one address: exactly one finishes.
	tabs := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true, is_single_response_per_email_enabled = true WHERE id = ?`, tabs)
	ids := make([]string, racers)
	for i := range ids {
		r := create("v2", tabs.String(), "tabs@example.com", false)
		if r.Status != 200 {
			t.Fatalf("tab %d: %d %s", i, r.Status, r.Body)
		}
		ids[i] = r.data(t)["id"].(string)
	}
	finishes := make(chan int, racers)
	for _, id := range ids {
		wg.Go(func() { finishes <- finish(id).Status })
	}
	wg.Wait()
	close(finishes)
	counts = map[int]int{}
	for s := range finishes {
		counts[s]++
	}
	if err := e.db.Raw(`SELECT count(*) FROM responses WHERE survey_id = ? AND finished`, tabs).Scan(&finished).Error; err != nil {
		t.Fatal(err)
	}
	if counts[200] != 1 || counts[409] != racers-1 || finished != 1 {
		t.Fatalf("racing finishes: statuses %v, %d finished", counts, finished)
	}
}

// The v1 create, the path the SDK takes for identified users, runs its own order of gates: the survey's status, the single-use link (whose 400s name the workspace environmentId, as v1 did) and the answers each refuse on their own.
func TestCreateResponseV1Gates(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	for _, status := range []string{"completed", "paused", "draft"} {
		sv := e.survey(ws, "status", status)
		details := e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}}).expectError(t, 403, "forbidden", "Survey is not accepting submissions")
		if details["surveyId"] != sv.String() {
			t.Fatalf("%s: details %v", status, details)
		}
	}

	single := e.survey(ws, "single_use", `{"enabled":true,"isEncrypted":false}`)
	link := func(suID, suToken string) map[string]any {
		return map[string]any{"url": webAppURL + "/s/" + single.String() + "?suId=" + suID + "&suToken=" + suToken}
	}
	valid := survey.SingleUseSignature(string(e.cfg.EncryptionKeyText), single.String(), "su-1")
	for name, c := range map[string]struct {
		body    map[string]any
		message string
	}{
		"no single-use id":  {map[string]any{"meta": link("su-1", valid)}, "Missing single use id"},
		"no link":           {map[string]any{"singleUseId": "su-1"}, "Missing or invalid URL in response metadata"},
		"forged signature":  {map[string]any{"singleUseId": "su-1", "meta": link("su-1", "forged")}, "Invalid single use id"},
		"another link's id": {map[string]any{"singleUseId": "su-2", "meta": link("su-1", valid)}, "Invalid single use id"},
	} {
		body := map[string]any{"surveyId": single.String(), "finished": true, "data": map[string]any{}}
		maps.Copy(body, c.body)
		details := e.post(t, respond(ws, "v1"), body).expectError(t, 400, "bad_request", c.message)
		if details["surveyId"] != single.String() || details["environmentId"] != ws.String() || details["workspaceId"] != nil {
			t.Fatalf("%s: details %v", name, details)
		}
	}
	body := map[string]any{"surveyId": single.String(), "finished": true, "data": map[string]any{}, "singleUseId": "su-1", "meta": link("su-1", valid)}
	r := e.post(t, respond(ws, "v1"), body)
	if r.Status != 200 {
		t.Fatalf("single-use link: %d %s", r.Status, r.Body)
	}
	e.post(t, respond(ws, "v1"), body).expectError(t, 409, "conflict", "Response already submitted for this single-use link")

	required := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[
		{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":true,"inputType":"text"},
		{"id":"q2","type":"openText","headline":{"default":"Q2"},"required":false,"inputType":"email"}]}]`)
	details := e.post(t, respond(ws, "v1"), map[string]any{"surveyId": required.String(), "finished": true, "data": map[string]any{"q1": "", "q2": "not an address"}}).expectError(t, 400, "bad_request", "Validation failed")
	if details["response.data.q1"] == nil || details["response.data.q2"] == nil {
		t.Fatalf("details %v", details)
	}
	if n := e.count(`SELECT count(*) FROM responses WHERE survey_id = ?`, required); n != 0 {
		t.Fatalf("%d responses written", n)
	}
}

// forma2's verifyResponseRecaptcha on both versions, and verifyRecaptchaToken's fail-closed rules.
func TestCreateResponseRecaptcha(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RecaptchaSiteKey, c.RecaptchaSecretKey = "site", "secret" })
	ws := e.workspace()
	sv := e.survey(ws, "recaptcha", `{"enabled":true,"threshold":0.5}`)
	body := func(token any) map[string]any {
		b := map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}}
		if token != nil {
			b["recaptchaToken"] = token
		}
		return b
	}
	for _, v := range []string{"v1", "v2"} {
		details := e.post(t, respond(ws, v), body(nil)).expectError(t, 400, "bad_request", "Missing recaptcha token")
		if details["code"] != codeRecaptchaFailed {
			t.Fatalf("details %v", details)
		}
	}
	cases := []struct {
		status int
		answer string
		ok     bool
	}{
		{200, `{"success":true,"score":0.9}`, true},
		{200, `{"success":true,"score":0.5}`, true},
		{200, `{"success":true}`, true},
		{200, `{"success":true,"score":0.4}`, false},
		{200, `{"success":false}`, false},
		{500, `{"success":true,"score":0.9}`, false},
		{200, `not json`, false},
	}
	for _, c := range cases {
		e.recaptchaAnswers(c.status, c.answer)
		r := e.post(t, respond(ws, "v2"), body("token-1"))
		if c.ok && r.Status != 200 {
			t.Fatalf("%s: %d %s", c.answer, r.Status, r.Body)
		}
		if !c.ok {
			r.expectError(t, 400, "bad_request", "reCAPTCHA verification failed")
		}
	}
	e.captcha.mu.Lock()
	form := e.captcha.forms[0]
	e.captcha.mu.Unlock()
	if form.Get("secret") != "secret" || form.Get("response") != "token-1" {
		t.Fatalf("siteverify form %v", form)
	}

	e.exec(`DELETE FROM organization_billing`)
	details := e.post(t, respond(ws, "v1"), body("token-1")).expectError(t, 404, "not_found", "Organization not found")
	if details["resource_id"] != nil {
		t.Fatalf("details %v", details)
	}

	t.Run("without keys", func(t *testing.T) {
		e := newEnv(t)
		ws := e.workspace()
		sv := e.survey(ws, "recaptcha", `{"enabled":true,"threshold":0.1}`)
		e.recaptchaAnswers(200, `{"success":true,"score":1}`)
		e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": true, "data": map[string]any{}, "recaptchaToken": "t"}).
			expectError(t, 400, "bad_request", "reCAPTCHA verification failed")
	})
}

const uploadBlocks = `[{"id":"b1","name":"Main","elements":[
	{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":false,"inputType":"text"},
	{"id":"choice","type":"multipleChoiceSingle","headline":{"default":"Pick"},"required":false,"choices":[{"id":"a","label":{"default":"A"}},{"id":"other","label":{"default":"Other"}}]},
	{"id":"files","type":"fileUpload","headline":{"default":"Files"},"required":false,"allowedFileExtensions":["pdf"]}]}]`

// forma2's validateClientFileUploads: an upload answer holds URLs of files stored for that workspace, survey and element only.
func TestCreateResponseFileUploads(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws, "blocks", uploadBlocks)
	scoped := "/storage/" + ws.String() + "/private/surveys/" + sv.String() + "/elements/files/report--fid--" + uuid.NewString() + ".pdf"
	cases := map[string]bool{
		scoped:                          true,
		webAppURL + scoped:              true,
		"https://evil.example" + scoped: false,
		strings.Replace(scoped, ws.String(), uuid.NewString(), 1): false,
		strings.Replace(scoped, "/files/", "/q1/", 1):             false,
		strings.Replace(scoped, ".pdf", ".png", 1):                false,
		"/storage/" + ws.String() + "/private/report.pdf":         false,
	}
	for url, ok := range cases {
		r := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{"files": []string{url}}})
		if ok && r.Status != 200 {
			t.Fatalf("%s: %d %s", url, r.Status, r.Body)
		}
		if !ok {
			r.expectError(t, 400, "bad_request", "Invalid file upload response")
		}
	}
	e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{"files": "not a list"}}).
		expectError(t, 400, "bad_request", "Invalid file upload response")
}

// An update is held to the same upload rule as a create, so a PUT cannot attach a file stored for another workspace, survey or element.
func TestUpdateResponseFileUploads(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws, "blocks", uploadBlocks)
	id := e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{"q1":"first"}'::jsonb) RETURNING id`, sv)
	scoped := "/storage/" + ws.String() + "/private/surveys/" + sv.String() + "/elements/files/report--fid--" + uuid.NewString() + ".pdf"
	for _, version := range []string{"v1", "v2"} {
		for _, url := range []string{
			strings.Replace(scoped, ws.String(), uuid.NewString(), 1),
			strings.Replace(scoped, sv.String(), uuid.NewString(), 1),
			strings.Replace(scoped, "/files/", "/q1/", 1),
			"/storage/" + ws.String() + "/private/report.pdf",
			"https://evil.example" + scoped,
		} {
			e.clock.nextWindow()
			e.do("PUT", "/api/"+version+"/client/"+ws.String()+"/responses/"+id.String(), map[string]any{"data": map[string]any{"files": []string{url}}}).
				expectError(t, 400, "bad_request", "Invalid file upload response")
		}
	}
	if d := e.responseData(t, id); d["files"] != nil {
		t.Fatalf("data %v", d)
	}
	e.clock.nextWindow()
	if r := e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+id.String(), map[string]any{"data": map[string]any{"files": []string{scoped}}}); r.Status != 200 {
		t.Fatalf("scoped upload: %d %s", r.Status, r.Body)
	}
}

// v2 refuses an "Other" text over 250 characters; v1 never checked it.
func TestCreateResponseOtherOptionLength(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws, "blocks", uploadBlocks)
	long := strings.Repeat("x", 251)
	details := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{"choice": long}}).
		expectError(t, 400, "bad_request", "Response exceeds character limit")
	if details["questionId"] != "choice" {
		t.Fatalf("details %v", details)
	}
	if r := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{"choice": strings.Repeat("x", 250)}}); r.Status != 200 {
		t.Fatalf("250 characters: %d %s", r.Status, r.Body)
	}
	if r := e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{"choice": long}}); r.Status != 200 {
		t.Fatalf("v1: %d %s", r.Status, r.Body)
	}
}

// The meta is rebuilt from the request: the user agent and the CDN's country, never the client's claims.
func TestCreateResponseMeta(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws)
	e.clock.nextWindow()
	r := e.do("POST", respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{},
		"meta": map[string]any{"url": "https://x.example/p?a=1", "source": "link", "country": "XX", "ipAddress": "1.2.3.4", "userAgent": map[string]any{"browser": "Fake"}, "screenWidth": 1280, "timezone": "Europe/Berlin"}},
		"CF-IPCountry", "JP")
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	var raw []byte
	_ = e.db.Raw(`SELECT meta FROM responses WHERE id = ?`, r.data(t)["id"]).Row().Scan(&raw)
	var meta map[string]any
	_ = json.Unmarshal(raw, &meta)
	ua := meta["userAgent"].(map[string]any)
	if meta["country"] != "JP" || meta["ipAddress"] != nil || ua["browser"] != "Chrome" || ua["os"] != "macOS" || ua["device"] != "desktop" ||
		meta["url"] != "https://x.example/p?a=1" || meta["screenWidth"] != 1280.0 || meta["timezone"] != "Europe/Berlin" {
		t.Fatalf("meta %v", meta)
	}
}

// v2's contactId, the token an identify answered or a contact id, attaches the response only to a contact of the workspace; v1's userId only to an existing contact.
func TestCreateResponseContacts(t *testing.T) {
	e := newEnv(t)
	ws, other := e.workspace(), e.workspace()
	sv := e.survey(ws)
	mineState := stateData(t, e.identify(t, ws, map[string]any{"userId": "mine"}))
	mineToken, mine := mineState["contactId"], e.contactOf(t, ws, mineState).String()
	theirsState := stateData(t, e.identify(t, other, map[string]any{"userId": "theirs"}))
	theirsToken, theirs := theirsState["contactId"], e.contactOf(t, other, theirsState).String()
	contactOf := func(r result) any {
		var c *string
		_ = e.db.Raw(`SELECT contact_id::text FROM responses WHERE id = ?`, r.data(t)["id"]).Row().Scan(&c)
		if c == nil {
			return nil
		}
		return *c
	}
	for _, ref := range []any{mineToken, mine} {
		if got := contactOf(e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "contactId": ref})); got != mine {
			t.Fatalf("own contact by %v: %v", ref, got)
		}
	}
	for _, ref := range []any{theirsToken, theirs, "ct1.not-a-token"} {
		if got := contactOf(e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "contactId": ref})); got != nil {
			t.Fatalf("another workspace's contact by %v: %v", ref, got)
		}
	}
	if got := contactOf(e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "userId": "mine"})); got != mine {
		t.Fatalf("v1 userId: %v", got)
	}
	if got := contactOf(e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "userId": "nobody"})); got != nil {
		t.Fatalf("v1 unknown userId: %v", got)
	}
	if n := e.count(`SELECT count(*) FROM contacts WHERE workspace_id = ?`, ws); n != 1 {
		t.Fatalf("the response path created contacts: %d", n)
	}
	// A display may be answered only by its own survey's response.
	otherSurvey := e.survey(ws)
	display := e.insert(`INSERT INTO displays (survey_id) VALUES (?) RETURNING id`, otherSurvey)
	e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "displayId": display.String()}).
		expectError(t, 400, "bad_request", "Display "+display.String()+" is associated with a different survey")
}

// forma2's putResponseHandler: the response must be one of the workspace's, the PIN applies, a client's verifiedEmail is dropped, and the answers are validated merged over the stored ones.
func TestUpdateResponse(t *testing.T) {
	e := newEnv(t)
	ws, other := e.workspace(), e.workspace()
	sv := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[
		{"id":"q1","type":"openText","headline":{"default":"Q1"},"required":true,"inputType":"text"},
		{"id":"q2","type":"openText","headline":{"default":"Q2"},"required":false,"inputType":"email"}]}]`)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true WHERE id = ?`, sv)
	id := e.insert(`INSERT INTO responses (survey_id, data, language) VALUES (?, '{"q1":"first","verifiedEmail":"real@example.com"}'::jsonb, 'de-DE') RETURNING id`, sv)
	put := func(ws uuid.UUID, body any) result {
		e.clock.nextWindow()
		return e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+id.String(), body)
	}
	details := put(other, map[string]any{"finished": true}).expectError(t, 404, "not_found", "Response not found")
	if details["resource_id"] != id.String() {
		t.Fatalf("details %v", details)
	}
	put(ws, map[string]any{"data": map[string]any{"q2": "not an address"}}).expectError(t, 400, "bad_request", "Validation failed")
	// The language falls back to the stored one.
	got := put(ws, map[string]any{"data": map[string]any{"q1": ""}}).expectError(t, 400, "bad_request", "Validation failed")
	if got["response.data.q1"] != "Bitte füllen Sie dieses Feld aus" {
		t.Fatalf("details %v", got)
	}
	if r := put(ws, map[string]any{"data": map[string]any{"q2": "a@b.co", "verifiedEmail": "forged@example.com"}, "finished": true}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if d := e.responseData(t, id); d["verifiedEmail"] != "real@example.com" || d["q1"] != "first" || d["q2"] != "a@b.co" {
		t.Fatalf("data %v", d)
	}
	put(ws, map[string]any{"finished": true}).expectError(t, 400, "bad_request", "Response is already finished")

	pinned := e.survey(ws, "pin", "1234")
	pid := e.insert(`INSERT INTO responses (survey_id) VALUES (?) RETURNING id`, pinned)
	e.clock.nextWindow()
	e.do("PUT", "/api/v2/client/"+ws.String()+"/responses/"+pid.String(), map[string]any{"finished": true}).expectError(t, 403, "forbidden", "Survey is protected by a PIN")
}

// NF-01: a link survey page sends the verification token it was opened with, and an update whose token proves another address than the one the response was created with is refused, so a second verified respondent whose page went on with the first one's progress cannot add to or finish the first one's response. An update without a token (the SDK, pages from before the token was sent) or with one that proves no address goes through as before.
func TestUpdateResponseVerifiedEmail(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws)
	e.exec(`UPDATE surveys SET is_verify_email_enabled = true WHERE id = ?`, sv)
	create := func(email string) string {
		t.Helper()
		url := webAppURL + "/s/" + sv.String() + "?verify=" + e.emailToken(t, sv, email)
		r := e.post(t, respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}, "meta": map[string]any{"url": url}})
		if r.Status != 200 {
			t.Fatalf("create: %d %s", r.Status, r.Body)
		}
		return r.data(t)["id"].(string)
	}
	put := func(version, id string, body map[string]any) result {
		e.clock.nextWindow()
		return e.do("PUT", "/api/"+version+"/client/"+ws.String()+"/responses/"+id, body)
	}

	id := create("a@example.com")
	for _, v := range []string{"v1", "v2"} {
		details := put(v, id, map[string]any{"data": map[string]any{}, "finished": true, "verifyToken": e.emailToken(t, sv, "b@example.com")}).
			expectError(t, 403, "forbidden", "Response was verified with another email")
		if details["surveyId"] != sv.String() {
			t.Fatalf("details %v", details)
		}
	}
	if d := e.responseData(t, id); d["verifiedEmail"] != "a@example.com" {
		t.Fatalf("data %v", d)
	}
	var finished bool
	if err := e.db.Raw(`SELECT finished FROM responses WHERE id = ?`, id).Scan(&finished).Error; err != nil || finished {
		t.Fatalf("a refused update finished the response: %v %v", finished, err)
	}
	// The address's own token, in any letter case, goes through.
	if r := put("v1", id, map[string]any{"data": map[string]any{}, "verifyToken": e.emailToken(t, sv, "A@Example.com")}); r.Status != 200 {
		t.Fatalf("own token: %d %s", r.Status, r.Body)
	}
	// No token, a null one, a token of another survey or a forged one prove no address and are not checked.
	other := e.survey(ws)
	for name, token := range map[string]any{"absent": nil, "null": nil, "other survey": e.emailToken(t, other, "b@example.com"), "forged": "forged"} {
		body := map[string]any{"data": map[string]any{}}
		if name != "absent" {
			body["verifyToken"] = token
		}
		if r := put("v1", id, body); r.Status != 200 {
			t.Fatalf("%s token: %d %s", name, r.Status, r.Body)
		}
	}
	put("v1", id, map[string]any{"verifyToken": 42}).expectError(t, 400, "bad_request", "Fields are missing or incorrectly formatted")

	// A response created before the survey verified emails carries no address, so no token proves it.
	plain := e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{}'::jsonb) RETURNING id`, sv)
	put("v1", plain.String(), map[string]any{"verifyToken": e.emailToken(t, sv, "a@example.com")}).
		expectError(t, 403, "forbidden", "Response was verified with another email")

	// Without email verification the token is ignored.
	unverified := e.survey(ws)
	uid := e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{"verifiedEmail":"a@example.com"}'::jsonb) RETURNING id`, unverified)
	if r := put("v1", uid.String(), map[string]any{"verifyToken": e.emailToken(t, unverified, "b@example.com")}); r.Status != 200 {
		t.Fatalf("verification off: %d %s", r.Status, r.Body)
	}

	if r := put("v2", id, map[string]any{"finished": true, "verifyToken": e.emailToken(t, sv, "a@example.com")}); r.Status != 200 {
		t.Fatalf("finish with own token: %d %s", r.Status, r.Body)
	}
}

// An unfinished response stored before the phone check narrowed to 7 to 15 digits keeps an answer that check now refuses. Updates that send other answers still go through; sending that answer again is refused.
func TestUpdateResponseStoredAnswers(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws, "blocks", `[{"id":"b1","name":"Main","elements":[
		{"id":"phone","type":"openText","headline":{"default":"Phone"},"required":false,"inputType":"phone"},
		{"id":"contact","type":"contactInfo","headline":{"default":"Contact"},"required":false,"firstName":{"show":true},"lastName":{"show":true},"email":{"show":true},"phone":{"show":true},"company":{"show":true}},
		{"id":"q2","type":"openText","headline":{"default":"Q2"},"required":false,"inputType":"email"}]}]`)
	id := e.insert(`INSERT INTO responses (survey_id, data) VALUES (?, '{"phone":"123-45","contact":["Ann","Lee","a@b.co","123-45",""]}'::jsonb) RETURNING id`, sv)
	put := func(body any) result {
		e.clock.nextWindow()
		return e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+id.String(), body)
	}
	if r := put(map[string]any{"data": map[string]any{"q2": "a@b.co"}}); r.Status != 200 {
		t.Fatalf("update of another answer: %d %s", r.Status, r.Body)
	}
	if r := put(map[string]any{"ttc": map[string]any{"q2": 1200}}); r.Status != 200 {
		t.Fatalf("update without data: %d %s", r.Status, r.Body)
	}
	details := put(map[string]any{"data": map[string]any{"phone": "123-45", "q2": "not an address"}}).expectError(t, 400, "bad_request", "Validation failed")
	if details["response.data.phone"] == nil || details["response.data.q2"] == nil || details["response.data.contact"] != nil {
		t.Fatalf("details %v", details)
	}
	if r := put(map[string]any{"data": map[string]any{"phone": "+1 555 123 4567"}, "finished": true}); r.Status != 200 {
		t.Fatalf("corrected answer: %d %s", r.Status, r.Body)
	}
	if d := e.responseData(t, id); d["phone"] != "+1 555 123 4567" || d["q2"] != "a@b.co" {
		t.Fatalf("data %v", d)
	}
}

// forma2's sendToPipeline resolved the respondent's locale from Accept-Language (findMatchingLocale, the best of the shipped locales) and queued it with the job, so follow-up emails are localized; without the header it is the default locale.
func TestResponsePipelineLocale(t *testing.T) {
	e := newEnv(t)
	ws := e.workspace()
	sv := e.survey(ws)
	localeOf := func(id any, event string) string {
		t.Helper()
		var locale string
		if err := e.db.Raw(`SELECT args->>'locale' FROM river_job WHERE kind = 'response-pipeline.process' AND args->'response'->>'id' = ? AND args->>'event' = ?`, id, event).Row().Scan(&locale); err != nil {
			t.Fatalf("%s job: %v", event, err)
		}
		return locale
	}

	e.clock.nextWindow()
	r := e.do("POST", respond(ws, "v2"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}}, "Accept-Language", "fr-FR,ja-JP;q=0.9")
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	id := r.data(t)["id"].(string)
	if got := localeOf(id, "responseCreated"); got != "ja-JP" {
		t.Fatalf("create locale %q", got)
	}
	e.clock.nextWindow()
	if r := e.do("PUT", "/api/v1/client/"+ws.String()+"/responses/"+id, map[string]any{"finished": true}, "Accept-Language", "zh-TW"); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got := localeOf(id, "responseUpdated"); got != "zh-Hant-TW" {
		t.Fatalf("update locale %q", got)
	}

	r = e.post(t, respond(ws, "v1"), map[string]any{"surveyId": sv.String(), "finished": false, "data": map[string]any{}})
	if r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if got := localeOf(r.data(t)["id"], "responseCreated"); got != "en-US" {
		t.Fatalf("default locale %q", got)
	}
}
