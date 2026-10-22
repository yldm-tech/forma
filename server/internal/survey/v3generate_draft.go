package survey

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/yldm-tech/forma/server/internal/platform/v3api"
)

// generateDraftSchema is forma2's ZGeneratedSurveyDraftForAI exported the way the AI SDK sends it (zod's toJSONSchema, draft 7, input, inlined, every object closed): the structured output every provider is asked for.
//
//go:embed v3generate_schema.json
var generateDraftSchema []byte

// forma2's GENERATED_SURVEY_* bounds and element types.
const (
	generatedMinBlocks            = 1
	generatedMaxBlocks            = 8
	generatedMinQuestionsPerBlock = 1
	generatedMaxQuestionsPerBlock = 4
)

var generatedElementTypes = []string{"openText", "multipleChoiceSingle", "multipleChoiceMulti", "nps", "rating", "csat", "ces", "ranking", "matrix", "date"}

// ratingLikeTypes are the element types that each get a block of their own.
var ratingLikeTypes = []string{"rating", "csat", "ces", "nps", "matrix", "ranking"}

// generatedDraft is forma2's TGeneratedSurveyDraft: what the model writes, before it becomes a create payload.
type generatedDraft struct {
	Language    string
	Name        string
	Description *string
	WelcomeCard *generatedWelcomeCard
	Ending      *generatedEnding
	Blocks      []generatedBlock
}

type generatedWelcomeCard struct {
	Enabled                          bool
	Headline, Subheader, ButtonLabel *string
}

type generatedEnding struct {
	Headline, Subheader *string
}

type generatedBlock struct {
	Name      string
	Questions []generatedElement
}

// generatedElement is forma2's TGeneratedSurveyElement. A nil list or pointer is the model's null (or an absent optional key); Range is 0 when null.
type generatedElement struct {
	Type                    string
	Headline                string
	Subheader               *string
	Required                bool
	Placeholder             *string
	LongAnswer              *bool
	Choices, Rows, Columns  []string
	LowerLabel, UpperLabel  *string
	Scale, Format           *string
	Range                   int
}

// draftParser reads a draft with zod's semantics: strings are trimmed before their bounds are checked, objects are strict, and nullable keys accept null. forAI is ZGeneratedSurveyDraftForAI, which also requires rows, columns and format and reads range only as a string; otherwise it is ZGeneratedSurveyDraft, which lets those three keys be absent and reads a numeric range too.
type draftParser struct {
	parser
	forAI bool
}

// parseGeneratedDraft is forma2's ZGeneratedSurveyDraft(ForAI).safeParse with its refinements; the issues are named under "generatedSurvey" when they have no path.
func parseGeneratedDraft(raw []byte, forAI bool) (*generatedDraft, []v3api.InvalidParam) {
	value, err := decodeValue(raw)
	if err != nil {
		return nil, []v3api.InvalidParam{{Name: "generatedSurvey", Reason: "Invalid JSON"}}
	}
	p := &draftParser{forAI: forAI}
	d := p.draft(value)
	if len(p.issues) > 0 {
		return nil, invalidParams(p.issues, "generatedSurvey")
	}
	return d, nil
}

func childPath(path []any, key any) []any {
	return append(slices.Clip(path), key)
}

func (p *draftParser) object(path []any, v any, present bool, allowed ...string) *object {
	o := asObject(v)
	if o == nil {
		p.expected(path, "object", v, present)
		return nil
	}
	var unknown []string
	for _, k := range o.keys {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, `"`+k+`"`)
		}
	}
	switch len(unknown) {
	case 0:
	case 1:
		p.add(path, "Unrecognized key: "+unknown[0], "")
	default:
		p.add(path, "Unrecognized keys: "+strings.Join(unknown, ", "), "")
	}
	return o
}

// text is a trimmed string of minLen to maxLen characters.
func (p *draftParser) text(path []any, v any, present bool, minLen, maxLen int) (string, bool) {
	s, ok := v.(string)
	if !ok {
		p.expected(path, "string", v, present)
		return "", false
	}
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n < minLen:
		p.add(path, fmt.Sprintf("Too small: expected string to have >=%d characters", minLen), "")
		return "", false
	case n > maxLen:
		p.add(path, fmt.Sprintf("Too big: expected string to have <=%d characters", maxLen), "")
		return "", false
	}
	return s, true
}

