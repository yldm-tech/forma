package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/authz"
	"github.com/yldm-tech/forma/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/survey/surveydoc"
)

// valuesLimit is forma2's VALUES_LIMIT: how many samples a text-like element or an Other option lists. Counts are never truncated.
const valuesLimit = 50

// summaryBatchSize is how many responses one read of the summary takes, forma2's batchSize.
const summaryBatchSize = 5000

// Summary is forma2's TSurveySummary.
type Summary struct {
	Meta    Meta            `json:"meta"`
	DropOff []DropOff       `json:"dropOff"`
	Summary []any           `json:"summary"`
	Quotas  []QuotaProgress `json:"quotas"`
}

// Meta is the summary's headline numbers.
type Meta struct {
	DisplayCount              int     `json:"displayCount"`
	TotalResponses            int     `json:"totalResponses"`
	StartsPercentage          float64 `json:"startsPercentage"`
	CompletedResponses        int     `json:"completedResponses"`
	CompletedPercentage       float64 `json:"completedPercentage"`
	DropOffCount              int     `json:"dropOffCount"`
	DropOffPercentage         float64 `json:"dropOffPercentage"`
	TTCAverage                float64 `json:"ttcAverage"`
	QuotasCompleted           int     `json:"quotasCompleted"`
	QuotasCompletedPercentage float64 `json:"quotasCompletedPercentage"`
}

// DropOff is one element's row of the drop-off table.
type DropOff struct {
	ElementID         string  `json:"elementId"`
	ElementType       string  `json:"elementType"`
	Headline          string  `json:"headline"`
	TTC               float64 `json:"ttc"`
	Impressions       int     `json:"impressions"`
	DropOffCount      int     `json:"dropOffCount"`
	DropOffPercentage float64 `json:"dropOffPercentage"`
}

// QuotaProgress is forma2's getQuotasSummary entry: how many responses a quota screened in.
type QuotaProgress struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Limit      int       `json:"limit"`
	Count      int       `json:"count"`
	Percentage float64   `json:"percentage"`
}

// summaryResponse is a response as the summary reads it, forma2's TSurveySummaryResponse.
type summaryResponse struct {
	ID                uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Data              map[string]any
	TTC               map[string]any
	Contact           *response.Contact
	ContactAttributes json.RawMessage
	Language          *string
	Finished          bool
}

// Sample is one answer an element summary lists, with who gave it.
type Sample struct {
	ID                *uuid.UUID        `json:"id,omitempty"`
	UpdatedAt         time.Time         `json:"updatedAt"`
	Value             any               `json:"value"`
	Contact           *response.Contact `json:"contact"`
	ContactAttributes json.RawMessage   `json:"contactAttributes"`
}

func (r *summaryResponse) sample(value any) Sample {
	id := r.ID
	return Sample{ID: &id, UpdatedAt: r.UpdatedAt, Value: value, Contact: r.Contact, ContactAttributes: r.ContactAttributes}
}

// Other is a value given through a choice element's Other option.
type Other struct {
	Value             any               `json:"value"`
	Contact           *response.Contact `json:"contact"`
	ContactAttributes json.RawMessage   `json:"contactAttributes"`
}

