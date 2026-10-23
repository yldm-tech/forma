package emailembed

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/responsemail"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// previewLinkTarget is forma2's PREVIEW_LINK_TARGET.
const previewLinkTarget = "_blank"

// The class lists forma2's styles module names.
const (
	choiceLinkClasses      = "bg-option-bg border-option-border text-option-label block rounded-option border border-solid px-option-x py-option-y font-survey text-option font-normal leading-5 no-underline"
	secondaryButtonClasses = "border-input-border inline-block rounded-button border border-solid px-button-x py-button-y font-survey text-button font-button leading-5 no-underline"
	scaleButtonClasses     = "border-input-border block w-full border border-solid p-0 text-center font-survey text-sm font-medium no-underline box-border"
	choiceMarkerClasses    = "inline-block h-4 w-4 box-border align-middle leading-4"
)

// emailRatingSmileySize is forma2's EMAIL_RATING_SMILEY_SIZE.
const emailRatingSmileySize = 40

// poweredByURL is the footer's link.
const poweredByURL = "https://forma.yldm.ai?utm_source=forma-app&utm_medium=email&utm_campaign=powered_by_badge"

// i18n is a TI18nString. getLocalizedValue reads only an object with a "default" key, and only its string values.
type i18n map[string]any

func (s *i18n) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	m, _ := v.(map[string]any)
	*s = m
	return nil
}

// local is forma2's getLocalizedValue(value, "default").
func (s i18n) local() string {
	v, _ := s["default"].(string)
	return v
}

type choice struct {
	ID       string `json:"id"`
	Label    i18n   `json:"label"`
	ImageURL string `json:"imageUrl"`
}

type toggle struct {
	Show        bool `json:"show"`
	Placeholder i18n `json:"placeholder"`
}

// element is the part of a TSurveyElement the preview reads.
type element struct {
	ID                   string   `json:"id"`
	Type                 string   `json:"type"`
	Headline             i18n     `json:"headline"`
	Subheader            i18n     `json:"subheader"`
	Placeholder          i18n     `json:"placeholder"`
	LongAnswer           *bool    `json:"longAnswer"`
	Label                i18n     `json:"label"`
	Required             bool     `json:"required"`
	IsColorCodingEnabled bool     `json:"isColorCodingEnabled"`
	LowerLabel           i18n     `json:"lowerLabel"`
	UpperLabel           i18n     `json:"upperLabel"`
	Scale                string   `json:"scale"`
	Range                float64  `json:"range"`
	Choices              []choice `json:"choices"`
	Rows                 []choice `json:"rows"`
	Columns              []choice `json:"columns"`
	ButtonExternal       bool     `json:"buttonExternal"`
	CTAButtonLabel       i18n     `json:"ctaButtonLabel"`
	ButtonURL            string   `json:"buttonUrl"`
	AddressLine1         *toggle  `json:"addressLine1"`
	AddressLine2         *toggle  `json:"addressLine2"`
	City                 *toggle  `json:"city"`
	State                *toggle  `json:"state"`
	Zip                  *toggle  `json:"zip"`
	Country              *toggle  `json:"country"`
	FirstName            *toggle  `json:"firstName"`
	LastName             *toggle  `json:"lastName"`
	Email                *toggle  `json:"email"`
	Phone                *toggle  `json:"phone"`
	Company              *toggle  `json:"company"`
}

// firstElement is the first element of the survey's blocks (forma2's getElementsFromBlocks(blocks)[0]); ok false when it has none.
func firstElement(blocks json.RawMessage) (*element, bool, error) {
	var bs []struct {
		Elements []element `json:"elements"`
	}
	if len(blocks) > 0 && string(blocks) != "null" {
		if err := json.Unmarshal(blocks, &bs); err != nil {
			return nil, false, err
		}
	}
	for _, b := range bs {
		if len(b.Elements) > 0 {
			return &b.Elements[0], true, nil
		}
	}
	return nil, false, nil
}

// renderer renders one preview: the style tokens, the translations of the viewer's locale, and where the survey and its images live.
type renderer struct {
	tok       *tokens
	t         func(key string) string
	webAppURL string
	publicURL string
}

func important(v string) string { return v + " !important" }

const forceLightColorScheme = "only light"

func forcedBackground(c string) css {
	return css{{"background", important(c)}, {"background-color", important(c)}, {"color-scheme", forceLightColorScheme}}
}

func forcedColor(c string) css {
	return css{{"color", important(c)}, {"color-scheme", forceLightColorScheme}}
}

func (r *renderer) choiceCardStyle() css {
	return forcedBackground(r.tok.optionBackgroundColor).with(css{{"border", important("1px solid " + r.tok.optionBorderColor)}}, forcedColor(r.tok.optionLabelColor), css{{"text-decoration", "none"}})
}

