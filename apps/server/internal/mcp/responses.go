package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/platform/v3api"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// This file is the two tools that read what respondents answered, list_responses and get_survey_summary (owner decision D-AO11 of 2026-10-25); forma2 had neither. They need the responses:read scope and, like the response views, read access to the survey's workspace. They have no v3 operation to run: they answer in the v3 tools' shapes, {data, meta} and the problem fields, from the response and analysis modules directly.
//
// Respondents are not named by default: a response's contact, the contact attributes captured with it, its IP address and the address an email-verified survey recorded for it (verifiedEmail) come back only when the call asks for them with includeContact, and the summary never carries them. Answers to questions that ask for contact details are answers like any other and are returned. The email verification token in a response's page URL is a credential and is never returned.

const (
	responsesDefaultLimit = 20
	responsesMaxLimit     = 100
	// summarySamplesDefault is how many answers a summary card that lists individual answers (open text, Other values, hidden fields, files) keeps; the analysis lists at most 50.
	summarySamplesDefault = 10
)

const invalidCursorDetail = "Invalid pagination cursor"

// verifiedEmailKey is the key under which the verified-email gate (clientapi's verifiedEmailGate) stores the address a response was verified with: the respondent's identity, which the CSV export lists as its Verified Email column, not an answer they typed.
const verifiedEmailKey = "verifiedEmail"

// problemResult is a v3 problem as a tool result.
func problemResult(p *v3api.Problem, requestID string) *sdk.CallToolResult {
	e := toolError{Status: p.Status, Title: p.Title, Detail: p.Detail, Code: p.Code, RequestID: requestID}
	if len(p.InvalidParams) > 0 {
		e.InvalidParams, _ = json.Marshal(p.InvalidParams)
	}
	return errorResult(e)
}

// readFailure maps what reading a survey's responses returned onto the tool result v3 would answer: a refusal, an unknown survey included, is the 403 that hides whether it exists, as v3's survey operations answer; a malformed filter is a 400; anything else is unexpected.
func (h *Handler) readFailure(ctx context.Context, p *principal, err error) *sdk.CallToolResult {
	var problem *v3api.Problem
	if errors.As(err, &problem) {
		return problemResult(problem, p.requestID)
	}
	if e, ok := apperr.As(err); ok {
		switch e.Status {
		case http.StatusForbidden, http.StatusNotFound:
			return problemResult(v3api.Forbidden(""), p.requestID)
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return problemResult(v3api.BadRequest(e.Message), p.requestID)
		}
	}
	return h.failed(ctx, p, err)
}

// responseFilter reads the filter both tools take: finished, the creation range (inclusive at both ends) and a keyword the free-text answers must contain.
func responseFilter(args map[string]any) (*response.Criteria, *v3api.Problem) {
	c := &response.Criteria{}
	if finished, ok := args["finished"].(bool); ok {
		c.Finished = &finished
	}
	var r response.Range
	for _, bound := range []struct {
		name string
		to   **time.Time
	}{{"createdFrom", &r.Min}, {"createdTo", &r.Max}} {
		raw := str(args, bound.name)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, v3api.BadRequest("Invalid input", v3api.InvalidParam{Name: bound.name, Reason: "Expected an ISO 8601 date-time with a time zone, such as 2026-01-31T00:00:00Z."})
		}
		*bound.to = &t
	}
	if r.Min != nil || r.Max != nil {
		c.CreatedAt = &r
	}
	if search := strings.TrimSpace(str(args, "search")); search != "" {
		c.Search = &search
	}
	return c, nil
}

// readableSurveyID resolves the surveyId argument and authorizes reading its responses. An id that names no survey, a cuid2 one included, is refused like a survey the caller may not read.
func (h *Handler) readableSurveyID(ctx context.Context, p *principal, args map[string]any) (uuid.UUID, error) {
	id, ok := rowid.Parse(str(args, "surveyId"))
	if !ok {
		return uuid.Nil, v3api.Forbidden("")
	}
	allowed, err := h.authz.Can(ctx, p.actor, authz.SurveyResponseRead, authz.Survey(id))
	if err != nil {
		return uuid.Nil, err
	}
	if !allowed {
		return uuid.Nil, v3api.Forbidden("")
	}
	return id, nil
}

