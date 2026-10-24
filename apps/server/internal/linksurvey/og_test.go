package linksurvey

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"unicode/utf16"
)

// forma2's og-params.test.ts.
func TestParseOgParams(t *testing.T) {
	parse := func(query string) ogParams {
		q, err := url.ParseQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		return parseOgParams(q)
	}
	t.Run("caps the name the renderer has to shape", func(t *testing.T) {
		if got := parse("name=" + strings.Repeat("a", ogNameMaxLength+500)).Name; len(got) != ogNameMaxLength {
			t.Fatalf("name has %d characters", len(got))
		}
		// JavaScript counts UTF-16 code units: 60 emoji are 120 units.
		if got := parse("name=" + url.QueryEscape(strings.Repeat("😀", 100))).Name; len(utf16.Encode([]rune(got))) != ogNameMaxLength {
			t.Fatalf("name has %d code units", len(utf16.Encode([]rune(got))))
		}
	})
	t.Run("keeps a name that already fits", func(t *testing.T) {
		if got := parse("name=Customer%20satisfaction").Name; got != "Customer satisfaction" {
			t.Fatalf("name %q", got)
		}
	})
	t.Run("falls back to the default colour for anything that is not a hex literal", func(t *testing.T) {
		for _, brand := range []string{"red", "javascript:alert(1)", "#12345", "#zzzzzz", ""} {
			p := parse("brandColor=" + url.QueryEscape(brand))
			if p.BrandColor != "#0000BF" || cssRGBA(backgroundColor(hexColor(p.BrandColor))) != "rgba(0, 0, 191, 0.75)" {
				t.Fatalf("%q: brand %q", brand, p.BrandColor)
			}
		}
	})
	t.Run("renders the requested colour at 75% for every hex form ZColor accepts", func(t *testing.T) {
		for _, brand := range []string{"#ff0000", "#f00", "#f00f", "#ff0000ff"} {
			if got := cssRGBA(backgroundColor(hexColor(parse("brandColor=" + url.QueryEscape(brand)).BrandColor))); got != "rgba(255, 0, 0, 0.75)" {
				t.Fatalf("%s: background %s", brand, got)
			}
		}
	})
	t.Run("defaults both values when the query string is empty", func(t *testing.T) {
		if p := parse(""); p != (ogParams{Name: "", BrandColor: "#0000BF"}) {
			t.Fatalf("params %+v", p)
		}
	})
}

func decodeOg(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// The card's layout: an 800x400 PNG with rounded transparent corners, the brand colour at 75% behind white cards, the title in slate-900 and the button in the brand colour.
func TestRenderOgImage(t *testing.T) {
	b, err := renderOgImage(ogParams{Name: "Link Feedback Survey", BrandColor: "#1e40af"})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeOg(t, b)
	if img.Bounds() != image.Rect(0, 0, 800, 400) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	if c := nrgbaAt(img, 0, 0); c.A != 0 {
		t.Fatalf("corner %v is not transparent", c)
	}
	// Premultiplied storage rounds a channel by one.
	near := func(a, b uint8) bool { return a == b || a+1 == b || a == b+1 }
	if c := nrgbaAt(img, 400, 395); !near(c.R, 0x1e) || !near(c.G, 0x40) || !near(c.B, 0xaf) || c.A != 191 {
		t.Fatalf("background %v, want the brand colour at 75%%", c)
	}
	if c := nrgbaAt(img, 60, 330); c.A != 255 || c.R != 255 || c.G != 255 {
		t.Fatalf("main card %v is not white", c)
	}
	// The button: right-aligned 40px inside the main card, under the title.
	if c := nrgbaAt(img, 696-10, 128+38+10); c != (color.NRGBA{R: 0x1e, G: 0x40, B: 0xaf, A: 255}) {
		t.Fatalf("button %v", c)
	}
	dark := 0
	for x := 88; x < 400; x++ {
		for y := 129; y < 165; y++ {
			if c := nrgbaAt(img, x, y); c.R < 0x40 && c.G < 0x40 && c.B < 0x60 {
				dark++
			}
		}
	}
	if dark < 200 {
		t.Fatalf("the title left %d dark pixels", dark)
	}
}

// A long name wraps inside the card instead of running off it, and an empty one still renders.
func TestRenderOgImageWrapsAndHandlesEmptyNames(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("Customer satisfaction ", 6), strings.Repeat("x", 120)} {
		b, err := renderOgImage(ogParams{Name: name, BrandColor: "#0000BF"})
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		decodeOg(t, b)
	}
	title, _, err := ogFaces()
	if err != nil {
		t.Fatal(err)
	}
	lines := wrap(title, strings.Repeat("Customer satisfaction ", 6), 616*64, 0)
	if len(lines) < 2 {
		t.Fatalf("%d lines", len(lines))
	}
	for _, l := range wrap(title, strings.Repeat("x", 120), 616*64, 0) {
		if l == "" {
			t.Fatal("empty line")
		}
	}
}
