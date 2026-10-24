package emailembed

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
)

// tokens is forma2's PreviewEmailStyleTokens: every color, size and radius the preview reads, resolved from the styling over forma2's STYLE_DEFAULTS.
type tokens struct {
	accentBackgroundColor        string
	brandColor                   string
	buttonBackgroundColor        string
	buttonBorderRadius           string
	buttonFontSize               string
	buttonFontWeight             string
	buttonPaddingX               string
	buttonPaddingY               string
	buttonTextColor              string
	cardBackgroundColor          string
	cardBorderColor              string
	elementDescriptionColor      string
	elementDescriptionFontSize   string
	elementDescriptionFontWeight string
	elementHeadlineColor         string
	elementHeadlineFontSize      string
	elementHeadlineFontWeight    string
	elementUpperLabelColor       string
	elementUpperLabelFontSize    string
	elementUpperLabelFontWeight  string
	fontFamily                   string
	inputBackgroundColor         string
	inputBorderRadius            string
	inputFontSize                string
	inputHeight                  string
	inputPaddingX                string
	inputPaddingY                string
	inputPlaceholderEmailColor   string
	inputTextColor               string
	inputColor                   string
	inputBorderColor             string
	optionBackgroundColor        string
	optionBorderColor            string
	optionBorderRadius           string
	optionFontSize               string
	optionLabelColor             string
	optionPaddingX               string
	optionPaddingY               string
	questionColor                string
	roundness                    float64
	signatureColor               string
}

// styleDefaults are the values of forma2's STYLE_DEFAULTS the preview falls back to (apps/web/lib/styling/constants.ts, the colors derived there from the default brand color #1e40af).
var styleDefaults = struct {
	brandColor, elementHeadlineColor, inputBgColor, inputBorderColor, cardBackgroundColor, cardBorderColor, optionBgColor, optionBorderColor string
	elementHeadlineFontSize, elementHeadlineFontWeight                                                                                       float64
	elementDescriptionFontSize, elementDescriptionFontWeight                                                                                 float64
	elementUpperLabelFontSize, elementUpperLabelFontWeight                                                                                   float64
	inputBorderRadius, inputHeight, inputFontSize, inputPaddingX, inputPaddingY, inputPlaceholderOpacity                                     float64
	buttonBorderRadius, buttonFontSize, buttonFontWeight, buttonPaddingX, buttonPaddingY                                                     float64
	optionBorderRadius, optionPaddingX, optionPaddingY, optionFontSize                                                                       float64
}{
	brandColor: "#1e40af", elementHeadlineColor: "#142a72", inputBgColor: "#edf0f9", inputBorderColor: "#a5b3df", cardBackgroundColor: "#ffffff", cardBorderColor: "#d2d9ef", optionBgColor: "#edf0f9", optionBorderColor: "#a5b3df",
	elementHeadlineFontSize: 16, elementHeadlineFontWeight: 500,
	elementDescriptionFontSize: 14, elementDescriptionFontWeight: 400,
	elementUpperLabelFontSize: 12, elementUpperLabelFontWeight: 400,
	inputBorderRadius: 8, inputHeight: 20, inputFontSize: 14, inputPaddingX: 8, inputPaddingY: 8, inputPlaceholderOpacity: 0.5,
	buttonBorderRadius: 8, buttonFontSize: 16, buttonFontWeight: 500, buttonPaddingX: 12, buttonPaddingY: 12,
	optionBorderRadius: 8, optionPaddingX: 16, optionPaddingY: 16, optionFontSize: 14,
}

// surveyEmailFontFamily is forma2's SURVEY_EMAIL_FONT_FAMILY.
const surveyEmailFontFamily = "Inter, Helvetica, Arial, sans-serif"

// styling is a TSurveyStyling document by key.
type styling map[string]json.RawMessage

