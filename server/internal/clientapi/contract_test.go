package clientapi

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// The contract test replays the exchanges recorded against forma2 (testdata/forma2, see its README) in an order their scenarios need, against data equivalent to what the recorder created, and requires the same status, the recorded headers and a body equal to the recorded one with its placeholders matched by kind. An id placeholder binds to the first id it meets and must name the same id wherever it appears again; ids are the rewrite's UUIDs where forma2 had cuids.

type recorded struct {
	Case    string `json:"case"`
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Query   map[string]string `json:"query"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"request"`
	Response struct {
		Status     int               `json:"status"`
		Headers    map[string]string `json:"headers"`
		BodyFormat string            `json:"bodyFormat"`
		Body       json.RawMessage   `json:"body"`
	} `json:"response"`
}

// coveredElsewhere are fixtures of the same recording whose routes other modules serve and replay: the survey pages and the OG image (internal/linksurvey/contract_test.go).
var coveredElsewhere = regexp.MustCompile(`^(page-|v1-og-image)`)

// replayOrder is the order the recorder's scenarios need: identify before a contact's displays, a display before the response that references it, the first quota response before the one that finds the quota full. The rate-limit cases come last, each after its window has been exhausted.
var replayOrder = []string{
	"v1-environment-options", "v1-displays-options", "v2-displays-options", "v1-responses-options", "v2-responses-options",
	"v1-storage-options", "v2-user-options",
	"js-surveys-umd",
	"v1-environment-invalid-id", "v1-environment-unknown-workspace", "v1-environment", "v2-environment",
	"v1-environment-legacy-environment-id", "v2-environment-legacy-environment-id", "v1-environment-debug-no-cache",
	"v2-user-identify-new", "v2-user-identify-existing", "v2-user-attributes-update", "v1-user-legacy-environment-id",
	"v1-user-identify-new", "v1-user-unknown-workspace", "v2-user-attributes-invalid-email", "v2-user-malformed-json", "v2-user-missing-userid",
	"v1-environment-after-identify",
	"v2-displays-create-anonymous", "v1-display-response-options", "v1-display-response-none", "v2-displays-create-contact", "v1-displays-create-userid",
	"v2-displays-unknown-survey", "v2-displays-unknown-workspace", "v2-displays-invalid-body", "v1-displays-invalid-body",
	"v1-display-response-unknown-display",
	"v2-responses-create-partial", "v1-responses-update-options", "v1-display-response-found", "v2-responses-create-contact", "v1-responses-create-userid",
	"v1-responses-update-finish", "v1-responses-update-already-finished", "v1-responses-update-invalid-body",
	"v1-responses-update-malformed-id", "v1-responses-update-unknown",
	"v2-responses-create-finished", "v2-responses-create-required-element-omitted", "v2-responses-create-rating-out-of-range",
	"v2-responses-create-missing-required", "v2-responses-create-invalid-body", "v2-responses-create-invalid-meta-action",
	"v2-responses-create-malformed-json", "v2-responses-create-paused-survey", "v2-responses-create-archived-survey",
	"v2-responses-create-unknown-survey", "v2-responses-create-unknown-workspace",
	"v2-responses-create-quota-screened-in", "v2-responses-create-quota-full", "v2-responses-create-for-quota-update", "v1-responses-update-quota-full",
	"v2-responses-create-single-use", "v2-responses-create-single-use-duplicate", "v2-responses-create-single-use-missing-id",
	"v2-responses-create-single-use-missing-url", "v2-responses-create-single-use-mismatched-id",
	"v1-storage-upload-init", "v2-storage-upload-init", "v1-storage-extension-not-allowed", "v1-storage-invalid-file-name",
	"v1-storage-malformed-json", "v1-storage-missing-fields", "v1-storage-no-file-upload-element", "v1-storage-unknown-survey",
	// After the storage:upload window has taken five requests.
	"v1-storage-rate-limited",
	// After the api:client window has taken a hundred.
	"v1-environment-rate-limited", "v1-responses-rate-limited", "v2-displays-rate-limited", "v2-responses-rate-limited",
}

// deviations are the recorded answers the rewrite gives differently on purpose; each rewrites the expected body and says why.
var deviations = map[string]struct {
	reason string
	expect func(body string) string
}{
	"v2-responses-create-missing-required": {
		reason: `The response's language is "default", which forma2's server resolved through a process-wide i18next instance whose language the previous request had set (de-DE here), so the message's language depended on unrelated traffic. The rewrite writes "default" messages in the survey's default language (en-US here).`,
		expect: func(body string) string {
			return strings.Replace(body, "Bitte füllen Sie dieses Feld aus", "Please fill out this field", 1)
		},
	},
}