// Count is a count with its share.
type Count struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// Summary entries, forma2's TSurveyElementSummary variants. Element is the element as the survey stores it.
type (
	SamplesSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Samples       []Sample        `json:"samples"`
	}
	FileUploadSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Files         []Sample        `json:"files"`
	}
	ChoiceCount struct {
		Value      string   `json:"value"`
		Count      int      `json:"count"`
		Percentage float64  `json:"percentage"`
		Others     *[]Other `json:"others,omitempty"`
	}
	MultipleChoiceSummary struct {
		Type           string          `json:"type"`
		Element        json.RawMessage `json:"element"`
		ResponseCount  int             `json:"responseCount"`
		SelectionCount int             `json:"selectionCount"`
		Choices        []ChoiceCount   `json:"choices"`
	}
	PictureCount struct {
		ID         string  `json:"id"`
		ImageURL   string  `json:"imageUrl,omitempty"`
		Count      int     `json:"count"`
		Percentage float64 `json:"percentage"`
	}
	PictureSelectionSummary struct {
		Type           string          `json:"type"`
		Element        json.RawMessage `json:"element"`
		ResponseCount  int             `json:"responseCount"`
		SelectionCount int             `json:"selectionCount"`
		Choices        []PictureCount  `json:"choices"`
	}
	RatingCount struct {
		Rating     Num `json:"rating"`
		Count      Num `json:"count"`
		Percentage Num `json:"percentage"`
	}
	DismissedCount struct {
		Count int `json:"count"`
	}
	Satisfaction struct {
		SatisfiedCount      int     `json:"satisfiedCount"`
		SatisfiedPercentage float64 `json:"satisfiedPercentage"`
	}
	ScaleSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		Average       Num             `json:"average"`
		ResponseCount int             `json:"responseCount"`
		Choices       []RatingCount   `json:"choices"`
		Dismissed     DismissedCount  `json:"dismissed"`
		CSAT          *Satisfaction   `json:"csat,omitempty"`
	}
	NPSSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Total         int             `json:"total"`
		Score         float64         `json:"score"`
		Promoters     Count           `json:"promoters"`
		Passives      Count           `json:"passives"`
		Detractors    Count           `json:"detractors"`
		Dismissed     Count           `json:"dismissed"`
		Choices       []RatingCount   `json:"choices"`
	}
	CTASummary struct {
		Type            string          `json:"type"`
		Element         json.RawMessage `json:"element"`
		ImpressionCount int             `json:"impressionCount"`
		ClickCount      int             `json:"clickCount"`
		SkipCount       int             `json:"skipCount"`
		ResponseCount   int             `json:"responseCount"`
		CTR             Count           `json:"ctr"`
	}
	ConsentSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Accepted      Count           `json:"accepted"`
		Dismissed     Count           `json:"dismissed"`
	}
	CalSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Booked        Count           `json:"booked"`
		Skipped       Count           `json:"skipped"`
	}
	ColumnPercentage struct {
		Column     string  `json:"column"`
		Percentage float64 `json:"percentage"`
	}
	MatrixRow struct {
		RowLabel             string             `json:"rowLabel"`
		ColumnPercentages    []ColumnPercentage `json:"columnPercentages"`
		TotalResponsesForRow int                `json:"totalResponsesForRow"`
	}
	MatrixSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Data          []MatrixRow     `json:"data"`
	}
	RankingCount struct {
		Value      string  `json:"value"`
		Count      int     `json:"count"`
		AvgRanking float64 `json:"avgRanking"`
	}
	RankingSummary struct {
		Type          string          `json:"type"`
		Element       json.RawMessage `json:"element"`
		ResponseCount int             `json:"responseCount"`
		Choices       []RankingCount  `json:"choices"`
	}
	HiddenFieldSummary struct {
		Type          string   `json:"type"`
		ID            string   `json:"id"`
		ResponseCount int      `json:"responseCount"`
		Samples       []Sample `json:"samples"`
	}
)

// Summary is forma2's getSurveySummaryAction: the survey's statistics over the responses matching the filter.
func (s *Service) Summary(ctx context.Context, userID, surveyID uuid.UUID, filter *response.Criteria) (*Summary, error) {
	sv, err := s.readableSurvey(ctx, userID, surveyID, authz.SurveyResponseRead)
	if err != nil {
		return nil, err
	}
	clause, err := response.Where(sv.Survey, filter)
	if err != nil {
		return nil, err
	}
	agg := newSummarizer(sv)
	if err := readSummaryResponses(ctx, s.db, surveyID, clause, agg.add); err != nil {
		return nil, err
	}
	displays, err := displayCount(ctx, s.db, surveyID, filter, clause)
	if err != nil {
		return nil, err
	}
	quotas, err := quotaProgress(ctx, s.db, surveyID)
	if err != nil {
		return nil, err
	}
	return agg.result(displays, quotas), nil
}