func (r *renderer) fieldPlaceholderStyle() css {
	return forcedBackground(r.tok.inputBackgroundColor).with(forcedColor(r.tok.inputPlaceholderEmailColor), css{{"border", important("1px solid " + r.tok.inputBorderColor)}})
}

func (r *renderer) inputTextStyle() css { return forcedColor(r.tok.inputTextColor) }

func (r *renderer) fieldLabelStyle() css {
	return forcedColor(r.tok.questionColor).with(css{{"font-family", r.tok.fontFamily}})
}

func (r *renderer) secondaryButtonStyle() css {
	return css{{"border", important("1px solid " + r.tok.inputBorderColor)}}.with(forcedColor(r.tok.questionColor))
}

func (r *renderer) primaryButtonStyle() css {
	return r.secondaryButtonStyle().with(forcedBackground(r.tok.buttonBackgroundColor), css{{"border", important("1px solid " + r.tok.buttonBackgroundColor)}}, forcedColor(r.tok.buttonTextColor))
}

func (r *renderer) lightModeTextStyle() css {
	return forcedColor(r.tok.elementHeadlineColor).with(css{{"font-family", r.tok.fontFamily}, {"font-size", r.tok.elementHeadlineFontSize}, {"font-weight", r.tok.elementHeadlineFontWeight}})
}

func (r *renderer) helperLabelTextStyle() css {
	return forcedColor(r.tok.elementUpperLabelColor).with(css{{"font-family", r.tok.fontFamily}, {"font-size", r.tok.elementUpperLabelFontSize}, {"font-weight", r.tok.elementUpperLabelFontWeight}})
}

func (r *renderer) inputShellStyle(overrides ...css) css {
	t := r.tok
	return r.fieldPlaceholderStyle().with(css{
		{"box-sizing", "border-box"}, {"border-radius", t.inputBorderRadius}, {"display", "block"}, {"font-family", t.fontFamily}, {"font-size", t.inputFontSize},
		{"line-height", "20px"}, {"overflow", "hidden"}, {"padding", t.inputPaddingY + " " + t.inputPaddingX}, {"width", "100%"},
	}).with(overrides...)
}

func (r *renderer) inputShellLinkStyle(overrides ...css) css {
	t := r.tok
	return r.inputTextStyle().with(css{
		{"display", "block"}, {"font-family", t.fontFamily}, {"font-size", t.inputFontSize}, {"font-weight", "400"}, {"line-height", "20px"},
		{"text-decoration", important("none")}, {"width", "100%"},
	}).with(overrides...)
}

func scaleColumnStyle(count int) css {
	return css{{"width", strconv.FormatFloat(100/float64(count), 'f', 4, 64) + "%"}}
}

type marker string

const (
	markerCheckbox marker = "checkbox"
	markerRadio    marker = "radio"
	markerRanking  marker = "ranking"
)

func (r *renderer) choiceMarkerStyle(m marker) css {
	if m == markerRanking {
		return css{{"background", important("transparent")}, {"background-color", important("transparent")}, {"color-scheme", forceLightColorScheme}, {"border", important("1px dashed " + r.tok.brandColor)}}
	}
	return forcedBackground("#ffffff").with(css{{"border", important("1px solid " + r.tok.inputBorderColor)}})
}

func choiceMarkerClass(m marker, withGap bool) string {
	c := choiceMarkerClasses + " rounded-full"
	if m == markerCheckbox {
		c = choiceMarkerClasses + " rounded-sm"
	}
	if withGap {
		return "mr-3 " + c
	}
	return c
}

// span is a plain element the Tailwind component inlined the classes of.
func (r *renderer) span(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	n := el("span", s.with(style), children...)
	n.class = class
	return n
}

// previewAccentColors are forma2's EMAIL_PREVIEW_ACCENT_COLORS.
var previewAccentColors = map[string]string{
	"bg-emerald-100": "#d1fae5", "bg-orange-100": "#ffedd5", "bg-rose-100": "#ffe4e6",
	"emerald-100": "#d1fae5", "orange-100": "#ffedd5", "rose-100": "#ffe4e6",
}

// npsOptionColor is forma2's getNPSOptionColor.
func npsOptionColor(i int) string {
	switch {
	case i > 8:
		return "bg-emerald-100"
	case i > 6:
		return "bg-orange-100"
	}
	return "bg-rose-100"
}

