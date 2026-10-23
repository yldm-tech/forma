package surveydoc

import (
	"fmt"
	"regexp"
	"strings"
)

// elementFieldLabels is forma2's ELEMENT_FIELD_TO_LABEL_MAP.
var elementFieldLabels = map[string]string{
	"headline": "question", "subheader": "description", "placeholder": "placeholder", "upperLabel": "upper label",
	"lowerLabel": "lower label", "consent.label": "checkbox label", "html": "description",
}

// cardFieldLabels is forma2's FIELD_TO_LABEL_MAP, used for the welcome card and endings.
var cardFieldLabels = map[string]string{
	"headline": "question", "subheader": "description", "buttonLabel": "next button label", "backButtonLabel": "back button label",
	"placeholder": "placeholder", "upperLabel": "upper label", "lowerLabel": "lower label", "consent.label": "checkbox label",
	"dismissButtonLabel": "dismiss button label", "html": "description", "cardHeadline": "note", "welcomeCardHtml": "welcome message",
	"endingCardButtonLabel": "button label",
}

// LanguageKey is the key a language's text is stored under: "default" for the default language, its code otherwise.
func LanguageKey(l Language) string {
	if l.Default {
		return "default"
	}
	return l.Code
}

// checkedLanguageKeys are the keys a translatable field must fill: the enabled languages', or only "default" when none is enabled.
func checkedLanguageKeys(languages []Language) []string {
	var keys []string
	for _, l := range languages {
		if l.Enabled {
			keys = append(keys, LanguageKey(l))
		}
	}
	if len(keys) == 0 {
		return []string{"default"}
	}
	return keys
}