// singleUseID is the canonical single-use id of the recorded single-use link.
const singleUseID = "clsingleuseaaaaaaaaaaaaa1"

// seedRecording creates the recorder's data (testdata/forma2/README.md) and returns the placeholders it already binds.
func (e *env) seedRecording(t *testing.T) map[string]string {
	ws := e.workspace("legacy_environment_id", "clegacyenvaaaaaaaaaaaaaa1")
	enUS := e.language(ws, "en-US", nil)
	deDE := e.language(ws, "de-DE", nil)
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'plan', 'Plan', 'custom', 'string')`, ws)
	checkout := e.insert(`INSERT INTO action_classes (workspace_id, name, type, key) VALUES (?, 'Checkout', 'code', 'checkout') RETURNING id`, ws)

	// The app survey's document is the one the recorded state shows.
	f := loadRecorded(t, "v1-environment")
	var state struct {
		Data struct {
			Data struct {
				Surveys []map[string]json.RawMessage `json:"surveys"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(f.Response.Body, &state); err != nil {
		t.Fatal(err)
	}
	doc := state.Data.Data.Surveys[0]
	blockID := "clappblockaaaaaaaaaaaaa1"
	app := e.survey(ws, "type", "app", "name", "App", "display_option", "displayMultiple",
		"welcome_card", string(doc["welcomeCard"]), "blocks", strings.ReplaceAll(string(doc["blocks"]), "<id:block:app>", blockID),
		"endings", string(doc["endings"]), "hidden_fields", string(doc["hiddenFields"]), "variables", string(doc["variables"]))
	e.exec(`INSERT INTO survey_triggers (survey_id, action_class_id) VALUES (?, ?)`, app, checkout)
	e.surveyLanguage(app, enUS, true)
	appSegment := e.segment(ws, app.String(), "[]", app)

	link := e.survey(ws, "name", "Link", "slug", "contract-feedback",
		"blocks", `[{"id":"clblocklinkaaaaaaaaaaaa1","name":"Main","elements":[
			{"id":"feedback","type":"openText","headline":{"default":"What should we improve?","de-DE":"Was sollten wir verbessern?"},"required":true,"inputType":"text","charLimit":{"enabled":false}},
			{"id":"score","type":"rating","headline":{"default":"How likely are you to recommend us?","de-DE":"Wie wahrscheinlich empfehlen Sie uns?"},"required":false,"scale":"number","range":5}]}]`,
		"endings", `[{"id":"clendscreenokaaaaaaaaaaa1","type":"endScreen","headline":{"default":"Thanks!"}},{"id":"clendscreenquotaaaaaaaaa2","type":"endScreen","headline":{"default":"We have enough answers"}}]`,
		"hidden_fields", `{"enabled":true,"fieldIds":["utm_source","campaign"]}`)
	e.surveyLanguage(link, enUS, true)
	e.surveyLanguage(link, deDE, false)
	e.ingestedField(ws, link, "utm_source")
	e.ingestedField(ws, link, "campaign")
	quota := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action, ending_card_id, count_partial_submissions)
		VALUES (?, 'Quota', 1, '{"connector":"and","conditions":[{"id":"clquotaconditionaaaaaaa1","leftOperand":{"type":"element","value":"feedback"},"operator":"equals","rightOperand":{"type":"static","value":"quota"}}]}'::jsonb, 'endSurvey', 'clendscreenquotaaaaaaaaa2', true) RETURNING id`, link)

	paused := e.survey(ws, "name", "Paused", "status", "paused")
	archived := e.survey(ws, "name", "Archived", "status", "paused")
	e.exec(`UPDATE surveys SET archived_at = now() WHERE id = ?`, archived)
	singleUse := e.survey(ws, "name", "Single use", "single_use", `{"enabled":true,"isEncrypted":true}`)
	upload := e.survey(ws, "name", "Upload", "blocks", `[{"id":"clblockuploadaaaaaaaaaa1","name":"Main","elements":[
		{"id":"attachment","type":"fileUpload","headline":{"default":"Attach a file"},"required":false,"allowMultipleFiles":false,"allowedFileExtensions":["png","pdf"],"maxSizeInMB":5}]}]`)
	unused := e.insert(`INSERT INTO displays (survey_id) VALUES (?) RETURNING id`, link)

	token, err := secret.EncryptForma2(singleUseID, e.cfg.EncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"<id:workspace>": ws.String(), "<id:language:en-US>": enUS.String(), "<id:actionClass:checkout>": checkout.String(),
		"<id:survey:app>": app.String(), "<id:segment:app>": appSegment.String(), "<id:block:app>": blockID,
		"<id:survey:link>": link.String(), "<id:quota>": quota.String(), "<id:survey:paused>": paused.String(),
		"<id:survey:archived>": archived.String(), "<id:survey:singleUse>": singleUse.String(), "<id:survey:upload>": upload.String(),
		"<id:display:anonymous-unused>": unused.String(), "<id:singleUse>": singleUseID, "<single-use-token>": url.QueryEscape(token),
		"<webapp-url>": webAppURL, "<s3-endpoint>": "http://localhost:9000",
	}
}

func loadRecorded(t *testing.T, name string) recorded {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "forma2", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f recorded
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

var placeholder = regexp.MustCompile(`<[a-z0-9-]+(?::[A-Za-z0-9:-]+)?>`)

// kinds are the patterns of the placeholders that stand for values differing on every run; an id placeholder not bound yet matches an id and binds to it.
var kinds = map[string]string{
	"<timestamp>":    `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z`,
	"<uuid>":         `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
	"<s3:policy>":    `[A-Za-z0-9+/=]+`,
	"<s3:signature>": `[0-9a-f]{64}`,
	"<s3:amz-date>":  `\d{8}T\d{6}Z`,
	"<yyyymmdd>":     `\d{8}`,
	"<id:unbound>":   `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
}

// replay is the state of one replay: the bound placeholders.
type replay struct {
	t     *testing.T
	bound map[string]string
}

// substitute fills a request's placeholders, which must all be bound by then.
func (r *replay) substitute(s string) string {
	return placeholder.ReplaceAllStringFunc(s, func(p string) string {
		v, ok := r.bound[p]
		if !ok {
			r.t.Fatalf("request placeholder %s is not bound yet", p)
		}
		return v
	})
}

// matchString matches a recorded string against an actual one, binding id placeholders it meets for the first time.
func (r *replay) matchString(want, got string) bool {
	locs := placeholder.FindAllStringIndex(want, -1)
	if len(locs) == 0 {
		return want == got
	}
	var pattern strings.Builder
	pattern.WriteString("^")
	var unbound []string
	last := 0
	for _, l := range locs {
		pattern.WriteString(regexp.QuoteMeta(want[last:l[0]]))
		p := want[l[0]:l[1]]
		switch {
		case r.bound[p] != "":
			pattern.WriteString(regexp.QuoteMeta(r.bound[p]))
		case kinds[p] != "":
			pattern.WriteString("(?:" + kinds[p] + ")")
		case strings.HasPrefix(p, "<id:"):
			pattern.WriteString("(" + kinds["<id:unbound>"] + ")")
			unbound = append(unbound, p)
		default:
			r.t.Fatalf("unknown placeholder %s", p)
		}
		last = l[1]
	}
	pattern.WriteString(regexp.QuoteMeta(want[last:]) + "$")
	m := regexp.MustCompile(pattern.String()).FindStringSubmatch(got)
	if m == nil {
		return false
	}
	for i, p := range unbound {
		if prev, ok := r.bound[p]; ok && prev != m[i+1] {
			return false
		}
		r.bound[p] = m[i+1]
	}
	return true
}

// match compares a recorded JSON value with an actual one, reporting the first difference.
func (r *replay) match(path string, want, got any) error {
	switch w := want.(type) {
	case string:
		g, ok := got.(string)
		if !ok || !r.matchString(w, g) {
			return fmt.Errorf("%s: got %#v, want %q", path, got, w)
		}
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: got %#v, want an object", path, got)
		}
		keys := func(m map[string]any) []string {
			var out []string
			for k := range m {
				out = append(out, k)
			}
			slices.Sort(out)
			return out
		}
		if !slices.Equal(keys(w), keys(g)) {
			return fmt.Errorf("%s: keys %v, want %v", path, keys(g), keys(w))
		}
		for _, k := range keys(w) {
			if err := r.match(path+"."+k, w[k], g[k]); err != nil {
				return err
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: got %#v, want %d elements", path, got, len(w))
		}
		for i := range w {
			if err := r.match(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("%s: got %#v, want %#v", path, got, want)
		}
	}
	return nil
}

// send replays one recorded request.
func (r *replay) send(e *env, f recorded) result {
	path := r.substitute(f.Request.Path)
	if len(f.Request.Query) > 0 {
		q := url.Values{}
		for k, v := range f.Request.Query {
			q.Set(k, r.substitute(v))
		}
		path += "?" + q.Encode()
	}
	var body any
	if raw := strings.TrimSpace(string(f.Request.Body)); raw != "" && raw != "null" {
		var s string
		if json.Unmarshal(f.Request.Body, &s) == nil {
			body = r.substitute(s)
		} else {
			body = r.substitute(raw)
		}
	}
	var headers []string
	for k, v := range f.Request.Headers {
		if k != "content-type" {
			headers = append(headers, k, v)
		}
	}
	return e.do(f.Request.Method, path, body, headers...)
}

func (r *replay) check(f recorded, res result) {
	t := r.t
	if res.Status != f.Response.Status {
		t.Fatalf("status %d, want %d: %s", res.Status, f.Response.Status, res.Body)
	}
	for name, want := range f.Response.Headers {
		if got := res.Header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	for _, name := range []string{"Access-Control-Allow-Origin", "X-Frame-Options", "Allow"} {
		if _, recorded := f.Response.Headers[strings.ToLower(name)]; !recorded && res.Header.Get(name) != "" {
			t.Errorf("header %s %q was not sent by forma2", name, res.Header.Get(name))
		}
	}
	switch f.Response.BodyFormat {
	case "json":
		expected := string(f.Response.Body)
		if d, ok := deviations[f.Case]; ok {
			expected = d.expect(expected)
		}
		var want, got any
		if err := json.Unmarshal([]byte(expected), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(res.Body, &got); err != nil {
			t.Fatalf("body %s is not JSON: %v", res.Body, err)
		}
		if err := r.match("body", want, got); err != nil {
			t.Fatalf("%v\nbody: %s", err, res.Body)
		}
	case "empty":
		if len(res.Body) != 0 {
			t.Fatalf("body %q, want none", res.Body)
		}
	}
}

// The client API answers every request recorded against forma2 as forma2 did.
func TestReplaysRecordedForma2Exchanges(t *testing.T) {
	e := newEnv(t)
	files, err := filepath.Glob(filepath.Join("testdata", "forma2", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, f := range files {
		if name := strings.TrimSuffix(filepath.Base(f), ".json"); !coveredElsewhere.MatchString(name) {
			all = append(all, name)
		}
	}
	ordered := slices.Clone(replayOrder)
	slices.Sort(ordered)
	slices.Sort(all)
	if !slices.Equal(ordered, all) {
		t.Fatalf("the replay order lists %v, the recording has %v", ordered, all)
	}

	r := &replay{t: t, bound: e.seedRecording(t)}
	for _, name := range replayOrder {
		f := loadRecorded(t, name)
		switch name {
		case "v1-storage-rate-limited":
			e.clock.nextWindow()
			upload := `{"fileName":"a.png","fileType":"image/png","surveyId":"` + r.bound["<id:survey:upload>"] + `","elementId":"attachment"}`
			for range 5 {
				if res := e.do("POST", "/api/v1/client/"+r.bound["<id:workspace>"]+"/storage", upload); res.Status != 200 {
					t.Fatalf("storage request before the limit: %d %s", res.Status, res.Body)
				}
			}
		case "v1-environment-rate-limited":
			e.clock.nextWindow()
			for range 100 {
				if res := e.do("GET", "/api/v1/client/"+r.bound["<id:workspace>"]+"/environment", nil); res.Status != 200 {
					t.Fatalf("request before the limit: %d %s", res.Status, res.Body)
				}
			}
		case "v1-responses-rate-limited", "v2-displays-rate-limited", "v2-responses-rate-limited":
			// The same exhausted window as v1-environment-rate-limited.
		default:
			e.clock.nextWindow()
		}
		res := r.send(e, f)
		t.Run(name, func(t *testing.T) {
			sub := &replay{t: t, bound: r.bound}
			sub.check(f, res)
		})
	}
	for name, d := range deviations {
		t.Logf("%s deviates from forma2 on purpose: %s", name, d.reason)
	}
}
