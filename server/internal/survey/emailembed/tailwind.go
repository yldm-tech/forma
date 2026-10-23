package emailembed

import (
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// staticClasses are the Tailwind utilities forma2's template uses that do not depend on the styling, as react-email's Tailwind component (Tailwind 4.3) inlined them.
var staticClasses = map[string]css{
	"m-0":            {{"margin", "0"}},
	"mr-8":           {{"margin-right", "2rem"}},
	"mr-3":           {{"margin-right", "0.75rem"}},
	"mt-2":           {{"margin-top", "0.5rem"}},
	"mt-3":           {{"margin-top", "0.75rem"}},
	"mt-4":           {{"margin-top", "1rem"}},
	"mt-8":           {{"margin-top", "2rem"}},
	"mb-2":           {{"margin-bottom", "0.5rem"}},
	"mb-3":           {{"margin-bottom", "0.75rem"}},
	"mx-0":           {{"margin-right", "0"}, {"margin-left", "0"}},
	"mx-auto":        {{"margin-right", "auto"}, {"margin-left", "auto"}},
	"my-2":           {{"margin-bottom", "0.5rem"}, {"margin-top", "0.5rem"}},
	"p-0":            {{"padding", "0"}},
	"p-8":            {{"padding", "2rem"}},
	"px-4":           {{"padding-right", "1rem"}, {"padding-left", "1rem"}},
	"py-2":           {{"padding-bottom", "0.5rem"}, {"padding-top", "0.5rem"}},
	"block":          {{"display", "block"}},
	"inline-block":   {{"display", "inline-block"}},
	"w-full":         {{"width", "100%"}},
	"h-full":         {{"height", "100%"}},
	"w-40":           {{"width", "10rem"}},
	"w-4":            {{"width", "1rem"}},
	"h-4":            {{"height", "1rem"}},
	"w-[250px]":      {{"width", "250px"}},
	"h-[150px]":      {{"height", "150px"}},
	"max-w-40":       {{"max-width", "10rem"}},
	"max-w-none":     {{"max-width", "none"}},
	"text-base":      {{"font-size", "1rem"}, {"line-height", "1.5"}},
	"text-sm":        {{"font-size", "0.875rem"}, {"line-height", "1.4285714285714286"}},
	"text-xs":        {{"font-size", "0.75rem"}, {"line-height", "1.3333333333333333"}},
	"leading-4":      {{"line-height", "1rem"}},
	"leading-5":      {{"line-height", "1.25rem"}},
	"leading-6":      {{"line-height", "1.5rem"}},
	"leading-[18px]": {{"line-height", "18px"}},
	"font-normal":    {{"font-weight", "400"}},
	"font-medium":    {{"font-weight", "500"}},
	"font-semibold":  {{"font-weight", "600"}},
	"text-inherit":   {{"color", "inherit"}},
	"text-left":      {{"text-align", "left"}},
	"text-center":    {{"text-align", "center"}},
	"text-right":     {{"text-align", "right"}},
	"table-auto":     {{"table-layout", "auto"}},
	"wrap-break-word": {
		{"overflow-wrap", "break-word"},
	},
	"border":         {{"border-style", "solid"}, {"border-width", "1px"}},
	"border-solid":   {{"border-style", "solid"}},
	"no-underline":   {{"text-decoration-line", "none"}},
	"box-border":     {{"box-sizing", "border-box"}},
	"align-middle":   {{"vertical-align", "middle"}},
	"rounded-sm":     {{"border-radius", "0.25rem"}},
	"rounded-full":   {{"border-radius", "9999px"}},
	"bg-emerald-100": {{"background-color", "rgb(208,250,229)"}},
	"bg-orange-100":  {{"background-color", "rgb(255,237,212)"}},
	"bg-rose-100":    {{"background-color", "rgb(255,228,230)"}},
}

// themeClass is a utility of the theme forma2's template configured from the style tokens; ok false for any other class.
func (t *tokens) themeClass(name string) (css, bool) {
	color := func(prop, c string) (css, bool) { return css{{prop, twColor(c)}}, true }
	length := func(value string, props ...string) (css, bool) {
		if value == "" {
			return nil, true
		}
		out := css{}
		for _, p := range props {
			out = append(out, decl{p, value})
		}
		return out, true
	}
	switch name {
	case "bg-card-bg-color":
		return color("background-color", t.cardBackgroundColor)
	case "border-card-border-color":
		return color("border-color", t.cardBorderColor)
	case "text-question-color":
		return color("color", t.questionColor)
	case "bg-input-color":
		return color("background-color", t.inputColor)
	case "text-input-placeholder":
		return color("color", t.inputPlaceholderEmailColor)
	case "text-input-text":
		return color("color", t.inputTextColor)
	case "text-signature-color":
		return color("color", t.signatureColor)
	case "bg-option-bg":
		return color("background-color", t.optionBackgroundColor)
	case "border-option-border":
		return color("border-color", t.optionBorderColor)
	case "text-option-label":
		return color("color", t.optionLabelColor)
	case "border-input-border":
		return color("border-color", t.inputBorderColor)
	case "rounded-custom":
		return length(jsvalue.FormatNumber(t.roundness)+"px", "border-radius")
	case "rounded-option":
		return length(t.optionBorderRadius, "border-radius")
	case "rounded-button":
		return length(t.buttonBorderRadius, "border-radius")
	case "px-option-x":
		return length(t.optionPaddingX, "padding-right", "padding-left")
	case "py-option-y":
		return length(t.optionPaddingY, "padding-bottom", "padding-top")
	case "px-button-x":
		return length(t.buttonPaddingX, "padding-right", "padding-left")
	case "py-button-y":
		return length(t.buttonPaddingY, "padding-bottom", "padding-top")
	case "text-input":
		return length(t.inputFontSize, "font-size")
	case "text-option":
		return length(t.optionFontSize, "font-size")
	case "text-button":
		return length(t.buttonFontSize, "font-size")
	case "font-button":
		return length(t.buttonFontWeight, "font-weight")
	case "font-survey":
		return css{{"font-family", twFontFamily(t.fontFamily)}}, true
	}
	return nil, false
}

// tw inlines classes as react-email's Tailwind component did: each known utility becomes its declarations, a leading-* utility's line height wins over a text size's, and unknown classes stay as the class attribute.
func (r *renderer) tw(classes string) (css, string) {
	out := css{}
	var leading css
	var unknown []string
	for _, name := range strings.Fields(classes) {
		decls, ok := staticClasses[name]
		if !ok {
			decls, ok = r.tok.themeClass(name)
		}
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if strings.HasPrefix(name, "leading-") {
			leading = leading.with(decls)
			continue
		}
		out = out.with(decls)
	}
	return out.with(leading), strings.Join(unknown, " ")
}

// twColor is a theme color as Tailwind inlined it: a hex color as rgb(), with its alpha when it has one; anything else as written.
func twColor(c string) string {
	hex := strings.TrimPrefix(c, "#")
	if !strings.HasPrefix(c, "#") || !hexDigits.MatchString(hex) {
		return c
	}
	var digits string
	switch len(hex) {
	case 3, 4:
		for _, ch := range hex {
			digits += strings.Repeat(string(ch), 2)
		}
	case 6, 8:
		digits = hex
	default:
		return c
	}
	parts := make([]string, 0, 4)
	for i := 0; i < 6; i += 2 {
		parts = append(parts, jsvalue.FormatNumber(float64(hexByte(digits[i:i+2]))))
	}
	if len(digits) == 8 {
		parts = append(parts, jsvalue.FormatNumber(float64(hexByte(digits[6:8]))/255))
	}
	return "rgb(" + strings.Join(parts, ",") + ")"
}

func hexByte(s string) int {
	n := 0
	for _, ch := range strings.ToLower(s) {
		n *= 16
		if ch >= 'a' {
			n += int(ch-'a') + 10
		} else {
			n += int(ch - '0')
		}
	}
	return n
}

// twFontFamily is a font stack as Tailwind inlined it: no space after the commas, quoted names in double quotes.
func twFontFamily(f string) string {
	parts := strings.Split(f, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && p[0] == '\'' && p[len(p)-1] == '\'' {
			p = `"` + p[1:len(p)-1] + `"`
		}
		parts[i] = p
	}
	return strings.Join(parts, ",")
}