// ratingNumberOptionColor is forma2's getRatingNumberOptionColor.
func ratingNumberOptionColor(rangeN, idx int) string {
	d := rangeN - idx
	switch {
	case rangeN > 5:
		if d < 2 {
			return "emerald-100"
		}
		if d < 4 {
			return "orange-100"
		}
	case rangeN < 5:
		if d < 1 {
			return "emerald-100"
		}
		if d < 2 {
			return "orange-100"
		}
	default:
		if d < 2 {
			return "emerald-100"
		}
		if d < 3 {
			return "orange-100"
		}
	}
	return "rose-100"
}

// smileyColor is forma2's getSmileyColor.
func smileyColor(rangeN, idx int) string {
	d := rangeN - idx
	switch {
	case rangeN > 5:
		if d < 3 {
			return "bg-emerald-100"
		}
		if d < 5 {
			return "bg-orange-100"
		}
	case rangeN < 5:
		if d < 2 {
			return "bg-emerald-100"
		}
		if d < 3 {
			return "bg-orange-100"
		}
	default:
		if d < 3 {
			return "bg-emerald-100"
		}
		if d < 4 {
			return "bg-orange-100"
		}
	}
	return "bg-rose-100"
}

// scaleOption are getScaleOptionStyle's options.
type scaleOption struct {
	borderTopColor string
	height         string
	connected      bool
	transparent    bool
	count, index   int
}

func (r *renderer) scaleOptionStyle(o scaleOption) css {
	t := r.tok
	height := o.height
	if height == "" {
		height = "46px"
	}
	lineHeight := height
	if o.borderTopColor != "" && strings.HasSuffix(height, "px") {
		if m := leadingFloat.FindString(jsvalue.Trim(height)); m != "" && !strings.Contains(m, "Infinity") {
			if n, err := strconv.ParseFloat(m, 64); err == nil {
				lineHeight = jsvalue.FormatNumber(math.Max(n-6, 0)) + "px"
			}
		}
	}
	connect := o.connected && o.count > 0
	background, border := t.inputBackgroundColor, "1px solid "+t.inputBorderColor
	if o.transparent {
		background, border = "transparent", "1px solid transparent"
	}
	radius := t.inputBorderRadius
	if connect {
		switch {
		case o.count <= 1:
		case o.index == 0:
			radius = t.inputBorderRadius + " 0 0 " + t.inputBorderRadius
		case o.index == o.count-1:
			radius = "0 " + t.inputBorderRadius + " " + t.inputBorderRadius + " 0"
		default:
			radius = "0"
		}
	}
	s := forcedBackground(background).with(css{{"border", important(border)}, {"border-radius", radius}})
	if connect && o.index > 0 {
		s = s.set("border-left", important("0"))
	}
	s = s.with(forcedColor(t.inputTextColor), css{{"height", height}, {"line-height", lineHeight}})
	if o.borderTopColor != "" {
		s = s.set("border-top", important("6px solid "+o.borderTopColor))
	}
	return s
}

// The rich-text attribute patterns of forma2's styles module; JavaScript's lazy `(.*?)\1` reads up to the next matching quote on the same line.
var (
	richTextStyleAttribute = regexp.MustCompile(`(?i)\sstyle=(?:"([^"\n\r\x{2028}\x{2029}]*)"|'([^'\n\r\x{2028}\x{2029}]*)')`)
	richTextClassAttribute = regexp.MustCompile(`(?i)\sclass=(?:"([^"\n\r\x{2028}\x{2029}]*)"|'([^'\n\r\x{2028}\x{2029}]*)')`)
)

const (
	nestedListItemClass       = "fb-editor-nested-listitem"
	nestedListItemMarkerStyle = "list-style-type:none"
)

// rewriteStyles applies fn to the value of every style attribute in attributes, keeping its quote.
func rewriteStyles(attributes string, fn func(value string) string) string {
	return richTextStyleAttribute.ReplaceAllStringFunc(attributes, func(m string) string {
		sub := richTextStyleAttribute.FindStringSubmatch(m)
		quote, value := `"`, sub[1]
		if strings.HasPrefix(m[len(m)-1:], "'") {
			quote, value = "'", sub[2]
		}
		return " style=" + quote + fn(value) + quote
	})
}

// normalizeRichTextSpacing is forma2's normalizeRichTextSpacing: every paragraph without margins, appended to its style or as one.
func normalizeRichTextSpacing(s string) string {
	return replaceOpeningTags(s, "p", func(attributes, _ string) string {
		if richTextStyleAttribute.MatchString(attributes) {
			return "<p" + rewriteStyles(attributes, func(v string) string {
				if trimmed := jsvalue.Trim(v); trimmed != "" {
					return trimmed + ";margin:0"
				}
				return "margin:0"
			}) + ">"
		}
		return "<p" + attributes + ` style="margin:0">`
	})
}

