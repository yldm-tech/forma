package linksurvey

import (
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/survey"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// The document head forma2's Next.js metadata produced for the survey pages: the root layout's title template and description, the link layout's viewport, and the page's own metadata.
const (
	// titleTemplate is the root layout's "%s | Forma"; rootTitle and rootDescription are its defaults.
	titleSuffix     = " | Forma"
	rootTitle       = "Forma"
	rootDescription = "Open-Source Survey Suite"
	// linkViewport is the link survey layout's viewport (viewportFit "contain"); rootViewport is Next's default, which /c and the /p not-found page keep.
	linkViewport = "width=device-width, initial-scale=1, viewport-fit=contain"
	rootViewport = "width=device-width, initial-scale=1"
	// defaultDescription is getBasicSurveyMetadata's fallback description, defaultTitle its title for a survey it cannot read.
	defaultDescription = "Please complete this survey."
	defaultTitle       = "Survey"
	// defaultBrandColor is forma2's COLOR_DEFAULTS.brandColor.
	defaultBrandColor = "#1e40af"
)

type metaTag struct {
	// Name or Property names the tag; exactly one is set.
	Name, Property, Content string
}

type linkTag struct {
	Rel, Href string
}

// head is one page's metadata.
type head struct {
	Title    string
	Viewport string
	Metas    []metaTag
	Links    []linkTag
}

// notFoundHead is what Next rendered with a 404: the root defaults and robots noindex.
func notFoundHead(viewport string) head {
	return head{Title: rootTitle, Viewport: viewport, Metas: []metaTag{{Name: "robots", Content: "noindex"}, {Name: "description", Content: rootDescription}}}
}

// render writes the tags in the order Next.js emitted them.
func (h head) render() string {
	var b strings.Builder
	b.WriteString("<title>" + html.EscapeString(h.Title) + "</title>")
	b.WriteString(`<meta name="viewport" content="` + html.EscapeString(h.Viewport) + `">`)
	for _, m := range h.Metas {
		attr, key := "name", m.Name
		if m.Property != "" {
			attr, key = "property", m.Property
		}
		b.WriteString("<meta " + attr + `="` + html.EscapeString(key) + `" content="` + html.EscapeString(m.Content) + `">`)
	}
	for _, l := range h.Links {
		b.WriteString(`<link rel="` + html.EscapeString(l.Rel) + `" href="` + html.EscapeString(l.Href) + `">`)
	}
	return b.String()
}

// linkMetadata is forma2's metadata document for a survey page, before the page-specific parts (canonical and robots on /s).
type linkMetadata struct {
	Title       string
	Description string
	// OgImage is the author's own OG image, replacing the generated card.
	OgImage string
}

// surveyMetadata is the link metadata document a survey stores (TSurveyMetadata).
type surveyMetadata struct {
	Title       surveydoc.I18nString `json:"title"`
	Description surveydoc.I18nString `json:"description"`
	OgImage     string               `json:"ogImage"`
}

// basicSurveyMetadata is forma2's getBasicSurveyMetadata: the title from the link metadata, else the welcome card's headline as text, else the survey's name; the description from the link metadata, else the default; Forma Cloud appends " | Forma" to a title the author did not set.
func basicSurveyMetadata(sv *survey.Survey, lang string, isCloud bool) linkMetadata {
	langCode := metadataLanguageCode(lang, sv.Languages)
	var md surveyMetadata
	_ = json.Unmarshal(sv.Metadata, &md)
	titleFromMetadata := md.Title[langCode]
	title := titleFromMetadata
	if title == "" {
		var wc surveydoc.WelcomeCard
		if json.Unmarshal(sv.WelcomeCard, &wc) == nil && wc.Enabled && wc.Headline != nil {
			title = surveydoc.TextContent(recallToHeadline(wc.Headline[langCode], sv, langCode))
		}
	}
	if title == "" {
		title = sv.Name
	}
	description := md.Description[langCode]
	if description == "" {
		description = defaultDescription
	}
	if titleFromMetadata == "" && isCloud {
		title += titleSuffix
	}
	return linkMetadata{Title: title, Description: description, OgImage: md.OgImage}
}

// brandColor is forma2's getMetadataBrandColor: the workspace's light brand colour, unless the workspace allows overrides and the survey overrides the theme.
func brandColor(workspaceStyling, surveyStyling json.RawMessage) string {
	type styling struct {
		AllowStyleOverwrite   bool `json:"allowStyleOverwrite"`
		OverwriteThemeStyling bool `json:"overwriteThemeStyling"`
		BrandColor            *struct {
			Light string `json:"light"`
		} `json:"brandColor"`
	}
	var ws, ss styling
	_ = json.Unmarshal(workspaceStyling, &ws)
	_ = json.Unmarshal(surveyStyling, &ss)
	chosen := ws
	if ws.AllowStyleOverwrite && ss.OverwriteThemeStyling {
		chosen = ss
	}
	if chosen.BrandColor == nil {
		return ""
	}
	return chosen.BrandColor.Light
}

// pageHead assembles the head of a survey page: forma2's getSurveyOpenGraphMetadata with the description and custom OG image overridden, the organization's favicon, and on /s the canonical link and robots directives.
func pageHead(base *url.URL, sv *survey.Survey, md linkMetadata, color, favicon string, viewport string, onLinkPage bool) head {
	if color == "" {
		color = defaultBrandColor
	}
	image := "/api/v1/client/og?brandColor=" + encodeURIComponent(color) + "&name=" + encodeURIComponent(md.Title)
	if md.OgImage != "" {
		image = md.OgImage
	}
	canonical := resolve(base, "/s/"+sv.ID.String())
	h := head{Title: md.Title + titleSuffix, Viewport: viewport}
	h.Metas = append(h.Metas, metaTag{Name: "description", Content: md.Description})
	if onLinkPage {
		h.Metas = append(h.Metas,
			metaTag{Name: "robots", Content: "noindex, follow"},
			metaTag{Name: "googlebot", Content: "noindex, follow, noimageindex"},
		)
	}
	h.Metas = append(h.Metas,
		metaTag{Property: "og:title", Content: md.Title},
		metaTag{Property: "og:description", Content: md.Description},
		metaTag{Property: "og:url", Content: canonical},
		metaTag{Property: "og:locale", Content: "en_US"},
		metaTag{Property: "og:image", Content: resolve(base, image)},
		metaTag{Property: "og:type", Content: "website"},
		metaTag{Name: "twitter:card", Content: "summary_large_image"},
		metaTag{Name: "twitter:title", Content: md.Title},
		metaTag{Name: "twitter:description", Content: md.Description},
		metaTag{Name: "twitter:image", Content: resolve(base, image)},
	)
	if onLinkPage {
		h.Links = append(h.Links, linkTag{Rel: "canonical", Href: canonical})
	}
	if favicon != "" {
		h.Links = append(h.Links, linkTag{Rel: "icon", Href: resolve(base, favicon)})
	}
	return h
}

// genericHead is the metadata of a personal link that does not resolve to a survey.
func genericHead() head {
	return head{Title: defaultTitle + titleSuffix, Viewport: rootViewport, Metas: []metaTag{{Name: "description", Content: defaultDescription}}}
}

// resolve is Next's metadataBase resolution: a relative URL is resolved against the public domain, an absolute one is kept.
func resolve(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

// encodeURIComponent is JavaScript's: everything but letters, digits and -_.!~*'() is percent-encoded as UTF-8.
func encodeURIComponent(s string) string {
	const unreserved = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(hexByte(c)))
	}
	return b.String()
}

