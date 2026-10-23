package survey

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/forma/server/internal/ai"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// forma2's generation call: 45 seconds, and an output budget that leaves room for a reasoning model's thinking before the JSON.
const (
	generationTimeout         = 45 * time.Second
	generationMaxOutputTokens = 8192
	generationTemperature     = 0.2
)

// generationSystemPrompt is forma2's buildV3SurveyGenerationSystemPrompt.
func generationSystemPrompt(surveyType string) string {
	lines := []string{
		"You generate concise Forma survey drafts.",
		"Return only data that matches the provided schema.",
		"Write all generated user-facing survey content in the same language as the user's request. " +
			"Detect the language from the request; if it clearly matches an allowed survey language, use that language. " +
			"If uncertain or no allowed survey language is a confident match, " +
			"use the preferred survey language from the user prompt.",
		"Return the survey language exactly as one of the allowed survey language codes from the user prompt.",
		"Allowed survey languages: " + strings.Join(userLocales, ", ") + ".",
		"Keep surveys focused: 3 to 6 questions is usually enough.",
		fmt.Sprintf("Group questions into %d to %d blocks. ", generatedMinBlocks, generatedMaxBlocks) +
			"Use one block for simple requests. Use multiple blocks when the user asks for sections, " +
			"pages, blocks, or clearly separate topics.",
		"Give each block a short, meaningful name. " +
			fmt.Sprintf("Use %d to %d questions per block.", generatedMinQuestionsPerBlock, generatedMaxQuestionsPerBlock),
		"Rating-like questions must be single-question blocks: rating, csat, ces, nps, matrix, and ranking. " +
			"Do not place rating-like questions together in the same block.",
		"Use only these question types: " + strings.Join(generatedElementTypes, ", ") + ".",
		"Use csat for satisfaction, ces for effort, nps for loyalty, ranking for priorities, " +
			"matrix for repeated row-and-column ratings, and date only when the requested feedback needs a date.",
		`For rating questions, set range to one of the string values "5", "7", or "10".`,
		`For csat questions, set range to "5". For ces questions, set range to "5" or "7".`,
		"Prefer clear, neutral question wording and short answer choices.",
		"Use required questions sparingly. Do not ask for sensitive personal data unless the prompt explicitly asks for it.",
		"Do not use file uploads, picture selection, address/contact collection, scheduling, CTA, consent, " +
			"embedded external forms, or any other question type outside the allowed list.",
		"Do not include branching, variables, hidden fields, URLs, files, scripts, markdown, HTML, or tracking instructions.",
		"The final product will be created as a " + surveyType + " survey draft.",
	}
	if surveyType == TypeApp {
		lines = append(lines, "This is an in-app survey shown inside the user's web or mobile app, so favor short questions that are easy to answer inside a small embedded widget.")
	}
	return strings.Join(lines, "\n")
}

// generationPrompt is forma2's buildV3SurveyGenerationPrompt.
func generationPrompt(prompt, surveyType, preferredLanguage string) string {
	return strings.Join([]string{
		"Create a draft " + surveyType + " survey from this request.",
		"Include a name, optional description, useful questions, and a simple ending.",
		"If the request is broad, choose a practical customer-feedback survey structure.",
		"Return the questions inside blocks. Use multiple blocks only when useful or requested.",
		"Keep rating-like questions in their own single-question blocks: rating, csat, ces, nps, matrix, and ranking.",
		"Use the same language as the request for all generated survey text. " +
			"If the request language is unclear or cannot be matched to an allowed survey language, " +
			"use the preferred survey language.",
		"If you enable the welcome card, include a short button label in the same language.",
		"Allowed survey languages: " + strings.Join(userLocales, ", ") + ".",
		"Preferred survey language: " + preferredLanguage + ".",
		"Set the returned language to exactly one allowed survey language code.",
		"",
		"User request:",
		prompt,
	}, "\n")
}