// readSummaryResponses reads the matching responses newest first in batches, resuming each batch after the last row of the previous one, and hands each to add.
func readSummaryResponses(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, clause response.Clause, add func(*summaryResponse)) error {
	var cursor *response.Cursor
	for {
		args := append([]any{surveyID}, clause.Args...)
		where := "r.survey_id = ? AND " + clause.SQL
		if cursor != nil {
			where += " AND (r.created_at < ? OR (r.created_at = ? AND r.id < ?))"
			args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
		}
		var rows []struct {
			ID                uuid.UUID
			CreatedAt         time.Time
			UpdatedAt         time.Time
			Data              []byte
			TTC               []byte
			ContactID         *uuid.UUID
			ContactUserID     *string
			ContactAttributes []byte
			Language          *string
			Finished          bool
		}
		err := db.WithContext(ctx).Raw(`SELECT r.id, r.created_at, r.updated_at, r.data, r.ttc, r.contact_id,
			(SELECT a.value_text FROM contact_attributes a JOIN contact_attribute_keys k ON k.id = a.attribute_key_id
				WHERE a.contact_id = r.contact_id AND k.key = 'userId') AS contact_user_id,
			r.contact_attributes, r.language, r.finished
			FROM responses r WHERE `+where+` ORDER BY r.created_at DESC, r.id DESC LIMIT ?`, append(args, summaryBatchSize)...).Scan(&rows).Error
		if err != nil {
			return fmt.Errorf("read responses for summary: %w", err)
		}
		for _, row := range rows {
			r := &summaryResponse{ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Language: row.Language, Finished: row.Finished, ContactAttributes: json.RawMessage(`{}`)}
			if err := json.Unmarshal(row.Data, &r.Data); err != nil {
				return fmt.Errorf("decode data of response %s: %w", row.ID, err)
			}
			if err := json.Unmarshal(row.TTC, &r.TTC); err != nil {
				return fmt.Errorf("decode ttc of response %s: %w", row.ID, err)
			}
			if row.ContactID != nil {
				r.Contact = &response.Contact{ID: *row.ContactID, UserID: row.ContactUserID}
			}
			if row.ContactAttributes != nil && string(row.ContactAttributes) != "null" {
				r.ContactAttributes = row.ContactAttributes
			}
			add(r)
		}
		if len(rows) < summaryBatchSize {
			return nil
		}
		last := rows[len(rows)-1]
		cursor = &response.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

// hasResponseFilter is forma2's hasFilter: the criteria name something other than the creation time, so only displays whose response matches count.
func hasResponseFilter(f *response.Criteria) bool {
	return f != nil && (f.Finished != nil || f.ResponseIDs != nil || f.ContactAttributes != nil || f.Data != nil || f.Variables != nil ||
		f.Reserved != nil || f.Tags != nil || f.Others != nil || f.Meta != nil || f.Quotas != nil)
}

// displayCount is forma2's getDisplayCountBySurveyId as the summary calls it: the survey's displays in the filter's creation window, and under any other filter only those linked to a matching response.
func displayCount(ctx context.Context, db *gorm.DB, surveyID uuid.UUID, filter *response.Criteria, clause response.Clause) (int, error) {
	where := "d.survey_id = ?"
	args := []any{surveyID}
	if filter != nil && filter.CreatedAt != nil {
		if filter.CreatedAt.Min != nil {
			where += " AND d.created_at >= ?"
			args = append(args, *filter.CreatedAt.Min)
		}
		if filter.CreatedAt.Max != nil {
			where += " AND d.created_at <= ?"
			args = append(args, *filter.CreatedAt.Max)
		}
	}
	if hasResponseFilter(filter) {
		where += " AND EXISTS (SELECT 1 FROM responses r WHERE r.display_id = d.id AND " + clause.SQL + ")"
		args = append(args, clause.Args...)
	}
	var n int64
	if err := db.WithContext(ctx).Raw("SELECT count(*) FROM displays d WHERE "+where, args...).Scan(&n).Error; err != nil {
		return 0, fmt.Errorf("count displays: %w", err)
	}
	return int(n), nil
}

// quotaProgress is forma2's getQuotasSummary: every quota of the survey, newest first, with the responses it screened in, regardless of the filter.
func quotaProgress(ctx context.Context, db *gorm.DB, surveyID uuid.UUID) ([]QuotaProgress, error) {
	var rows []QuotaProgress
	if err := db.WithContext(ctx).Raw(`SELECT q.id, q.name, q."limit",
		(SELECT count(*) FROM response_quota_links l WHERE l.quota_id = q.id AND l.status = 'screenedIn') AS count
		FROM survey_quotas q WHERE q.survey_id = ? ORDER BY q.created_at DESC, q.id DESC`, surveyID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load quota progress: %w", err)
	}
	for i := range rows {
		rows[i].Percentage = percent(float64(rows[i].Count), float64(rows[i].Limit))
	}
	if rows == nil {
		rows = []QuotaProgress{}
	}
	return rows, nil
}

// elementAggregate accumulates one element's statistics over the responses, in the order they are read.
type elementAggregate interface {
	add(r *summaryResponse)
	// result is the element's summary entry; ok is false for an element the summary leaves out.
	result(dropOff []DropOff) (entry any, ok bool)
}

// summarizer computes forma2's getSurveySummaryMeta, getSurveySummaryDropOff and getElementSummary in one pass over the responses, keeping only the samples it reports.
type summarizer struct {
	sv *surveyDoc

	total, completed int
	ttcSum           float64
	ttcCount         int

	blockTotal  map[string]float64
	blockCount  map[string]int
	impressions []int
	dropOffs    []int

	elements []elementAggregate
	hidden   []*hiddenAggregate
}

func newSummarizer(sv *surveyDoc) *summarizer {
	a := &summarizer{sv: sv, impressions: make([]int, len(sv.Elements)), dropOffs: make([]int, len(sv.Elements)),
		blockTotal: map[string]float64{}, blockCount: map[string]int{}}
	for i := range sv.Elements {
		a.elements = append(a.elements, newElementAggregate(sv, i))
	}
	for _, f := range embeddeddata.OfSource(sv.Fields, embeddeddata.SourceIngested) {
		a.hidden = append(a.hidden, &hiddenAggregate{key: f.StorageKey, samples: []Sample{}})
	}
	return a
}

// blockTimes is forma2's getBlockTimesForResponse: the longest time the response spent on any element of each block.
func (a *summarizer) blockTimes(r *summaryResponse) map[string]float64 {
	times := make(map[string]float64, len(a.sv.BlockIDs))
	for _, id := range a.sv.BlockIDs {
		times[id] = 0
	}
	for _, el := range a.sv.Elements {
		if t, ok := r.TTC[el.ID].(float64); ok {
			times[el.BlockID] = math.Max(times[el.BlockID], t)
		}
	}
	return times
}

func (a *summarizer) add(r *summaryResponse) {
	a.total++
	if r.Finished {
		a.completed++
	}
	times := a.blockTimes(r)
	total := 0.0
	summed := map[string]bool{}
	for _, id := range a.sv.BlockIDs {
		if !summed[id] {
			summed[id] = true
			total += times[id]
		}
	}
	if total <= 0 {
		total, _ = r.TTC["_total"].(float64)
	}
	if total > 0 {
		a.ttcCount++
		a.ttcSum += total
	}
	for id := range summed {
		if t := times[id]; t > 0 {
			a.blockTotal[id] += t
			a.blockCount[id]++
		}
	}
	lastSeen := -1
	for i, el := range a.sv.Elements {
		// forma2's wasElementSeen: time spent on the element or an answer to it, rather than a replay of the survey's logic.
		if _, answered := r.Data[el.ID]; positive(r.TTC[el.ID]) || answered {
			a.impressions[i]++
			lastSeen = i
		}
	}
	if !r.Finished && lastSeen >= 0 {
		a.dropOffs[lastSeen]++
	}
	for _, e := range a.elements {
		if e != nil {
			e.add(r)
		}
	}
	for _, h := range a.hidden {
		h.add(r)
	}
}

func (a *summarizer) dropOff(displays int) []DropOff {
	n := len(a.sv.Elements)
	impressions := slices.Clone(a.impressions)
	dropOffs := slices.Clone(a.dropOffs)
	percentages := make([]float64, n)
	if n > 0 {
		if !a.sv.WelcomeCardEnabled {
			// Every display is an impression of the first element; a response without a display row (a link survey, a lost display call) can push the impressions past the displays, so the count is clamped like the percentage.
			dropOffs[0] = max(0, displays-impressions[0])
			if impressions[0]-displays < 0 {
				percentages[0] = float64(displays-impressions[0]) / float64(displays) * 100
			}
			impressions[0] = displays
		} else if impressions[0] > 0 {
			percentages[0] = float64(dropOffs[0]) / float64(impressions[0]) * 100
		}
	}
	for i := 1; i < n; i++ {
		if impressions[i] != 0 {
			percentages[i] = float64(dropOffs[i]) / float64(impressions[i]) * 100
		}
	}
	out := make([]DropOff, n)
	for i, el := range a.sv.Elements {
		ttc := 0.0
		if c := a.blockCount[el.BlockID]; c > 0 {
			ttc = round2(a.blockTotal[el.BlockID] / float64(c))
		}
		out[i] = DropOff{ElementID: el.ID, ElementType: el.Type, Headline: surveydoc.TextContent(el.Headline.Default()), TTC: ttc,
			Impressions: impressions[i], DropOffCount: dropOffs[i], DropOffPercentage: round2(percentages[i])}
	}
	return out
}

func (a *summarizer) result(displays int, quotas []QuotaProgress) *Summary {
	meta := Meta{DisplayCount: displays, TotalResponses: a.total, CompletedResponses: a.completed, DropOffCount: a.total - a.completed}
	if displays > 0 {
		meta.StartsPercentage = round2(float64(a.total) / float64(displays) * 100)
		meta.CompletedPercentage = round2(float64(a.completed) / float64(displays) * 100)
	}
	meta.DropOffPercentage = percent(float64(meta.DropOffCount), float64(a.total))
	if a.ttcCount > 0 {
		meta.TTCAverage = round2(a.ttcSum / float64(a.ttcCount))
	}
	for _, q := range quotas {
		if q.Count >= q.Limit {
			meta.QuotasCompleted++
		}
	}
	if len(quotas) > 0 {
		meta.QuotasCompletedPercentage = float64(meta.QuotasCompleted) / float64(len(quotas)) * 100
	}
	dropOff := a.dropOff(displays)
	entries := []any{}
	for _, e := range a.elements {
		if e == nil {
			continue
		}
		if entry, ok := e.result(dropOff); ok {
			entries = append(entries, entry)
		}
	}
	for _, h := range a.hidden {
		entries = append(entries, h.result())
	}
	return &Summary{Meta: meta, DropOff: dropOff, Summary: entries, Quotas: quotas}
}

// newElementAggregate is the aggregate for the element's type; nil for a type the summary has no entry for.
func newElementAggregate(sv *surveyDoc, index int) elementAggregate {
	el := &sv.Elements[index]
	switch el.Type {
	case surveydoc.ElementOpenText, surveydoc.ElementDate:
		return &samplesAggregate{el: el, keep: func(v any) bool { s, ok := v.(string); return ok && s != "" }}
	case surveydoc.ElementAddress, surveydoc.ElementContactInfo:
		return &samplesAggregate{el: el, keep: func(v any) bool { a, ok := v.([]any); return ok && len(a) > 0 }}
	case surveydoc.ElementFileUpload:
		return &samplesAggregate{el: el, files: true, keep: func(v any) bool { _, ok := v.([]any); return ok }}
	case surveydoc.ElementMultipleChoiceSingle, surveydoc.ElementMultipleChoiceMulti:
		return newChoiceAggregate(sv, el)
	case surveydoc.ElementPictureSelection:
		return newPictureAggregate(el)
	case surveydoc.ElementRating, surveydoc.ElementCSAT, surveydoc.ElementCES:
		return newScaleAggregate(el)
	case surveydoc.ElementNPS:
		return newNPSAggregate(el)
	case surveydoc.ElementCTA:
		return &ctaAggregate{el: el, index: index}
	case surveydoc.ElementConsent:
		return &outcomeAggregate{el: el, positive: "accepted"}
	case surveydoc.ElementCal:
		return &outcomeAggregate{el: el, positive: "booked"}
	case surveydoc.ElementMatrix:
		return newMatrixAggregate(sv, el)
	case surveydoc.ElementRanking:
		return newRankingAggregate(sv, el)
	}
	return nil
}

// samplesAggregate is the OpenText, Date, Address, ContactInfo and FileUpload summaries: the answers keep accepts, counted, newest first up to valuesLimit.
type samplesAggregate struct {
	el      *element
	keep    func(v any) bool
	files   bool
	count   int
	samples []Sample
}

func (g *samplesAggregate) add(r *summaryResponse) {
	v := member(r.Data, g.el.ID)
	if !g.keep(v) {
		return
	}
	g.count++
	if len(g.samples) < valuesLimit {
		g.samples = append(g.samples, r.sample(v))
	}
}

func (g *samplesAggregate) result([]DropOff) (any, bool) {
	samples := g.samples
	if samples == nil {
		samples = []Sample{}
	}
	if g.files {
		return FileUploadSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.count, Files: samples}, true
	}
	return SamplesSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.count, Samples: samples}, true
}