// suppressNestedListMarkers is forma2's suppressNestedListMarkers: the structural list item Lexical wraps a nested list in shows no marker.
func suppressNestedListMarkers(s string) string {
	return replaceOpeningTags(s, "li", func(attributes, tag string) string {
		var classes []string
		if m := richTextClassAttribute.FindStringSubmatch(attributes); m != nil {
			classes = regexp.MustCompile(`\s+`).Split(m[1]+m[2], -1)
		}
		nested := false
		for _, c := range classes {
			nested = nested || c == nestedListItemClass
		}
		if !nested {
			return tag
		}
		if richTextStyleAttribute.MatchString(attributes) {
			return "<li" + rewriteStyles(attributes, func(v string) string {
				trimmed := jsvalue.Trim(v)
				if strings.Contains(trimmed, nestedListItemMarkerStyle) {
					return v
				}
				if trimmed != "" {
					return trimmed + ";" + nestedListItemMarkerStyle
				}
				return nestedListItemMarkerStyle
			}) + ">"
		}
		return "<li" + attributes + ` style="` + nestedListItemMarkerStyle + `">`
	})
}

// replaceOpeningTags is forma2's replaceOpeningTags: every `<name …>` (ASCII case-insensitive, the name ending at a non-word character) replaced by fn(attributes, tag), scanning on after each replacement.
func replaceOpeningTags(s, name string, fn func(attributes, tag string) string) string {
	var b strings.Builder
	lower := strings.ToLower(s)
	prefix := "<" + name
	copied, from := 0, 0
	for {
		i := strings.Index(lower[from:], prefix)
		if i < 0 {
			break
		}
		start := from + i
		after := start + len(prefix)
		if after < len(s) && isWordByte(s[after]) {
			from = start + 1
			continue
		}
		end := strings.IndexByte(s[after:], '>')
		if end < 0 {
			break
		}
		end += after
		b.WriteString(s[copied:start])
		b.WriteString(fn(s[after:end], s[start:end+1]))
		copied, from = end+1, end+1
	}
	if copied == 0 {
		return s
	}
	b.WriteString(s[copied:])
	return b.String()
}

func isWordByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// prefilledURL is forma2's buildSurveyEmailUrl: the survey URL with the parameters set, in URLSearchParams order and encoding.
func prefilledURL(surveyURL string, params ...[2]string) string {
	base, query, _ := strings.Cut(surveyURL, "?")
	hash := ""
	if i := strings.IndexByte(query, '#'); i >= 0 {
		query, hash = query[:i], query[i:]
	} else if i := strings.IndexByte(base, '#'); i >= 0 {
		base, hash = base[:i], base[i:]
	}
	var pairs [][2]string
	if query != "" {
		for _, part := range strings.Split(query, "&") {
			if part == "" {
				continue
			}
			k, v, _ := strings.Cut(part, "=")
			pairs = append(pairs, [2]string{formDecode(k), formDecode(v)})
		}
	}
	for _, p := range params {
		found := false
		out := pairs[:0:0]
		for _, q := range pairs {
			if q[0] != p[0] {
				out = append(out, q)
			} else if !found {
				out = append(out, p)
				found = true
			}
		}
		if !found {
			out = append(out, p)
		}
		pairs = out
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = formEncode(p[0]) + "=" + formEncode(p[1])
	}
	return base + "?" + strings.Join(parts, "&") + hash
}

// formEncode is the application/x-www-form-urlencoded serializer URLSearchParams uses.
func formEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)>>4, 16)+strconv.FormatInt(int64(c)&15, 16)))
		}
	}
	return b.String()
}