// generationRequest is forma2's buildV3SurveyGenerationRequest, the one model call the blocking and the streaming route both make. The answer is held to the ForAI schema and its refinements, as the AI SDK held it to the zod schema.
func generationRequest(in *GenerateInput) ai.ObjectRequest {
	preferred := defaultV3Language
	if len(in.Languages) > 0 {
		preferred = in.Languages[0]
	}
	temperature := generationTemperature
	return ai.ObjectRequest{
		Request: ai.Request{
			System: generationSystemPrompt(in.Type), Prompt: generationPrompt(in.Prompt, in.Type, preferred),
			Temperature: &temperature, MaxOutputTokens: generationMaxOutputTokens, Timeout: generationTimeout,
		},
		Schema:            generateDraftSchema,
		SchemaName:        "FormaSurveyDraft",
		SchemaDescription: "A concise Forma survey draft that can be converted to a v3 create payload.",
		Check: func(object json.RawMessage) error {
			if _, params := parseGeneratedDraft(object, true); len(params) > 0 {
				return fmt.Errorf("%s: %s", params[0].Name, params[0].Reason)
			}
			return nil
		},
	}
}

// i18n is forma2's text(): the value in the draft's one language.
func i18n(value, language string) *object {
	o := newObject()
	o.set(language, strings.TrimSpace(value))
	return o
}

func setText(o *object, key string, value *string, language string) {
	if value != nil && *value != "" {
		o.set(key, i18n(*value, language))
	}
}

// labeled is forma2's createLabeledItem: prefix_N with its label.
func labeled(prefix string, labels []string, language string) []any {
	out := make([]any, len(labels))
	for i, label := range labels {
		item := newObject()
		item.set("id", fmt.Sprintf("%s_%d", prefix, i+1))
		item.set("label", i18n(label, language))
		out[i] = item
	}
	return out
}

func scaleLabels(o *object, e generatedElement, language string) {
	setText(o, "lowerLabel", e.LowerLabel, language)
	setText(o, "upperLabel", e.UpperLabel, language)
}

// buildElement is forma2's buildElement: the element's defaults by type.
func buildElement(e generatedElement, index int, language string) *object {
	o := newObject()
	o.set("id", fmt.Sprintf("q_%d_%s", index+1, surveydoc.NewID()[:8]))
	o.set("headline", i18n(e.Headline, language))
	setText(o, "subheader", e.Subheader, language)
	o.set("required", e.Required)
	o.set("isDraft", true)
	scale := "number"
	if e.Scale != nil && *e.Scale != "" {
		scale = *e.Scale
	}
	switch e.Type {
	case "openText":
		o.set("type", "openText")
		setText(o, "placeholder", e.Placeholder, language)
		o.set("longAnswer", e.LongAnswer != nil && *e.LongAnswer)
		o.set("inputType", "text")
		charLimit := newObject()
		charLimit.set("enabled", false)
		o.set("charLimit", charLimit)
	case "multipleChoiceSingle", "multipleChoiceMulti":
		o.set("type", e.Type)
		o.set("choices", labeled("choice", e.Choices, language))
		o.set("shuffleOption", "none")
		o.set("displayType", "list")
	case "ranking":
		o.set("type", "ranking")
		o.set("choices", labeled("choice", e.Choices, language))
		o.set("shuffleOption", "none")
	case "matrix":
		o.set("type", "matrix")
		o.set("rows", labeled("row", e.Rows, language))
		o.set("columns", labeled("column", e.Columns, language))
		o.set("shuffleOption", "none")
	case "date":
		o.set("type", "date")
		format := "M-d-y"
		if e.Format != nil && *e.Format != "" {
			format = *e.Format
		}
		o.set("format", format)
	case "nps":
		o.set("type", "nps")
		scaleLabels(o, e, language)
		o.set("isColorCodingEnabled", false)
	case "csat", "ces":
		o.set("type", e.Type)
		o.set("scale", scale)
		r := e.Range
		if r == 0 {
			r = 5
			if e.Type == "ces" {
				r = 7
			}
		}
		o.set("range", r)
		scaleLabels(o, e, language)
		o.set("isColorCodingEnabled", false)
	default:
		o.set("type", "rating")
		o.set("scale", scale)
		r := e.Range
		if r == 0 {
			r = 5
		}
		o.set("range", r)
		scaleLabels(o, e, language)
		o.set("isColorCodingEnabled", false)
	}
	return o
}

