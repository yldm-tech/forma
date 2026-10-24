package survey

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/jsonschema"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

func mustDecode(t *testing.T, raw string) any {
	t.Helper()
	v, err := decodeValue([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The strictness cases of forma2's schemas.test.ts.
func TestUnsupportedFields(t *testing.T) {
	doc := mustDecode(t, `{
	  "name": "x", "extra": 1,
	  "languages": [{"code": "de-DE", "alias": "German"}],
	  "welcomeCard": {"enabled": true, "headline": {"en-US": "Hi", "de-de": "Hallo", "DE-DE": "Hallo", "xx": "?"}},
	  "blocks": [{"name": "B", "color": "red", "elements": [
	    {"id": "q", "type": "openText", "headline": {"en-US": "Q"}, "required": true, "range": 5, "choices": []},
	    {"type": "rating", "headline": {"fr-FR": "Q"}}
	  ]}],
	  "endings": [{"type": "redirectToUrl", "headline": {"en-US": "x"}}],
	  "metadata": {"title": {"default": "x", "en-US": "y"}}
	}`)
	params := unsupportedFields(doc, createRootKeys, defaultV3Language, nil)
	byName := map[string]v3api.InvalidParam{}
	for _, p := range params {
		if _, dup := byName[p.Name]; !dup {
			byName[p.Name] = p
		}
	}
	want := map[string]struct{ code, reason string }{
		"extra":                        {v3api.CodeUnsupportedField, "Unsupported field 'extra' for the survey document. Allowed fields: blocks, defaultLanguage, distribution, endings, hiddenFields, languages, metadata, name, status, targeting, type, variables, welcomeCard, workspaceId"},
		"languages.0.alias":            {v3api.CodeUnsupportedField, "Unsupported field 'alias'"},
		"welcomeCard.headline.DE-DE":   {v3api.CodeDuplicateLocale, "Language key 'DE-DE' duplicates 'de-de' after locale normalization"},
		"welcomeCard.headline.xx":      {v3api.CodeInvalidLocale, "Language key 'xx' is not a valid locale code"},
		"blocks.0.color":               {v3api.CodeUnsupportedField, "Unsupported field 'color'"},
		"blocks.0.elements.0.range":    {v3api.CodeUnsupportedField, "Unsupported field 'range' for element type 'openText'. Allowed fields: charLimit, headline, id, imageUrl, inputType, insightsEnabled, isDraft, longAnswer, placeholder, required, subheader, type, validation, videoUrl"},
		"blocks.0.elements.1.id":       {v3api.CodeMissingRequiredField, "Missing required field 'id' for element type 'rating'"},
		"blocks.0.elements.1.headline": {"", "Translatable field must include the defaultLanguage locale 'en-US'"},
		"endings.0.id":                 {v3api.CodeMissingRequiredField, "Missing required field 'id' for ending type 'redirectToUrl'"},
		"endings.0.headline":           {v3api.CodeUnsupportedField, "Unsupported field 'headline' for ending type 'redirectToUrl'. Allowed fields: id, label, type, url"},
		"metadata.title.default":       {"", "Use the defaultLanguage locale code instead of the internal 'default' translation key"},
	}
	for name, w := range want {
		p, ok := byName[name]
		if !ok {
			t.Errorf("no param %s among %+v", name, params)
			continue
		}
		if p.Code != w.code || p.Reason != w.reason {
			t.Errorf("%s: %q %q, want %q %q", name, p.Code, p.Reason, w.code, w.reason)
		}
	}
}

func TestNormalizeAndParse(t *testing.T) {
	schemas, err := jsonschema.New()
	if err != nil {
		t.Fatal(err)
	}
	doc := mustDecode(t, `{
	  "workspaceId": "0199ad73-6c55-7ac3-a4d3-24a7b1a7a2a0", "name": "Survey", "defaultLanguage": "de_de",
	  "languages": [{"code": "en-us"}],
	  "blocks": [{"name": "B", "elements": [
	    {"id": "q", "type": "openText", "headline": {"en-US": "Q", "de-DE": "F"}, "required": true},
	    {"id": "n", "type": "nps", "headline": {"en-us": "N", "de-de": "N"}, "required": false, "validation": {"rules": []}}
	  ]}],
	  "variables": [{"name": "score", "type": "number"}]
	}`)
	o := asObject(doc)
	normalizeDocument(o, defaultV3Language, true, false, true, nil)
	p := &parser{docs: surveydoc.NewValidator(schemas)}
	d := p.parse(o, parseCreate, nil, "", "")
	if len(p.issues) > 0 {
		t.Fatalf("issues %+v", p.issues)
	}
	if d.DefaultLanguage != "de-DE" || len(d.Languages) != 1 || d.Languages[0].Code != "en-US" || !d.Languages[0].Enabled || d.Type != TypeLink || d.Status != StatusDraft {
		t.Fatalf("parsed %+v", d)
	}
	blocks := string(encodeValue(d.Blocks))
	for _, want := range []string{`"headline":{"default":"F","en-US":"Q"}`, `"inputType":"text"`, `"charLimit":{"enabled":false}`, `"isColorCodingEnabled":false`, `"logic":"and"`} {
		if !strings.Contains(blocks, want) {
			t.Errorf("blocks %s lack %s", blocks, want)
		}
	}
	vars := string(encodeValue(d.Variables))
	if !strings.Contains(vars, `"value":0`) || !strings.Contains(vars, `"id":"`) {
		t.Errorf("variables %s", vars)
	}
	if wc := string(encodeValue(d.WelcomeCard)); wc != `{"enabled":false,"timeToFinish":true,"showResponseCount":false}` {
		t.Errorf("welcome card %s", wc)
	}

	bad := mustDecode(t, `{"workspaceId": "x", "name": 1, "type": "web", "languages": [{"code": "de-DE", "default": true}, {"code": "de-DE", "enabled": false}],
	  "blocks": [{"name": "B", "elements": [{"id": "q", "type": "openText", "headline": {"default": "Q"}, "required": true}]}],
	  "distribution": {"displayOption": "displaySome"}}`)
	p = &parser{docs: surveydoc.NewValidator(schemas)}
	p.parse(asObject(bad), parseCreate, nil, "", "")
	got := map[string]string{}
	for _, is := range p.issues {
		got[is.Name()] = is.Message
	}
	for name, message := range map[string]string{
		"name":                "Invalid input: expected string, received number",
		"type":                `Invalid option: expected one of "link"|"app"`,
		"languages.1.code":    "Language 'de-DE' is duplicated",
		"languages.0.default": "The default language entry must match defaultLanguage",
	} {
		if got[name] != message {
			t.Errorf("%s: %q, want %q (all %v)", name, got[name], message, got)
		}
	}
}

func TestLanguageAndReferenceParams(t *testing.T) {
	d := &v3Document{
		DefaultLanguage: "en-US",
		Languages:       []v3LanguageInput{{Code: "de-DE", Enabled: true}},
		Metadata:        mustDecode(t, `{"title": {"default": "T", "fr-FR": "x"}}`),
		WelcomeCard:     mustDecode(t, `{"enabled": false}`),
		Blocks: mustDecode(t, `[{"id": "b1", "name": "B", "logicFallback": "b1", "elements": [{"id": "q", "type": "openText", "headline": {"default": "Q #recall:plan/fallback:x# #recall:ghost/fallback:x#", "de-DE": "F"}, "required": true}]},
			{"id": "b1", "name": "C", "elements": [{"id": "Plan", "type": "openText", "headline": {"default": "Q", "de-DE": "F"}, "required": true}]}]`),
		Endings:      mustDecode(t, `[]`),
		HiddenFields: mustDecode(t, `{"enabled": true, "fieldIds": ["plan", "plan"]}`),
		Variables:    mustDecode(t, `[]`),
	}
	params, _, err := validateDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]v3api.InvalidParam{}
	for _, p := range params {
		byName[p.Name] = append(byName[p.Name], p)
	}
	check := func(name, code string) {
		t.Helper()
		for _, p := range byName[name] {
			if p.Code == code {
				return
			}
		}
		t.Errorf("no %s at %s among %+v", code, name, params)
	}
	check("metadata.title.fr-FR", v3api.CodeUnsupportedLocale)
	check("metadata.title", v3api.CodeMissingTranslation)
	check("blocks.1.id", v3api.CodeDuplicateIdentifier)
	// Identifiers collide across namespaces case-insensitively; the later one is reported.
	check("hiddenFields.fieldIds.0", v3api.CodeDuplicateIdentifier)
	check("hiddenFields.fieldIds.1", v3api.CodeDuplicateIdentifier)
	check("blocks.0.logicFallback", v3api.CodeInvalidReference)
	check("blocks.0.elements.0.headline.default", v3api.CodeDanglingReference)
	for _, p := range byName["blocks.0.elements.0.headline.default"] {
		if p.Identifier == "plan" {
			t.Error("a recall of a hidden field was reported")
		}
	}
	raw, _ := json.Marshal(params)
	if strings.Contains(string(raw), "-fLang-") {
		t.Error("an editor marker leaked")
	}
}