// invalidLanguageKeys is forma2's validateLabelForAllLanguages: the checked languages whose text is missing or blank once markup is removed.
func invalidLanguageKeys(label I18nString, languages []Language) []string {
	var out []string
	for _, key := range checkedLanguageKeys(languages) {
		if label[key] == "" || TextContent(label[key]) == "" {
			out = append(out, key)
		}
	}
	return out
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

// elementLabelIssue is forma2's validateElementLabels.
func elementLabelIssue(field string, label I18nString, languages []Language, b, e int, skipArticle bool) *Issue {
	invalid := invalidLanguageKeys(label, languages)
	p := path("blocks", b, "elements", e, field)
	prefix := "The "
	if skipArticle {
		prefix = ""
	}
	name := field
	if l, ok := elementFieldLabels[field]; ok {
		name = l
	}
	if contains(invalid, "default") {
		return &Issue{Path: p, Message: fmt.Sprintf("%s%s in question %d of block %d is missing", prefix, name, e+1, b+1)}
	}
	for _, l := range languages {
		if _, present := label[l.Code]; !l.Default && !present {
			return &Issue{Path: p, Message: fmt.Sprintf("The %s in question %d of block %d is not present for the following languages: %s", field, e+1, b+1, l.Code)}
		}
	}
	if len(invalid) > 0 {
		return &Issue{
			Path:    p,
			Message: fmt.Sprintf("%s%s in question %d of block %d is missing for the following languages:  -fLang- %s", prefix, name, e+1, b+1, strings.Join(invalid, ",")),
			Params:  map[string]any{"invalidLanguageCodes": invalid},
		}
	}
	return nil
}

// cardLabelIssue is forma2's validateCardFieldsForAllLanguages; ending is -1 for the welcome card.
func cardLabelIssue(field string, label I18nString, languages []Language, ending int) *Issue {
	cardLabel, p := "Welcome card", path("welcomeCard", field)
	if ending >= 0 {
		cardLabel, p = fmt.Sprintf("Ending card %d", ending+1), path("endings", ending, field)
	}
	invalid := invalidLanguageKeys(label, languages)
	name := field
	if l, ok := cardFieldLabels[field]; ok {
		name = l
	}
	if contains(invalid, "default") {
		return &Issue{Path: p, Message: fmt.Sprintf("The %s on the %s is missing", name, cardLabel)}
	}
	for _, l := range languages {
		if _, present := label[l.Code]; !l.Default && !present {
			return &Issue{Path: p, Message: fmt.Sprintf("The %s in %s is not present for the following languages: %s", field, cardLabel, l.Code)}
		}
	}
	if len(invalid) > 0 {
		return &Issue{
			Path:    p,
			Message: fmt.Sprintf("The %s on the %s is missing for the following languages:  -fLang- %s", name, cardLabel, strings.Join(invalid, ", ")),
			Params:  map[string]any{"invalidLanguageCodes": invalid},
		}
	}
	return nil
}

// duplicateLabelLanguages is forma2's findLanguageCodesForDuplicateLabels, with "default" named by the default language's code as the refinement reports it. defaultOnly is true when the only affected key is the unnamed "default".
func duplicateLabelLanguages(labels []I18nString, languages []Language) (codes []string, defaultOnly bool) {
	for _, key := range checkedLanguageKeys(languages) {
		seen := map[string]bool{}
		for _, label := range labels {
			text, ok := label[key]
			if !ok || trimJS(text) == "" {
				continue
			}
			text = trimJS(text)
			if seen[text] {
				codes = append(codes, key)
				break
			}
			seen[text] = true
		}
	}
	if len(codes) == 0 {
		return nil, false
	}
	defaultCode := "default"
	for _, l := range languages {
		if l.Default {
			defaultCode = l.Code
			break
		}
	}
	for i, c := range codes {
		if c == "default" {
			codes[i] = defaultCode
		}
	}
	return codes, len(codes) == 1 && codes[0] == "default"
}

func (is *issues) addIssue(i *Issue) {
	if i != nil {
		*is = append(*is, *i)
	}
}

func (is *issues) duplicateLabels(p []any, what string, labels []I18nString, languages []Language) {
	codes, defaultOnly := duplicateLabelLanguages(labels, languages)
	if len(codes) == 0 {
		return
	}
	suffix := "for the following languages:"
	var params map[string]any
	if defaultOnly {
		suffix = ""
	} else {
		params = map[string]any{"invalidLanguageCodes": codes}
	}
	*is = append(*is, Issue{Path: p, Message: fmt.Sprintf("%s %s", what, suffix), Params: params})
}

var calHostPattern = regexp.MustCompile(`^[a-zA-Z0-9-]{1,63}(?:\.[a-zA-Z0-9-]{1,63})+$`)

// isCalHost is the hostname rule of forma2's Cal element: labels of 1 to 63 letters, digits and hyphens, at least two of them, none starting or ending with a hyphen.
func isCalHost(host string) bool {
	if !calHostPattern.MatchString(host) {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

// HasBlocksWithElements is forma2's hasBlocks: at least one block holds an element.
func (s *Survey) HasBlocksWithElements() bool {
	for _, b := range s.Blocks {
		if len(b.Elements) > 0 {
			return true
		}
	}
	return false
}

// Refine is forma2's surveyRefinement for a block-based survey: the cross-field rules over the whole document (translations complete for every enabled language, element ids unique across blocks, logic that references what exists and does not loop, endings and follow-ups that are complete).
func Refine(s *Survey) []Issue {
	var is issues
	hasBlocks := s.HasBlocksWithElements()
	if !hasBlocks {
		is.add(path("questions"), "Survey must have either questions or blocks with elements")
	}
	langs := s.Languages

	if wc := s.WelcomeCard; wc.Enabled {
		if wc.Headline != nil {
			is.addIssue(cardLabelIssue("cardHeadline", wc.Headline, langs, -1))
		}
		if wc.Subheader != nil && trimJS(wc.Subheader.Default()) != "" {
			is.addIssue(cardLabelIssue("welcomeCardSubheader", wc.Subheader, langs, -1))
		}
		if wc.ButtonLabel != nil && trimJS(wc.ButtonLabel.Default()) != "" {
			is.addIssue(cardLabelIssue("buttonLabel", wc.ButtonLabel, langs, -1))
		}
	}

	if hasBlocks {
		blockIDs := make([]string, len(s.Blocks))
		for i, b := range s.Blocks {
			blockIDs[i] = b.ID
		}
		if d := firstDuplicate(blockIDs); d >= 0 {
			is.add(path("blocks", d, "id"), "Block IDs must be unique")
		}
		seen := map[string]bool{}
		for b := range s.Blocks {
			for e, el := range s.Blocks[b].Elements {
				if seen[el.ID] {
					is.add(path("blocks", b, "elements", e, "id"), fmt.Sprintf(`Element ID "%s" is used in multiple blocks. Element IDs must be unique across all blocks.`, el.ID))
				}
				seen[el.ID] = true
			}
		}
		elements := s.elementIndex()
		for b := range s.Blocks {
			for e := range s.Blocks[b].Elements {
				is.elementLabels(b, e, &s.Blocks[b].Elements[e], langs)
			}
			is = append(is, blockLogicIssues(s, b, elements)...)
		}
		for _, id := range cyclicBlocks(s.Blocks) {
			for b := range s.Blocks {
				if s.Blocks[b].ID == id {
					is.add(path("blocks", b, "logic"), fmt.Sprintf("Conditional Logic: Cyclic logic detected in block %d (%s).", b+1, s.Blocks[b].Name))
					break
				}
			}
		}
	}

	for i := range s.Endings {
		is.endingRules(i, &s.Endings[i], langs)
	}
	is.followUpRules(s)
	return is
}

func (is *issues) elementLabels(b, e int, el *Element, langs []Language) {
	is.addIssue(elementLabelIssue("headline", el.Headline, langs, b, e, false))
	if el.Subheader != nil && trimJS(el.Subheader.Default()) != "" {
		is.addIssue(elementLabelIssue("subheader", el.Subheader, langs, b, e, false))
	}
	base := path("blocks", b, "elements", e)
	withPath := func(i *Issue, p []any) {
		if i != nil {
			i.Path = p
			*is = append(*is, *i)
		}
	}
	switch el.Type {
	case ElementOpenText:
		if el.Placeholder != nil && trimJS(el.Placeholder.Default()) != "" && len(langs) > 1 {
			is.addIssue(elementLabelIssue("placeholder", el.Placeholder, langs, b, e, false))
		}
	case ElementMultipleChoiceSingle, ElementMultipleChoiceMulti, ElementRanking:
		labels := make([]I18nString, len(el.Choices))
		for c, choice := range el.Choices {
			labels[c] = choice.Label
			withPath(elementLabelIssue(fmt.Sprintf("Choice %d", c+1), choice.Label, langs, b, e, true), extend(base, "choices", c))
		}
		is.duplicateLabels(extend(base, "choices"), fmt.Sprintf("Element %d in block %d has duplicate choice labels", e+1, b+1), labels, langs)
	case ElementConsent:
		withPath(elementLabelIssue("consent.label", el.Label, langs, b, e, false), extend(base, "label"))
	case ElementCTA:
		if el.IsExternalButton() {
			label := el.CTAButtonLabel
			if label == nil {
				label = I18nString{}
			}
			withPath(elementLabelIssue("ctaButtonLabel", label, langs, b, e, false), extend(base, "ctaButtonLabel"))
			if el.ButtonURL == nil || trimJS(*el.ButtonURL) == "" {
				is.add(extend(base, "buttonUrl"), fmt.Sprintf("Element %d in block %d: Button URL is required when external button is enabled", e+1, b+1))
			} else if m := SafeURLIssue(*el.ButtonURL); m != "" {
				is.add(extend(base, "buttonUrl"), fmt.Sprintf("Element %d in block %d: %s", e+1, b+1, m))
			}
		}
	case ElementMatrix:
		rows := make([]I18nString, len(el.Rows))
		for r, row := range el.Rows {
			rows[r] = row.Label
			withPath(elementLabelIssue(fmt.Sprintf("Row %d", r+1), row.Label, langs, b, e, true), extend(base, "rows", r))
		}
		cols := make([]I18nString, len(el.Columns))
		for c, col := range el.Columns {
			cols[c] = col.Label
			withPath(elementLabelIssue(fmt.Sprintf("Column %d", c+1), col.Label, langs, b, e, true), extend(base, "columns", c))
		}
		is.duplicateLabels(extend(base, "rows"), fmt.Sprintf("Question %d in block %d has duplicate row labels", e+1, b+1), rows, langs)
		is.duplicateLabels(extend(base, "columns"), fmt.Sprintf("Question %d in block %d has duplicate column labels", e+1, b+1), cols, langs)
	case ElementFileUpload:
		if el.AllowedFileExtensions != nil && len(*el.AllowedFileExtensions) == 0 {
			is.add(extend(base, "allowedFileExtensions"), fmt.Sprintf("Question %d in block %d must have atleast one allowed file extension", e+1, b+1))
		}
	case ElementCal:
		if el.CalHost != nil && !isCalHost(*el.CalHost) {
			is.add(extend(base, "calHost"), fmt.Sprintf("Question %d in block %d must have a valid host name", e+1, b+1))
		}
	case ElementContactInfo:
		is.subFields(b, e, "Contact Info", []subField{
			{"Company", "company", el.Company}, {"Email", "email", el.Email}, {"First Name", "firstname", el.FirstName},
			{"Last Name", "lastname", el.LastName}, {"Phone", "phone", el.Phone},
		}, langs)
	case ElementAddress:
		is.subFields(b, e, "Address", []subField{
			{"Address Line 1", "addressline1", el.AddressLine1}, {"Address Line 2", "addressline2", el.AddressLine2}, {"City", "city", el.City},
			{"State", "state", el.State}, {"Zip", "zip", el.Zip}, {"Country", "country", el.Country},
		}, langs)
	}
}

type subField struct {
	label string
	// key is forma2's path key: the label lowercased without spaces.
	key   string
	input *ToggleInput
}

func (is *issues) subFields(b, e int, kind string, fields []subField, langs []Language) {
	base := path("blocks", b, "elements", e)
	shown := false
	for _, f := range fields {
		if f.input != nil && f.input.Show {
			shown = true
		}
	}
	if !shown {
		is.add(base, fmt.Sprintf("At least one field must be shown in the %s question %d in block %d", kind, e+1, b+1))
	}
	for _, f := range fields {
		if f.input == nil || !f.input.Show {
			continue
		}
		if i := elementLabelIssue("Label for field "+f.label, f.input.Placeholder, langs, b, e, true); i != nil {
			i.Path = extend(base, f.key)
			*is = append(*is, *i)
		}
	}
}

func (is *issues) endingRules(i int, e *Ending, langs []Language) {
	switch e.Type {
	case "endScreen":
		headline := e.Headline
		if headline == nil {
			headline = I18nString{}
		}
		is.addIssue(cardLabelIssue("cardHeadline", headline, langs, i))
		if e.Subheader != nil {
			is.addIssue(cardLabelIssue("subheader", e.Subheader, langs, i))
		}
		if e.ButtonLabel != nil || e.ButtonLink != nil {
			if e.ButtonLabel == nil {
				is.add(path("endings", i, "buttonLabel"), fmt.Sprintf("Ending card %d: Button label cannot be empty", i+1))
			} else {
				is.addIssue(cardLabelIssue("endingCardButtonLabel", e.ButtonLabel, langs, i))
			}
			if e.ButtonLink == nil || trimJS(*e.ButtonLink) == "" {
				is.add(path("endings", i, "buttonLink"), fmt.Sprintf("Ending card %d: Button link cannot be empty", i+1))
			} else if m := EndingCardURLIssue(*e.ButtonLink); m != "" {
				is.add(path("endings", i, "buttonLink"), fmt.Sprintf("Ending card %d: %s", i+1, m))
			}
		}
	case "redirectToUrl":
		if e.Label == nil || trimJS(*e.Label) == "" {
			is.add(path("endings", i, "label"), fmt.Sprintf("Redirect Url label cannot be empty for ending Card %d.", i+1))
		}
		if e.URL == nil || trimJS(*e.URL) == "" {
			is.add(path("endings", i, "url"), fmt.Sprintf("Ending card %d: Redirect URL cannot be empty", i+1))
		} else if m := EndingCardURLIssue(*e.URL); m != "" {
			is.add(path("endings", i, "url"), fmt.Sprintf("Ending card %d: %s", i+1, m))
		}
	}
}

// followUpRules checks every follow-up that is not being deleted: its recipient must be an email field of the survey, a hidden field, the verified email, or an address, and an endings trigger must name an ending.
func (is *issues) followUpRules(s *Survey) {
	if len(s.FollowUps) == 0 {
		return
	}
	valid := []string{"verifiedEmail"}
	for _, b := range s.Blocks {
		for _, el := range b.Elements {
			if (el.Type == ElementOpenText && el.InputType == "email") || (el.Type == ElementContactInfo && el.Email != nil && el.Email.Show) {
				valid = append(valid, el.ID)
			}
		}
	}
	valid = append(valid, s.HiddenFields.FieldIDs...)
	index := 0
	for _, f := range s.FollowUps {
		if f.Deleted {
			continue
		}
		if to := f.Action.Properties.To; to != "" {
			if !contains(valid, to) && !IsEmail(to) {
				is.add(path("followUps"), fmt.Sprintf("The action in follow up %d has an invalid email field", index+1))
			}
			if f.Trigger.Type == "endings" && (f.Trigger.Properties == nil || len(f.Trigger.Properties.EndingIDs) == 0) {
				is.add(path("followUps"), fmt.Sprintf("The trigger in follow up %d has no ending selected", index+1))
			}
		}
		index++
	}
}

var emailPattern = regexp.MustCompile(`^[A-Za-z0-9_'+\-.]*[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`)

// IsEmail is zod's email check: no leading dot, no two dots in a row, a domain with a letter TLD.
func IsEmail(s string) bool {
	return !strings.HasPrefix(s, ".") && !strings.Contains(s, "..") && emailPattern.MatchString(s)
}

// cyclicBlocks is forma2's findBlocksWithCyclicLogic: a depth-first search over jump targets, the logic fallback and the implicit next block (endings are not blocks and end the walk). Every block found on a cycle is returned, in the order the search marks them.
func cyclicBlocks(blocks []Block) []string {
	visited := map[string]bool{}
	onStack := map[string]bool{}
	var cyclic []string
	marked := map[string]bool{}
	mark := func(id string) {
		if !marked[id] {
			marked[id] = true
			cyclic = append(cyclic, id)
		}
	}
	indexOf := func(id string) int {
		for i, b := range blocks {
			if b.ID == id {
				return i
			}
		}
		return -1
	}
	var check func(id string) bool
	check = func(id string) bool {
		if !visited[id] {
			visited[id] = true
			onStack[id] = true
			i := indexOf(id)
			follow := func(dest string) bool {
				if indexOf(dest) < 0 {
					return false
				}
				if (!visited[dest] && check(dest)) || onStack[dest] {
					mark(id)
					onStack[id] = false
					return true
				}
				return false
			}
			if i >= 0 {
				for _, logic := range blocks[i].Logic {
					for _, a := range logic.Actions {
						if a.Objective == "jumpToBlock" && follow(a.Target) {
							return true
						}
					}
				}
				if blocks[i].LogicFallback != "" && follow(blocks[i].LogicFallback) {
					return true
				}
			}
			if i+1 < len(blocks) && follow(blocks[i+1].ID) {
				return true
			}
		}
		onStack[id] = false
		return false
	}
	for _, b := range blocks {
		check(b.ID)
	}
	return cyclic
}