// translated is forma2's checkForI18n: a choice answer given in a response language mapped back to the default-language labels. A multi-select or ranking answer becomes a list (a single string a one-element list); an answer that is neither a string nor a list, which forma2 failed on, becomes an empty list.
func translated(el *element, answer any, lang string) any {
	label := func(v any) any {
		s, ok := v.(string)
		if !ok {
			return v
		}
		for _, c := range el.Choices {
			if l, has := c.Label[lang]; has && l == s {
				if d := c.Label.Default(); d != "" {
					return d
				}
				return s
			}
		}
		return s
	}
	switch el.Type {
	case surveydoc.ElementMultipleChoiceMulti, surveydoc.ElementRanking:
		var values []any
		switch x := answer.(type) {
		case string:
			values = []any{x}
		case []any:
			values = x
		}
		out := make([]any, len(values))
		for i, v := range values {
			out[i] = label(v)
		}
		return out
	case surveydoc.ElementMultipleChoiceSingle:
		return label(answer)
	}
	return answer
}

// choiceAggregate is the MultipleChoiceSingle and MultipleChoiceMulti summary.
type choiceAggregate struct {
	sv         *surveyDoc
	el         *element
	labels     []string
	counts     map[string]int
	other      *surveydoc.Choice
	noneLabel  string
	hasNone    bool
	noneCount  int
	others     []Other
	otherCount int
	selections int
	responses  int
}