func formDecode(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (r *renderer) previewURL(surveyURL string) string {
	return prefilledURL(surveyURL, [2]string{"preview", "true"})
}

func (r *renderer) answerURL(surveyURL, elementID, value string) string {
	return prefilledURL(surveyURL, [2]string{"preview", "true"}, [2]string{elementID, value}, [2]string{"skipPrefilled", "true"})
}

// resolveStorageURL is forma2's resolveStorageUrl for a public file: a relative /storage path on the public domain.
func (r *renderer) resolveStorageURL(u string) string {
	return responsemail.ResolveStorageURL(r.publicURL, u)
}

// elementHeader is forma2's PreviewElementHeader over the email package's ElementHeader.
func (r *renderer) elementHeader(headline, subheader, className string) []any {
	text := r.lightModeTextStyle()
	out := []any{r.container("text-question-color m-0 block text-base leading-6 font-semibold "+className, text,
		el("div", nil, raw(suppressNestedListMarkers(normalizeRichTextSpacing(headline)))))}
	if subheader != "" {
		sub := text.with(forcedColor(r.tok.elementDescriptionColor), css{{"font-size", r.tok.elementDescriptionFontSize}, {"font-weight", r.tok.elementDescriptionFontWeight}})
		out = append(out, r.container("text-question-color m-0 mt-2 block p-0 text-sm leading-6 font-normal", sub,
			el("div", nil, raw(suppressNestedListMarkers(normalizeRichTextSpacing(subheader))))))
	}
	return out
}

// card is forma2's PreviewEmailCard: the content in the card the styling describes, with the footer.
func (r *renderer) card(children ...any) *node {
	t := r.tok
	footer := r.container("mx-auto mt-8 text-center", nil,
		r.link(poweredByURL, "text-signature-color text-xs", forcedColor(t.signatureColor).with(css{{"font-family", t.fontFamily}}), r.t("common.powered_by_forma")))
	style := forcedBackground(t.cardBackgroundColor).with(
		css{{"border", important("1px solid " + t.cardBorderColor)}, {"border-radius", jsvalue.FormatNumber(t.roundness) + "px"}},
		forcedColor(t.questionColor), css{{"font-family", t.fontFamily}})
	s := r.section("bg-card-bg-color border-card-border-color rounded-custom mx-0 my-2 border border-solid p-8 text-inherit", style, append(children, footer)...)
	s.attrs = append(s.attrs, [2]string{"bgcolor", t.cardBackgroundColor})
	return s
}

// questionCard is forma2's PreviewQuestionCard: the card with the element's header above the content.
func (r *renderer) questionCard(headline, subheader string, children ...any) *node {
	return r.card(append(r.elementHeader(headline, subheader, "mr-8"), children...)...)
}

// layoutTable is the full-width table forma2's open-text and input-link previews lay their shell out in.
func layoutTable(style css, shell *node) *node {
	return el("table", style, el("tbody", nil, el("tr", nil, el("td", css{{"width", "100%"}}, shell)))).
		attr("border", "0").attr("cellpadding", "0").attr("cellspacing", "0").attr("role", "presentation").attr("width", "100%")
}

func (r *renderer) choiceList(choices []choice, href func(id string) string, m marker) *node {
	sections := make([]any, 0, len(choices))
	for _, c := range choices {
		label := c.Label.local()
		sections = append(sections, r.section("mt-2 w-full", nil,
			r.link(href(c.ID), choiceLinkClasses, r.choiceCardStyle(),
				r.span(choiceMarkerClass(m, true), r.choiceMarkerStyle(m), " "),
				el("span", css{{"vertical-align", "middle"}}, label))))
	}
	return r.container("mx-0 max-w-none", nil, sections...)
}

// icon is a lucide-react icon (0.577) as it rendered on the server.
func icon(name string, color string, size int, style css, shapes string) *node {
	n := el("svg", style, raw(shapes)).
		attr("xmlns", "http://www.w3.org/2000/svg").attr("width", strconv.Itoa(size)).attr("height", strconv.Itoa(size)).attr("viewBox", "0 0 24 24").
		attr("fill", "none").attr("stroke", color).attr("stroke-width", "2").attr("stroke-linecap", "round").attr("stroke-linejoin", "round")
	n.class = "lucide lucide-" + name
	return n.attr("aria-hidden", "true")
}

const (
	calendarDaysShapes          = `<path d="M8 2v4"></path><path d="M16 2v4"></path><rect width="18" height="18" x="3" y="4" rx="2"></rect><path d="M3 10h18"></path><path d="M8 14h.01"></path><path d="M12 14h.01"></path><path d="M16 14h.01"></path><path d="M8 18h.01"></path><path d="M12 18h.01"></path><path d="M16 18h.01"></path>`
	uploadShapes                = `<path d="M12 3v12"></path><path d="m17 8-5-5-5 5"></path><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>`
	squareArrowOutUpRightShapes = `<path d="M21 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h6"></path><path d="m21 3-9 9"></path><path d="M15 3h6v6"></path>`
)

// inputLink is forma2's PreviewInputLink: an input-like shell that links to the survey, with an icon and a label.
type inputLink struct {
	href, iconName, iconShapes, label, minHeight string
	accent, centered, dashed                     bool
}

func (r *renderer) inputLink(o inputLink) *node {
	t := r.tok
	shell := css{}
	if o.accent {
		shell = shell.with(forcedBackground(t.accentBackgroundColor))
	}
	if o.dashed {
		shell = shell.set("border", important("2px dashed "+t.inputBorderColor))
	}
	shell = shell.with(css{{"margin-top", "1rem"}, {"text-align", "center"}})
	link := css{{"text-align", "center"}}
	if o.minHeight != "" {
		shell = shell.set("min-height", o.minHeight)
		if o.centered {
			shell = shell.with(css{{"height", o.minHeight}, {"padding-bottom", "0"}, {"padding-top", "0"}})
			link = link.with(css{{"height", o.minHeight}, {"line-height", o.minHeight}})
		}
	}
	return layoutTable(css{{"border-collapse", "separate"}, {"border-spacing", "0"}, {"width", "100%"}},
		el("div", r.inputShellStyle(shell),
			r.link(o.href, "font-survey text-input text-input-text block w-full text-center font-normal no-underline", r.inputShellLinkStyle(link),
				icon(o.iconName, t.inputTextColor, 18, css{{"display", "inline-block"}, {"margin-right", "8px"}, {"vertical-align", "middle"}}, o.iconShapes),
				el("span", css{{"vertical-align", "middle"}}, o.label))))
}

// field is one sub-field of an address or contact-info element.
type field struct {
	toggle   *toggle
	fallback string
}

func (r *renderer) fieldList(fields []field, href string) []any {
	var out []any
	for _, f := range fields {
		if f.toggle == nil || !f.toggle.Show {
			continue
		}
		label := f.toggle.Placeholder.local()
		if label == "" {
			label = f.fallback
		}
		out = append(out, r.section("mt-3 w-full", nil,
			r.text("text-question-color font-survey m-0 mb-2 text-sm leading-6 font-normal", r.fieldLabelStyle(), label),
			el("div", r.inputShellStyle(), r.link(href, "font-survey text-input text-input-placeholder block w-full no-underline", r.inputShellLinkStyle(), " "))))
	}
	return out
}

// scaleOptionColumn is forma2's PreviewScaleOptionColumn.
func (r *renderer) scaleOptionColumn(href string, count int, style css, children ...any) *node {
	return r.column("", scaleColumnStyle(count), r.link(href, scaleButtonClasses, style, children...))
}

// scaleLabels is forma2's PreviewScaleLabels: the lower label under the first option, the upper under the last.
func (r *renderer) scaleLabels(lower, upper i18n, count int) *node {
	cols := make([]any, count)
	for i := range cols {
		var label any
		if i == 0 || i == count-1 {
			text := lower.local()
			if i != 0 {
				text = upper.local()
			}
			label = r.text("m-0 text-xs leading-[18px]", r.helperLabelTextStyle().set("text-align", "center"), text)
		}
		col := r.column("", scaleColumnStyle(count).set("text-align", "center"))
		if label != nil {
			col.children = []any{label}
		}
		cols[i] = col
	}
	return r.section("mt-2 w-full", nil, r.row("", nil, cols...))
}

// smileyFaces are forma2's RatingSmiley faces and, by range, the ones it uses.
var (
	smileyFaces = []string{"tired", "weary", "persevering", "frowning", "confused", "neutral", "slightly-smiling", "smiling-face-with-smiling-eyes", "grinning-face-with-smiling-eyes", "grinning-squinting"}
	smileyIcons = map[int][]int{10: {0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, 7: {1, 3, 4, 5, 6, 8, 9}, 6: {0, 2, 4, 5, 7, 9}, 5: {3, 4, 5, 6, 7}, 4: {4, 5, 6, 7}, 3: {4, 5, 7}}
)

// smiley is forma2's RatingSmiley, inactive, centered, at the email size.
func (r *renderer) smiley(idx, rangeN int, colors bool) *node {
	face := "undefined"
	if icons, ok := smileyIcons[rangeN]; ok && idx < len(icons) {
		face = smileyFaces[icons[idx]]
	}
	color := "bg-fill-none"
	if colors {
		color = smileyColor(rangeN, idx)
	}
	size := strconv.Itoa(emailRatingSmileySize)
	box := strconv.Itoa(2*emailRatingSmileySize) + "px"
	s, class := r.tw(color + " rounded-full")
	img := el("img", s).attr("data-testid", face).attr("src", r.webAppURL+"/smiley-icons/"+face+"-face.png").attr("alt", face).attr("width", size).attr("height", size)
	img.class = class
	return el("table", css{{"width", box}, {"height", box}, {"margin-left", "auto"}, {"margin-right", "auto"}},
		el("tbody", nil, el("tr", nil, el("td", nil, img).attr("align", "center").attr("valign", "middle"))))
}

func (r *renderer) ratingContent(scale string, i, rangeN int, colors bool) any {
	switch scale {
	case "smiley":
		return r.smiley(i, rangeN, colors)
	case "number":
		return strconv.Itoa(i + 1)
	case "star":
		return r.img(r.webAppURL+"/star-icons/star.png", "", css{{"display", "inline-block"}, {"margin-top", "9px"}, {"vertical-align", "middle"}},
			[2]string{"alt", "star"}, [2]string{"width", "28"}, [2]string{"height", "28"})
	}
	return nil
}

// render is forma2's PreviewEmailTemplate: the survey's first element as email-safe HTML whose answers link to the survey prefilled.
func (r *renderer) render(first *element, surveyURL, locale string) *node {
	headline := responsemail.ParseRecall(first.Headline.local(), nil, nil, locale, false)
	subheader := responsemail.ParseRecall(first.Subheader.local(), nil, nil, locale, false)
	preview := r.previewURL(surveyURL)
	t := r.tok
	answer := func(value string) string { return r.answerURL(surveyURL, first.ID, value) }

	switch first.Type {
	case surveydoc.ElementOpenText:
		placeholder := ""
		if first.Placeholder != nil {
			placeholder = first.Placeholder.local()
		}
		if placeholder == "" {
			placeholder = " "
		}
		height := css{{"min-height", t.inputHeight}}
		if first.LongAnswer == nil || *first.LongAnswer {
			height = css{{"height", "64px"}, {"min-height", "64px"}}
		}
		shell := r.fieldPlaceholderStyle().with(r.inputShellStyle(height), css{{"text-align", "left"}})
		text := forcedColor(t.inputPlaceholderEmailColor).with(css{{"font-family", t.fontFamily}, {"font-size", t.inputFontSize}, {"font-weight", "400"}, {"line-height", "20px"}, {"text-decoration", important("none")}})
		return r.questionCard(headline, subheader, r.section("w-full", nil,
			layoutTable(css{{"border-collapse", "separate"}, {"border-spacing", "0"}, {"margin-top", "1rem"}, {"width", "100%"}},
				el("div", shell, r.link(preview, "font-survey text-input text-input-placeholder no-underline", text, el("span", text, placeholder))))))

	case surveydoc.ElementConsent:
		var buttons []any
		if !first.Required {
			buttons = append(buttons, r.button(answer("dismissed"), r.secondaryButtonStyle(), r.t("emails.reject")))
		}
		marginLeft := "8px"
		if first.Required {
			marginLeft = "0"
		}
		buttons = append(buttons, r.button(answer("accepted"), r.primaryButtonStyle().set("margin-left", marginLeft), r.t("emails.accept")))
		return r.questionCard(headline, subheader,
			r.section("mt-4 w-full", nil, el("div", r.inputShellStyle(), el("span", r.inputTextStyle().with(css{{"font-family", t.fontFamily}, {"font-size", "16px"}, {"font-weight", "500"}, {"line-height", "24px"}}), first.Label.local()))),
			r.section("mt-4 text-right", nil, buttons...))

	case surveydoc.ElementNPS:
		const count = 11
		height := "41px"
		if first.IsColorCodingEnabled {
			height = "47px"
		}
		cols := make([]any, count)
		for i := range cols {
			o := scaleOption{height: height, connected: true, count: count, index: i}
			if first.IsColorCodingEnabled {
				o.borderTopColor = previewAccentColors[npsOptionColor(i)]
			}
			cols[i] = r.scaleOptionColumn(answer(strconv.Itoa(i)), count, r.scaleOptionStyle(o), strconv.Itoa(i))
		}
		return r.card(r.section("w-full", nil, append(r.elementHeader(headline, subheader, ""),
			r.container("mx-0 mt-4 w-full max-w-none", nil, r.section("w-full", nil, r.row("", nil, cols...)), r.scaleLabels(first.LowerLabel, first.UpperLabel, count)))...))

	case surveydoc.ElementCTA:
		var content []any
		if first.ButtonExternal && first.CTAButtonLabel != nil && first.ButtonURL != "" && surveydoc.IsSafeLinkURL(first.ButtonURL) {
			content = append(content, r.section("mt-4 text-left", css{{"text-align", "left"}},
				r.button(first.ButtonURL, r.primaryButtonStyle(), first.CTAButtonLabel.local(),
					icon("square-arrow-out-up-right", t.buttonTextColor, 16, css{{"display", "inline-block"}, {"margin-left", "8px"}, {"vertical-align", "text-bottom"}}, squareArrowOutUpRightShapes))))
		}
		return r.questionCard(headline, subheader, content...)

	case surveydoc.ElementRating, surveydoc.ElementCSAT, surveydoc.ElementCES:
		count := int(first.Range)
		number := first.Scale == "number"
		height := ""
		if number {
			height = "41px"
			if first.IsColorCodingEnabled {
				height = "47px"
			}
		}
		cols := make([]any, 0, count)
		for i := range count {
			o := scaleOption{height: height, connected: number, transparent: first.Scale == "star" || first.Scale == "smiley", count: count, index: i}
			if first.IsColorCodingEnabled && number {
				o.borderTopColor = previewAccentColors[ratingNumberOptionColor(count, i+1)]
			}
			var content []any
			if c := r.ratingContent(first.Scale, i, count, first.IsColorCodingEnabled); c != nil {
				content = append(content, c)
			}
			cols = append(cols, r.scaleOptionColumn(answer(strconv.Itoa(i+1)), count, r.scaleOptionStyle(o), content...))
		}
		return r.card(r.section("w-full", nil, append(r.elementHeader(headline, subheader, ""),
			r.container("mx-0 mt-4 w-full max-w-none", nil, r.section("w-full", nil, r.row("", nil, cols...)), r.scaleLabels(first.LowerLabel, first.UpperLabel, count)))...))

	case surveydoc.ElementMultipleChoiceMulti:
		return r.questionCard(headline, subheader, r.choiceList(first.Choices, answer, markerCheckbox))

	case surveydoc.ElementRanking:
		return r.questionCard(headline, subheader, r.choiceList(first.Choices, func(string) string { return preview }, markerRanking))

	case surveydoc.ElementMultipleChoiceSingle:
		return r.questionCard(headline, subheader, r.choiceList(first.Choices, answer, markerRadio))

	case surveydoc.ElementPictureSelection:
		links := make([]any, 0, len(first.Choices))
		for _, c := range first.Choices {
			links = append(links, r.link(answer(c.ID), "rounded-custom mr-3 mb-3 inline-block h-[150px] w-[250px]", nil,
				r.img(r.resolveStorageURL(c.ImageURL), "rounded-custom h-full w-full", nil)))
		}
		return r.questionCard(headline, subheader, r.section("mx-0 mt-4", nil, links...))

	case surveydoc.ElementCal:
		return r.card(r.container("", nil, append(r.elementHeader(headline, subheader, ""),
			r.section("mt-4 text-center", nil, r.button(preview, r.primaryButtonStyle(), r.t("emails.schedule_your_meeting"))))...))

	case surveydoc.ElementDate:
		return r.questionCard(headline, subheader, r.section("w-full", nil,
			r.inputLink(inputLink{href: preview, iconName: "calendar-days", iconShapes: calendarDaysShapes, label: r.t("emails.select_a_date")})))

	case surveydoc.ElementMatrix:
		light := r.lightModeTextStyle()
		header := []any{r.column("w-40 px-4 py-2 wrap-break-word", nil)}
		for _, c := range first.Columns {
			header = append(header, r.column("text-question-color max-w-40 px-4 py-2 text-center wrap-break-word", light.set("text-align", "center"), c.Label.local()))
		}
		rows := []any{r.row("", nil, header...)}
		for i, row := range first.Rows {
			cells := []any{r.column("w-40 px-4 py-2 wrap-break-word", light, row.Label.local())}
			for range first.Columns {
				cells = append(cells, r.column("text-question-color px-4 py-2 text-center", light.set("text-align", "center"),
					r.span(choiceMarkerClass(markerRadio, false), r.choiceMarkerStyle(markerRadio), " ")))
			}
			if i%2 == 0 {
				rows = append(rows, r.row("bg-input-color", forcedBackground(t.inputBackgroundColor), cells...))
			} else {
				// forma2 gave the odd rows an empty className, which reaches the markup as an empty class attribute.
				rows = append(rows, r.row("", nil, cells...).attr("class", ""))
			}
		}
		return r.questionCard(headline, subheader,
			r.container("mx-0", nil, r.section("w-full table-auto", nil, rows...)),
			r.section("mt-4 text-right", nil, r.button(preview, r.primaryButtonStyle(), r.t("common.continue"))))

	case surveydoc.ElementAddress:
		return r.questionCard(headline, subheader, r.fieldList([]field{
			{first.AddressLine1, "Address line 1"}, {first.AddressLine2, "Address line 2"}, {first.City, "City"},
			{first.State, "State"}, {first.Zip, "ZIP code"}, {first.Country, "Country"},
		}, preview)...)

	case surveydoc.ElementContactInfo:
		return r.questionCard(headline, subheader, r.fieldList([]field{
			{first.FirstName, "First name"}, {first.LastName, "Last name"}, {first.Email, "Email"}, {first.Phone, "Phone"}, {first.Company, "Company"},
		}, preview)...)

	case surveydoc.ElementFileUpload:
		return r.questionCard(headline, subheader, r.section("w-full", nil,
			r.inputLink(inputLink{href: preview, iconName: "upload", iconShapes: uploadShapes, label: r.t("emails.click_or_drag_to_upload_files"), minHeight: "96px", accent: true, centered: true, dashed: true})))
	}
	return nil
}
