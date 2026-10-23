package seed

import (
	"encoding/json"
	"fmt"
	"time"
)

// text is an i18n string in the survey document's shape.
func text(s string) map[string]string { return map[string]string{"default": s} }

func choice(id, label string) map[string]any { return map[string]any{"id": id, "label": text(label)} }

func field(placeholder string, required bool) map[string]any {
	return map[string]any{"show": true, "required": required, "placeholder": text(placeholder)}
}

// kitchenSink is forma2's "Kitchen Sink Survey": one element of every type the editor offers, in one block. forma2's seed listed twelve; four it left out (file upload, CSAT, CES and Cal.com booking) are here too. Picture selection is not: its images must be files in storage (the editor accepts only /storage paths or http(s) URLs), which a database seed cannot write. Ids are fixed, so a second seed rewrites the same document.
var kitchenSink = []map[string]any{
	{"id": "feedback", "type": "openText", "headline": text("What do you think of Forma?"), "subheader": text("Please be honest!"), "required": true, "placeholder": text("Your feedback here..."), "longAnswer": true, "inputType": "text", "charLimit": map[string]any{"enabled": false}},
	{"id": "frequency", "type": "multipleChoiceSingle", "headline": text("How often do you use Forma?"), "required": true, "choices": []any{choice("daily", "Daily"), choice("weekly", "Weekly"), choice("monthly", "Monthly"), choice("rarely", "Rarely")}},
	{"id": "features", "type": "multipleChoiceMulti", "headline": text("Which features do you use?"), "required": false, "choices": []any{choice("surveys", "Surveys"), choice("analytics", "Analytics"), choice("integrations", "Integrations"), choice("actions", "Action Tracking")}},
	{"id": "recommend", "type": "nps", "headline": text("How likely are you to recommend Forma?"), "required": true, "lowerLabel": text("Not likely"), "upperLabel": text("Very likely")},
	{"id": "docs", "type": "cta", "headline": text("Check out our documentation!"), "required": false, "buttonExternal": true, "buttonUrl": "https://forma.ylam.ai/docs", "ctaButtonLabel": text("Go to Docs")},
	{"id": "experience", "type": "rating", "headline": text("Rate your overall experience"), "required": true, "scale": "star", "range": 5, "lowerLabel": text("Poor"), "upperLabel": text("Excellent")},
	{"id": "satisfaction", "type": "csat", "headline": text("How satisfied are you with Forma?"), "required": false, "scale": "smiley", "range": 5},
	{"id": "effort", "type": "ces", "headline": text("Forma made it easy to collect feedback."), "required": false, "scale": "number", "range": 5},
	{"id": "terms", "type": "consent", "headline": text("Do you agree to our terms?"), "required": true, "label": text("I agree to the terms and conditions")},
	{"id": "started", "type": "date", "headline": text("When did you start using Forma?"), "required": true, "format": "M-d-y"},
	{"id": "aspects", "type": "matrix", "headline": text("How do you feel about these aspects?"), "required": true, "shuffleOption": "none",
		"rows":    []any{choice("design", "UI Design"), choice("performance", "Performance"), choice("documentation", "Documentation")},
		"columns": []any{choice("disappointed", "Disappointed"), choice("neutral", "Neutral"), choice("satisfied", "Satisfied")}},
	{"id": "location", "type": "address", "headline": text("Where are you located?"), "required": true,
		"addressLine1": field("Address Line 1", true), "addressLine2": field("Address Line 2", false), "city": field("City", true),
		"state": field("State", true), "zip": field("Zip", true), "country": field("Country", true)},
	{"id": "priorities", "type": "ranking", "headline": text("Rank these features"), "required": true, "choices": []any{choice("featurea", "Feature A"), choice("featureb", "Feature B"), choice("featurec", "Feature C")}},
	{"id": "contact", "type": "contactInfo", "headline": text("How can we reach you?"), "required": true,
		"firstName": field("First Name", true), "lastName": field("Last Name", true), "email": field("Email", true),
		"phone": field("Phone", false), "company": field("Company", false)},
	{"id": "screenshot", "type": "fileUpload", "headline": text("Attach a screenshot, if you like"), "required": false, "allowMultipleFiles": false},
	{"id": "booking", "type": "cal", "headline": text("Book a call with us"), "required": false, "calUserName": "forma"},
}