// normalizeBlocks is forma2's normalizeGeneratedSurveyBlocks: in a block of several questions, each rating-like question moves into a block of its own named after its headline, and the questions around it stay together in order.
func normalizeBlocks(blocks []generatedBlock) []generatedBlock {
	var out []generatedBlock
	for _, b := range blocks {
		if len(b.Questions) == 1 {
			out = append(out, b)
			continue
		}
		var pending []generatedElement
		flush := func() {
			if len(pending) > 0 {
				out = append(out, generatedBlock{Name: b.Name, Questions: pending})
				pending = nil
			}
		}
		for _, q := range b.Questions {
			if !slices.Contains(ratingLikeTypes, q.Type) {
				pending = append(pending, q)
				continue
			}
			flush()
			out = append(out, generatedBlock{Name: q.Headline, Questions: []generatedElement{q}})
		}
		flush()
	}
	return out
}

// buildCreatePayload is forma2's buildCreatePayload: a v3 create body for the draft, in the draft's language, with forma2's defaults for everything the model does not write.
func buildCreatePayload(in *GenerateInput, d *generatedDraft) *object {
	language := d.Language
	p := newObject()
	p.set("workspaceId", in.WorkspaceID.String())
	p.set("type", in.Type)
	p.set("name", d.Name)
	p.set("status", "draft")
	p.set("defaultLanguage", language)
	lang := newObject()
	lang.set("code", language)
	lang.set("default", true)
	lang.set("enabled", true)
	p.set("languages", []any{lang})

	metadata := newObject()
	metadata.set("title", i18n(d.Name, language))
	setText(metadata, "description", d.Description, language)
	p.set("metadata", metadata)

	welcome := newObject()
	if wc := d.WelcomeCard; wc != nil && wc.Enabled {
		headline, button := d.Name, "Start"
		if wc.Headline != nil {
			headline = *wc.Headline
		}
		if wc.ButtonLabel != nil {
			button = *wc.ButtonLabel
		}
		welcome.set("enabled", true)
		welcome.set("headline", i18n(headline, language))
		setText(welcome, "subheader", wc.Subheader, language)
		welcome.set("buttonLabel", i18n(button, language))
		welcome.set("timeToFinish", true)
		welcome.set("showResponseCount", false)
	} else {
		welcome.set("enabled", false)
	}
	p.set("welcomeCard", welcome)

	index := 0
	blocks := make([]any, 0, len(d.Blocks))
	for _, b := range d.Blocks {
		elements := make([]any, len(b.Questions))
		for i, q := range b.Questions {
			elements[i] = buildElement(q, index, language)
			index++
		}
		block := newObject()
		block.set("id", surveydoc.NewID())
		block.set("name", b.Name)
		block.set("elements", elements)
		blocks = append(blocks, block)
	}
	p.set("blocks", blocks)

	ending := newObject()
	ending.set("id", surveydoc.NewID())
	ending.set("type", "endScreen")
	headline := "Thanks for your feedback"
	if d.Ending != nil && d.Ending.Headline != nil {
		headline = *d.Ending.Headline
	}
	ending.set("headline", i18n(headline, language))
	if d.Ending != nil {
		setText(ending, "subheader", d.Ending.Subheader, language)
	}
	p.set("endings", []any{ending})

	hidden := newObject()
	hidden.set("enabled", false)
	p.set("hiddenFields", hidden)
	p.set("variables", []any{})
	// AI writes content only; an app survey gets a valid default distribution (display once, no triggers) the author finishes in the editor.
	if in.Type == TypeApp {
		distribution := newObject()
		distribution.set("displayOption", "displayOnce")
		distribution.set("triggers", []any{})
		p.set("distribution", distribution)
	}
	return p
}