func newChoiceAggregate(sv *surveyDoc, el *element) *choiceAggregate {
	g := &choiceAggregate{sv: sv, el: el, counts: map[string]int{}}
	for i, c := range el.Choices {
		switch c.ID {
		case "other":
			if g.other == nil {
				g.other = &el.Choices[i]
			}
		case "none":
			if !g.hasNone {
				g.hasNone, g.noneLabel = true, c.Label.Default()
			}
		default:
			if _, seen := g.counts[c.Label.Default()]; !seen {
				g.labels = append(g.labels, c.Label.Default())
			}
			g.counts[c.Label.Default()] = 0
		}
	}
	jsKeyOrder(g.labels)
	return g
}

func (g *choiceAggregate) count(r *summaryResponse, value any) {
	g.selections++
	if s, ok := value.(string); ok {
		if _, known := g.counts[s]; known {
			g.counts[s]++
			return
		}
		if g.noneLabel != "" && s == g.noneLabel {
			g.noneCount++
			return
		}
	}
	if g.other != nil {
		g.otherCount++
		if len(g.others) < valuesLimit {
			g.others = append(g.others, Other{Value: value, Contact: r.Contact, ContactAttributes: r.ContactAttributes})
		}
	}
}

func (g *choiceAggregate) add(r *summaryResponse) {
	answer := member(r.Data, g.el.ID)
	if lang := g.sv.languageCode(r.Language); lang != "default" {
		answer = translated(g.el, answer, lang)
	}
	valid := false
	if list, ok := answer.([]any); ok && g.el.Type == surveydoc.ElementMultipleChoiceMulti {
		for _, v := range list {
			if truthy(v) {
				g.count(r, v)
				valid = true
			}
		}
	} else if s, ok := answer.(string); ok && g.el.Type == surveydoc.ElementMultipleChoiceSingle && s != "" {
		g.count(r, s)
		valid = true
	}
	if valid {
		g.responses++
	}
}

