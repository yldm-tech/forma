package ai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// forma2's example dataset (summary/lib/example-responses.ts): twenty responses, about a fifth of them abandoned, and six displays nobody answered, which together put the completion rate near 62%.
const (
	exampleResponseCount       = 20
	exampleImpressionOnlyCount = 6
	// ExampleAIGeneratedTagName tags every generated response so it can be told apart from a real one.
	ExampleAIGeneratedTagName = "AI-generated example response"
	exampleDropOffRate        = 0.2
	exampleResponseSpreadDays = 10

	exampleOpenTextTimeout              = 45 * time.Second
	exampleOpenTextMaxOutputTokens      = 4096
	exampleOpenTextMaxAnswersPerChunk   = 20
	exampleDefaultLanguage              = "default"
	exampleMetaSource                   = "example-generation"
	exampleNoSupportedElementsMessage   = "This survey doesn't contain any question types we can synthesize answers for yet."
	exampleArchivedSurveyMessage        = "Cannot generate example responses for an archived survey."
	exampleSurveyHasResponsesMessage    = "Example responses can only be generated for a survey that has no responses yet."
	exampleOpenTextGenerationFailureLog = "Failed to generate open-text example responses with AI; using fallback answers"
)

// Weighted toward common values so the charts do not look uniformly random.
var (
	exampleBrowsers  = []string{"Chrome", "Chrome", "Chrome", "Safari", "Safari", "Firefox", "Edge"}
	exampleDevices   = []string{"desktop", "desktop", "desktop", "mobile", "mobile", "tablet"}
	exampleOS        = []string{"macOS", "Windows", "Windows", "iOS", "iOS", "Android", "Linux"}
	exampleCountries = []string{"US", "US", "DE", "GB", "FR", "IN", "BR", "JP", "CA", "AU", "NL", "ES"}
)

// exampleTTCBands are forma2's rough milliseconds per element type, so the average time per question looks plausible.
var exampleTTCBands = map[string][2]int{
	surveydoc.ElementOpenText:             {8000, 30000},
	surveydoc.ElementMultipleChoiceSingle: {2000, 7000},
	surveydoc.ElementMultipleChoiceMulti:  {4000, 12000},
	surveydoc.ElementRating:               {2000, 5000},
	surveydoc.ElementNPS:                  {2500, 6000},
	surveydoc.ElementCSAT:                 {2000, 5000},
	surveydoc.ElementCES:                  {3000, 7000},
	surveydoc.ElementDate:                 {3000, 8000},
	surveydoc.ElementRanking:              {8000, 20000},
	surveydoc.ElementMatrix:               {6000, 18000},
	surveydoc.ElementAddress:              {15000, 45000},
	surveydoc.ElementContactInfo:          {10000, 30000},
	surveydoc.ElementPictureSelection:     {3000, 9000},
	surveydoc.ElementConsent:              {2000, 5000},
	surveydoc.ElementSlider:               {2500, 6000},
}

var exampleDefaultTTCBand = [2]int{3000, 10000}

// exampleProfile is a simulated respondent, forma2's TExampleRespondentProfile; the prompt carries it as JSON.
type exampleProfile struct {
	Sentiment  string   `json:"sentiment"`
	Persona    string   `json:"persona"`
	Verbosity  string   `json:"verbosity"`
	Polish     string   `json:"polish"`
	Priorities []string `json:"priorities"`
}

func preset(sentiment, persona, verbosity, polish, priorityA, priorityB string) exampleProfile {
	return exampleProfile{Sentiment: sentiment, Persona: persona, Verbosity: verbosity, Polish: polish, Priorities: []string{priorityA, priorityB}}
}

var exampleProfiles = []exampleProfile{
	preset("promoter", "product manager at a growing SaaS team", "detailed", "clean", "team adoption", "speed"),
	preset("positive", "founder at a small company", "brief", "casual", "cost", "setup"),
	preset("neutral", "operations lead comparing internal tools", "normal", "clean", "reliability", "reporting"),
	preset("skeptical", "software engineer maintaining data workflows", "normal", "rough", "missing features", "reliability"),
	preset("positive", "customer success manager", "detailed", "clean", "team adoption", "support"),
	preset("promoter", "executive stakeholder", "brief", "clean", "reporting", "speed"),
	preset("neutral", "designer running user research", "detailed", "casual", "setup", "team adoption"),
	preset("detractor", "developer evaluating the product for a side project", "brief", "rough", "cost", "missing features"),
	preset("positive", "analytics owner at a mid-market team", "normal", "clean", "reporting", "reliability"),
	preset("skeptical", "team lead with a busy roadmap", "normal", "casual", "speed", "setup"),
}

// The wire format of address and contact-info answers is a fixed-length string array in the order of addressFields and contactInfoFields; a hidden field contributes an empty string. The fixtures are in that order.
var (
	addressFixtures = [][]string{
		{"14 Example Lane", "", "Springfield", "IL", "62701", "US"},
		{"82 Demo Street", "Suite 4", "Berlin", "BE", "10115", "DE"},
		{"5 Sample Road", "", "London", "", "SW1A 1AA", "GB"},
		{"31 Test Avenue", "Floor 2", "Toronto", "ON", "M5H 2N2", "CA"},
		{"9 Fictional Blvd", "", "Sydney", "NSW", "2000", "AU"},
	}
	contactInfoFixtures = [][]string{
		{"Alex", "Morgan", "alex.morgan@example.com", "+1 555 0101", "Example Labs"},
		{"Jamie", "Taylor", "jamie.taylor@example.com", "+44 20 7946 0102", "Demo Works"},
		{"Sam", "Rivers", "sam.rivers@example.com", "+49 30 1234 0103", "Sample Systems"},
		{"Riley", "Chen", "riley.chen@example.com", "+61 2 5550 0104", "Fictional Studio"},
		{"Jordan", "Lee", "jordan.lee@example.com", "+1 555 0105", "Placeholder Co"},
	}
)