func hexByte(c byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[c>>4], digits[c&0x0f]})
}

// recallPattern is forma2's RECALL_TOKEN_PATTERN.
var recallPattern = regexp.MustCompile(`#recall:([A-Za-z0-9_-]+)/fallback:([^#]*)#`)

// reservedFieldNames are the names in forma2's RESERVED_FIELD_CATALOG, in catalog order.
var reservedFieldNames = []string{
	"source", "url", "country", "action", "browser", "os", "deviceType", "ipAddress", "finished", "language", "responseId", "surveyId",
	"durationSeconds", "startedAt", "finishedAt", "pagePath", "pageReferrer", "utmSource", "utmMedium", "utmCampaign", "utmTerm", "utmContent",
	"screenWidth", "screenHeight", "viewportWidth", "viewportHeight", "timezone", "locale",
}

var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// titleCase is forma2's formatFieldNameToTitleCase: camelCase and snake_case words, each capitalised.
func titleCase(name string) string {
	words := strings.Split(camelBoundary.ReplaceAllString(name, "${1}_${2}"), "_")
	out := make([]string, 0, len(words))
	for _, w := range words {
		if w != "" {
			out = append(out, strings.ToUpper(w[:1])+w[1:])
		}
	}
	return strings.Join(out, " ")
}

// recallToHeadline is forma2's recallToHeadline without the slash form: each recall token becomes "@" and the label of what it recalls, an ingested field's name, an element's headline as text, a computed field's name or a reserved field's title, in that order, else the token's id. Nested tokens inside a label become "___". Labels come from the fields the survey declares, as forma2's getDeclaredEmbeddedFields derives them.
func recallToHeadline(text string, sv *survey.Survey, lang string) string {
	if !strings.Contains(text, "#recall:") {
		return text
	}
	var blocks []struct {
		Elements []struct {
			ID       string               `json:"id"`
			Headline surveydoc.I18nString `json:"headline"`
		} `json:"elements"`
	}
	_ = json.Unmarshal(sv.Blocks, &blocks)
	var variables []embeddeddata.Variable
	_ = json.Unmarshal(sv.Variables, &variables)
	var hidden surveydoc.HiddenFields
	_ = json.Unmarshal(sv.HiddenFields, &hidden)
	declared := embeddeddata.DesiredFields(variables, hidden.FieldIDs)

	label := func(id string) string {
		for _, f := range declared {
			if f.StorageKey == id && f.Source == embeddeddata.SourceIngested {
				return f.Name
			}
		}
		for _, b := range blocks {
			for _, el := range b.Elements {
				if el.ID == id {
					if h := el.Headline[lang]; h != "" {
						return surveydoc.TextContent(h)
					}
					return ""
				}
			}
		}
		for _, f := range declared {
			if f.StorageKey == id && f.Source == embeddeddata.SourceComputed {
				return f.Name
			}
		}
		for _, name := range reservedFieldNames {
			if name == id {
				return titleCase(name)
			}
		}
		return ""
	}
	return recallPattern.ReplaceAllStringFunc(text, func(token string) string {
		id := recallPattern.FindStringSubmatch(token)[1]
		l := label(id)
		if l == "" {
			l = id
		}
		return "@" + recallPattern.ReplaceAllLiteralString(l, "___")
	})
}
