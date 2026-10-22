package linksurvey

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// The OG image of a link survey, forma2's /api/v1/client/og (app/api/v1/client/og): an 800x400 card in the survey's brand colour with its title and a "Begin!" button. forma2 laid it out with Satori; the boxes below are that layout resolved to pixels (1rem = 16px).
const (
	ogWidth  = 800
	ogHeight = 400
	// ogNameMaxLength is forma2's OG_NAME_MAX_LENGTH, in UTF-16 code units as JavaScript's slice counts them.
	ogNameMaxLength = 120
	// ogBackgroundOpacity is forma2's OG_BACKGROUND_OPACITY.
	ogBackgroundOpacity = 0.75
	// ogDefaultBrandColor is forma2's OG_DEFAULT_BRAND_COLOR.
	ogDefaultBrandColor = "#0000BF"
)

// zColor is forma2's ZColor: a hex colour of 3, 4, 6 or 8 digits.
var zColor = regexp.MustCompile(`^#(?:[A-Fa-f0-9]{3}|[A-Fa-f0-9]{4}|[A-Fa-f0-9]{6}|[A-Fa-f0-9]{8})$`)

// ogParams is forma2's parseOgParams: a length-capped name and a brand colour that is a hex literal, never arbitrary caller text.
type ogParams struct {
	Name       string
	BrandColor string
}

func parseOgParams(q url.Values) ogParams {
	name := q.Get("name")
	if units := utf16.Encode([]rune(name)); len(units) > ogNameMaxLength {
		name = string(utf16.Decode(units[:ogNameMaxLength]))
	}
	brand := q.Get("brandColor")
	if !zColor.MatchString(brand) {
		brand = ogDefaultBrandColor
	}
	return ogParams{Name: name, BrandColor: brand}
}

// hexColor reads a colour zColor accepts; the 4- and 8-digit forms carry alpha.
func hexColor(hex string) color.NRGBA {
	h := strings.TrimPrefix(hex, "#")
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, c := range h {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		h = b.String()
	}
	if len(h) == 6 {
		h += "ff"
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)} //nolint:gosec // each shift keeps the byte it names
}

// backgroundColor is forma2's hexToRGBA(brandColor, 0.75): the brand colour's RGB at 75% opacity, its own alpha ignored.
func backgroundColor(brand color.NRGBA) color.NRGBA {
	return color.NRGBA{R: brand.R, G: brand.G, B: brand.B, A: uint8(math.Round(255 * ogBackgroundOpacity))}
}

// cssRGBA prints a colour as forma2's hexToRGBA did.
func cssRGBA(c color.NRGBA) string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", c.R, c.G, c.B, strconv.FormatFloat(ogBackgroundOpacity, 'f', -1, 64))
}

// ogFonts are parsed once; a Face is not safe for concurrent use, so each render makes its own.
var ogFonts = sync.OnceValues(func() ([2]*opentype.Font, error) {
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return [2]*opentype.Font{}, err
	}
	regular, err := opentype.Parse(goregular.TTF)
	return [2]*opentype.Font{bold, regular}, err
})

// ogFaces are the h2 (2rem, weight 700) and the button label (1.5rem).
func ogFaces() (title, label font.Face, err error) {
	fonts, err := ogFonts()
	if err != nil {
		return nil, nil, err
	}
	if title, err = opentype.NewFace(fonts[0], &opentype.FaceOptions{Size: 32, DPI: 72, Hinting: font.HintingNone}); err != nil {
		return nil, nil, err
	}
	label, err = opentype.NewFace(fonts[1], &opentype.FaceOptions{Size: 24, DPI: 72, Hinting: font.HintingNone})
	return title, label, err
}

type box struct {
	x, y, w, h, radius float64
	// rotate is the CSS rotation in degrees, about the box's centre.
	rotate float64
}

// fill paints b in c with source-over compositing, the way the browser paints a box over its parent.
func fill(dst *image.RGBA, b box, c color.NRGBA) {
	z := vector.NewRasterizer(ogWidth, ogHeight)
	cx, cy := b.x+b.w/2, b.y+b.h/2
	sin, cos := math.Sincos(b.rotate * math.Pi / 180)
	pt := func(x, y float64) (float32, float32) {
		dx, dy := x-cx, y-cy
		return float32(cx + dx*cos - dy*sin), float32(cy + dx*sin + dy*cos)
	}
	r := math.Min(b.radius, math.Min(b.w, b.h)/2)
	// k places the cubic control points of a quarter circle.
	k := r * 0.5522847498
	x0, y0, x1, y1 := b.x, b.y, b.x+b.w, b.y+b.h
	z.MoveTo(pt(x0+r, y0))
	z.LineTo(pt(x1-r, y0))
	cube(z, pt, x1-r+k, y0, x1, y0+r-k, x1, y0+r)
	z.LineTo(pt(x1, y1-r))
	cube(z, pt, x1, y1-r+k, x1-r+k, y1, x1-r, y1)
	z.LineTo(pt(x0+r, y1))
	cube(z, pt, x0+r-k, y1, x0, y1-r+k, x0, y1-r)
	z.LineTo(pt(x0, y0+r))
	cube(z, pt, x0, y0+r-k, x0+r-k, y0, x0+r, y0)
	z.ClosePath()
	z.Draw(dst, dst.Bounds(), image.NewUniform(c), image.Point{})
}