// exampleElement is a survey element with the one field surveydoc.Element does not read: a picture selection's allowMulti.
type exampleElement struct {
	surveydoc.Element
	AllowMulti bool
}

func addressFields(e *surveydoc.Element) []*surveydoc.ToggleInput {
	return []*surveydoc.ToggleInput{e.AddressLine1, e.AddressLine2, e.City, e.State, e.Zip, e.Country}
}

func contactInfoFields(e *surveydoc.Element) []*surveydoc.ToggleInput {
	return []*surveydoc.ToggleInput{e.FirstName, e.LastName, e.Email, e.Phone, e.Company}
}

func shown(fields []*surveydoc.ToggleInput) bool {
	for _, f := range fields {
		if f != nil && f.Show {
			return true
		}
	}
	return false
}

func localized(s surveydoc.I18nString) string { return s[exampleDefaultLanguage] }

// stripHTML is forma2's stripHtml: tags become spaces, the common entities are decoded and whitespace is collapsed, so the model and the fallback matcher read prose rather than the editor's markup.
func stripHTML(value string) string {
	var b strings.Builder
	inside := false
	for _, r := range value {
		switch {
		case r == '<':
			inside = true
		case r == '>' && inside:
			inside = false
			b.WriteByte(' ')
		case !inside:
			b.WriteRune(r)
		}
	}
	out := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(b.String())
	return strings.Join(strings.Fields(out), " ")
}

func plainHeadline(e *exampleElement) string {
	if e == nil {
		return ""
	}
	return stripHTML(localized(e.Headline))
}

func plainSubheader(e *exampleElement) string {
	if e == nil {
		return ""
	}
	if raw := localized(e.Subheader); raw != "" {
		return stripHTML(raw)
	}
	return ""
}

