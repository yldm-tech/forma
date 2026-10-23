package survey

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// The template catalog is forma2's apps/web/lib/templates.ts (customSurveyTemplate followed by templates(t)), rendered once to JSON with every translation replaced by a {{namespace.key}} marker and every generated id by a tplidNNNNNN placeholder. Rendering a template resolves the markers in the requested locale and gives the placeholders fresh ids, consistently, so logic that names a block or element keeps pointing at it.
//
// catalog/locales holds the translations the catalog uses, taken from web/locales; TestCatalogueMatchesWebLocales keeps them in step.
//
//go:embed catalog/templates.json catalog/locales/*.json
var catalogFS embed.FS

// CustomTemplateID is forma2's CUSTOM_SURVEY_TEMPLATE_ID.
const CustomTemplateID = "custom"

// XMTemplateIDs are forma2's XM_TEMPLATE_IDS, the experience-management templates.
var XMTemplateIDs = []string{"nps", "star-rating", "csat", "ces", "smileys", "enps"}

// catalogLocales are the locales the catalog is translated into; any other renders in en-US.
var catalogLocales = []string{"en-US", "ja-JP", "zh-Hans-CN", "zh-Hant-TW"}

// userLocales is forma2's ZUserLocale, the dashboard's languages.
var userLocales = []string{
	"de-DE", "en-US", "es-ES", "fr-FR", "hu-HU", "ja-JP", "nl-NL", "pt-BR", "pt-PT", "ro-RO", "ru-RU", "sv-SE", "tr-TR", "zh-Hans-CN", "zh-Hant-TW",
}

const defaultLocale = "en-US"

// Template is forma2's TTemplate.
type Template struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Role        string          `json:"role,omitempty"`
	Channels    []string        `json:"channels,omitempty"`
	Industries  []string        `json:"industries,omitempty"`
	Preset      json.RawMessage `json:"preset"`
}

// Preset is a template's survey document.
type Preset struct {
	Name         string          `json:"name"`
	WelcomeCard  json.RawMessage `json:"welcomeCard"`
	Blocks       json.RawMessage `json:"blocks"`
	Endings      json.RawMessage `json:"endings"`
	HiddenFields json.RawMessage `json:"hiddenFields"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	Variables    json.RawMessage `json:"variables,omitempty"`
}

// Catalog is the template catalog and its translations, loaded once.
type Catalog struct {
	templates []any
	messages  map[string]map[string]string
}

// LoadCatalog reads the embedded catalog; a catalog that does not parse is a build defect.
func LoadCatalog() (*Catalog, error) {
	raw, err := catalogFS.ReadFile("catalog/templates.json")
	if err != nil {
		return nil, fmt.Errorf("read template catalog: %w", err)
	}
	c := &Catalog{messages: map[string]map[string]string{}}
	if err := json.Unmarshal(raw, &c.templates); err != nil {
		return nil, fmt.Errorf("parse template catalog: %w", err)
	}
	for _, locale := range catalogLocales {
		b, err := catalogFS.ReadFile("catalog/locales/" + locale + ".json")
		if err != nil {
			return nil, fmt.Errorf("read template translations %s: %w", locale, err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("parse template translations %s: %w", locale, err)
		}
		c.messages[locale] = m
	}
	return c, nil
}

// TemplateLocale is forma2's resolveTemplateTextLocale: a dashboard locale renders in itself, anything else in en-US.
func TemplateLocale(code string) string {
	if slices.Contains(userLocales, code) {
		return code
	}
	return defaultLocale
}

// IsSurveyDefaultLanguage is forma2's ZSurveyDefaultLanguageCode: a language the survey runtime has strings for, or a dashboard locale.
func IsSurveyDefaultLanguage(code string) bool {
	return langcode.IsSurveyRuntimeLanguage(code) || slices.Contains(userLocales, code)
}

// translate looks a key up in the locale, then in en-US; a key missing from both renders as itself and the empty key as nothing, as i18next does.
func (c *Catalog) translate(locale, key string) string {
	if key == "" {
		return ""
	}
	if m, ok := c.messages[locale][key]; ok {
		return m
	}
	if m, ok := c.messages[defaultLocale][key]; ok {
		return m
	}
	return key
}

var (
	markerPattern      = regexp.MustCompile(`^\{\{([^}]*)\}\}$`)
	placeholderPattern = regexp.MustCompile(`^tplid\d{6}$`)
)

// render deep-copies a catalog value, translating markers and replacing placeholder ids through ids.
func (c *Catalog) render(v any, locale string, ids map[string]string) any {
	switch x := v.(type) {
	case string:
		if m := markerPattern.FindStringSubmatch(x); m != nil {
			return c.translate(locale, m[1])
		}
		if placeholderPattern.MatchString(x) {
			id, ok := ids[x]
			if !ok {
				id = surveydoc.NewID()
				ids[x] = id
			}
			return id
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = c.render(e, locale, ids)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = c.render(e, locale, ids)
		}
		return out
	}
	return v
}

func (c *Catalog) template(raw any, locale string) (*Template, error) {
	b, err := json.Marshal(c.render(raw, locale, map[string]string{}))
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	var t Template
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("decode template: %w", err)
	}
	return &t, nil
}

// Templates renders the catalog in a locale: the custom template, then forma2's templates in their order.
func (c *Catalog) Templates(locale string) ([]*Template, error) {
	out := make([]*Template, 0, len(c.templates))
	for _, raw := range c.templates {
		t, err := c.template(raw, locale)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Template renders one template by id, or nil when the catalog has none, forma2's getTemplateById.
func (c *Catalog) Template(id, locale string) (*Template, error) {
	for _, raw := range c.templates {
		if obj, ok := raw.(map[string]any); ok && obj["id"] == id {
			return c.template(raw, locale)
		}
	}
	return nil, nil
}

// copySuffix is the localized "(copy)" a duplicated name carries; n above 1 is "(copy n)".
func (c *Catalog) copySuffix(locale string, n int) string {
	if n <= 1 {
		return c.translate(locale, "common.duplicate_copy")
	}
	return strings.ReplaceAll(c.translate(locale, "common.duplicate_copy_number"), "{copyNumber}", fmt.Sprint(n))
}

// withWorkspaceName is forma2's replacePresetPlaceholders: the first $[workspaceName] in the preset's name and in every element's default headline and subheader becomes the workspace's name.
func withWorkspaceName(preset *Preset, workspaceName string) error {
	const placeholder = "$[workspaceName]"
	preset.Name = strings.Replace(preset.Name, placeholder, workspaceName, 1)
	var blocks []map[string]any
	if err := json.Unmarshal(preset.Blocks, &blocks); err != nil {
		return fmt.Errorf("decode template blocks: %w", err)
	}
	for _, block := range blocks {
		elements, _ := block["elements"].([]any)
		for _, e := range elements {
			el, _ := e.(map[string]any)
			for _, field := range []string{"headline", "subheader"} {
				if text, ok := el[field].(map[string]any); ok {
					if d, ok := text["default"].(string); ok {
						text["default"] = strings.Replace(d, placeholder, workspaceName, 1)
					}
				}
			}
		}
	}
	b, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("encode template blocks: %w", err)
	}
	preset.Blocks = b
	return nil
}
