package surveydoc

import (
	"fmt"
	"strings"
)

// i18nDefaultMessage is ZI18nString's refinement.
const i18nDefaultMessage = "I18n string must have a 'default' key"

func (is *issues) i18n(p []any, s I18nString) {
	if s != nil && !s.HasDefault() {
		is.add(p, i18nDefaultMessage)
	}
}

func (is *issues) storageURL(p []any, s *string) {
	if s != nil && !IsStorageURL(*s) {
		is.add(p, StorageURLMessage)
	}
}

// elementIDIssues is forma2's ZSurveyElementId refinement; every rule that fails is reported.
func elementIDIssues(id string) []string {
	var out []string
	if isForbiddenID(id) {
		out = append(out, "Element id is not allowed")
	}
	if strings.Contains(id, " ") {
		out = append(out, "Element id not allowed, avoid using spaces.")
	}
	if !IsLegacyIDCharset(id) {
		out = append(out, "Element id not allowed, use only alphanumeric characters, hyphens, or underscores.")
	}
	return out
}

// firstDuplicate is the index of the first value that repeats an earlier one, as forma2's findIndex((id, index) => ids.indexOf(id) !== index) found it, or -1.
func firstDuplicate(values []string) int {
	seen := map[string]bool{}
	for i, v := range values {
		if seen[v] {
			return i
		}
		seen[v] = true
	}
	return -1
}

// FieldIssues are the refinements zod attached to individual fields of the survey document (ZI18nString's default key, id rules, URL schemes, per-element rules, uniqueness within one list). Unlike the cross-field refinement they do not depend on the rest of the survey.
func FieldIssues(s *Survey) []Issue {
	var is issues
	wc := &s.WelcomeCard
	is.i18n(path("welcomeCard", "headline"), wc.Headline)
	is.i18n(path("welcomeCard", "subheader"), wc.Subheader)
	is.storageURL(path("welcomeCard", "fileUrl"), wc.FileURL)
	is.i18n(path("welcomeCard", "buttonLabel"), wc.ButtonLabel)
	is.storageURL(path("welcomeCard", "videoUrl"), wc.VideoURL)
	if wc.Enabled && wc.Headline == nil {
		is.add(path("welcomeCard"), "Welcome card must have a headline")
	}

	for b := range s.Blocks {
		is.blockFields(b, &s.Blocks[b])
	}
	blockIDs := make([]string, len(s.Blocks))
	for i, b := range s.Blocks {
		blockIDs[i] = b.ID
	}
	if d := firstDuplicate(blockIDs); d >= 0 {
		is.add(path("blocks", d, "id"), "Block IDs must be unique")
	}

	for i := range s.Endings {
		is.endingFields(i, &s.Endings[i])
	}
	endingIDs := make([]string, len(s.Endings))
	for i, e := range s.Endings {
		endingIDs[i] = e.ID
	}
	if d := firstDuplicate(endingIDs); d >= 0 {
		is.add(path("endings", d, "id"), "Ending IDs must be unique")
	}

	for i, field := range s.HiddenFields.FieldIDs {
		p := path("hiddenFields", "fieldIds", i)
		if isForbiddenID(field) {
			is.add(p, "Hidden field id is not allowed")
		}
		if strings.Contains(field, " ") {
			is.add(p, "Hidden field id not allowed, avoid using spaces.")
		}
		if !IsLegacyIDCharset(field) {
			is.add(p, "Hidden field id not allowed, use only alphanumeric characters, hyphens, or underscores.")
		}
	}

	variableIDs := make([]string, len(s.Variables))
	variableNames := make([]string, len(s.Variables))
	for i, v := range s.Variables {
		if !legacyVariableName.MatchString(v.Name) {
			is.add(path("variables", i, "variables"), "Variable name can only contain lowercase letters, numbers, and underscores")
		}
		variableIDs[i], variableNames[i] = v.ID, v.Name
	}
	if firstDuplicate(variableIDs) >= 0 {
		is.add(path("variables"), "Variable IDs must be unique")
	}
	if firstDuplicate(variableNames) >= 0 {
		is.add(path("variables"), "Variable names must be unique")
	}

	for i, f := range s.FollowUps {
		is.followUpTrigger(path("followUps", i, "trigger"), f.Trigger)
	}

	is.i18n(path("metadata", "title"), s.MetadataTitle)
	is.i18n(path("metadata", "description"), s.MetadataDescription)
	return is
}