func choiceLabels(e *exampleElement) []string {
	var out []string
	for _, c := range e.Choices {
		if l := localized(c.Label); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func pictureIDs(e *exampleElement) []string {
	var out []string
	for _, c := range e.Choices {
		if c.ID != "" {
			out = append(out, c.ID)
		}
	}
	return out
}

func matrixLabels(e *exampleElement) (rows, columns []string) {
	for _, r := range e.Rows {
		if l := localized(r.Label); l != "" {
			rows = append(rows, l)
		}
	}
	for _, c := range e.Columns {
		if l := localized(c.Label); l != "" {
			columns = append(columns, l)
		}
	}
	return rows, columns
}

// supportedForGeneration is forma2's isElementSupportedForGeneration: the types it can synthesize an answer for, when the element has something to answer with.
func supportedForGeneration(e *exampleElement) bool {
	switch e.Type {
	case surveydoc.ElementOpenText, surveydoc.ElementRating, surveydoc.ElementNPS, surveydoc.ElementConsent, surveydoc.ElementCSAT, surveydoc.ElementCES, surveydoc.ElementDate:
		return true
	case surveydoc.ElementMultipleChoiceSingle, surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementRanking:
		return len(choiceLabels(e)) > 0
	case surveydoc.ElementMatrix:
		rows, columns := matrixLabels(e)
		return len(rows) > 0 && len(columns) > 0
	case surveydoc.ElementAddress:
		return shown(addressFields(&e.Element))
	case surveydoc.ElementContactInfo:
		return shown(contactInfoFields(&e.Element))
	case surveydoc.ElementPictureSelection:
		return len(pictureIDs(e)) > 0
	case surveydoc.ElementSlider:
		scale, ok := e.SliderScale()
		if !ok {
			return false
		}
		field, _ := scale.Issue()
		return field == ""
	}
	return false
}

// exampleSurvey is what generation reads of a survey.
type exampleSurvey struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Name        string
	Archived    bool
	WelcomeCard surveydoc.WelcomeCard
	Endings     []surveydoc.Ending
	// Elements are the blocks' elements in order, each id once (forma2's collectSurveyElements).
	Elements []*exampleElement
	// DefaultLanguage is the code of the survey's default language, nil for a survey without languages; OtherLanguages are its enabled non-default codes.
	DefaultLanguage *string
	OtherLanguages  []string
}

func (s *exampleSurvey) element(id string) *exampleElement {
	for _, e := range s.Elements {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// supported are the elements an answer can be synthesized for, in survey order.
func (s *exampleSurvey) supported() []*exampleElement {
	var out []*exampleElement
	for _, e := range s.Elements {
		if _, ok := exampleTTCBands[e.Type]; ok && supportedForGeneration(e) {
			out = append(out, e)
		}
	}
	return out
}

// randomInt is a uniform integer in [0, n). Preview data needs no cryptographic randomness; crypto/rand keeps static analysis quiet and costs nothing at this volume, as in forma2.
func randomInt(n int) int {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.LittleEndian.Uint64(b[:]) % uint64(n)) //nolint:gosec // n is a small positive slice length or range
}

func randomFloat() float64 { return float64(randomInt(1<<32)) / (1 << 32) }

func pick(values []string) string { return values[randomInt(len(values))] }

func randomBetween(lo, hi int) int { return lo + randomInt(hi-lo+1) }

// exampleRow is one planned response, forma2's TExampleResponsePlanRow. Keys keeps the order the answers were planned in, which the prompt shows.
type exampleRow struct {
	RowID       string
	Index       int
	Profile     exampleProfile
	Keys        []string
	Data        map[string]any
	TTC         map[string]float64
	Finished    bool
	EndingID    *string
	Language    *string
	Meta        map[string]any
	CreatedAt   time.Time
	OpenTextIDs []string
}

func (r *exampleRow) set(id string, value any) {
	if _, ok := r.Data[id]; !ok {
		r.Keys = append(r.Keys, id)
	}
	r.Data[id] = value
}

func hashString(value string) int {
	acc := 0
	for _, r := range value {
		acc = (acc*31 + int(r)) % 997
	}
	return acc
}

func skipOptional(e *exampleElement, rowIndex int) bool {
	return !e.Required && (hashString(e.ID)+rowIndex)%5 == 0
}

var lumpyPattern = []int{0, 0, 1, 0, 2, 1, 0, 3, 1, 4}

func lumpyIndex(optionCount, rowIndex int) int {
	if optionCount <= 1 {
		return 0
	}
	return min(lumpyPattern[rowIndex%len(lumpyPattern)], optionCount-1)
}

// jsRound is JavaScript's Math.round, which rounds halves up.
func jsRound(x float64) int { return int(math.Floor(x + 0.5)) }

func scoreFor(p exampleProfile, rowIndex, lo, hi int) int {
	top := max(lo, hi)
	span := float64(top - lo)
	high := lo + max(0, jsRound(span*0.75))
	middle := lo + max(0, jsRound(span*0.5))
	low := lo + max(0, jsRound(span*0.25))
	even := rowIndex%2 == 0
	switch p.Sentiment {
	case "promoter":
		if even {
			return top
		}
		return max(lo, top-1)
	case "positive":
		if rowIndex%3 == 0 {
			return max(lo, high)
		}
		return max(lo, top)
	case "neutral":
		if even {
			return min(top, middle)
		}
		return min(top, middle+1)
	case "skeptical":
		if even {
			return max(lo, low)
		}
		return max(lo, middle)
	}
	if even {
		return lo
	}
	return max(lo, low)
}

func npsFor(p exampleProfile, rowIndex int) int {
	scores := map[string][2]int{"promoter": {10, 9}, "positive": {9, 8}, "neutral": {8, 7}, "skeptical": {6, 5}, "detractor": {3, 2}}[p.Sentiment]
	return scores[rowIndex%2]
}

func rangeOr(r *float64, fallback int) int {
	if r == nil {
		return fallback
	}
	return int(*r)
}

func rotate(values []string, start int) []string {
	return append(append([]string{}, values[start:]...), values[:start]...)
}

func uniqueStrings(values []string) []string {
	var out []string
	for _, v := range values {
		if !contains(out, v) {
			out = append(out, v)
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

// fixtureAnswer is an address or contact-info answer in its wire format: the fixture's value for every shown field, an empty string for every hidden one.
func fixtureAnswer(fields []*surveydoc.ToggleInput, fixture []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		if f != nil && f.Show {
			out[i] = fixture[i]
		}
	}
	return out
}

// closedAnswer is forma2's buildClosedAnswerForElement after transformAnswerForElement; nil for an open text, which the model answers.
func closedAnswer(e *exampleElement, p exampleProfile, rowIndex int, now time.Time) any {
	switch e.Type {
	case surveydoc.ElementMultipleChoiceSingle:
		labels := choiceLabels(e)
		return labels[lumpyIndex(len(labels), rowIndex)]
	case surveydoc.ElementMultipleChoiceMulti:
		labels := choiceLabels(e)
		first := lumpyIndex(len(labels), rowIndex)
		picks := []string{labels[first]}
		if len(labels) > 1 && rowIndex%3 != 0 {
			picks = append(picks, labels[(first+1+(rowIndex%(len(labels)-1)))%len(labels)])
		}
		return uniqueStrings(picks)
	case surveydoc.ElementRating, surveydoc.ElementCSAT, surveydoc.ElementCES:
		return scoreFor(p, rowIndex, 1, rangeOr(e.Range, 5))
	case surveydoc.ElementNPS:
		return npsFor(p, rowIndex)
	case surveydoc.ElementDate:
		daysAgo := ((rowIndex + 1) * 23) % 365
		day := now.Add(-time.Duration(daysAgo) * 24 * time.Hour).UTC()
		if e.IncludeTime != nil && *e.IncludeTime {
			// Office hours on the quarter hour, so the times read like ones people pick.
			return time.Date(day.Year(), day.Month(), day.Day(), 9+(rowIndex*5)%9, 15*(rowIndex%4), 0, 0, time.UTC).Format("2006-01-02T15:04")
		}
		return day.Format("2006-01-02")
	case surveydoc.ElementSlider:
		scale, _ := e.SliderScale()
		return scale.Value(scoreFor(p, rowIndex, 0, int(math.Round(scale.Steps()))))
	case surveydoc.ElementRanking:
		labels := choiceLabels(e)
		return rotate(labels, lumpyIndex(len(labels), rowIndex))
	case surveydoc.ElementMatrix:
		rows, columns := matrixLabels(e)
		out := map[string]any{}
		for i, row := range rows {
			out[row] = columns[lumpyIndex(len(columns), rowIndex+i)]
		}
		return out
	case surveydoc.ElementAddress:
		return fixtureAnswer(addressFields(&e.Element), addressFixtures[rowIndex%len(addressFixtures)])
	case surveydoc.ElementContactInfo:
		return fixtureAnswer(contactInfoFields(&e.Element), contactInfoFixtures[rowIndex%len(contactInfoFixtures)])
	case surveydoc.ElementPictureSelection:
		ids := pictureIDs(e)
		first := lumpyIndex(len(ids), rowIndex)
		if !e.AllowMulti || len(ids) == 1 {
			return []string{ids[first]}
		}
		picks := []string{ids[first]}
		if rowIndex%3 != 0 {
			picks = append(picks, ids[(first+1)%len(ids)])
		}
		return uniqueStrings(picks)
	case surveydoc.ElementConsent:
		return "accepted"
	}
	return nil
}

func ttcFor(e *exampleElement) float64 {
	band, ok := exampleTTCBands[e.Type]
	if !ok {
		band = exampleDefaultTTCBand
	}
	return float64(randomBetween(band[0], band[1]))
}

// pickLanguage is forma2's pickResponseLanguage: an enabled non-default language about 30% of the time, else the default's code, as a real respondent's answer carries it; nil for a survey without languages.
func pickLanguage(s *exampleSurvey) *string {
	if len(s.OtherLanguages) == 0 || randomInt(10) >= 3 {
		return s.DefaultLanguage
	}
	code := pick(s.OtherLanguages)
	return &code
}

// pickCreatedAt spreads the responses over the last ten days, weighted toward recent ones so the responses-over-time chart trends upward.
func pickCreatedAt(now time.Time) time.Time {
	daysAgo := math.Pow(randomFloat(), 1.6) * exampleResponseSpreadDays
	return now.Add(-time.Duration(daysAgo * float64(24*time.Hour)))
}

// planRows is forma2's buildExampleResponsePlan: every closed answer decided, the open texts left to the model, the first fifth dropped off part-way.
func planRows(s *exampleSurvey, now time.Time) []*exampleRow {
	supported := s.supported()
	if len(supported) == 0 {
		return nil
	}
	ordered := make([]string, len(supported))
	for i, e := range supported {
		ordered[i] = e.ID
	}
	var finishedEnding *string
	if len(s.Endings) > 0 {
		id := s.Endings[0].ID
		finishedEnding = &id
	}
	rows := make([]*exampleRow, exampleResponseCount)
	for i := range rows {
		row := &exampleRow{
			RowID: fmt.Sprintf("row_%d", i), Index: i, Profile: exampleProfiles[i%len(exampleProfiles)], Data: map[string]any{}, TTC: map[string]float64{},
			Finished: i >= int(math.Ceil(exampleResponseCount*exampleDropOffRate)), Language: pickLanguage(s),
			Meta:      map[string]any{"source": exampleMetaSource, "userAgent": map[string]any{"browser": pick(exampleBrowsers), "device": pick(exampleDevices), "os": pick(exampleOS)}, "country": pick(exampleCountries)},
			CreatedAt: pickCreatedAt(now),
		}
		if row.Finished {
			row.EndingID = finishedEnding
		}
		for _, e := range supported {
			if skipOptional(e, i) {
				continue
			}
			if e.Type == surveydoc.ElementOpenText {
				row.OpenTextIDs = append(row.OpenTextIDs, e.ID)
				row.TTC[e.ID] = ttcFor(e)
				continue
			}
			answer := closedAnswer(e, row.Profile, i, now)
			if answer == nil {
				continue
			}
			row.set(e.ID, answer)
			row.TTC[e.ID] = ttcFor(e)
		}
		dropOff(row, ordered)
		rows[i] = row
	}
	return rows
}

// dropOff is forma2's applyDropOffToPlanRow: an unfinished row keeps only the answers before the element it abandoned at.
func dropOff(row *exampleRow, ordered []string) {
	if len(ordered) <= 1 || row.Finished {
		return
	}
	kept := ordered[:1+row.Index%(len(ordered)-1)]
	var keys []string
	for _, id := range row.Keys {
		if contains(kept, id) {
			keys = append(keys, id)
		} else {
			delete(row.Data, id)
		}
	}
	row.Keys = keys
	for id := range row.TTC {
		if !contains(kept, id) {
			delete(row.TTC, id)
		}
	}
	var open []string
	for _, id := range row.OpenTextIDs {
		if contains(kept, id) {
			open = append(open, id)
		}
	}
	row.OpenTextIDs = open
}

// chunkRows is forma2's chunkOpenTextRows: rows in order, at most twenty requested answers per model call unless one row alone asks for more.
func chunkRows(rows []*exampleRow) [][]*exampleRow {
	var chunks [][]*exampleRow
	var current []*exampleRow
	requested := 0
	for _, row := range rows {
		if len(current) > 0 && requested+len(row.OpenTextIDs) > exampleOpenTextMaxAnswersPerChunk {
			chunks = append(chunks, current)
			current, requested = nil, 0
		}
		current = append(current, row)
		requested += len(row.OpenTextIDs)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// The prompt's context, forma2's buildOpenTextLlmContext, in its key order.
type promptElement struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Headline  string   `json:"headline"`
	Subheader string   `json:"subheader,omitempty"`
	Required  bool     `json:"required"`
	Choices   []string `json:"choices,omitempty"`
	Scale     *string  `json:"scale,omitempty"`
	Range     *float64 `json:"range,omitempty"`
	Rows      []string `json:"rows,omitempty"`
	Columns   []string `json:"columns,omitempty"`
}

type promptQuestion struct {
	ElementID string `json:"elementId"`
	Question  string `json:"question"`
	Subheader string `json:"subheader,omitempty"`
}

type promptAnswer struct {
	ElementID string  `json:"elementId"`
	Question  string  `json:"question"`
	Type      *string `json:"type,omitempty"`
	Answer    any     `json:"answer"`
}

type promptRow struct {
	RowID                    string           `json:"rowId"`
	Profile                  exampleProfile   `json:"profile"`
	RequestedOpenTextAnswers []promptQuestion `json:"requestedOpenTextAnswers"`
	PlannedAnswers           []promptAnswer   `json:"plannedAnswers"`
	Finished                 bool             `json:"finished"`
}

type promptContext struct {
	SurveyTitle       string          `json:"surveyTitle"`
	SurveyDescription string          `json:"surveyDescription,omitempty"`
	SurveyElements    []promptElement `json:"surveyElements"`
	OpenTextElements  []promptElement `json:"openTextElements"`
	Rows              []promptRow     `json:"rows"`
}

func elementForPrompt(e *exampleElement) promptElement {
	out := promptElement{ID: e.ID, Type: e.Type, Headline: plainHeadline(e), Subheader: plainSubheader(e), Required: e.Required}
	switch e.Type {
	case surveydoc.ElementMultipleChoiceSingle, surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementRanking:
		out.Choices = choiceLabels(e)
	case surveydoc.ElementRating, surveydoc.ElementCES, surveydoc.ElementCSAT:
		out.Scale, out.Range = e.Scale, e.Range
	case surveydoc.ElementMatrix:
		out.Rows, out.Columns = matrixLabels(e)
	}
	return out
}

func buildPromptContext(s *exampleSurvey, rows []*exampleRow) promptContext {
	supported := s.supported()
	requested := map[string]bool{}
	for _, row := range rows {
		for _, id := range row.OpenTextIDs {
			requested[id] = true
		}
	}
	ctx := promptContext{SurveyTitle: s.Name, SurveyElements: []promptElement{}, OpenTextElements: []promptElement{}, Rows: []promptRow{}}
	if h := localized(s.WelcomeCard.Headline); h != "" {
		ctx.SurveyDescription = stripHTML(h)
	}
	byID := map[string]*exampleElement{}
	for _, e := range supported {
		byID[e.ID] = e
		ctx.SurveyElements = append(ctx.SurveyElements, elementForPrompt(e))
		if requested[e.ID] {
			ctx.OpenTextElements = append(ctx.OpenTextElements, elementForPrompt(e))
		}
	}
	for _, row := range rows {
		pr := promptRow{RowID: row.RowID, Profile: row.Profile, RequestedOpenTextAnswers: []promptQuestion{}, PlannedAnswers: []promptAnswer{}, Finished: row.Finished}
		for _, id := range row.OpenTextIDs {
			pr.RequestedOpenTextAnswers = append(pr.RequestedOpenTextAnswers, promptQuestion{ElementID: id, Question: plainHeadline(byID[id]), Subheader: plainSubheader(byID[id])})
		}
		for _, id := range row.Keys {
			a := promptAnswer{ElementID: id, Question: plainHeadline(byID[id]), Answer: row.Data[id]}
			if e := byID[id]; e != nil {
				a.Type = &e.Type
			}
			pr.PlannedAnswers = append(pr.PlannedAnswers, a)
		}
		ctx.Rows = append(ctx.Rows, pr)
	}
	return ctx
}

// stringifyPrompt is JSON.stringify(value, null, 2): two-space indentation and no HTML escaping.
func stringifyPrompt(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

const exampleSystemPrompt = `You are simulating real survey respondents. For each row, write a short, on-topic answer to each requested open-text question.

Hard requirements:
- Output one object per rowId provided.
- For every elementId listed in that row's requestedOpenTextAnswers, return a non-empty string answer. Never omit a requested elementId. Never invent new elementIds.
- Each answer must directly address that specific question's headline (e.g. a "what is holding you back" question needs a reason or hesitation; a "primary reason for visiting" question needs a motive).
- Match the language of the question headline.

Style:
- Write like a different real person on each row. Vary sentence length and openings.
- Use the row's profile (sentiment, verbosity, polish) to shape tone, but stay on the survey's actual topic — do not paste the profile's priorities into the answer if they don't fit the question.
- Two answers within the same row must not be byte-identical and should not share the same opening clause.
- Sound human: contractions are fine, the occasional lowercase start is fine, no corporate buzzwords.

Example output shape:
{ "responses": [
  { "rowId": "row_0", "answers": [
    { "elementId": "<elementId1>", "answer": "..." },
    { "elementId": "<elementId2>", "answer": "..." }
  ] },
  ...
] }`

const examplePromptIntro = `Write one short answer for each requestedOpenTextAnswers entry in every row below. Every requested elementId must map to a non-empty string in your output. Stay grounded in the actual question headline for each elementId.

Survey context (JSON):
`

// exampleAnswersSchema is forma2's buildOpenTextResponsesSchema: one entry per row, each with its element ids and non-empty answers.
var exampleAnswersSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["responses"],"properties":{"responses":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["rowId","answers"],"properties":{"rowId":{"type":"string","minLength":1},"answers":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["elementId","answer"],"properties":{"elementId":{"type":"string","minLength":1},"answer":{"type":"string","minLength":1}}}}}}}}}`)

type exampleAnswers struct {
	Responses []struct {
		RowID   string `json:"rowId"`
		Answers []struct {
			ElementID string `json:"elementId"`
			Answer    string `json:"answer"`
		} `json:"answers"`
	} `json:"responses"`
}

// fillOpenText is forma2's fillOpenTextAnswers: the model answers the open texts chunk by chunk; a chunk that fails, and any answer it leaves out, falls back to a canned answer for the profile, and no response repeats an answer.
func (x *ExampleResponses) fillOpenText(ctx context.Context, s *exampleSurvey, organizationID uuid.UUID, rows []*exampleRow) {
	var needing []*exampleRow
	for _, row := range rows {
		if len(row.OpenTextIDs) > 0 {
			needing = append(needing, row)
		}
	}
	if len(needing) == 0 {
		return
	}
	answers := map[string]map[string]string{}
	temperature := 0.0
	for _, chunk := range chunkRows(needing) {
		prompt, err := stringifyPrompt(buildPromptContext(s, chunk))
		if err != nil {
			x.log.ErrorContext(ctx, exampleOpenTextGenerationFailureLog, "organizationId", organizationID.String(), "err", err)
			continue
		}
		out, err := x.svc.GenerateObject(ctx, organizationID, FeatureExampleResponses, ObjectRequest{
			Request: Request{System: exampleSystemPrompt, Prompt: examplePromptIntro + prompt, Temperature: &temperature, MaxOutputTokens: exampleOpenTextMaxOutputTokens, Timeout: exampleOpenTextTimeout},
			Schema:  exampleAnswersSchema,
		})
		if err != nil {
			x.log.ErrorContext(ctx, exampleOpenTextGenerationFailureLog, "organizationId", organizationID.String(), "err", err)
			continue
		}
		var parsed exampleAnswers
		if err := json.Unmarshal(out.Object, &parsed); err != nil {
			x.log.ErrorContext(ctx, exampleOpenTextGenerationFailureLog, "organizationId", organizationID.String(), "err", err)
			continue
		}
		for _, r := range parsed.Responses {
			byElement := map[string]string{}
			for _, a := range r.Answers {
				byElement[a.ElementID] = a.Answer
			}
			answers[r.RowID] = byElement
		}
	}
	for _, row := range rows {
		var seen []string
		for _, id := range row.OpenTextIDs {
			answer := answers[row.RowID][id]
			if answer == "" {
				answer = fallbackAnswer(row.Profile, s.element(id), 0)
			}
			for offset := 1; contains(seen, answer) && offset <= 4; offset++ {
				answer = fallbackAnswer(row.Profile, s.element(id), offset)
			}
			seen = append(seen, answer)
			row.set(id, answer)
		}
	}
}

var fallbackAnswers = map[string]map[string][]string{
	"promoter": {
		"brief":  {"Loved it.", "All good here.", "Honestly a yes.", "No complaints."},
		"normal": {"Pretty close to what I was hoping for.", "Glad I came across this today.", "Lining up with what I had in mind.", "Hits the spot for me right now."},
		"detailed": {
			"Genuinely happy with how this is going. Nothing major I would change today.",
			"This is one of the better ones I have come across — would happily come back.",
			"Felt easy and straightforward, which is what I was after.",
			"Met my expectations and then some, no real reservations.",
		},
	},
	"positive": {
		"brief":  {"Looks good.", "Pretty solid.", "Working so far.", "Glad I checked."},
		"normal": {"Mostly what I was looking for, with a couple of small things.", "Liking it so far, just want to see a bit more.", "Generally fits, I just need a moment to decide.", "Seems promising, leaning yes."},
		"detailed": {
			"Overall a good experience — it covers most of what I was after, even if a couple of small things could be smoother.",
			"Feels close to what I need. A few minor questions left, but nothing that would stop me.",
			"Mostly positive impression; I would just want a bit more clarity on one or two details.",
			"Largely lines up with what I was hoping for, with a couple of small caveats I am working through.",
		},
	},
	"neutral": {
		"brief":  {"It's fine.", "Hard to say yet.", "Need more info.", "Open to it."},
		"normal": {"Comparing a few options before I decide.", "Mostly seems fine, a couple of things on my mind.", "Open to it, just thinking it through.", "Probably need a bit more time before I commit."},
		"detailed": {
			"Honestly still weighing this up — some parts work for me, others I want to think about more.",
			"On the fence right now. Not negative on it, just not ready to commit either.",
			"Reasonable so far, but I want to compare against a couple of alternatives before deciding.",
			"Mixed feeling at the moment. Nothing wrong exactly, just nothing that pushes me one way or the other.",
		},
	},
	"skeptical": {
		"brief":  {"Not sure.", "Hesitant.", "Some concerns.", "Probably not today."},
		"normal": {"A few things make me hesitant before going further.", "Not quite convinced yet, leaning cautious.", "I have some concerns I would want answered first.", "Want to do a bit more digging before I move on it."},
		"detailed": {
			"Honestly, there are a couple of things that give me pause. I would not say no, but I am not ready to commit either.",
			"Some parts work for me and others raise questions. Probably going to hold off for now.",
			"I want to like this, but a few rough edges make me want to wait and see.",
			"Mostly hesitant. Nothing dealbreaking, just enough small things that I am not moving forward today.",
		},
	},
	"detractor": {
		"brief":  {"Not for me.", "Doesn't fit.", "Probably no.", "Missing the mark."},
		"normal": {"Doesn't quite fit what I am looking for right now.", "Not feeling like it lines up with what I need.", "Leaning no — not seeing enough of a reason yet.", "Not really speaking to what I am here for today."},
		"detailed": {
			"Honestly this doesn't quite land for me. A few things felt off and I am not getting the value I was hoping for.",
			"Not really seeing the fit. The experience didn't match what I was expecting and I would probably look elsewhere.",
			"I came in optimistic but a few specific things made me reconsider. Probably not the right thing for me today.",
			"Not connecting with this one. The pieces I cared about most didn't quite show up the way I needed them to.",
		},
	},
}

func withFirst(value string, f func(rune) rune) string {
	r, size := utf8.DecodeRuneInString(value)
	if size == 0 {
		return value
	}
	return string(f(r)) + value[size:]
}

// fallbackStyle is forma2's applyFallbackStyle: closing punctuation, and a lowercase start for a rough or briefly casual respondent.
func fallbackStyle(answer string, p exampleProfile) string {
	trimmed := strings.TrimSpace(answer)
	if !strings.HasSuffix(trimmed, ".") && !strings.HasSuffix(trimmed, "!") && !strings.HasSuffix(trimmed, "?") {
		trimmed += "."
	}
	if p.Polish == "rough" || (p.Polish == "casual" && p.Verbosity == "brief") {
		return withFirst(trimmed, unicode.ToLower)
	}
	return withFirst(trimmed, unicode.ToUpper)
}

// fallbackAnswer is forma2's buildFallbackOpenTextAnswer: a product-feedback answer tinted by the profile's priorities when the question reads like one, else a survey-agnostic answer for the profile's sentiment and verbosity.
func fallbackAnswer(p exampleProfile, e *exampleElement, offset int) string {
	headline := strings.ToLower(plainHeadline(e))
	variant := (hashString(p.Persona+"-"+headline) + offset) % 4
	first := p.Priorities[0]
	switch {
	case strings.Contains(headline, "who") || strings.Contains(headline, "people") || strings.Contains(headline, "benefit from"):
		answers := []string{
			fmt.Sprintf("Teams like %ss, especially when %s matters.", p.Persona, first),
			fmt.Sprintf("Best fit is probably %ss who care about %s.", p.Persona, first),
			fmt.Sprintf("%ss would get the most out of it.", p.Persona),
			fmt.Sprintf("Mostly %ss with a lot of pressure around %s.", p.Persona, first),
		}
		return fallbackStyle(answers[variant], p)
	case strings.Contains(headline, "benefit"):
		answers := []string{
			fmt.Sprintf("Better %s without much extra setup.", first),
			fmt.Sprintf("It saves time around %s, which is what I notice first.", first),
			fmt.Sprintf("Clearer %s for the team.", first),
			fmt.Sprintf("%s feels easier to stay on top of now.", first),
		}
		answer := answers[variant]
		if p.Verbosity == "brief" {
			answer, _, _ = strings.Cut(answer, ",")
		}
		return fallbackStyle(answer, p)
	case strings.Contains(headline, "improve") || strings.Contains(headline, "better"):
		second := first
		if len(p.Priorities) > 1 {
			second = p.Priorities[1]
		}
		answers := []string{
			fmt.Sprintf("Make %s easier to configure.", second),
			fmt.Sprintf("A bit more guidance around %s would help.", second),
			fmt.Sprintf("Tighten up %s; that is where I still slow down.", second),
			fmt.Sprintf("%s could be clearer for first-time users.", second),
		}
		return fallbackStyle(answers[variant], p)
	}
	return fallbackStyle(fallbackAnswers[p.Sentiment][p.Verbosity][variant], p)
}

// ExampleResponsesResult is what a generation wrote.
type ExampleResponsesResult struct {
	CreatedCount int `json:"createdCount"`
}

// ExampleResponses is forma2's generateExampleResponsesAction: LLM-authored example responses for a survey that has none yet, so its summary can be explored before it is shared.
type ExampleResponses struct {
	svc     *Service
	db      *gorm.DB
	quotas  QuotaScreener
	limiter *ratelimit.Limiter
	audit   *audit.Emitter
	log     *slog.Logger
	now     func() time.Time
}

// QuotaScreener screens a response just written into its survey's quotas, inside the transaction that wrote it: response.Service's quota evaluation, an interface because the response package imports this one.
type QuotaScreener interface {
	ScreenQuotas(ctx context.Context, tx *gorm.DB, surveyID, responseID uuid.UUID) error
}

func NewExampleResponses(svc *Service, db *gorm.DB, quotas QuotaScreener, limiter *ratelimit.Limiter, emitter *audit.Emitter, log *slog.Logger) *ExampleResponses {
	return &ExampleResponses{svc: svc, db: db, quotas: quotas, limiter: limiter, audit: emitter, log: log, now: time.Now}
}

// Generate is forma2's generateExampleResponsesAction: one generation per user per minute (which also closes the double-click race on the empty-survey check), workspace.write on the survey's workspace, AI available to the organization, a survey neither archived nor already answered, and one all-or-nothing write. Every attempt is audited as an update of the survey, a refused or failed one as a failure.
func (x *ExampleResponses) Generate(ctx context.Context, userID, surveyID uuid.UUID) (*ExampleResponsesResult, error) {
	var organizationID uuid.UUID
	out, err := x.generate(ctx, userID, surveyID, &organizationID)
	e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetSurvey, TargetID: surveyID.String(), ActorType: audit.ActorUser, Status: audit.StatusSuccess}
	if organizationID != uuid.Nil {
		e.OrganizationID = organizationID.String()
	}
	if err != nil {
		e.Status = audit.StatusFailure
	} else {
		e.NewObject = out
	}
	x.audit.Log(ctx, audit.FromRequest(ctx, e))
	return out, err
}

func (x *ExampleResponses) generate(ctx context.Context, userID, surveyID uuid.UUID, organizationID *uuid.UUID) (*ExampleResponsesResult, error) {
	if err := x.limiter.Apply(ctx, ratelimit.ActionGenerateExampleResponses, userID.String()); err != nil {
		return nil, err
	}
	owner, err := x.svc.repo.surveyOwner(ctx, surveyID)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, apperr.NotFound("Survey", surveyID.String())
	}
	if err := x.svc.authz.Require(ctx, authz.User(userID), authz.WorkspaceWrite, authz.Workspace(owner.WorkspaceID)); err != nil {
		return nil, err
	}
	*organizationID = owner.OrganizationID
	if _, err := x.svc.Assert(ctx, owner.OrganizationID); err != nil {
		return nil, err
	}
	s, err := loadExampleSurvey(ctx, x.db, surveyID)
	if err != nil {
		return nil, err
	}
	if s.Archived {
		return nil, apperr.OperationNotAllowed(exampleArchivedSurveyMessage)
	}
	var answered int64
	if err := x.db.WithContext(ctx).Raw(`SELECT count(*) FROM responses WHERE survey_id = ?`, surveyID).Row().Scan(&answered); err != nil {
		return nil, fmt.Errorf("count survey responses: %w", err)
	}
	if answered > 0 {
		return nil, apperr.OperationNotAllowed(exampleSurveyHasResponsesMessage)
	}
	now := x.now()
	rows := planRows(s, now)
	if len(rows) == 0 {
		return nil, apperr.InvalidInput(apperr.CodeBadRequest, exampleNoSupportedElementsMessage)
	}
	x.fillOpenText(ctx, s, owner.OrganizationID, rows)
	impressions := make([]time.Time, exampleImpressionOnlyCount)
	for i := range impressions {
		impressions[i] = pickCreatedAt(now)
	}
	if err := persistExampleDataset(ctx, x.db, x.quotas, s, rows, impressions); err != nil {
		return nil, err
	}
	return &ExampleResponsesResult{CreatedCount: len(rows)}, nil
}

// loadExampleSurvey reads the survey document and its languages; a survey deleted meanwhile is not found.
func loadExampleSurvey(ctx context.Context, db *gorm.DB, surveyID uuid.UUID) (*exampleSurvey, error) {
	var rows []struct {
		WorkspaceID uuid.UUID
		Name        string
		ArchivedAt  *time.Time
		WelcomeCard []byte
		Blocks      []byte
		Endings     []byte
	}
	if err := db.WithContext(ctx).Raw(`SELECT workspace_id, name, archived_at, welcome_card, blocks, endings FROM surveys WHERE id = ?`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, apperr.NotFound("Survey", surveyID.String())
	}
	r := rows[0]
	s := &exampleSurvey{ID: surveyID, WorkspaceID: r.WorkspaceID, Name: r.Name, Archived: r.ArchivedAt != nil}
	if err := json.Unmarshal(r.WelcomeCard, &s.WelcomeCard); err != nil {
		return nil, fmt.Errorf("decode survey welcome card: %w", err)
	}
	if err := json.Unmarshal(r.Endings, &s.Endings); err != nil {
		return nil, fmt.Errorf("decode survey endings: %w", err)
	}
	var blocks []surveydoc.Block
	if err := json.Unmarshal(r.Blocks, &blocks); err != nil {
		return nil, fmt.Errorf("decode survey blocks: %w", err)
	}
	var multi []struct {
		Elements []struct {
			ID         string `json:"id"`
			AllowMulti bool   `json:"allowMulti"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(r.Blocks, &multi); err != nil {
		return nil, fmt.Errorf("decode survey blocks: %w", err)
	}
	for bi, b := range blocks {
		for ei := range b.Elements {
			e := &exampleElement{Element: b.Elements[ei], AllowMulti: multi[bi].Elements[ei].AllowMulti}
			if existing := s.element(e.ID); existing != nil {
				*existing = *e
				continue
			}
			s.Elements = append(s.Elements, e)
		}
	}
	var languages []struct {
		Code      string
		IsDefault bool
		Enabled   bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT l.code, sl.is_default, sl.enabled FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id = ? ORDER BY l.code`, surveyID).Scan(&languages).Error; err != nil {
		return nil, fmt.Errorf("load survey languages: %w", err)
	}
	for _, l := range languages {
		switch {
		case l.IsDefault && s.DefaultLanguage == nil:
			code := l.Code
			s.DefaultLanguage = &code
		case !l.IsDefault && l.Enabled:
			s.OtherLanguages = append(s.OtherLanguages, l.Code)
		}
	}
	return s, nil
}

// ttcWithTotal is a finished response's times with their sum under _total, as the response creation path stores them.
func ttcWithTotal(ttc map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(ttc)+1)
	total := 0.0
	for k, v := range ttc {
		out[k] = v
		total += v
	}
	out["_total"] = total
	return out
}

var errExampleTagMissing = errors.New("example response tag was not written")

// persistExampleDataset is forma2's persistExampleResponseDataset: one transaction writes the tag (kept when it already exists), a backdated display and response per row, screened into the survey's quotas as forma2's createResponseWithQuotaEvaluation did and with the tag on it, and the displays nobody answered, so a failure part-way leaves nothing behind and the survey stays eligible for another attempt. Generated responses are seeded rather than submitted: no pipeline event is queued for them, as in forma2.
func persistExampleDataset(ctx context.Context, db *gorm.DB, quotas QuotaScreener, s *exampleSurvey, rows []*exampleRow, impressions []time.Time) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tagIDs []uuid.UUID
		if err := tx.Raw(`INSERT INTO tags (workspace_id, name) VALUES (?, ?) ON CONFLICT (workspace_id, name) DO UPDATE SET name = tags.name RETURNING id`, s.WorkspaceID, ExampleAIGeneratedTagName).Scan(&tagIDs).Error; err != nil {
			return fmt.Errorf("upsert example response tag: %w", err)
		}
		if len(tagIDs) == 0 {
			return errExampleTagMissing
		}
		for _, row := range rows {
			var displayID uuid.UUID
			if err := tx.Raw(`INSERT INTO displays (survey_id, created_at) VALUES (?, ?) RETURNING id`, s.ID, row.CreatedAt).Row().Scan(&displayID); err != nil {
				return fmt.Errorf("create example display: %w", err)
			}
			ttc := row.TTC
			if row.Finished {
				ttc = ttcWithTotal(ttc)
			}
			data, err := json.Marshal(row.Data)
			if err != nil {
				return fmt.Errorf("encode example response data: %w", err)
			}
			ttcJSON, err := json.Marshal(ttc)
			if err != nil {
				return fmt.Errorf("encode example response ttc: %w", err)
			}
			meta, err := json.Marshal(row.Meta)
			if err != nil {
				return fmt.Errorf("encode example response meta: %w", err)
			}
			var responseID uuid.UUID
			if err := tx.Raw(`INSERT INTO responses (survey_id, display_id, finished, ending_id, data, ttc, meta, language, created_at) VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?, ?) RETURNING id`,
				s.ID, displayID, row.Finished, row.EndingID, string(data), string(ttcJSON), string(meta), row.Language, row.CreatedAt).Row().Scan(&responseID); err != nil {
				return fmt.Errorf("create example response: %w", err)
			}
			if err := quotas.ScreenQuotas(ctx, tx, s.ID, responseID); err != nil {
				return fmt.Errorf("screen example response: %w", err)
			}
			if err := tx.Exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, responseID, tagIDs[0]).Error; err != nil {
				return fmt.Errorf("tag example response: %w", err)
			}
		}
		for _, at := range impressions {
			if err := tx.Exec(`INSERT INTO displays (survey_id, created_at) VALUES (?, ?)`, s.ID, at).Error; err != nil {
				return fmt.Errorf("create example impression: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist example responses: %w", err)
	}
	return nil
}
