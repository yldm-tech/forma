package survey

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

var update = flag.Bool("update", false, "rewrite catalog/locales from apps/web/locales")

// catalog/locales/*.json must stay the strings of apps/web/locales the template catalog uses; run with -update after changing apps/web/locales.
func TestCatalogueMatchesWebLocales(t *testing.T) {
	for _, locale := range catalogLocales {
		raw, err := os.ReadFile(filepath.Join(repotest.Root(t), "apps/web/locales", locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var web struct {
			Templates map[string]string `json:"templates"`
			Workspace struct {
				XMTemplates map[string]string `json:"xm-templates"`
			} `json:"workspace"`
			Common map[string]any `json:"common"`
		}
		if err := json.Unmarshal(raw, &web); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		for k, v := range web.Templates {
			want["templates."+k] = v
		}
		for k, v := range web.Workspace.XMTemplates {
			want["workspace.xm-templates."+k] = v
		}
		for _, k := range []string{"duplicate_copy", "duplicate_copy_number"} {
			want["common."+k], _ = web.Common[k].(string)
		}
		path := filepath.Join("catalog", "locales", locale+".json")
		if *update {
			keys := make([]string, 0, len(want))
			for k := range want {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var buf bytes.Buffer
			buf.WriteString("{\n")
			for i, k := range keys {
				kb, _ := json.Marshal(k)
				vb, _ := json.Marshal(want[k])
				buf.WriteString("  " + string(kb) + ": " + string(vb))
				if i < len(keys)-1 {
					buf.WriteString(",")
				}
				buf.WriteString("\n")
			}
			buf.WriteString("}\n")
			if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		raw, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from apps/web/locales/%s.json; run go test ./internal/survey -run TestCatalogueMatchesWebLocales -update", path, locale)
		}
	}
}

func TestCatalogRendersEveryTemplate(t *testing.T) {
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	untranslated := regexp.MustCompile(`"(templates|workspace\.xm-templates)\.[a-z0-9_]+"`)
	for _, locale := range catalogLocales {
		templates, err := c.Templates(locale)
		if err != nil {
			t.Fatal(err)
		}
		if len(templates) != 68 || templates[0].ID != CustomTemplateID {
			t.Fatalf("%s: %d templates", locale, len(templates))
		}
		for _, tpl := range templates {
			raw, _ := json.Marshal(tpl)
			if strings.Contains(string(raw), "{{") || strings.Contains(string(raw), "tplid") || tpl.Name == "" || tpl.Description == "" {
				t.Fatalf("%s/%s was not rendered: %s", locale, tpl.ID, raw[:200])
			}
			// Every marker names a key the catalog translations have: a missing key would render as the key itself.
			if keys := untranslated.FindAll(raw, -1); len(keys) > 0 {
				t.Fatalf("%s/%s renders translation keys: %s", locale, tpl.ID, keys)
			}
		}
	}
	// Placeholders get fresh ids per rendering, consistently within one: logic keeps naming its target.
	first, _ := c.Template("cart-abandonment", "en-US")
	second, _ := c.Template("cart-abandonment", "en-US")
	if bytes.Equal(first.Preset, second.Preset) {
		t.Fatal("two renderings share ids")
	}
	var preset Preset
	if err := json.Unmarshal(first.Preset, &preset); err != nil {
		t.Fatal(err)
	}
	var blocks []struct {
		ID    string `json:"id"`
		Logic []struct {
			Actions []struct {
				Target string `json:"target"`
			} `json:"actions"`
		} `json:"logic"`
	}
	if err := json.Unmarshal(preset.Blocks, &blocks); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, b := range blocks {
		ids[b.ID] = true
	}
	jumps := 0
	for _, b := range blocks {
		for _, l := range b.Logic {
			for _, a := range l.Actions {
				if a.Target != "" {
					jumps++
					if !ids[a.Target] {
						t.Fatalf("jump to %s, not a block of the rendering", a.Target)
					}
				}
			}
		}
	}
	if jumps == 0 {
		t.Fatal("the template has no logic to check")
	}
	if missing, _ := c.Template("no-such-template", "en-US"); missing != nil {
		t.Fatal("an unknown template rendered")
	}
	if c.copySuffix("ja-JP", 1) != "(コピー)" || c.copySuffix("en-US", 3) != "(copy 3)" || c.copySuffix("fr-FR", 1) != "(copy)" {
		t.Fatal("copy suffix")
	}
	if TemplateLocale("de-DE") != "de-DE" || TemplateLocale("it-IT") != "en-US" || !IsSurveyDefaultLanguage("it-IT") || IsSurveyDefaultLanguage("xx-XX") {
		t.Fatal("template locale")
	}
}

// A template's ending card thanks the respondent and stops there: attribution is the "Powered by" badge, which a workspace can turn off, not a button to Forma on every survey made from the catalog.
func TestCatalogEndingsHaveNoButton(t *testing.T) {
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	templates, err := c.Templates(defaultLocale)
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range templates {
		var preset struct {
			Endings []map[string]any `json:"endings"`
		}
		if err := json.Unmarshal(tpl.Preset, &preset); err != nil {
			t.Fatal(err)
		}
		if len(preset.Endings) == 0 {
			t.Fatalf("%s has no ending", tpl.ID)
		}
		for _, ending := range preset.Endings {
			if _, ok := ending["buttonLabel"]; ok {
				t.Errorf("%s: ending %v has a button label", tpl.ID, ending["id"])
			}
			if _, ok := ending["buttonLink"]; ok {
				t.Errorf("%s: ending %v has a button link", tpl.ID, ending["id"])
			}
		}
	}
}

// adminRootSegments are the first path segments of the admin pages, which exist only under /app.
var adminRootSegments = []string{"account", "auth", "billing-confirmation", "email-change-without-verification-success", "invite", "organizations", "setup", "verify-email-change", "workspaces"}

// A survey created from a template stores the template's URLs in its own definition, where a deploy cannot change them, so a link to a Forma admin page must use its address under /app: the same path at the root is not the app.
func TestCatalogLinksAdminPagesUnderApp(t *testing.T) {
	raw, err := catalogFS.ReadFile("catalog/templates.json")
	if err != nil {
		t.Fatal(err)
	}
	formaURL := regexp.MustCompile(`https?://forma\.yldm\.ai(/[^"?#\s]*)?`)
	for _, m := range formaURL.FindAllSubmatch(raw, -1) {
		first, _, _ := strings.Cut(strings.TrimPrefix(string(m[1]), "/"), "/")
		if slices.Contains(adminRootSegments, first) {
			t.Errorf("%s links an admin page outside /app; use %s%s", m[0], config.AppBasePath, m[1])
		}
	}
}

func TestWithWorkspaceName(t *testing.T) {
	p := &Preset{Name: "$[workspaceName] survey", Blocks: json.RawMessage(`[{"id": "b", "elements": [{"headline": {"default": "Do you like $[workspaceName]? $[workspaceName]!"}, "subheader": {"default": "x", "de": "$[workspaceName]"}}]}]`)}
	if err := withWorkspaceName(p, "Acme"); err != nil {
		t.Fatal(err)
	}
	if p.Name != "Acme survey" || !strings.Contains(string(p.Blocks), `"Do you like Acme? $[workspaceName]!"`) || !strings.Contains(string(p.Blocks), `"de":"$[workspaceName]"`) {
		t.Fatalf("%s %s", p.Name, p.Blocks)
	}
}