func (is *issues) blockFields(b int, block *Block) {
	base := path("blocks", b)
	elementIDs := make([]string, len(block.Elements))
	for e := range block.Elements {
		elementIDs[e] = block.Elements[e].ID
		is.elementFields(extend(base, "elements", e), &block.Elements[e])
	}
	for l, logic := range block.Logic {
		lp := extend(base, "logic", l)
		is.conditionFields(extend(lp, "conditions"), &logic.Conditions)
		for a, action := range logic.Actions {
			is.actionFields(extend(lp, "actions", a), action)
		}
	}
	is.i18n(extend(base, "buttonLabel"), block.ButtonLabel)
	is.i18n(extend(base, "backButtonLabel"), block.BackButtonLabel)
	if d := firstDuplicate(elementIDs); d >= 0 {
		is.add(extend(base, d, "id"), "Element IDs must be unique within a block")
	}
}

func (is *issues) elementFields(p []any, el *Element) {
	for _, m := range elementIDIssues(el.ID) {
		is.add(extend(p, "id"), m)
	}
	is.i18n(extend(p, "headline"), el.Headline)
	is.i18n(extend(p, "subheader"), el.Subheader)
	is.storageURL(extend(p, "imageUrl"), el.ImageURL)
	is.storageURL(extend(p, "videoUrl"), el.VideoURL)
	is.i18n(extend(p, "placeholder"), el.Placeholder)
	is.i18n(extend(p, "label"), el.Label)
	is.i18n(extend(p, "otherOptionPlaceholder"), el.OtherOptionPlaceholder)
	is.i18n(extend(p, "lowerLabel"), el.LowerLabel)
	is.i18n(extend(p, "upperLabel"), el.UpperLabel)
	is.i18n(extend(p, "html"), el.HTML)
	is.i18n(extend(p, "ctaButtonLabel"), el.CTAButtonLabel)
	for c, choice := range el.Choices {
		if el.Type == ElementPictureSelection {
			if !IsStorageURL(choice.ImageURL) {
				is.add(extend(p, "choices", c, "imageUrl"), StorageURLMessage)
			}
			continue
		}
		is.i18n(extend(p, "choices", c, "label"), choice.Label)
	}
	for r, row := range el.Rows {
		is.i18n(extend(p, "rows", r, "label"), row.Label)
	}
	for c, col := range el.Columns {
		is.i18n(extend(p, "columns", c, "label"), col.Label)
	}
	for _, t := range []struct {
		name  string
		input *ToggleInput
	}{
		{"addressLine1", el.AddressLine1}, {"addressLine2", el.AddressLine2}, {"city", el.City}, {"state", el.State}, {"zip", el.Zip}, {"country", el.Country},
		{"firstName", el.FirstName}, {"lastName", el.LastName}, {"email", el.Email}, {"phone", el.Phone}, {"company", el.Company},
	} {
		if t.input != nil {
			is.i18n(extend(p, t.name, "placeholder"), t.input.Placeholder)
		}
	}

	switch el.Type {
	case ElementOpenText:
		if cl := el.CharLimit; cl != nil {
			if cl.Enabled != nil && *cl.Enabled && cl.Min == nil && cl.Max == nil {
				is.add(p, "Enter the values for either minimum or maximum field")
			}
			if (cl.Min != nil && *cl.Min < 0) || (cl.Max != nil && *cl.Max < 0) {
				is.add(p, "The character limit values should be positive")
			}
			if cl.Min != nil && cl.Max != nil && *cl.Min > *cl.Max {
				is.add(p, "Minimum value cannot be greater than the maximum value")
			}
		}
	case ElementCTA:
		if el.IsExternalButton() {
			if el.ButtonURL == nil || trimJS(*el.ButtonURL) == "" {
				is.add(extend(p, "buttonUrl"), "Button URL is required when external button is enabled")
			} else if !IsSafeLinkURL(*el.ButtonURL) {
				is.add(extend(p, "buttonUrl"), "Please enter a valid http(s), mailto: or tel: URL")
			}
			if trimJS(el.CTAButtonLabel.Default()) == "" {
				is.add(extend(p, "ctaButtonLabel"), "Button label is required when external button is enabled")
			}
		}
	}
}