// value is the key's value; nil when it is absent or null, as `??` sees it.
func (s styling) value(key string) any {
	raw, ok := s[key]
	if !ok {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// light is `styling[key]?.light`: the color's light value, ok false when it is absent or null.
func (s styling) light(key string) (string, bool) {
	c, ok := s.value(key).(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := c["light"].(string)
	return v, ok
}

// colorOr is the first of the keys' light colors that is set, or fallback.
func (s styling) colorOr(fallback string, keys ...string) string {
	for _, k := range keys {
		if v, ok := s.light(k); ok {
			return v
		}
	}
	return fallback
}

// dimension is forma2's getPreviewDimension: a number, or a string that reads as one, in px; any other string as it is; "" for nothing.
func dimension(value any, fallback float64) string {
	if value == nil {
		value = fallback
	}
	switch v := value.(type) {
	case float64:
		return jsvalue.FormatNumber(v) + "px"
	case string:
		if v == "" {
			return ""
		}
		if !math.IsNaN(jsvalue.ParseNumber(v)) {
			return v + "px"
		}
		return v
	}
	return ""
}

// fontWeight is forma2's getPreviewFontWeight.
func fontWeight(value any, fallback float64) string {
	if value == nil {
		value = fallback
	}
	switch v := value.(type) {
	case float64:
		return jsvalue.FormatNumber(v)
	case string:
		return v
	}
	return ""
}

// opacity is forma2's getPreviewOpacity: Number(value), clamped to [0, 1]; 1 when it is not a finite number.
func opacity(value any, fallback float64) float64 {
	if value == nil {
		value = fallback
	}
	n := jsvalue.Number(value)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 1
	}
	return math.Min(math.Max(n, 0), 1)
}

var leadingFloat = regexp.MustCompile(`^[+-]?(?:Infinity|[0-9]+\.?[0-9]*(?:[eE][+-]?[0-9]+)?|\.[0-9]+(?:[eE][+-]?[0-9]+)?)`)

// roundness is forma2's getPreviewRoundness: a finite number, or the number a string starts with (parseFloat), else 8.
func roundness(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case string:
		m := leadingFloat.FindString(jsvalue.Trim(v))
		if m == "" || strings.Contains(m, "Infinity") {
			return 8
		}
		f, err := strconv.ParseFloat(m, 64)
		if err != nil || math.IsInf(f, 0) {
			return 8
		}
		return f
	}
	return 8
}

// newTokens is forma2's getPreviewEmailStyleTokens.
func newTokens(s styling) *tokens {
	questionColor := s.colorOr(styleDefaults.elementHeadlineColor, "elementHeadlineColor")
	brandColor := s.colorOr(styleDefaults.brandColor, "brandColor")
	inputTextColor := s.colorOr(questionColor, "inputTextColor")
	inputBackgroundColor := s.colorOr(styleDefaults.inputBgColor, "inputBgColor")
	inputPlaceholderColor := mixColor(inputTextColor, "#ffffff", 0.3)
	placeholderOpacity := opacity(s.value("inputPlaceholderOpacity"), styleDefaults.inputPlaceholderOpacity)
	optionBackgroundColor := s.colorOr(styleDefaults.optionBgColor, "optionBgColor", "inputBgColor")
	buttonBackgroundColor := s.colorOr(brandColor, "buttonBgColor")
	previewRoundness := roundness(s.value("roundness"))
	signatureColor := mixColor(questionColor, "#ffffff", 0.2)
	if isLight(questionColor) {
		signatureColor = mixColor(questionColor, "#000000", 0.2)
	}
	buttonTextColor, ok := s.light("buttonTextColor")
	if !ok {
		buttonTextColor = "#ffffff"
		if isLight(buttonBackgroundColor) {
			buttonTextColor = "#0f172a"
		}
	}
	fontFamily, ok := s.value("fontFamily").(string)
	if !ok {
		fontFamily = surveyEmailFontFamily
	}
	return &tokens{
		accentBackgroundColor:        s.colorOr(mixColor(brandColor, "#ffffff", 0.8), "accentBgColor"),
		brandColor:                   brandColor,
		buttonBackgroundColor:        buttonBackgroundColor,
		buttonBorderRadius:           dimension(s.value("buttonBorderRadius"), styleDefaults.buttonBorderRadius),
		buttonFontSize:               dimension(s.value("buttonFontSize"), styleDefaults.buttonFontSize),
		buttonFontWeight:             fontWeight(s.value("buttonFontWeight"), styleDefaults.buttonFontWeight),
		buttonPaddingX:               dimension(s.value("buttonPaddingX"), styleDefaults.buttonPaddingX),
		buttonPaddingY:               dimension(s.value("buttonPaddingY"), styleDefaults.buttonPaddingY),
		buttonTextColor:              buttonTextColor,
		cardBackgroundColor:          s.colorOr(styleDefaults.cardBackgroundColor, "cardBackgroundColor"),
		cardBorderColor:              s.colorOr(styleDefaults.cardBorderColor, "cardBorderColor"),
		elementDescriptionColor:      s.colorOr(questionColor, "elementDescriptionColor"),
		elementDescriptionFontSize:   dimension(s.value("elementDescriptionFontSize"), styleDefaults.elementDescriptionFontSize),
		elementDescriptionFontWeight: fontWeight(s.value("elementDescriptionFontWeight"), styleDefaults.elementDescriptionFontWeight),
		elementHeadlineColor:         s.colorOr(questionColor, "elementHeadlineColor"),
		elementHeadlineFontSize:      dimension(s.value("elementHeadlineFontSize"), styleDefaults.elementHeadlineFontSize),
		elementHeadlineFontWeight:    fontWeight(s.value("elementHeadlineFontWeight"), styleDefaults.elementHeadlineFontWeight),
		elementUpperLabelColor:       s.colorOr(questionColor, "elementUpperLabelColor"),
		elementUpperLabelFontSize:    dimension(s.value("elementUpperLabelFontSize"), styleDefaults.elementUpperLabelFontSize),
		elementUpperLabelFontWeight:  fontWeight(s.value("elementUpperLabelFontWeight"), styleDefaults.elementUpperLabelFontWeight),
		fontFamily:                   fontFamily,
		inputBackgroundColor:         inputBackgroundColor,
		inputBorderColor:             s.colorOr(styleDefaults.inputBorderColor, "inputBorderColor"),
		inputBorderRadius:            dimension(s.value("inputBorderRadius"), styleDefaults.inputBorderRadius),
		inputColor:                   inputBackgroundColor,
		inputFontSize:                dimension(s.value("inputFontSize"), styleDefaults.inputFontSize),
		inputHeight:                  dimension(s.value("inputHeight"), styleDefaults.inputHeight),
		inputPaddingX:                dimension(s.value("inputPaddingX"), styleDefaults.inputPaddingX),
		inputPaddingY:                dimension(s.value("inputPaddingY"), styleDefaults.inputPaddingY),
		inputPlaceholderEmailColor:   mixColor(inputPlaceholderColor, inputBackgroundColor, 1-placeholderOpacity),
		inputTextColor:               inputTextColor,
		optionBackgroundColor:        optionBackgroundColor,
		optionBorderColor:            s.colorOr(styleDefaults.optionBorderColor, "optionBorderColor", "inputBorderColor"),
		optionBorderRadius:           dimension(s.value("optionBorderRadius"), styleDefaults.optionBorderRadius),
		optionFontSize:               dimension(s.value("optionFontSize"), styleDefaults.optionFontSize),
		optionLabelColor:             s.colorOr(questionColor, "optionLabelColor"),
		optionPaddingX:               dimension(s.value("optionPaddingX"), styleDefaults.optionPaddingX),
		optionPaddingY:               dimension(s.value("optionPaddingY"), styleDefaults.optionPaddingY),
		questionColor:                questionColor,
		roundness:                    previewRoundness,
		signatureColor:               signatureColor,
	}
}

var hexDigits = regexp.MustCompile(`(?i)^[a-f\d]+$`)

// normalizeHex is forma2's normalizeHex: every hex form a color may take (#RGB, #RGBA, #RRGGBB, #RRGGBBAA, the "#" optional) as lowercase #rrggbb without alpha; ok false for anything else.
func normalizeHex(color string) (string, bool) {
	hex := strings.TrimPrefix(color, "#")
	if !hexDigits.MatchString(hex) {
		return "", false
	}
	switch len(hex) {
	case 3, 4:
		return strings.ToLower("#" + strings.Repeat(hex[0:1], 2) + strings.Repeat(hex[1:2], 2) + strings.Repeat(hex[2:3], 2)), true
	case 6, 8:
		return strings.ToLower("#" + hex[:6]), true
	}
	return "", false
}

// channels are a color's red, green and blue; zero for a color that is not hex, as forma2's mixColor read it.
func channels(color string) [3]float64 {
	hex, ok := normalizeHex(color)
	if !ok {
		return [3]float64{}
	}
	var out [3]float64
	for i := range out {
		n, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		out[i] = float64(n)
	}
	return out
}

// mixColor is forma2's mixColor: color mixed with mixWith by weight, as #rrggbb.
func mixColor(color, mixWith string, weight float64) string {
	a, b := channels(color), channels(mixWith)
	var sb strings.Builder
	sb.WriteByte('#')
	for i := range a {
		// Math.round: halves round up.
		v := int64(math.Floor(a[i]*(1-weight) + b[i]*weight + 0.5))
		sb.WriteString(leftPad(strconv.FormatInt(v, 16)))
	}
	return sb.String()
}

func leftPad(s string) string {
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

// isLight is forma2's isLight. forma2 threw on a color that is not hex, failing the whole preview; such a color counts as dark here.
func isLight(color string) bool {
	if _, ok := normalizeHex(color); !ok {
		return false
	}
	c := channels(color)
	return c[0]*0.299+c[1]*0.587+c[2]*0.114 > 128
}
