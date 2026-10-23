package emailembed

import (
	"html"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// decl is one CSS declaration.
type decl struct{ prop, value string }

// css is an inline style in declaration order. Setting a property already present replaces its value where it stands, as spreading one style object over another does.
type css []decl

func (c css) set(prop, value string) css {
	for i := range c {
		if c[i].prop == prop {
			c[i].value = value
			return c
		}
	}
	return append(c, decl{prop, value})
}

// with is c with every declaration of the others applied over it in turn.
func (c css) with(others ...css) css {
	out := append(css(nil), c...)
	for _, o := range others {
		for _, d := range o {
			out = out.set(d.prop, d.value)
		}
	}
	return out
}

func (c css) has(prop string) bool {
	for _, d := range c {
		if d.prop == prop {
			return true
		}
	}
	return false
}

// String renders the style as React does: declarations with an empty value are left out.
func (c css) String() string {
	var parts []string
	for _, d := range c {
		if d.value != "" {
			parts = append(parts, d.prop+":"+d.value)
		}
	}
	return strings.Join(parts, ";")
}

// raw is markup inserted as it is (dangerouslySetInnerHTML).
type raw string

// node is an element. Children are nodes, text (escaped) and raw markup.
type node struct {
	tag      string
	attrs    [][2]string
	style    css
	class    string
	children []any
}

func (n *node) attr(name, value string) *node {
	n.attrs = append(n.attrs, [2]string{name, value})
	return n
}

var voidElements = map[string]bool{"img": true, "br": true, "hr": true}

func (n *node) render(b *strings.Builder) {
	b.WriteString("<" + n.tag)
	for _, a := range n.attrs {
		b.WriteString(" " + a[0] + `="` + html.EscapeString(a[1]) + `"`)
	}
	if n.class != "" {
		b.WriteString(` class="` + html.EscapeString(n.class) + `"`)
	}
	if s := n.style.String(); s != "" {
		b.WriteString(` style="` + html.EscapeString(s) + `"`)
	}
	if voidElements[n.tag] {
		b.WriteString(" />")
		return
	}
	b.WriteString(">")
	renderChildren(b, n.children)
	b.WriteString("</" + n.tag + ">")
}

func renderChildren(b *strings.Builder, children []any) {
	for _, c := range children {
		switch v := c.(type) {
		case *node:
			if v != nil {
				v.render(b)
			}
		case raw:
			b.WriteString(string(v))
		case string:
			b.WriteString(html.EscapeString(v))
		case []any:
			renderChildren(b, v)
		}
	}
}

func el(tag string, style css, children ...any) *node {
	return &node{tag: tag, style: style, children: children}
}

// presentationTable is the table every react-email layout component renders.
func presentationTable(style css) *node {
	return el("table", style).attr("align", "center").attr("width", "100%").attr("border", "0").attr("cellpadding", "0").attr("cellspacing", "0").attr("role", "presentation")
}

// The react-email components forma2's template is built from (@react-email/components 1.0.12), each with its default style under the classes and inline style it was given, as the Tailwind component merged them.

func (r *renderer) section(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	t := presentationTable(s.with(style))
	t.class = class
	t.children = []any{el("tbody", nil, el("tr", nil, el("td", nil, children...)))}
	return t
}

func (r *renderer) container(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	t := presentationTable(css{{"max-width", "37.5em"}}.with(s, style))
	t.class = class
	t.children = []any{el("tbody", nil, el("tr", css{{"width", "100%"}}, el("td", nil, children...)))}
	return t
}

func (r *renderer) row(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	t := presentationTable(s.with(style))
	t.class = class
	t.children = []any{el("tbody", css{{"width", "100%"}}, el("tr", css{{"width", "100%"}}, children...))}
	return t
}

func (r *renderer) column(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	td := el("td", s.with(style), children...).attr("data-id", "__react-email-column")
	td.class = class
	return td
}

func (r *renderer) link(href, classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	a := el("a", css{{"color", "#067df7"}, {"text-decoration-line", "none"}}.with(s, style), children...).attr("href", href).attr("target", previewLinkTarget)
	a.class = class
	return a
}

func (r *renderer) img(src, classes string, style css, attrs ...[2]string) *node {
	s, class := r.tw(classes)
	i := el("img", css{{"display", "block"}, {"outline", "none"}, {"border", "none"}, {"text-decoration", "none"}}.with(s, style)).attr("src", src)
	i.attrs = append(i.attrs, attrs...)
	i.class = class
	return i
}

// text is react-email's Text: 14px on 24px, with 16px above and below unless the style sets them, the margins spelled out per side.
func (r *renderer) text(classes string, style css, children ...any) *node {
	s, class := r.tw(classes)
	s = s.with(style)
	margins := css{}
	if !s.has("margin-top") {
		margins = margins.set("margin-top", "16px")
	}
	if !s.has("margin-bottom") {
		margins = margins.set("margin-bottom", "16px")
	}
	sides := map[string]string{"margin-top": "", "margin-right": "", "margin-bottom": "", "margin-left": ""}
	for _, d := range margins.with(s) {
		switch d.prop {
		case "margin":
			top, right, bottom, left := boxSides(d.value)
			sides = map[string]string{"margin-top": top, "margin-right": right, "margin-bottom": bottom, "margin-left": left}
		case "margin-top", "margin-right", "margin-bottom", "margin-left":
			sides[d.prop] = d.value
		}
	}
	final := css{{"font-size", "14px"}, {"line-height", "24px"}}.with(s)
	for _, side := range []string{"margin-top", "margin-right", "margin-bottom", "margin-left"} {
		if sides[side] != "" {
			final = final.set(side, sides[side])
		}
	}
	p := el("p", final, children...)
	p.class = class
	return p
}

// boxSides reads a margin or padding shorthand of one to four values.
func boxSides(v string) (top, right, bottom, left string) {
	f := strings.Fields(v)
	switch len(f) {
	case 1:
		return f[0], f[0], f[0], f[0]
	case 2:
		return f[0], f[1], f[0], f[1]
	case 3:
		return f[0], f[1], f[2], f[1]
	case 4:
		return f[0], f[1], f[2], f[3]
	}
	return "", "", "", ""
}

// toPx is react-email's convertToPx: px, em, rem and % (of 600px) as px; anything else 0.
func toPx(v string) float64 {
	units := []struct {
		suffix string
		scale  float64
	}{{"px", 1}, {"rem", 16}, {"em", 16}, {"%", 6}}
	for _, u := range units {
		if n, ok := strings.CutSuffix(v, u.suffix); ok {
			f, err := strconv.ParseFloat(n, 64)
			if err != nil || n == "" || strings.ContainsAny(n, "+-eE") {
				return 0
			}
			return f * u.scale
		}
	}
	return 0
}

// button is react-email's Button with the classes forma2 gave every button of the preview: an inline-block link padded from its style, with the hair spaces and text raise that pad it in Outlook.
func (r *renderer) button(href string, style css, children ...any) *node {
	s, class := r.tw(secondaryButtonClasses)
	s = s.with(style)
	var pad [4]string // top, right, bottom, left
	for _, d := range s {
		switch d.prop {
		case "padding":
			pad[0], pad[1], pad[2], pad[3] = boxSides(d.value)
		case "padding-top":
			pad[0] = d.value
		case "padding-right":
			pad[1] = d.value
		case "padding-bottom":
			pad[2] = d.value
		case "padding-left":
			pad[3] = d.value
		}
	}
	var px [4]float64
	set := [4]bool{}
	for i, v := range pad {
		if v != "" {
			px[i], set[i] = toPx(v), true
		}
	}
	final := css{{"line-height", "100%"}, {"text-decoration", "none"}, {"display", "inline-block"}, {"max-width", "100%"}, {"mso-padding-alt", "0px"}}.with(s)
	for i, prop := range []string{"padding-top", "padding-right", "padding-bottom", "padding-left"} {
		if set[i] {
			final = final.set(prop, cssPx(px[i]))
		}
	}
	textRaise := (px[0] + px[2]) * 3 / 4
	leftWidth, leftSpaces := fontWidthAndSpaces(px[3])
	rightWidth, rightSpaces := fontWidthAndSpaces(px[1])
	inner := css{{"max-width", "100%"}, {"display", "inline-block"}, {"line-height", "120%"}, {"mso-padding-alt", "0px"}}
	if set[2] {
		inner = inner.set("mso-text-raise", cssPx(px[2]*3/4))
	}
	a := el("a", final,
		el("span", nil, raw(`<!--[if mso]><i style="mso-font-width:`+jsvalue.FormatNumber(leftWidth*100)+`%;mso-text-raise:`+jsvalue.FormatNumber(textRaise)+`" hidden>`+strings.Repeat("&#8202;", leftSpaces)+`</i><![endif]-->`)),
		el("span", inner, children...),
		el("span", nil, raw(`<!--[if mso]><i style="mso-font-width:`+jsvalue.FormatNumber(rightWidth*100)+`%" hidden>`+strings.Repeat("&#8202;", rightSpaces)+`&#8203;</i><![endif]-->`)),
	).attr("href", href).attr("target", previewLinkTarget)
	a.class = class
	return a
}

// cssPx is a number as React writes a length: px appended unless it is 0.
func cssPx(n float64) string {
	if n == 0 {
		return "0"
	}
	return jsvalue.FormatNumber(n) + "px"
}

// fontWidthAndSpaces is react-email's computeFontWidthAndSpaceCount: the fewest hair spaces, each at most 5 times as wide, that fill width.
func fontWidthAndSpaces(width float64) (float64, int) {
	if width == 0 {
		return 0, 0
	}
	count := 1
	for width/float64(count)/2 > 5 {
		count++
	}
	return width / float64(count) / 2, count
}