// conditionFields is ZSingleCondition's refinement, applied to every single condition of a group.
func (is *issues) conditionFields(p []any, c *Condition) {
	if c.IsGroup {
		for i := range c.Conditions {
			is.conditionFields(extend(p, "conditions", i), &c.Conditions[i])
		}
		return
	}
	is.singleConditionFields(p, c)
}

func (is *issues) singleConditionFields(p []any, c *Condition) {
	if !OperatorTakesNoRightOperand(c.Operator) {
		if c.RightOperand == nil {
			is.add(extend(p, "rightOperand"), fmt.Sprintf(`Conditional Logic: right operand is required for operator "%s"`, c.Operator))
		} else if c.RightOperand.Type == "static" {
			if s, ok := c.RightOperand.String(); ok && s == "" {
				is.add(p, fmt.Sprintf(`Conditional Logic: right operand value cannot be empty for operator "%s"`, c.Operator))
			}
		}
	} else if c.RightOperand != nil {
		is.add(extend(p, "rightOperand"), fmt.Sprintf(`Conditional Logic: right operand should not be present for operator "%s"`, c.Operator))
	}
}

func (is *issues) actionFields(p []any, a Action) {
	switch a.Objective {
	case "calculate":
		if a.Operator == "divide" && a.Value != nil && a.Value.Type == "static" {
			if n, ok := a.Value.Number(); ok && n == 0 {
				is.add(extend(p, "value", "value"), "Conditional Logic: Cannot divide by zero")
			}
		}
	case "requireAnswer":
		if len(elementIDIssues(a.Target)) > 0 {
			is.add(extend(p, "target", "target"), "Conditional Logic: Target element id is not a valid element id")
		}
	case "jumpToBlock":
		if !IsCUID2(a.Target) {
			is.add(extend(p, "target"), "Conditional Logic: Target block id is not a valid block id")
		}
	}
}

func (is *issues) endingFields(i int, e *Ending) {
	p := path("endings", i)
	is.i18n(extend(p, "headline"), e.Headline)
	is.i18n(extend(p, "subheader"), e.Subheader)
	is.i18n(extend(p, "buttonLabel"), e.ButtonLabel)
	if e.ButtonLink != nil {
		if m := EndingCardURLIssue(*e.ButtonLink); m != "" {
			is.add(extend(p, "buttonLink"), m)
		}
	}
	is.storageURL(extend(p, "imageUrl"), e.ImageURL)
	is.storageURL(extend(p, "videoUrl"), e.VideoURL)
	if e.URL != nil {
		if m := EndingCardURLIssue(*e.URL); m != "" {
			is.add(extend(p, "url"), m)
		}
	}
}

// followUpTrigger is ZSurveyFollowUpTrigger's refinement.
func (is *issues) followUpTrigger(p []any, t FollowUpTrigger) {
	if t.Type == "response" && t.Properties != nil {
		is.add(p, "Properties should be null for response type")
	}
	if t.Type == "endings" && t.Properties == nil {
		is.add(p, "Properties must be defined for endings type")
	}
}

// OperatorsWithoutRightOperand is forma2's operatorsWithoutRightOperand.
var OperatorsWithoutRightOperand = []string{
	"isSubmitted", "isSkipped", "isClicked", "isNotClicked", "isAccepted", "isBooked",
	"isPartiallySubmitted", "isCompletelySubmitted", "isSet", "isNotSet", "isEmpty", "isNotEmpty",
}

// OperatorTakesNoRightOperand reports whether an operator is one of OperatorsWithoutRightOperand.
func OperatorTakesNoRightOperand(op string) bool {
	for _, o := range OperatorsWithoutRightOperand {
		if o == op {
			return true
		}
	}
	return false
}
