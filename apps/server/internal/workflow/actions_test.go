package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mrkdwn"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/responsemail"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

const (
	testSlackURL   = "https://hooks.slack.com/services/T000/B000/XXXXXXXXXXXXXXXX"
	testSigningKey = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
)

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	c, err := secret.NewCipher(config.Config{EncryptionKey: bytes.Repeat([]byte{9}, 32), EncryptionKeyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func httpNode(id, url string, headers ...any) map[string]any {
	if headers == nil {
		headers = []any{}
	}
	return map[string]any{"id": id, "type": "action", "actionType": ActionHTTPRequest, "config": map[string]any{
		"method": "POST", "url": url, "headers": headers, "body": `{"answer":"{{answers.q1}}"}`, "signingSecret": nil,
	}}
}

func chatNode(id, actionType string, webhookURL map[string]any, message string) map[string]any {
	return map[string]any{"id": id, "type": "action", "actionType": actionType, "config": map[string]any{"webhookUrl": webhookURL, "message": message}}
}

func tagNode(id, tagID string) map[string]any {
	return map[string]any{"id": id, "type": "action", "actionType": ActionTagResponse, "config": map[string]any{"tagId": tagID}}
}

func attributeNode(id, key, value string) map[string]any {
	return map[string]any{"id": id, "type": "action", "actionType": ActionSetContactAttribute, "config": map[string]any{"attributeKey": key, "value": value}}
}

// chain is testDefinition with the nodes run one after another after the trigger.
func chain(nodes ...map[string]any) map[string]any {
	d := testDefinition()
	list := make([]any, len(nodes))
	edges := []any{}
	previous := "trigger"
	for i, n := range nodes {
		list[i] = n
		edges = append(edges, map[string]any{"id": "e-" + n["id"].(string), "source": previous, "target": n["id"]})
		previous = n["id"].(string)
	}
	d["nodes"], d["edges"] = list, edges
	return d
}

func issueAt(t *testing.T, issues []Issue, name, message string) {
	t.Helper()
	for _, is := range issues {
		if is.Name() == name && strings.Contains(is.Message, message) {
			return
		}
	}
	t.Fatalf("no issue %s %q in %+v", name, message, issues)
}

func TestActionDefinitions(t *testing.T) {
	t.Parallel()
	ws := uuid.New()
	c := testCipher(t)
	sealed := func(d map[string]any) *Definition {
		t.Helper()
		def, issues := ParseDefinition(raw(t, d))
		if len(issues) > 0 {
			t.Fatalf("%+v", issues)
		}
		if issues, err := sealSecrets(c, ws, def, nil); err != nil || len(issues) > 0 {
			t.Fatalf("seal: %v %+v", err, issues)
		}
		return def
	}

	t.Run("every action is executable once configured, and a stored definition parses to itself", func(t *testing.T) {
		t.Parallel()
		def := sealed(chain(
			httpNode("h", "https://api.example.com/hook", map[string]any{"name": "X-Plain", "value": "1"}, map[string]any{"name": "Authorization", "secret": map[string]any{"value": "Bearer abcdefghijkl"}}),
			chatNode("s", ActionSlackMessage, map[string]any{"value": testSlackURL}, "New response to {{survey.name}}"),
			chatNode("d", ActionDiscordMessage, map[string]any{"value": "https://discord.com/api/webhooks/1/abc"}, "Hi"),
			chatNode("m", ActionTeamsMessage, map[string]any{"value": "https://acme.webhook.office.com/webhookb2/abc"}, "Hi"),
			tagNode("t", uuid.NewString()),
			attributeNode("a", "plan", "{{answers.q1}}"),
		))
		stored := mustJSON(def)
		if strings.Contains(string(stored), "Bearer abcdefghijkl") || strings.Contains(string(stored), "XXXXXXXXXXXXXXXX") {
			t.Fatalf("a secret was stored in plaintext: %s", stored)
		}
		again, issues := ParseExecutable(stored)
		if len(issues) > 0 || string(mustJSON(again)) != string(stored) {
			t.Fatalf("%+v\n%s\n%s", issues, stored, mustJSON(again))
		}
		steps, err := Plan(again)
		if err != nil || len(steps) != 6 || steps[0].Type != ActionHTTPRequest || steps[5].Node.Attribute.AttributeKey != "plan" {
			t.Fatalf("%+v %v", steps, err)
		}
		if h := again.Nodes[0].HTTP.Headers[1].Secret; h.Value != nil || h.Sealed == "" || h.Hint != "…ijkl" {
			t.Fatalf("header secret %+v", h)
		}
		if again.Nodes[1].Chat.WebhookURL.Hint != "hooks.slack.com/…XXXX" {
			t.Fatalf("webhook hint %q", again.Nodes[1].Chat.WebhookURL.Hint)
		}
	})

	t.Run("an unconfigured action is storable but not executable", func(t *testing.T) {
		t.Parallel()
		d := chain(
			map[string]any{"id": "h", "type": "action", "actionType": ActionHTTPRequest, "config": map[string]any{
				"method": "GET", "url": "ftp://example.com", "headers": []any{
					map[string]any{"name": "", "value": "x"}, map[string]any{"name": "bad name", "value": "x"}, map[string]any{"name": "Webhook-Signature", "value": "x"},
					map[string]any{"name": "X-Token", "secret": map[string]any{}}, map[string]any{"name": "X-Line", "value": "a\r\nInjected: 1"},
				}, "body": `{"a":1}`, "signingSecret": map[string]any{},
			}},
			chatNode("s", ActionSlackMessage, map[string]any{}, " "),
			tagNode("t", "not-a-uuid"),
			attributeNode("a", "", ""),
		)
		if _, issues := ParseDefinition(raw(t, d)); len(issues) > 0 {
			t.Fatalf("draft: %+v", issues)
		}
		_, issues := ParseExecutable(raw(t, d))
		issueAt(t, issues, "nodes.0.config.url", "http_request node h has an invalid url")
		issueAt(t, issues, "nodes.0.config.headers.0.name", "is missing headers.0.name")
		issueAt(t, issues, "nodes.0.config.headers.1.name", "has an invalid headers.1.name")
		issueAt(t, issues, "nodes.0.config.headers.2.name", "has an invalid headers.2.name")
		issueAt(t, issues, "nodes.0.config.headers.3.secret", "is missing headers.3.secret")
		issueAt(t, issues, "nodes.0.config.headers.4.value", "has an invalid headers.4.value")
		issueAt(t, issues, "nodes.0.config.body", "has an invalid body")
		issueAt(t, issues, "nodes.0.config.signingSecret", "is missing signingSecret")
		issueAt(t, issues, "nodes.1.config.webhookUrl", "slack_message node s is missing webhookUrl")
		issueAt(t, issues, "nodes.1.config.message", "is missing message")
		issueAt(t, issues, "nodes.2.config.tagId", "tag_response node t has an invalid tagId")
		issueAt(t, issues, "nodes.3.config.attributeKey", "is missing attributeKey")
		issueAt(t, issues, "nodes.3.config.value", "is missing value")

		blank := chain(httpNode("h", " "))
		blank["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["body"] = "{not json"
		_, issues = ParseExecutable(raw(t, blank))
		issueAt(t, issues, "nodes.0.config.url", "is missing url")
		issueAt(t, issues, "nodes.0.config.body", "has an invalid body")
	})

	t.Run("refuses malformed action configs", func(t *testing.T) {
		t.Parallel()
		unknown := chain(map[string]any{"id": "x", "type": "action", "actionType": "run_script", "config": map[string]any{}})
		_, issues := ParseDefinition(raw(t, unknown))
		issueAt(t, issues, "nodes.0.actionType", "Invalid input")

		method := chain(httpNode("h", "https://example.com"))
		method["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["method"] = "TRACE"
		_, issues = ParseDefinition(raw(t, method))
		issueAt(t, issues, "nodes.0.config.method", `expected one of "GET"|"POST"|"PUT"|"PATCH"|"DELETE"`)

		headers := []any{map[string]any{"name": "A", "value": "1", "secret": map[string]any{"value": "2"}}, map[string]any{"name": "B"}}
		for range maxHeaders {
			headers = append(headers, map[string]any{"name": "C", "value": "1"})
		}
		_, issues = ParseDefinition(raw(t, chain(httpNode("h", "https://example.com", headers...))))
		issueAt(t, issues, "nodes.0.config.headers.0", "either a value or a secret, not both")
		issueAt(t, issues, "nodes.0.config.headers.1", "needs a value or a secret")
		issueAt(t, issues, "nodes.0.config.headers", "Too big: expected array to have <=20 items")

		both := chain(chatNode("s", ActionSlackMessage, map[string]any{"value": testSlackURL, "sealed": "abc"}, "Hi"))
		_, issues = ParseDefinition(raw(t, both))
		issueAt(t, issues, "nodes.0.config.webhookUrl", "either a new value or its sealed form")

		noConfig := chain(map[string]any{"id": "t", "type": "action", "actionType": ActionTagResponse, "config": "x"})
		_, issues = ParseDefinition(raw(t, noConfig))
		issueAt(t, issues, "nodes.0.config", "expected object, received string")
	})
}

func TestSealSecrets(t *testing.T) {
	t.Parallel()
	c := testCipher(t)
	ws := uuid.New()
	def := func(t *testing.T, d map[string]any) *Definition {
		t.Helper()
		parsed, issues := ParseDefinition(raw(t, d))
		if len(issues) > 0 {
			t.Fatalf("%+v", issues)
		}
		return parsed
	}
	header := map[string]any{"name": "Authorization", "secret": map[string]any{"value": "  Bearer abcdefghijkl  "}}
	d := def(t, chain(httpNode("h", "https://api.example.com/a", header)))
	d.Nodes[0].HTTP.SigningSecret = &SecretValue{Value: new(testSigningKey)}
	if issues, err := sealSecrets(c, ws, d, []any{"definition"}); err != nil || len(issues) > 0 {
		t.Fatalf("%v %+v", err, issues)
	}
	h := d.Nodes[0].HTTP
	if plain, err := openSecret(c, *h.Headers[0].Secret, headerSecretContext(ws, "https://API.example.com/other?x=1")); err != nil || plain != "Bearer abcdefghijkl" {
		t.Fatalf("the header does not open for its origin: %q %v", plain, err)
	}
	for _, other := range []struct {
		ws  uuid.UUID
		url string
	}{{uuid.New(), "https://api.example.com/a"}, {ws, "https://evil.example.com/a"}, {ws, "http://api.example.com/a"}, {ws, "https://api.example.com:8443/a"}} {
		if _, err := openSecret(c, *h.Headers[0].Secret, headerSecretContext(other.ws, other.url)); !errors.Is(err, errSecretUnavailable) {
			t.Fatalf("a header secret opened for %v", other)
		}
	}
	if _, err := openSecret(c, *h.SigningSecret, headerSecretContext(ws, h.URL)); err == nil {
		t.Fatal("the signing secret opened as a header secret")
	}

	// Sealed secrets kept as they are stay valid; a new URL host, or another workspace, asks for them again.
	if issues, err := sealSecrets(c, ws, d, []any{"definition"}); err != nil || len(issues) > 0 {
		t.Fatalf("resealing: %v %+v", err, issues)
	}
	d.Nodes[0].HTTP.URL = "https://evil.example.com/collect"
	issues, err := sealSecrets(c, ws, d, []any{"definition"})
	if err != nil || len(issues) != 2 {
		t.Fatalf("%v %+v", err, issues)
	}
	issueAt(t, issues, "definition.nodes.0.config.headers.0.secret", secretForOtherDestination)
	issueAt(t, issues, "definition.nodes.0.config.signingSecret", secretForOtherDestination)

	refused := def(t, chain(
		chatNode("s", ActionSlackMessage, map[string]any{"value": "https://attacker.example/services/x"}, "Hi"),
		chatNode("d", ActionDiscordMessage, map[string]any{"value": testSlackURL}, "Hi"),
		httpNode("h", "https://api.example.com"),
	))
	refused.Nodes[2].HTTP.SigningSecret = &SecretValue{Value: new("not-a-secret")}
	issues, err = sealSecrets(c, ws, refused, nil)
	if err != nil {
		t.Fatal(err)
	}
	issueAt(t, issues, "nodes.0.config.webhookUrl", "Enter the incoming-webhook URL Slack gives you.")
	issueAt(t, issues, "nodes.1.config.webhookUrl", "Enter the incoming-webhook URL Discord gives you.")
	issueAt(t, issues, "nodes.2.config.signingSecret", signingSecretInvalid)

	// A header secret is a header value: a line break in it would be refused by every request.
	broken := def(t, chain(httpNode("h", "https://api.example.com", map[string]any{"name": "X-Key", "secret": map[string]any{"value": "abcdefghijkl\r\nX-Evil: 1"}})))
	issues, err = sealSecrets(c, ws, broken, nil)
	if err != nil {
		t.Fatal(err)
	}
	issueAt(t, issues, "nodes.0.config.headers.0.secret", headerValueControlChars)

	// A chat URL sealed for one chat does not open for another, and an emptied secret stays empty.
	slack := def(t, chain(chatNode("s", ActionSlackMessage, map[string]any{"value": testSlackURL}, "Hi")))
	if issues, err := sealSecrets(c, ws, slack, nil); err != nil || len(issues) > 0 {
		t.Fatalf("%v %+v", err, issues)
	}
	// A sealed value sent back keeps the hint of what it seals, whatever hint comes with it.
	wantHint := slack.Nodes[0].Chat.WebhookURL.Hint
	if wantHint != "hooks.slack.com/…XXXX" {
		t.Fatalf("hint %q", wantHint)
	}
	slack.Nodes[0].Chat.WebhookURL.Hint = "hooks.slack.com/#ceo-private"
	if issues, err := sealSecrets(c, ws, slack, nil); err != nil || len(issues) > 0 || slack.Nodes[0].Chat.WebhookURL.Hint != wantHint {
		t.Fatalf("a made-up hint was kept: %q %v %+v", slack.Nodes[0].Chat.WebhookURL.Hint, err, issues)
	}
	slack.Nodes[0].ActionType = ActionDiscordMessage
	if issues, _ := sealSecrets(c, ws, slack, nil); len(issues) != 1 {
		t.Fatalf("a Slack URL was accepted for Discord: %+v", issues)
	}
	slack.Nodes[0].Chat.WebhookURL = SecretValue{Value: new("  ")}
	if issues, _ := sealSecrets(c, ws, slack, nil); len(issues) != 0 || slack.Nodes[0].Chat.WebhookURL != (SecretValue{}) {
		t.Fatalf("an emptied secret: %+v %+v", issues, slack.Nodes[0].Chat.WebhookURL)
	}
}

func TestChatWebhookURLs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action, url string
		ok          bool
	}{
		{ActionSlackMessage, testSlackURL, true},
		{ActionSlackMessage, "http://hooks.slack.com/services/T/B/X", false},
		{ActionSlackMessage, "https://hooks.slack.com/services/", false},
		{ActionSlackMessage, "https://hooks.slack.com.evil.example/services/T/B/X", false},
		{ActionSlackMessage, "https://user:pw@hooks.slack.com/services/T/B/X", false},
		{ActionDiscordMessage, "https://discord.com/api/webhooks/123/token", true},
		{ActionDiscordMessage, "https://discordapp.com/api/webhooks/123/token", true},
		{ActionDiscordMessage, "https://discord.com/api/other/123", false},
		{ActionTeamsMessage, "https://acme.webhook.office.com/webhookb2/x", true},
		{ActionTeamsMessage, "https://prod-01.westus.logic.azure.com:443/workflows/x/triggers/manual/paths/invoke", true},
		{ActionTeamsMessage, "https://default1.environment.api.powerplatform.com/powerautomate/automations/direct/workflows/x", true},
		{ActionTeamsMessage, "https://webhook.office.com/x", false},
		{ActionTeamsMessage, "https://evil.example/.webhook.office.com", false},
		{ActionHTTPRequest, testSlackURL, false},
	} {
		if got := ValidChatWebhookURL(tc.action, tc.url); got != tc.ok {
			t.Errorf("ValidChatWebhookURL(%s, %s) = %v", tc.action, tc.url, got)
		}
	}
}

func testAnswers(t *testing.T) *Template {
	t.Helper()
	r := &response.Response{
		ID: uuid.MustParse("0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a11"), SurveyID: uuid.MustParse(testSurveyID), Finished: true,
		Contact:           &response.Contact{ID: uuid.MustParse("0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a12")},
		ContactAttributes: json.RawMessage(`{"email":"jane@example.com","plan":"pro"}`),
		Data:              json.RawMessage(`{"q1":"He said \"hi\" <b>","rating":9,"choices":["a","b"],"address":{"city":"Kyoto","zip":""}}`),
		Variables:         json.RawMessage(`{"score":42}`),
	}
	a, err := responsemail.Decode(r)
	if err != nil {
		t.Fatal(err)
	}
	return responseTemplate(r, &responsemail.Survey{Name: "NPS & more"}, a)
}

func TestTemplates(t *testing.T) {
	t.Parallel()
	tpl := testAnswers(t)
	plain := func(s string) string { return s }
	got := tpl.RenderText("{{survey.name}}: {{ answers.q1 }} ({{answers.rating}}/10, {{answers.choices}}) {{answers.address}} {{variables.score}} {{contact.attributes.plan}} {{contact.id}} {{answers.missing}}{{nope}}", plain)
	want := `NPS & more: He said "hi" <b> (9/10, a; b) city: Kyoto 42 pro 0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a12 `
	if got != want {
		t.Fatalf("text\n got %q\nwant %q", got, want)
	}
	if got := tpl.RenderText("<{{answers.q1}}>", mrkdwn.Escape); got != "<He said \"hi\" &lt;b\u200b&gt;>" {
		t.Fatalf("escaped %q", got)
	}

	body, err := tpl.RenderJSON(`{"id":"{{response.id}}","rating":"{{answers.rating}}","label":"Rating {{answers.rating}}","q1":"{{answers.q1}}","data":"{{response.data}}","n":[1,"{{variables.score}}"],"none":"{{answers.missing}}","finished":"{{response.finished}}"}`)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	if doc["rating"] != 9.0 || doc["label"] != "Rating 9" || doc["q1"] != `He said "hi" <b>` || doc["none"] != nil || doc["finished"] != true ||
		doc["data"].(map[string]any)["address"].(map[string]any)["city"] != "Kyoto" || doc["n"].([]any)[1] != 42.0 || doc["id"] != "0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a11" {
		t.Fatalf("rendered %s", body)
	}
	if strings.Contains(string(body), "\\u003c") {
		t.Fatalf("HTML was escaped: %s", body)
	}
	for _, bad := range []string{`{"a":`, `{"a":1} {"b":2}`, `{{answers.q1}}`} {
		if _, err := tpl.RenderJSON(bad); err == nil {
			t.Errorf("RenderJSON(%q) accepted", bad)
		}
	}

	test := testTemplate(uuid.New(), testSurveyID, "Feedback")
	if got := test.RenderText("{{survey.name}} {{answers.q1}} {{ response.id }}", plain); got != "Feedback {{answers.q1}} {{ response.id }}" {
		t.Fatalf("test template %q", got)
	}
	if body, err := test.RenderJSON(`{"a":"{{answers.q1}}","b":"x {{survey.name}}"}`); err != nil || string(body) != `{"a":"{{answers.q1}}","b":"x Feedback"}` {
		t.Fatalf("test body %s %v", body, err)
	}
}

// A choice answer's choiceId and default placeholders are the same whatever language the response was given in; an Other text keeps its text and becomes the other choice.
func TestChoicePlaceholders(t *testing.T) {
	t.Parallel()
	label := func(def, ja, zh string) surveydoc.I18nString {
		return surveydoc.I18nString{"default": def, "ja": ja, "zh-Hans": zh}
	}
	elements := []surveydoc.Element{
		{ID: "pmf", Type: surveydoc.ElementMultipleChoiceSingle, Choices: []surveydoc.Choice{
			{ID: "very", Label: label("Very disappointed", "とても残念", "非常失望")},
			{ID: "not", Label: label("Not disappointed", "残念ではない", "")},
			{ID: "other", Label: label("Other", "その他", "其他")},
		}},
		{ID: "tools", Type: surveydoc.ElementMultipleChoiceMulti, Choices: []surveydoc.Choice{
			{ID: "a", Label: label("Alpha", "アルファ", "阿尔法")}, {ID: "b", Label: label("Beta", "ベータ", "贝塔")},
		}},
		{ID: "order", Type: surveydoc.ElementRanking, Choices: []surveydoc.Choice{
			{ID: "x", Label: label("First", "一番", "第一")}, {ID: "y", Label: label("Second", "二番", "第二")},
		}},
		{ID: "note", Type: surveydoc.ElementOpenText},
	}
	render := func(lang, data, tmpl string) (string, map[string]any) {
		t.Helper()
		r := &response.Response{ID: uuid.New(), SurveyID: uuid.MustParse(testSurveyID), Data: json.RawMessage(data)}
		a, err := responsemail.Decode(r)
		if err != nil {
			t.Fatal(err)
		}
		tpl := &Template{Fixed: map[string]any{}, Answers: a, Attributes: map[string]any{}, Elements: elements, Language: lang}
		body, err := tpl.RenderJSON(`{"id":"{{answers.pmf.choiceId}}","label":"{{answers.pmf.default}}","tools":"{{answers.tools.choiceId}}","order":"{{answers.order.default}}"}`)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		return tpl.RenderText(tmpl, func(s string) string { return s }), doc
	}
	const tmpl = "{{answers.pmf.choiceId}} {{answers.pmf.default}} | {{answers.tools.choiceId}} | {{answers.tools.default}} | {{answers.order.choiceId}} | {{answers.note.default}}{{answers.missing.choiceId}}"
	const want = "very Very disappointed | a; b | Alpha; Beta | y; x | "
	for _, tc := range []struct{ lang, data string }{
		{"default", `{"pmf":"Very disappointed","tools":["Alpha","Beta"],"order":["Second","First"],"note":"hi"}`},
		{"ja", `{"pmf":"とても残念","tools":["アルファ","ベータ"],"order":["二番","一番"],"note":"hi"}`},
		{"zh-Hans", `{"pmf":"非常失望","tools":["阿尔法","贝塔"],"order":["第二","第一"],"note":"hi"}`},
		// A response whose language the survey no longer has still matches its labels in any language.
		{"default", `{"pmf":"非常失望","tools":["アルファ","Beta"],"order":["二番","First"],"note":"hi"}`},
	} {
		text, doc := render(tc.lang, tc.data, tmpl)
		if text != want {
			t.Errorf("%s %s:\n got %q\nwant %q", tc.lang, tc.data, text, want)
		}
		if doc["id"] != "very" || doc["label"] != "Very disappointed" || fmt.Sprint(doc["tools"]) != "[a b]" || fmt.Sprint(doc["order"]) != "[Second First]" {
			t.Errorf("%s JSON %v", tc.lang, doc)
		}
	}
	// zh-Hans has no translation of "Not disappointed", so the respondent saw, and the response stored, the default label.
	if text, _ := render("zh-Hans", `{"pmf":"Not disappointed"}`, "{{answers.pmf.choiceId}}"); text != "not" {
		t.Fatalf("untranslated label: %q", text)
	}
	// An Other text is the other choice, and its default stays as typed.
	if text, _ := render("ja", `{"pmf":"自由記述"}`, "{{answers.pmf.choiceId}}={{answers.pmf.default}}"); text != "other=自由記述" {
		t.Fatalf("other: %q", text)
	}
	// Without an other choice, an unknown value has no id and is left out of a list.
	if text, _ := render("ja", `{"tools":["アルファ","what"]}`, "{{answers.tools.choiceId}}|{{answers.tools.default}}"); text != "a|Alpha; what" {
		t.Fatalf("unknown: %q", text)
	}
	// An answer whose key really ends in .default is still that answer.
	if text, _ := render("default", `{"pmf.default":"stored"}`, "{{answers.pmf.default}}"); text != "stored" {
		t.Fatalf("stored key: %q", text)
	}
	// A test send keeps the placeholders as written.
	test := testTemplate(uuid.New(), testSurveyID, "Feedback")
	if got := test.RenderText("{{answers.pmf.choiceId}}", func(s string) string { return s }); got != "{{answers.pmf.choiceId}}" {
		t.Fatalf("test send %q", got)
	}
}

// A subject is rendered on one line: an answer cannot add a header, and the author's own line breaks are dropped as before.
func TestRenderSubject(t *testing.T) {
	t.Parallel()
	r := &response.Response{ID: uuid.New(), SurveyID: uuid.MustParse(testSurveyID), Data: json.RawMessage(`{"nps":3,"q1":"line\r\nBcc: x@evil.example\u2028end","long":"` + strings.Repeat("あ", 1200) + `"}`)}
	a, err := responsemail.Decode(r)
	if err != nil {
		t.Fatal(err)
	}
	tpl := responseTemplate(r, &responsemail.Survey{Name: "Q4"}, a)
	for _, tc := range []struct{ in, want string }{
		{"New {{survey.name}} feedback: NPS {{ answers.nps }}", "New Q4 feedback: NPS 3"},
		{"Re: {{answers.q1}}", "Re: line  Bcc: x@evil.example end"},
		{"New\r\nBcc: x@evil.example response {{answers.missing}}", "NewBcc: x@evil.example response"},
		{"Literal {{ not a placeholder", "Literal {{ not a placeholder"},
	} {
		if got := tpl.RenderSubject(tc.in); got != tc.want {
			t.Errorf("RenderSubject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := tpl.RenderSubject("{{answers.long}}")
	if n := len([]rune(long)); n != maxSubjectLength || !strings.HasSuffix(long, "…") {
		t.Fatalf("long subject of %d characters", n)
	}
}

func TestChatPayloads(t *testing.T) {
	t.Parallel()
	tpl := testAnswers(t)
	encode := func(v any) string {
		b, err := encodeJSON(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	slack := encode(chatPayload(ActionSlackMessage, tpl, "*New:* {{answers.q1}} <https://example.com|open>"))
	if slack != `{"text":"*New:* He said \"hi\" &lt;b`+"\u200b"+`&gt; <https://example.com|open>"}` {
		t.Fatalf("slack %s", slack)
	}
	discord := chatPayload(ActionDiscordMessage, tpl, strings.Repeat("x", 2500)).(map[string]any)
	if n := len([]rune(discord["content"].(string))); n != discordMaxContent || !strings.HasSuffix(discord["content"].(string), "…") {
		t.Fatalf("discord content of %d", n)
	}
	if string(mustJSON(discord["allowed_mentions"])) != `{"parse":[]}` {
		t.Fatalf("discord mentions %v", discord["allowed_mentions"])
	}
	teams := encode(chatPayload(ActionTeamsMessage, tpl, "Rating {{answers.rating}}"))
	if !strings.Contains(teams, `"contentType":"application/vnd.microsoft.card.adaptive"`) || !strings.Contains(teams, `"text":"Rating 9"`) {
		t.Fatalf("teams %s", teams)
	}

	// An answer's markdown is escaped for Discord and Teams, so it cannot post a disguised link or formatting; the template's own markup stays.
	r := &response.Response{ID: uuid.New(), SurveyID: uuid.MustParse(testSurveyID), Finished: true,
		Data: json.RawMessage(`{"q1":"[Your account is locked](https://evil.example) **now** <@&123> # x"}`)}
	answers, err := responsemail.Decode(r)
	if err != nil {
		t.Fatal(err)
	}
	evil := responseTemplate(r, &responsemail.Survey{Name: "NPS"}, answers)
	const escaped = `\[Your account is locked\]\(https://evil.example\) \*\*now\*\* \<@&123\> \# x`
	discordText := chatPayload(ActionDiscordMessage, evil, "**New:** {{answers.q1}}").(map[string]any)["content"]
	if discordText != "**New:** "+escaped {
		t.Fatalf("discord %q", discordText)
	}
	card := chatPayload(ActionTeamsMessage, evil, "**New:** {{answers.q1}}").(map[string]any)["attachments"].([]any)[0].(map[string]any)["content"].(map[string]any)
	if text := card["body"].([]any)[0].(map[string]any)["text"]; text != "**New:** "+escaped {
		t.Fatalf("teams %q", text)
	}
	if got := markdownEscape(`a\b-c_d~e|f` + "`"); got != `a\\b\-c\_d\~e\|f\`+"`" {
		t.Fatalf("escape %q", got)
	}

	// The same for Slack: an answer cannot notify the channel, mention anyone, post a disguised link or take on formatting; the template's mrkdwn, its link included, stays.
	r = &response.Response{ID: uuid.New(), SurveyID: uuid.MustParse(testSurveyID), Finished: true,
		Data: json.RawMessage(`{"q1":"<!channel> <!here> <@U123> @here <https://evil.example/login|Your Forma session expired> *urgent* _now_ ~x~ ` + "`code`" + ` > quote"}`)}
	if answers, err = responsemail.Decode(r); err != nil {
		t.Fatal(err)
	}
	evil = responseTemplate(r, &responsemail.Survey{Name: "NPS"}, answers)
	const z = "\u200b"
	slackText := chatPayload(ActionSlackMessage, evil, "*New:* {{answers.q1}} <https://example.com|open>").(map[string]any)["text"]
	want := "*New:* &lt;!channel" + z + "&gt; &lt;!here" + z + "&gt; &lt;@U123" + z + "&gt; @" + z + "here &lt;https://evil.example/login|Your Forma session expired" + z + "&gt; " +
		z + "*" + z + "urgent" + z + "*" + z + " " + z + "_" + z + "now" + z + "_" + z + " " + z + "~" + z + "x" + z + "~" + z + " " + z + "`" + z + "code" + z + "`" + z + " " + z + "&gt; quote" +
		" <https://example.com|open>"
	if slackText != want {
		t.Fatalf("slack\n got %q\nwant %q", slackText, want)
	}
}

// receiver records the requests an httptest server gets and answers each with the next status of a script, the last one repeating.
type receiver struct {
	mu       sync.Mutex
	statuses []int
	requests []*http.Request
	bodies   []string
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req.Clone(context.Background()))
	r.bodies = append(r.bodies, string(body))
	status := r.statuses[min(len(r.requests), len(r.statuses))-1]
	w.WriteHeader(status)
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func serve(t *testing.T, statuses ...int) (*receiver, string) {
	t.Helper()
	rec := &receiver{statuses: statuses}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

func testActions(t *testing.T, guard *ssrf.Guard) *actions {
	a := newActions(nil, guard, testCipher(t))
	a.retryWaits = []time.Duration{time.Millisecond, time.Millisecond}
	return a
}

func TestDeliverRetriesOnlyTransientFailures(t *testing.T) {
	t.Parallel()
	a := testActions(t, ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true}))
	env := actionEnv{retry: true}
	for _, tc := range []struct {
		name      string
		statuses  []int
		status    string
		requests  int
		permanent bool
		reason    string
	}{
		{"accepted", []int{http.StatusNoContent}, StepSucceeded, 1, false, ""},
		{"recovers after a 5xx", []int{http.StatusBadGateway, http.StatusOK}, StepSucceeded, 2, false, ""},
		{"recovers after a 429", []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusAccepted}, StepSucceeded, 3, false, ""},
		{"gives up after three tries", []int{http.StatusServiceUnavailable}, StepFailed, 3, false, reasonStatus},
		{"a 4xx is final", []int{http.StatusNotFound, http.StatusOK}, StepFailed, 1, true, reasonStatus},
		{"a redirect is final", []int{http.StatusFound}, StepFailed, 1, true, reasonRedirect},
	} {
		rec, url := serve(t, tc.statuses...)
		o := a.deliver(context.Background(), env, http.MethodPost, url+"/hook", http.Header{}, []byte(`{}`))
		if o.status != tc.status || rec.count() != tc.requests || o.permanent != tc.permanent || o.output["attempts"] != tc.requests || o.reason != tc.reason {
			t.Errorf("%s: %+v after %d requests", tc.name, o, rec.count())
		}
	}
	// Without retry, as a test send, one try is all.
	rec, url := serve(t, http.StatusInternalServerError, http.StatusOK)
	if o := a.deliver(context.Background(), actionEnv{}, http.MethodGet, url, http.Header{}, nil); o.status != StepFailed || o.err != "Status 500" || rec.count() != 1 || rec.requests[0].Method != http.MethodGet {
		t.Fatalf("no retry: %+v after %d", o, rec.count())
	}
}

// A refusing receiver's answer is recorded on the step, cut to maxResponseExcerpt bytes of valid text; an accepting receiver's is not.
func TestDeliverRecordsRefusalExcerpt(t *testing.T) {
	t.Parallel()
	a := testActions(t, ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true}))
	deliver := func(status int, body string) outcome {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)
		return a.deliver(context.Background(), actionEnv{}, http.MethodPost, srv.URL, http.Header{}, []byte(`{}`))
	}
	if o := deliver(http.StatusNotFound, "  no_service\n"); o.output["responseBody"] != "no_service" || o.err != "Status 404" {
		t.Fatalf("404: %+v", o)
	}
	// 170 three-byte characters cross the limit inside the 171st: the cut character is dropped, not sent as invalid UTF-8.
	long := deliver(http.StatusInternalServerError, strings.Repeat("あ", 300))
	if got := long.output["responseBody"].(string); got != strings.Repeat("あ", maxResponseExcerpt/3) {
		t.Fatalf("500: %d bytes %q", len(got), got)
	}
	if o := deliver(http.StatusOK, "fine"); o.status != StepSucceeded || o.output["responseBody"] != nil {
		t.Fatalf("200: %+v", o)
	}
	if o := deliver(http.StatusBadRequest, ""); o.output["responseBody"] != nil {
		t.Fatalf("empty body: %+v", o)
	}
}

// A step's run log input carries the label its author gave it, which the run detail names the step by, and whether the step lets the run go on past its failure.
func TestStepInputLabel(t *testing.T) {
	t.Parallel()
	label := "POST to CRM"
	step := Step{ID: "h", Type: ActionHTTPRequest, Node: Node{ID: "h", Label: &label, HTTP: &HTTPRequestConfig{Method: "POST", URL: "https://crm.example/hook?token=abc"}}}
	if in := stepInput(step); in["label"] != label || in["url"] != "https://crm.example" {
		t.Fatalf("input %v", in)
	}
	blank := " "
	step.Node.Label = &blank
	if _, ok := stepInput(step)["label"]; ok {
		t.Fatal("a blank label was recorded")
	}
	if _, ok := stepInput(step)["continueOnFailure"]; ok {
		t.Fatal("continueOnFailure was recorded for a step that stops the run")
	}
	step.Node.ContinueOnFailure = true
	if in := stepInput(step); in["continueOnFailure"] != true {
		t.Fatalf("input %v", in)
	}
}

// staticResolver answers every name with one address.
type staticResolver string

func (s staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr(string(s))}, nil
}

func TestDeliverRefusesInternalTargets(t *testing.T) {
	t.Parallel()
	rec, url := serve(t, http.StatusOK)
	port := url[strings.LastIndex(url, ":"):]
	a := testActions(t, ssrf.New(config.Config{}).WithResolver(staticResolver("10.0.0.7")))
	for _, target := range []string{
		url, "http://localhost" + port, "http://[::1]" + port, "http://169.254.169.254/latest/meta-data", "http://10.1.2.3/hook",
		"http://0x7f000001" + port, "http://metadata.google.internal/", "https://internal.example/hook", "file:///etc/passwd",
	} {
		o := a.deliver(context.Background(), actionEnv{retry: true}, http.MethodPost, target, http.Header{}, []byte(`{}`))
		if o.status != StepFailed || !o.permanent || !strings.HasPrefix(o.err, "URL refused: ") || o.reason != reasonURLRefused {
			t.Errorf("%s: %+v", target, o)
		}
	}
	if rec.count() != 0 {
		t.Fatalf("a refused target was reached %d times", rec.count())
	}
}

func TestHTTPRequestAction(t *testing.T) {
	t.Parallel()
	ws := uuid.New()
	a := testActions(t, ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true}))
	rec, url := serve(t, http.StatusOK)
	d, issues := ParseDefinition(raw(t, chain(httpNode("h", url+"/hook?token=abc",
		map[string]any{"name": "X-Plain", "value": "plain"}, map[string]any{"name": "authorization", "secret": map[string]any{"value": "Bearer abcdefghijkl"}},
	))))
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	d.Nodes[0].HTTP.SigningSecret = &SecretValue{Value: new(testSigningKey)}
	if issues, err := sealSecrets(a.cipher, ws, d, nil); err != nil || len(issues) > 0 {
		t.Fatalf("%v %+v", err, issues)
	}
	step := d.Nodes[0]
	runID := uuid.New()
	env := actionEnv{workspaceID: ws, template: testAnswers(t), messageID: stepMessageID(runID, step.ID), retry: true}
	o := a.perform(context.Background(), env, step)
	if o.status != StepSucceeded || rec.count() != 1 {
		t.Fatalf("%+v", o)
	}
	req, body := rec.requests[0], rec.bodies[0]
	if req.Method != http.MethodPost || req.URL.RawQuery != "token=abc" || body != `{"answer":"He said \"hi\" <b>"}` ||
		req.Header.Get("Authorization") != "Bearer abcdefghijkl" || req.Header.Get("X-Plain") != "plain" || req.Header.Get("Content-Type") != "application/json" ||
		req.Header.Get("Webhook-Id") != stepMessageID(runID, "h") {
		t.Fatalf("request %s %v %s", req.Method, req.Header, body)
	}
	wh, err := standardwebhooks.NewWebhook(testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := wh.Verify([]byte(body), req.Header); err != nil {
		t.Fatalf("the signature does not verify: %v", err)
	}
	if in := stepInput(Step{Type: ActionHTTPRequest, Node: step}); in["url"] != strings.TrimSuffix(url, "/") || strings.Contains(string(mustJSON(in)), "token") {
		t.Fatalf("the run log input names the URL's path or query: %v", in)
	}

	// The same step on another workspace cannot read its secrets and fails without sending.
	env.workspaceID = uuid.New()
	if o := a.perform(context.Background(), env, step); o.status != StepFailed || !o.permanent || o.reason != reasonSecret || rec.count() != 1 {
		t.Fatalf("foreign workspace: %+v", o)
	}

	// A header value the transport would refuse, such as a secret sealed before line breaks were checked, fails the step for good without a request.
	env.workspaceID = ws
	sealed, err := a.cipher.Seal([]byte("abc\r\ndef"), headerSecretContext(ws, step.HTTP.URL))
	if err != nil {
		t.Fatal(err)
	}
	step.HTTP.Headers[1].Secret = &SecretValue{Sealed: base64.StdEncoding.EncodeToString(sealed)}
	if o := a.perform(context.Background(), env, step); o.status != StepFailed || !o.permanent || o.reason != reasonConfig || rec.count() != 1 {
		t.Fatalf("control character: %+v, %d requests", o, rec.count())
	}
}