// responseCursor is the opaque cursor of list_responses: the position of the page's last response in the list order, newest first.
func encodeResponseCursor(r response.Response) string {
	raw, _ := json.Marshal(response.Cursor{CreatedAt: r.CreatedAt, ID: r.ID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeResponseCursor(s string) (*response.Cursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, false
	}
	var c response.Cursor
	if json.Unmarshal(raw, &c) != nil || c.ID == uuid.Nil || c.CreatedAt.IsZero() {
		return nil, false
	}
	return &c, true
}

// answer is one question a response answered, in the survey's order.
type answer struct {
	QuestionID string `json:"questionId"`
	Type       string `json:"type"`
	Headline   string `json:"headline"`
	Value      any    `json:"value"`
}

// responseItem is a response as list_responses returns it: the answers by question, with the question's headline in the survey's default language, rather than the stored data keyed by element id.
type responseItem struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Finished  bool      `json:"finished"`
	Language  *string   `json:"language"`
	Answers   []answer  `json:"answers"`
	// OtherData is what the response holds outside the survey's current questions: hidden fields, embedded data, and answers to questions since removed.
	OtherData map[string]any  `json:"otherData"`
	Variables json.RawMessage `json:"variables"`
	Tags      []string        `json:"tags"`
	Meta      map[string]any  `json:"meta"`
	// Contact and ContactAttributes are set only with includeContact.
	Contact           *response.Contact `json:"contact,omitempty"`
	ContactAttributes json.RawMessage   `json:"contactAttributes,omitempty"`
}

// decodeObject reads a stored JSON object, empty for null or nothing.
func decodeObject(raw json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func newResponseItem(sv *response.Survey, r response.Response, includeContact bool) (responseItem, error) {
	data, err := decodeObject(r.Data)
	if err != nil {
		return responseItem{}, fmt.Errorf("decode data of response %s: %w", r.ID, err)
	}
	meta, err := decodeObject(r.Meta)
	if err != nil {
		return responseItem{}, fmt.Errorf("decode meta of response %s: %w", r.ID, err)
	}
	item := responseItem{
		ID: r.ID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Finished: r.Finished, Language: r.Language,
		Answers: []answer{}, OtherData: map[string]any{}, Variables: r.Variables, Tags: make([]string, len(r.Tags)), Meta: meta,
	}
	if len(bytes.TrimSpace(item.Variables)) == 0 {
		item.Variables = json.RawMessage("{}")
	}
	for i, t := range r.Tags {
		item.Tags[i] = t.Name
	}
	for _, el := range sv.Elements() {
		if v, ok := data[el.ID]; ok {
			item.Answers = append(item.Answers, answer{QuestionID: el.ID, Type: el.Type, Headline: surveydoc.TextContent(el.Headline.Default()), Value: v})
			delete(data, el.ID)
		}
	}
	item.OtherData = data
	withoutVerifyToken(item.Meta)
	if includeContact {
		item.Contact, item.ContactAttributes = r.Contact, r.ContactAttributes
	} else {
		delete(item.Meta, "ipAddress")
		delete(item.OtherData, verifiedEmailKey)
	}
	return item, nil
}

// withoutVerifyToken takes the email verification token out of the page URL a response was submitted from. A survey that requires email verification takes a response only from a URL whose verify parameter is a token for it, valid for seven days and naming the respondent's address, so the stored URL carries a credential that would let whoever reads it submit responses as that address. It is not respondent data and is never returned.
func withoutVerifyToken(meta map[string]any) {
	raw, ok := meta["url"].(string)
	if !ok {
		return
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		// The gate refuses a URL it cannot parse, so this one carried no accepted token; it is dropped rather than returned unchecked.
		delete(meta, "url")
		return
	}
	q := u.Query()
	if !q.Has("verify") {
		return
	}
	q.Del("verify")
	u.RawQuery = q.Encode()
	meta["url"] = u.String()
}

// listResponses is list_responses: a page of the survey's responses, newest first, matching the filter.
func (h *Handler) listResponses(ctx context.Context, p *principal, args map[string]any) *sdk.CallToolResult {
	filter, problem := responseFilter(args)
	if problem != nil {
		return problemResult(problem, p.requestID)
	}
	limit := responsesDefaultLimit
	if n, ok := args["limit"].(float64); ok {
		limit = int(n)
	}
	var cursor *response.Cursor
	if raw := str(args, "cursor"); raw != "" {
		c, ok := decodeResponseCursor(raw)
		if !ok {
			return problemResult(v3api.BadRequest(invalidCursorDetail, v3api.InvalidParam{Name: "cursor", Reason: invalidCursorDetail}), p.requestID)
		}
		cursor = c
	}
	surveyID, err := h.readableSurveyID(ctx, p, args)
	if err != nil {
		return h.readFailure(ctx, p, err)
	}
	sv, err := response.LoadSurvey(ctx, h.db, surveyID)
	if err != nil {
		return h.failed(ctx, p, err)
	}
	if sv == nil {
		return problemResult(v3api.Forbidden(""), p.requestID)
	}
	// The filter names no segment, so no segment evaluator is needed.
	clause, err := response.Where(ctx, nil, sv, filter)
	if err != nil {
		return h.readFailure(ctx, p, err)
	}
	// One more than the page tells whether another follows.
	rows, err := response.Page(ctx, h.db, surveyID, clause, cursor, limit+1, 0)
	if err != nil {
		return h.failed(ctx, p, err)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		c := encodeResponseCursor(rows[len(rows)-1].Response)
		next = &c
	}
	includeContact, _ := args["includeContact"].(bool)
	items := make([]responseItem, len(rows))
	for i, r := range rows {
		if items[i], err = newResponseItem(sv, r.Response, includeContact); err != nil {
			return h.failed(ctx, p, err)
		}
	}
	body, err := json.Marshal(map[string]any{"data": items, "meta": map[string]any{"limit": limit, "nextCursor": next}})
	if err != nil {
		return h.failed(ctx, p, err)
	}
	return toolResult(http.StatusOK, http.Header{}, body, p.requestID)
}

// getSurveySummary is get_survey_summary: the statistics the survey's summary page shows, over the responses matching the filter. Each card names its question by id, type and headline instead of carrying the whole element, keeps at most samplesPerQuestion of the individual answers and of the breakdown values it lists, and none of them names a contact.
func (h *Handler) getSurveySummary(ctx context.Context, p *principal, args map[string]any) *sdk.CallToolResult {
	filter, problem := responseFilter(args)
	if problem != nil {
		return problemResult(problem, p.requestID)
	}
	samples := summarySamplesDefault
	if n, ok := args["samplesPerQuestion"].(float64); ok {
		samples = int(n)
	}
	surveyID, ok := rowid.Parse(str(args, "surveyId"))
	if !ok {
		return problemResult(v3api.Forbidden(""), p.requestID)
	}
	summary, err := h.analysis.SummaryAs(ctx, p.actor, surveyID, filter)
	if err != nil {
		return h.readFailure(ctx, p, err)
	}
	data, err := compactSummary(summary, samples)
	if err != nil {
		return h.failed(ctx, p, err)
	}
	data["surveyId"] = surveyID
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return h.failed(ctx, p, err)
	}
	return toolResult(http.StatusOK, http.Header{}, body, p.requestID)
}

// sampleLists are the keys under which a summary lists individual answers: open-text and hidden-field samples, uploaded files, and a choice's Other values.
var sampleLists = map[string]bool{"samples": true, "files": true, "others": true}

// compactSummary is the summary as an MCP client reads it, as a JSON object: the element of every card reduced to its id, type and headline, every list of individual answers cut to samples entries, and every contact and contact attribute snapshot left out.
func compactSummary(summary any, samples int) (map[string]any, error) {
	raw, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("encode summary: %w", err)
	}
	doc, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("decode summary: %w", err)
	}
	cards, _ := doc["summary"].([]any)
	for _, card := range cards {
		if m, ok := card.(map[string]any); ok {
			if el, ok := m["element"].(map[string]any); ok {
				m["element"] = compactElement(el)
			}
		}
	}
	withoutRespondents(doc, samples)
	return doc, nil
}