func (g *choiceAggregate) result([]DropOff) (any, bool) {
	total := float64(g.responses)
	choices := []ChoiceCount{}
	for _, label := range g.labels {
		choices = append(choices, ChoiceCount{Value: label, Count: g.counts[label], Percentage: percent(float64(g.counts[label]), total)})
	}
	if g.other != nil {
		label := g.other.Label.Default()
		if label == "" {
			label = "Other"
		}
		others := g.others
		if others == nil {
			others = []Other{}
		}
		choices = append(choices, ChoiceCount{Value: label, Count: g.otherCount, Percentage: percent(float64(g.otherCount), total), Others: &others})
	}
	if g.hasNone && g.noneLabel != "" {
		choices = append(choices, ChoiceCount{Value: g.noneLabel, Count: g.noneCount, Percentage: percent(float64(g.noneCount), total)})
	}
	return MultipleChoiceSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.responses, SelectionCount: g.selections, Choices: choices}, true
}

// pictureAggregate is the PictureSelection summary: selections by choice id, any list an answer, an empty one included.
type pictureAggregate struct {
	el         *element
	counts     map[string]int
	selections int
	responses  int
}

func newPictureAggregate(el *element) *pictureAggregate {
	return &pictureAggregate{el: el, counts: map[string]int{}}
}

func (g *pictureAggregate) add(r *summaryResponse) {
	list, ok := member(r.Data, g.el.ID).([]any)
	if !ok {
		return
	}
	g.responses++
	for _, v := range list {
		g.selections++
		g.counts[jsvalue.String(v)]++
	}
}

func (g *pictureAggregate) result([]DropOff) (any, bool) {
	choices := []PictureCount{}
	for _, c := range g.el.Choices {
		n := g.counts[c.ID]
		choices = append(choices, PictureCount{ID: c.ID, ImageURL: c.ImageURL, Count: n, Percentage: percent(float64(n), float64(g.responses))})
	}
	return PictureSelectionSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.responses, SelectionCount: g.selections, Choices: choices}, true
}

// buckets counts numeric answers by value the way forma2's object of counts did: the expected values start at 0, an unexpected value opens a bucket that counts NaN, and the buckets come out in property order.
type buckets struct {
	keys   []string
	counts map[string]float64
}

func newBuckets(from, to float64) *buckets {
	b := &buckets{counts: map[string]float64{}}
	for i := from; i <= to; i++ {
		key := jsvalue.FormatNumber(i)
		b.keys = append(b.keys, key)
		b.counts[key] = 0
	}
	return b
}

func (b *buckets) add(v float64) {
	key := jsvalue.FormatNumber(v)
	if _, ok := b.counts[key]; !ok {
		b.keys = append(b.keys, key)
		b.counts[key] = math.NaN()
		return
	}
	b.counts[key]++
}

func (b *buckets) choices(total float64) []RatingCount {
	keys := slices.Clone(b.keys)
	jsKeyOrder(keys)
	out := make([]RatingCount, len(keys))
	for i, k := range keys {
		n := b.counts[k]
		p := 0.0
		if total > 0 {
			p = round2(n / total * 100)
		}
		out[i] = RatingCount{Rating: Num(parseIntJS(k)), Count: Num(n), Percentage: Num(p)}
	}
	return out
}

func elementRange(el *element) float64 {
	if el.Range == nil {
		return 0
	}
	return *el.Range
}