func cube(z *vector.Rasterizer, pt func(x, y float64) (float32, float32), bx, by, cx, cy, dx, dy float64) {
	bx2, by2 := pt(bx, by)
	cx2, cy2 := pt(cx, cy)
	dx2, dy2 := pt(dx, dy)
	z.CubeTo(bx2, by2, cx2, cy2, dx2, dy2)
}

func withOpacity(c color.NRGBA, opacity float64) color.NRGBA {
	c.A = uint8(math.Round(float64(c.A) * opacity))
	return c
}

// wrap breaks text into lines no wider than width, at spaces where it can and inside a word that alone is too wide.
func wrap(face font.Face, text string, width fixed.Int26_6, spacing fixed.Int26_6) []string {
	measure := func(s string) fixed.Int26_6 {
		w := font.MeasureString(face, s)
		if n := len([]rune(s)); n > 1 {
			w += spacing * fixed.Int26_6(n-1)
		}
		return w
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if measure(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for measure(word) > width {
			runes := []rune(word)
			cut := len(runes) - 1
			for cut > 1 && measure(string(runes[:cut])) > width {
				cut--
			}
			lines = append(lines, string(runes[:cut]))
			word = string(runes[cut:])
		}
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// drawText draws one line with its top at y, glyph by glyph so the letter spacing applies.
func drawText(dst *image.RGBA, face font.Face, text string, x, y float64, spacing fixed.Int26_6, c color.Color) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: face}
	d.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y*64) + face.Metrics().Ascent}
	prev := rune(-1)
	for _, r := range text {
		if prev >= 0 {
			d.Dot.X += face.Kern(prev, r) + spacing
		}
		d.DrawString(string(r))
		prev = r
	}
}

// renderOgImage draws forma2's card. The boxes are Satori's layout of the forma2 markup: the root fills the image, the two tilted cards and the main card are absolutely placed (left, top + margin-top), the title starts 3.75rem into the main card behind 2rem of padding, and the button row sits right under the title, 2.5rem from the card's right edge, its translucent shadow block 0.5rem further right and down.
func renderOgImage(p ogParams) ([]byte, error) {
	title, label, err := ogFaces()
	if err != nil {
		return nil, fmt.Errorf("load og fonts: %w", err)
	}
	brand := hexColor(p.BrandColor)
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	img := image.NewRGBA(image.Rect(0, 0, ogWidth, ogHeight))
	fill(img, box{x: 0, y: 0, w: ogWidth, h: ogHeight, radius: 12}, backgroundColor(brand))
	fill(img, box{x: 48, y: 12 + 52, w: 0.80 * ogWidth, h: 0.60 * ogHeight, radius: 12, rotate: -4}, withOpacity(white, 0.2))
	fill(img, box{x: 52, y: 20 + 48, w: 0.84 * ogWidth, h: 0.60 * ogHeight, radius: 12, rotate: -3}, withOpacity(white, 0.6))
	card := box{x: 56, y: 36.8 + 32, w: 0.85 * ogWidth, h: 0.67 * ogHeight, radius: 12}
	fill(img, card, white)

	// letter-spacing: -0.025em of a 32px font.
	spacing := fixed.Int26_6(math.Round(-0.025 * 32 * 64))
	lineHeight := float64(title.Metrics().Height) / 64
	textTop := card.y + 60
	lines := wrap(title, p.Name, fixed.Int26_6((card.w-64)*64), spacing)
	for i, line := range lines {
		drawText(img, title, line, card.x+32, textTop+float64(i)*lineHeight, spacing, color.NRGBA{R: 0x0f, G: 0x17, B: 0x2a, A: 255})
	}

	rowTop := textTop + float64(len(lines))*lineHeight
	rowRight := card.x + card.w - 40
	button := box{x: rowRight - 152, y: rowTop, w: 152, h: 72, radius: 12}
	fill(img, box{x: button.x + 8, y: button.y + 8, w: 152, h: 72, radius: 12}, withOpacity(brand, 0.5))
	// box-shadow: 0 1px 3px rgb(0 0 0 / 0.1), approximated by the offset outline it leaves under the button.
	fill(img, box{x: button.x - 1, y: button.y, w: button.w + 2, h: button.h + 2, radius: 13}, color.NRGBA{A: 26})
	fill(img, button, brand)
	labelWidth := float64(font.MeasureString(label, "Begin!")) / 64
	labelHeight := float64(label.Metrics().Height) / 64
	drawText(img, label, "Begin!", button.x+(button.w-labelWidth)/2, button.y+(button.h-labelHeight)/2, 0, white)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode og image: %w", err)
	}
	return buf.Bytes(), nil
}
