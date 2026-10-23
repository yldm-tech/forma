package emailembed

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// recording is testdata/forma2.json: forma2's own renderings of the preview, recorded by running its getPreviewEmailTemplateHtml and extractEmailBodyFragment (at 3db326f, with @react-email/components 1.0.12, @react-email/render 2.1.0, Tailwind 4.3.1 and en-US copy) on the surveys of its embed-survey-preview-email fixture, one per element type, plus variants of scale, color coding, required, rich text with recall, and stylings from none to every token overridden. tokens is getPreviewEmailStyleTokens of each case's styling.
type recording struct {
	WebAppURL string `json:"webAppUrl"`
	PublicURL string `json:"publicUrl"`
	Locale    string `json:"locale"`
	Cases     []struct {
		Name      string          `json:"name"`
		SurveyURL string          `json:"surveyUrl"`
		Styling   json.RawMessage `json:"styling"`
		Blocks    json.RawMessage `json:"blocks"`
		Tokens    map[string]any  `json:"tokens"`
		HTML      string          `json:"html"`
	} `json:"cases"`
}

func loadRecording(t *testing.T) recording {
	t.Helper()
	b, err := os.ReadFile("testdata/forma2.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec recording
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

// TestTokensMatchForma2 compares the resolved style tokens with forma2's for every recorded styling.
func TestTokensMatchForma2(t *testing.T) {
	for _, c := range loadRecording(t).Cases {
		var s styling
		if err := json.Unmarshal(c.Styling, &s); err != nil {
			t.Fatal(err)
		}
		tok := newTokens(s)
		got := map[string]any{
			"accentBackgroundColor": tok.accentBackgroundColor, "brandColor": tok.brandColor, "buttonBackgroundColor": tok.buttonBackgroundColor,
			"buttonBorderRadius": tok.buttonBorderRadius, "buttonFontSize": tok.buttonFontSize, "buttonFontWeight": tok.buttonFontWeight,
			"buttonPaddingX": tok.buttonPaddingX, "buttonPaddingY": tok.buttonPaddingY, "buttonTextColor": tok.buttonTextColor,
			"cardBackgroundColor": tok.cardBackgroundColor, "cardBorderColor": tok.cardBorderColor, "elementDescriptionColor": tok.elementDescriptionColor,
			"elementDescriptionFontSize": tok.elementDescriptionFontSize, "elementDescriptionFontWeight": tok.elementDescriptionFontWeight,
			"elementHeadlineColor": tok.elementHeadlineColor, "elementHeadlineFontSize": tok.elementHeadlineFontSize, "elementHeadlineFontWeight": tok.elementHeadlineFontWeight,
			"elementUpperLabelColor": tok.elementUpperLabelColor, "elementUpperLabelFontSize": tok.elementUpperLabelFontSize, "elementUpperLabelFontWeight": tok.elementUpperLabelFontWeight,
			"fontFamily": tok.fontFamily, "inputBackgroundColor": tok.inputBackgroundColor, "inputBorderRadius": tok.inputBorderRadius, "inputColor": tok.inputColor,
			"inputFontSize": tok.inputFontSize, "inputHeight": tok.inputHeight, "inputPaddingX": tok.inputPaddingX, "inputPaddingY": tok.inputPaddingY,
			"inputPlaceholderEmailColor": tok.inputPlaceholderEmailColor, "inputTextColor": tok.inputTextColor, "inputBorderColor": tok.inputBorderColor,
			"optionBackgroundColor": tok.optionBackgroundColor, "optionBorderColor": tok.optionBorderColor, "optionBorderRadius": tok.optionBorderRadius,
			"optionFontSize": tok.optionFontSize, "optionLabelColor": tok.optionLabelColor, "optionPaddingX": tok.optionPaddingX, "optionPaddingY": tok.optionPaddingY,
			"questionColor": tok.questionColor, "roundness": tok.roundness, "signatureColor": tok.signatureColor,
		}
		want := map[string]any{}
		for k, v := range c.Tokens {
			if k != "inputShadow" {
				want[k] = v
			}
		}
		if !reflect.DeepEqual(got, want) {
			for k := range want {
				if !reflect.DeepEqual(got[k], want[k]) {
					t.Errorf("%s: %s = %v, forma2 %v", c.Name, k, got[k], want[k])
				}
			}
		}
	}
}

// TestRendersAsForma2 renders every recorded survey and compares it with forma2's markup: the same elements in the same order, the same attributes, the same style declarations and the same text. What it does not compare is layout that carries no meaning: the whitespace React Email's pretty printer put between tags, the order of attributes, and the order of declarations within a style.
func TestRendersAsForma2(t *testing.T) {
	rec := loadRecording(t)
	g, err := NewGenerator(rec.WebAppURL, rec.PublicURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := g.HTML(c.Blocks, c.Styling, c.SurveyURL, rec.Locale)
			if err != nil {
				t.Fatal(err)
			}
			want, have := canonical(t, c.HTML), canonical(t, got)
			if want != have {
				wl, hl := strings.Split(want, "\n"), strings.Split(have, "\n")
				for i := range max(len(wl), len(hl)) {
					var w, h string
					if i < len(wl) {
						w = wl[i]
					}
					if i < len(hl) {
						h = hl[i]
					}
					if w != h {
						t.Fatalf("differs from forma2 at line %d:\nforma2: %s\ngot:    %s", i+1, w, h)
					}
				}
			}
		})
	}
}

// canonical is the markup as one line per node: elements with their attributes sorted and their style declarations sorted, text with its whitespace collapsed, whitespace-only text dropped.
func canonical(t *testing.T, markup string) string {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(markup), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	var walk func(n *html.Node, depth int)
	walk = func(n *html.Node, depth int) {
		indent := strings.Repeat("  ", depth)
		switch n.Type {
		case html.ElementNode:
			attrs := make([]string, 0, len(n.Attr))
			for _, a := range n.Attr {
				v := a.Val
				if a.Key == "style" {
					v = canonicalStyle(v)
				}
				attrs = append(attrs, a.Key+"="+v)
			}
			sort.Strings(attrs)
			b.WriteString(indent + "<" + n.Data + " " + strings.Join(attrs, " ") + ">\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, depth+1)
			}
		case html.TextNode:
			if s := collapse(n.Data); s != "" {
				b.WriteString(indent + "text " + s + "\n")
			}
		case html.CommentNode:
			b.WriteString(indent + "comment " + collapse(n.Data) + "\n")
		}
	}
	for _, n := range nodes {
		walk(n, 0)
	}
	return b.String()
}

func canonicalStyle(s string) string {
	var decls []string
	for _, d := range strings.Split(s, ";") {
		prop, value, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		decls = append(decls, strings.TrimSpace(prop)+":"+collapse(value))
	}
	sort.Strings(decls)
	return strings.Join(decls, ";")
}

// collapse trims HTML whitespace and folds runs of it into one space; a no-break space is content, not whitespace.
func collapse(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }), " ")
}