// scaleAggregate is forma2's computeNumericScaleStats for Rating, CSAT and CES: numeric answers in buckets 1..range, a response that spent time on the element without a number counted as dismissed and kept out of the percentages.
type scaleAggregate struct {
	el        *element
	buckets   *buckets
	count     int
	sum       float64
	dismissed int
}

func newScaleAggregate(el *element) *scaleAggregate {
	return &scaleAggregate{el: el, buckets: newBuckets(1, elementRange(el))}
}

func (g *scaleAggregate) add(r *summaryResponse) {
	if n, ok := member(r.Data, g.el.ID).(float64); ok {
		g.count++
		g.buckets.add(n)
		g.sum += n
	} else if positive(r.TTC[g.el.ID]) {
		g.dismissed++
	}
}

func (g *scaleAggregate) result([]DropOff) (any, bool) {
	out := ScaleSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.count, Choices: g.buckets.choices(float64(g.count)), Dismissed: DismissedCount{Count: g.dismissed}}
	if g.count > 0 {
		out.Average = Num(round2(g.sum / float64(g.count)))
	}
	if g.el.Type == surveydoc.ElementCSAT {
		// The top two ratings of five are "satisfied"; a bucket holding NaN counts as 0, as `|| 0` read it.
		satisfied := 0
		for _, k := range []string{"4", "5"} {
			if n := g.buckets.counts[k]; !math.IsNaN(n) {
				satisfied += int(n)
			}
		}
		out.CSAT = &Satisfaction{SatisfiedCount: satisfied, SatisfiedPercentage: percent(float64(satisfied), float64(g.count))}
	}
	return out, true
}

// npsAggregate is the NPS summary: 9 and 10 promote, 7 and 8 are passive, the rest detract, and a dismissal counts towards the total every share is taken of.
type npsAggregate struct {
	el                                         *element
	buckets                                    *buckets
	promoters, passives, detractors, dismissed int
	total                                      int
}

func newNPSAggregate(el *element) *npsAggregate {
	return &npsAggregate{el: el, buckets: newBuckets(0, 10)}
}

func (g *npsAggregate) add(r *summaryResponse) {
	if n, ok := member(r.Data, g.el.ID).(float64); ok {
		g.total++
		g.buckets.add(n)
		switch {
		case n >= 9:
			g.promoters++
		case n >= 7:
			g.passives++
		default:
			g.detractors++
		}
	} else if positive(r.TTC[g.el.ID]) {
		g.total++
		g.dismissed++
	}
}

func (g *npsAggregate) result([]DropOff) (any, bool) {
	total := float64(g.total)
	share := func(n int) Count { return Count{Count: n, Percentage: percent(float64(n), total)} }
	return NPSSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.total, Total: g.total,
		Score: percent(float64(g.promoters-g.detractors), total), Promoters: share(g.promoters), Passives: share(g.passives),
		Detractors: share(g.detractors), Dismissed: share(g.dismissed), Choices: g.buckets.choices(total)}, true
}

// ctaAggregate is the CTA summary, kept only for a button to an external link: clicks against the element's impressions.
type ctaAggregate struct {
	el                 *element
	index              int
	clicked, dismissed int
}

func (g *ctaAggregate) add(r *summaryResponse) {
	switch member(r.Data, g.el.ID) {
	case "clicked":
		g.clicked++
	case "dismissed":
		g.dismissed++
	}
}

func (g *ctaAggregate) result(dropOff []DropOff) (any, bool) {
	if !g.el.IsExternalButton() {
		return nil, false
	}
	impressions := 0
	for _, d := range dropOff {
		if d.ElementID == g.el.ID {
			impressions = d.Impressions
			break
		}
	}
	return CTASummary{Type: g.el.Type, Element: g.el.Raw, ImpressionCount: impressions, ClickCount: g.clicked, SkipCount: g.dismissed,
		ResponseCount: g.clicked + g.dismissed, CTR: Count{Count: g.clicked, Percentage: percent(float64(g.clicked), float64(impressions))}}, true
}

// outcomeAggregate is the Consent and Cal summaries: the positive answer ("accepted", "booked") against responses that spent time on the element without it.
type outcomeAggregate struct {
	el                  *element
	positive            string
	positives, negative int
}

func (g *outcomeAggregate) add(r *summaryResponse) {
	if member(r.Data, g.el.ID) == g.positive {
		g.positives++
	} else if positive(r.TTC[g.el.ID]) {
		g.negative++
	}
}

func (g *outcomeAggregate) result([]DropOff) (any, bool) {
	total := float64(g.positives + g.negative)
	yes := Count{Count: g.positives, Percentage: percent(float64(g.positives), total)}
	no := Count{Count: g.negative, Percentage: percent(float64(g.negative), total)}
	if g.el.Type == surveydoc.ElementCal {
		return CalSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.positives + g.negative, Booked: yes, Skipped: no}, true
	}
	return ConsentSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.positives + g.negative, Accepted: yes, Dismissed: no}, true
}