func (p *draftParser) nullableText(o *object, key string, path []any, minLen, maxLen int) *string {
	v, present := o.get(key)
	if present && v == nil {
		return nil
	}
	s, ok := p.text(childPath(path, key), v, present, minLen, maxLen)
	if !ok {
		return nil
	}
	return &s
}

func (p *draftParser) boolean(path []any, v any, present bool) bool {
	b, ok := v.(bool)
	if !ok {
		p.expected(path, "boolean", v, present)
	}
	return b
}

// list is forma2's ZGeneratedChoiceList: 2 to 8 trimmed strings of 1 to 80 characters.
func (p *draftParser) list(path []any, v any, present bool) []string {
	items, ok := v.([]any)
	if !ok {
		p.expected(path, "array", v, present)
		return nil
	}
	switch {
	case len(items) < 2:
		p.add(path, "Too small: expected array to have >=2 items", "")
	case len(items) > 8:
		p.add(path, "Too big: expected array to have <=8 items", "")
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, _ := p.text(childPath(path, i), item, true, 1, 80)
		out = append(out, s)
	}
	return out
}

func (p *draftParser) draft(v any) *generatedDraft {
	o := p.object(nil, v, true, "language", "name", "description", "welcomeCard", "ending", "blocks")
	if o == nil {
		return nil
	}
	d := &generatedDraft{}
	lang, present := o.get("language")
	d.Language = p.enum([]any{"language"}, lang, present, userLocales)
	name, present := o.get("name")
	d.Name, _ = p.text([]any{"name"}, name, present, 1, 220)
	d.Description = p.nullableText(o, "description", nil, 1, 320)

	if wc, present := o.get("welcomeCard"); !present || wc != nil {
		path := []any{"welcomeCard"}
		if w := p.object(path, wc, present, "enabled", "headline", "subheader", "buttonLabel"); w != nil {
			enabled, present := w.get("enabled")
			d.WelcomeCard = &generatedWelcomeCard{
				Enabled:     p.boolean(childPath(path, "enabled"), enabled, present),
				Headline:    p.nullableText(w, "headline", path, 1, 220),
				Subheader:   p.nullableText(w, "subheader", path, 1, 320),
				ButtonLabel: p.nullableText(w, "buttonLabel", path, 1, 220),
			}
		}
	}
	if en, present := o.get("ending"); !present || en != nil {
		path := []any{"ending"}
		if e := p.object(path, en, present, "headline", "subheader"); e != nil {
			d.Ending = &generatedEnding{Headline: p.nullableText(e, "headline", path, 1, 220), Subheader: p.nullableText(e, "subheader", path, 1, 320)}
		}
	}

	blocks, present := o.get("blocks")
	items, ok := blocks.([]any)
	if !ok {
		p.expected([]any{"blocks"}, "array", blocks, present)
		return d
	}
	switch {
	case len(items) < generatedMinBlocks:
		p.add([]any{"blocks"}, fmt.Sprintf("Too small: expected array to have >=%d items", generatedMinBlocks), "")
	case len(items) > generatedMaxBlocks:
		p.add([]any{"blocks"}, fmt.Sprintf("Too big: expected array to have <=%d items", generatedMaxBlocks), "")
	}
	for i, item := range items {
		d.Blocks = append(d.Blocks, p.block([]any{"blocks", i}, item))
	}
	return d
}

func (p *draftParser) block(path []any, v any) generatedBlock {
	var b generatedBlock
	o := p.object(path, v, true, "name", "questions")
	if o == nil {
		return b
	}
	name, present := o.get("name")
	b.Name, _ = p.text(childPath(path, "name"), name, present, 1, 220)
	questions, present := o.get("questions")
	qpath := childPath(path, "questions")
	items, ok := questions.([]any)
	if !ok {
		p.expected(qpath, "array", questions, present)
		return b
	}
	switch {
	case len(items) < generatedMinQuestionsPerBlock:
		p.add(qpath, fmt.Sprintf("Too small: expected array to have >=%d items", generatedMinQuestionsPerBlock), "")
	case len(items) > generatedMaxQuestionsPerBlock:
		p.add(qpath, fmt.Sprintf("Too big: expected array to have <=%d items", generatedMaxQuestionsPerBlock), "")
	}
	for i, item := range items {
		b.Questions = append(b.Questions, p.element(childPath(qpath, i), item))
	}
	return b
}

