// Package emailembed is a survey's email embed, forma2's preview email template (apps/web/modules/email/components/preview-email-template.tsx rendered by getEmailTemplateHtml): the survey's first element as email-safe HTML in the survey's card style, each answer a link that opens the survey with it prefilled. The share modal's Email tab shows it and copies it, and mails it to the member as a preview (sendEmbedSurveyPreviewEmailAction).
//
// forma2 rendered the template with React Email: components that lay out in tables, a Tailwind pass that inlines every class against a theme built from the styling, and lucide icons. This package writes the same markup directly; testdata/forma2.json holds forma2's own renderings, which the tests compare against element by element and style declaration by style declaration.
package emailembed

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Locales are the catalogues the preview's copy is translated into; any other locale renders in en-US.
var Locales = []string{"en-US", "ja-JP", "zh-Hans-CN", "zh-Hant-TW"}

const defaultLocale = "en-US"

// catalogKeys are the web catalogue's keys the preview uses.
var catalogKeys = []string{
	"common.continue", "common.powered_by_forma",
	"emails.accept", "emails.click_or_drag_to_upload_files", "emails.reject", "emails.schedule_your_meeting", "emails.select_a_date",
}

// locales/*.json are those keys of web/locales; TestLocalesMatchWebLocales keeps them in step.
//
//go:embed locales/*.json
var localeFS embed.FS

// Generator renders previews. WebAppURL is where the rating icons are served (forma2's WEBAPP_URL); PublicURL is the public domain relative storage URLs resolve on.
type Generator struct {
	webAppURL string
	publicURL string
	catalog   map[string]map[string]string
}

func NewGenerator(webAppURL, publicURL string) (*Generator, error) {
	catalog := map[string]map[string]string{}
	for _, locale := range Locales {
		b, err := localeFS.ReadFile("locales/" + locale + ".json")
		if err != nil {
			return nil, fmt.Errorf("read email embed catalogue %s: %w", locale, err)
		}
		var messages map[string]string
		if err := json.Unmarshal(b, &messages); err != nil {
			return nil, fmt.Errorf("parse email embed catalogue %s: %w", locale, err)
		}
		catalog[locale] = messages
	}
	return &Generator{webAppURL: strings.TrimRight(webAppURL, "/"), publicURL: strings.TrimRight(publicURL, "/"), catalog: catalog}, nil
}

// translator is i18next's t for locale: a key missing there falls back to en-US, and one missing there too renders as the key.
func (g *Generator) translator(locale string) func(string) string {
	messages, ok := g.catalog[locale]
	if !ok {
		messages = g.catalog[defaultLocale]
	}
	return func(key string) string {
		if m, ok := messages[key]; ok {
			return m
		}
		if m, ok := g.catalog[defaultLocale][key]; ok {
			return m
		}
		return key
	}
}

// HTML is forma2's getEmailTemplateHtml: the preview of the survey whose blocks are given, in the styling it resolves to, linking to surveyURL, with its copy in locale. A survey without elements, or whose first element has no preview, renders as "".
func (g *Generator) HTML(blocks, stylingDoc json.RawMessage, surveyURL, locale string) (string, error) {
	first, ok, err := firstElement(blocks)
	if err != nil {
		return "", fmt.Errorf("decode survey blocks: %w", err)
	}
	if !ok {
		return "", nil
	}
	var s styling
	if len(stylingDoc) > 0 && string(stylingDoc) != "null" {
		if err := json.Unmarshal(stylingDoc, &s); err != nil {
			return "", fmt.Errorf("decode survey styling: %w", err)
		}
	}
	r := &renderer{tok: newTokens(s), t: g.translator(locale), webAppURL: g.webAppURL, publicURL: g.publicURL}
	n := r.render(first, surveyURL, locale)
	if n == nil {
		return "", nil
	}
	var b strings.Builder
	n.render(&b)
	return b.String(), nil
}

// ResolveStyling is forma2's getStyling: the survey's own styling when the workspace allows overriding it and the survey does, the workspace's otherwise.
func ResolveStyling(workspaceStyling, surveyStyling json.RawMessage) json.RawMessage {
	var ws struct {
		AllowStyleOverwrite bool `json:"allowStyleOverwrite"`
	}
	var ss struct {
		OverwriteThemeStyling bool `json:"overwriteThemeStyling"`
	}
	_ = json.Unmarshal(workspaceStyling, &ws)
	_ = json.Unmarshal(surveyStyling, &ss)
	if ws.AllowStyleOverwrite && ss.OverwriteThemeStyling {
		return surveyStyling
	}
	return workspaceStyling
}

// StripPreview is what forma2 did to the HTML before it left the app, in the copied embed code and the mailed preview: the survey links lose their preview=true parameter.
func StripPreview(html string) string {
	html = strings.ReplaceAll(html, "?preview=true&amp;", "?")
	html = strings.ReplaceAll(html, "?preview=true&;", "?")
	return strings.ReplaceAll(html, "?preview=true", "")
}