func TestChangedHTTPRequests(t *testing.T) {
	t.Parallel()
	parse := func(d map[string]any) *Definition {
		def, issues := ParseDefinition(raw(t, d))
		if len(issues) > 0 {
			t.Fatal(issues)
		}
		return def
	}
	withHTTP := parse(chain(httpNode("h", "https://example.com/a"), tagNode("t", uuid.NewString())))
	if !changedHTTPRequests(nil, withHTTP) || changedHTTPRequests(nil, parse(chain(tagNode("t", uuid.NewString())))) {
		t.Fatal("a new http_request")
	}
	relabelled := parse(chain(httpNode("h", "https://example.com/a"), tagNode("t", uuid.NewString())))
	relabelled.Nodes[0].Label = new("Renamed")
	if changedHTTPRequests(withHTTP, relabelled) {
		t.Fatal("a relabelled http_request with the same config counts as changed")
	}
	moved := parse(chain(httpNode("h", "https://example.com/b")))
	if !changedHTTPRequests(withHTTP, moved) || changedHTTPRequests(withHTTP, parse(chain(tagNode("t", uuid.NewString())))) {
		t.Fatal("a changed or removed http_request")
	}
}

func TestRedactActionSecrets(t *testing.T) {
	t.Parallel()
	d := chain(
		httpNode("h", "https://api.example.com/hook?token=abc", map[string]any{"name": "X-Key", "value": "k1"}, map[string]any{"name": "X-Other", "value": "k2"}),
		chatNode("s", ActionSlackMessage, map[string]any{"sealed": "c2VhbGVk", "hint": "hooks.slack.com"}, "Hi {{answers.q1}}"),
		tagNode("t", "0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a13"),
	)
	var doc any
	_ = json.Unmarshal(raw(t, d), &doc)
	out := string(mustJSON(redactDefinition(doc, "key")))
	for _, leaked := range []string{"token=abc", "k1", "k2", "c2VhbGVk", "hooks.slack.com", "answers.q1"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("%q survived redaction: %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"signingSecret":null`) || !strings.Contains(out, `"tagId":"0192a7a6-3f6e-7c4d-9b1a-5e2f8c3d4a13"`) || !strings.Contains(out, `"method":"POST"`) {
		t.Fatalf("redaction dropped what is not personal: %s", out)
	}
	var redacted struct {
		Nodes []struct {
			Config struct {
				Headers []string `json:"headers"`
			} `json:"config"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &redacted); err != nil || len(redacted.Nodes[0].Config.Headers) != 2 || redacted.Nodes[0].Config.Headers[0] == redacted.Nodes[0].Config.Headers[1] {
		t.Fatalf("headers are not redacted one by one: %s %v", out, err)
	}
}

// The step message id is the same on every delivery of a step and differs between steps and runs, so a receiver can drop a repeat.
func TestStepMessageID(t *testing.T) {
	t.Parallel()
	run := uuid.New()
	first, again := stepMessageID(run, "a"), stepMessageID(uuid.MustParse(run.String()), "a")
	if first != again || first == stepMessageID(run, "b") || first == stepMessageID(uuid.New(), "a") || len(first) != 64 {
		t.Fatal("message ids")
	}
}