// seedSurvey is one survey the base seed writes: forma2's four, with the number of responses it generated for each.
type seedSurvey struct {
	id        string
	name      string
	status    string
	elements  []map[string]any
	responses int
}

var seedSurveys = []seedSurvey{
	{id: "clseedsurveykitchen00", name: "Kitchen Sink Survey", status: "inProgress", elements: kitchenSink, responses: 50},
	{id: "clseedsurveycsat000000", name: "CSAT Survey", status: "inProgress", responses: 50, elements: []map[string]any{
		{"id": "satisfied", "type": "rating", "headline": text("How satisfied are you with our product?"), "required": true, "scale": "smiley", "range": 5},
	}},
	{id: "clseedsurveydraft00000", name: "Draft Survey", status: "draft", elements: []map[string]any{
		{"id": "soon", "type": "openText", "headline": text("Coming soon..."), "required": false, "inputType": "text", "charLimit": map[string]any{"enabled": false}},
	}},
	{id: "clseedsurveycomplete00", name: "Exit Survey", status: "completed", responses: 50, elements: []map[string]any{
		{"id": "reason", "type": "multipleChoiceSingle", "headline": text("Why are you leaving?"), "required": true, "choices": []any{
			choice("expensive", "Too expensive"), choice("alternative", "Found a better alternative"), choice("missing", "Missing features"),
		}},
	}},
}

// blocksOf puts the elements in one block, as forma2's seed did.
func blocksOf(surveyKey string, elements []map[string]any) json.RawMessage {
	raw, err := json.Marshal([]any{map[string]any{"id": surveyKey + "block", "name": "Main Block", "elements": elements}})
	if err != nil {
		panic(err)
	}
	return raw
}

// endingsOf is the one ending card every seeded survey closes with.
func endingsOf(surveyKey string) json.RawMessage {
	raw, err := json.Marshal([]any{map[string]any{
		"id": surveyKey + "end", "type": "endScreen", "headline": text("Thank you!"), "subheader": text("We appreciate your feedback."),
	}})
	if err != nil {
		panic(err)
	}
	return raw
}

// translated gives every i18n string of a survey document a text for each of the codes, the default text marked with the code, as a survey whose languages are all enabled must carry.
func translated(doc json.RawMessage, codes []string) json.RawMessage {
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		panic(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if def, ok := v["default"].(string); ok {
				for _, code := range codes {
					v[code] = "[" + code + "] " + def
				}
				return
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(v)
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}

// answer is forma2's generateQuestionResponse made deterministic: the i-th response's value for an element, cycling through the options instead of drawing them at random, so two seeds write the same data. nil leaves the element unanswered.
func answer(e map[string]any, i int) any {
	pick := func(key string) map[string]any {
		items := e[key].([]any)
		return items[i%len(items)].(map[string]any)
	}
	label := func(c map[string]any) string { return c["label"].(map[string]string)["default"] }
	switch e["type"] {
	case "openText":
		return fmt.Sprintf("Sample response %d", i)
	case "multipleChoiceSingle":
		return label(pick("choices"))
	case "multipleChoiceMulti":
		choices := e["choices"].([]any)
		return []string{label(choices[0].(map[string]any)), label(choices[1+i%(len(choices)-1)].(map[string]any))}
	case "nps":
		return i % 11
	case "rating", "csat", "ces":
		return 1 + i%e["range"].(int)
	case "cta":
		return "clicked"
	case "consent":
		return "accepted"
	case "date":
		return time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format(time.DateOnly)
	case "matrix":
		out := map[string]string{}
		columns := e["columns"].([]any)
		for r, row := range e["rows"].([]any) {
			out[label(row.(map[string]any))] = label(columns[(i+r)%len(columns)].(map[string]any))
		}
		return out
	case "address":
		return []string{"Main St 1", "", "Berlin", "Berlin", "10115", "Germany"}
	case "ranking":
		choices := e["choices"].([]any)
		out := make([]string, len(choices))
		for k := range choices {
			out[k] = label(choices[(i+k)%len(choices)].(map[string]any))
		}
		return out
	case "contactInfo":
		return []string{"John", "Doe", fmt.Sprintf("john.doe.%d@example.com", i), "", ""}
	}
	return nil
}

// responseData is the i-th response's answers to every element that has a generated answer.
func responseData(elements []map[string]any, i int) map[string]any {
	data := map[string]any{}
	for _, e := range elements {
		if v := answer(e, i); v != nil {
			data[e["id"].(string)] = v
		}
	}
	return data
}