// compactElement is an element's id, type and headline in the survey's default language, as text.
func compactElement(el map[string]any) map[string]any {
	headline := ""
	if h, ok := el["headline"].(map[string]any); ok {
		if s, ok := h["default"].(string); ok {
			headline = surveydoc.TextContent(s)
		}
	}
	return map[string]any{"id": el["id"], "type": el["type"], "headline": headline}
}

// breakdowns are the keys under which a summary groups a free-text field's values with their counts: a hidden field's values and a choice's Other values. Each lists raw values as respondents gave them, so it is cut to samples values like the sample lists.
var breakdowns = map[string]bool{"breakdown": true, "othersBreakdown": true}

// withoutRespondents removes contact and contactAttributes everywhere under node and cuts the sample lists and the breakdowns to samples entries.
func withoutRespondents(node any, samples int) {
	switch v := node.(type) {
	case map[string]any:
		delete(v, "contact")
		delete(v, "contactAttributes")
		for key, child := range v {
			if list, ok := child.([]any); ok && sampleLists[key] && len(list) > samples {
				v[key] = list[:samples]
			}
			if b, ok := child.(map[string]any); ok && breakdowns[key] {
				cutBreakdown(b, samples)
			}
			withoutRespondents(v[key], samples)
		}
	case []any:
		for _, child := range v {
			withoutRespondents(child, samples)
		}
	}
}

// cutBreakdown keeps a breakdown's samples most frequent values and adds the answers of the values it drops to the rest, whose share is recomputed over every answer the breakdown counts, as the analysis computes it.
func cutBreakdown(b map[string]any, samples int) {
	values, _ := b["values"].([]any)
	if len(values) <= samples {
		return
	}
	rest := jsonInt(b["restCount"])
	total := rest
	for i, value := range values {
		n := 0
		if m, ok := value.(map[string]any); ok {
			n = jsonInt(m["count"])
		}
		total += n
		if i >= samples {
			rest += n
		}
	}
	b["values"] = values[:samples]
	b["restCount"] = rest
	b["restPercentage"] = 0.0
	if total > 0 {
		b["restPercentage"] = math.Round(float64(rest)/float64(total)*100*100) / 100
	}
}

// jsonInt is a count decoded with UseNumber, 0 when it is not one.
func jsonInt(v any) int {
	n, _ := v.(json.Number)
	i, _ := n.Int64()
	return int(i)
}