func (p *draftParser) element(path []any, v any) generatedElement {
	var e generatedElement
	o := p.object(path, v, true, "type", "headline", "subheader", "required", "placeholder", "longAnswer", "choices", "rows", "columns", "lowerLabel", "upperLabel", "scale", "format", "range")
	if o == nil {
		return e
	}
	t, present := o.get("type")
	e.Type = p.enum(childPath(path, "type"), t, present, generatedElementTypes)
	h, present := o.get("headline")
	e.Headline, _ = p.text(childPath(path, "headline"), h, present, 1, 220)
	e.Subheader = p.nullableText(o, "subheader", path, 1, 320)
	r, present := o.get("required")
	e.Required = p.boolean(childPath(path, "required"), r, present)
	e.Placeholder = p.nullableText(o, "placeholder", path, 1, 120)
	if la, present := o.get("longAnswer"); !present || la != nil {
		b := p.boolean(childPath(path, "longAnswer"), la, present)
		e.LongAnswer = &b
	}
	// rows, columns and format may be absent outside the ForAI schema; every other key must be sent, null where it does not apply.
	skip := func(key string, optional bool) bool {
		v, present := o.get(key)
		return v == nil && (present || optional)
	}
	nullableList := func(key string, optional bool) []string {
		if skip(key, optional) {
			return nil
		}
		v, present := o.get(key)
		return p.list(childPath(path, key), v, present)
	}
	e.Choices = nullableList("choices", false)
	e.Rows = nullableList("rows", !p.forAI)
	e.Columns = nullableList("columns", !p.forAI)
	e.LowerLabel = p.nullableText(o, "lowerLabel", path, 1, 60)
	e.UpperLabel = p.nullableText(o, "upperLabel", path, 1, 60)
	if !skip("scale", false) {
		sc, present := o.get("scale")
		s := p.enum(childPath(path, "scale"), sc, present, []string{"number", "smiley", "star"})
		e.Scale = &s
	}
	if !skip("format", !p.forAI) {
		f, present := o.get("format")
		s := p.enum(childPath(path, "format"), f, present, []string{"M-d-y", "d-M-y", "y-M-d"})
		e.Format = &s
	}
	e.Range = p.rangeValue(o, path)
	p.refine(path, &e, o)
	return e
}

// rangeValue is ZGeneratedRatingRange: "5", "7" or "10", and outside the ForAI schema the same numbers, as a number.
func (p *draftParser) rangeValue(o *object, path []any) int {
	v, present := o.get("range")
	rpath := childPath(path, "range")
	if present && v == nil {
		return 0
	}
	options := []string{"5", "7", "10"}
	if n, ok := number(v); ok && !p.forAI {
		v = strings.TrimSuffix(fmt.Sprintf("%g", n), ".0")
	}
	switch p.enum(rpath, v, present, options) {
	case "5":
		return 5
	case "7":
		return 7
	case "10":
		return 10
	}
	return 0
}

// refine is forma2's validateGeneratedSurveyElement.
func (p *draftParser) refine(path []any, e *generatedElement, o *object) {
	switch e.Type {
	case "multipleChoiceSingle", "multipleChoiceMulti", "ranking":
		if v, _ := o.get("choices"); v == nil {
			p.add(childPath(path, "choices"), "Choice questions must include choices", "")
		}
	case "matrix":
		if v, _ := o.get("rows"); v == nil {
			p.add(childPath(path, "rows"), "Matrix questions must include rows", "")
		}
		if v, _ := o.get("columns"); v == nil {
			p.add(childPath(path, "columns"), "Matrix questions must include columns", "")
		}
	case "csat":
		if e.Range != 5 {
			p.add(childPath(path, "range"), "CSAT questions must use a range of 5", "")
		}
	case "ces":
		if e.Range != 5 && e.Range != 7 {
			p.add(childPath(path, "range"), "CES questions must use a range of 5 or 7", "")
		}
	}
}