// matrixAggregate is the Matrix summary: each row's answer, read in the response's language, counted under the default-language row and column.
type matrixAggregate struct {
	sv        *surveyDoc
	el        *element
	rows      []string
	columns   []string
	counts    map[string]map[string]int
	responses int
}

func newMatrixAggregate(sv *surveyDoc, el *element) *matrixAggregate {
	g := &matrixAggregate{sv: sv, el: el, counts: map[string]map[string]int{}}
	for _, c := range el.Columns {
		g.columns = append(g.columns, c.Label.Default())
	}
	for _, r := range el.Rows {
		label := r.Label.Default()
		g.rows = append(g.rows, label)
		g.counts[label] = map[string]int{}
		for _, c := range g.columns {
			g.counts[label][c] = 0
		}
	}
	return g
}

func (g *matrixAggregate) add(r *summaryResponse) {
	answer := member(r.Data, g.el.ID)
	if !truthy(answer) {
		return
	}
	g.responses++
	selected, _ := answer.(map[string]any)
	lang := g.sv.languageCode(r.Language)
	for _, row := range g.el.Rows {
		value := member(selected, row.Label[lang])
		var column *surveydoc.Choice
		for i, c := range g.el.Columns {
			if value == c.Label[lang] {
				column = &g.el.Columns[i]
				break
			}
		}
		if column == nil {
			continue
		}
		if def := column.Label.Default(); def != "" && slices.Contains(g.columns, def) {
			g.counts[row.Label.Default()][def]++
		}
	}
}

func (g *matrixAggregate) result([]DropOff) (any, bool) {
	data := []MatrixRow{}
	for _, row := range g.rows {
		total := 0
		for _, c := range g.columns {
			total += g.counts[row][c]
		}
		percentages := []ColumnPercentage{}
		for _, c := range g.columns {
			p := 0.0
			if total > 0 {
				p = toFixed2(float64(g.counts[row][c]) / float64(total) * 100)
			}
			percentages = append(percentages, ColumnPercentage{Column: c, Percentage: p})
		}
		data = append(data, MatrixRow{RowLabel: row, ColumnPercentages: percentages, TotalResponsesForRow: total})
	}
	return MatrixSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.responses, Data: data}, true
}

// rankingAggregate is the Ranking summary: the average position (from 1) each known choice was ranked at.
type rankingAggregate struct {
	sv        *surveyDoc
	el        *element
	labels    []string
	sums      map[string]float64
	counts    map[string]int
	responses int
}

func newRankingAggregate(sv *surveyDoc, el *element) *rankingAggregate {
	g := &rankingAggregate{sv: sv, el: el, sums: map[string]float64{}, counts: map[string]int{}}
	for _, c := range el.Choices {
		g.labels = append(g.labels, c.Label.Default())
	}
	return g
}

func (g *rankingAggregate) add(r *summaryResponse) {
	answer := member(r.Data, g.el.ID)
	if lang := g.sv.languageCode(r.Language); lang != "default" {
		answer = translated(g.el, answer, lang)
	}
	list, ok := answer.([]any)
	if !ok {
		return
	}
	g.responses++
	for i, v := range list {
		if s, ok := v.(string); ok && slices.Contains(g.labels, s) {
			g.sums[s] += float64(i + 1)
			g.counts[s]++
		}
	}
}

func (g *rankingAggregate) result([]DropOff) (any, bool) {
	choices := []RankingCount{}
	for _, label := range g.labels {
		n := g.counts[label]
		avg := 0.0
		if n > 0 {
			avg = round2(g.sums[label] / float64(n))
		}
		choices = append(choices, RankingCount{Value: label, Count: n, AvgRanking: avg})
	}
	return RankingSummary{Type: g.el.Type, Element: g.el.Raw, ResponseCount: g.responses, Choices: choices}, true
}

// hiddenAggregate is a hidden field's summary: its non-empty text values; a number is not listed.
type hiddenAggregate struct {
	key     string
	count   int
	samples []Sample
}

func (g *hiddenAggregate) add(r *summaryResponse) {
	s, ok := r.Data[g.key].(string)
	if !ok || s == "" {
		return
	}
	g.count++
	if len(g.samples) < valuesLimit {
		g.samples = append(g.samples, Sample{UpdatedAt: r.UpdatedAt, Value: s, Contact: r.Contact, ContactAttributes: r.ContactAttributes})
	}
}

func (g *hiddenAggregate) result() HiddenFieldSummary {
	return HiddenFieldSummary{Type: "hiddenField", ID: g.key, ResponseCount: g.count, Samples: g.samples}
}
