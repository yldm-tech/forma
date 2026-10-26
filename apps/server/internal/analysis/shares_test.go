package analysis

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
)

// Read-only results links: managed by whoever may publish the survey, read without a session by whoever holds the key, and never showing who answered.

const shareBlocks = `[
	{"id":"b1","name":"B1","elements":[
		{"id":"q1","type":"openText","headline":{"default":"What could be better?"},"required":false,"inputType":"text"},
		{"id":"nps","type":"nps","headline":{"default":"Recommend?"},"required":false},
		{"id":"pick","type":"multipleChoiceSingle","headline":{"default":"Pick"},"required":false,
			"choices":[{"id":"a","label":{"default":"A"}},{"id":"b","label":{"default":"B"}},{"id":"other","label":{"default":"Other"}}]},
		{"id":"ci","type":"contactInfo","headline":{"default":"Your details"},"required":false},
		{"id":"cv","type":"fileUpload","headline":{"default":"CV"},"required":false,"allowMultipleFiles":false}
	]}
]`

// public sends a request without a session, as a link's reader does.
func (e *env) public(path string, body any) *httptest.ResponseRecorder {
	e.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webApp)
	req.Header.Set(httpapi.CSRFHeader, "XMLHttpRequest")
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

type sharedResultsBody struct {
	Link    SharedResultsLink `json:"link"`
	Survey  SharedSurvey      `json:"survey"`
	Summary struct {
		Meta    Meta              `json:"meta"`
		Summary []json.RawMessage `json:"summary"`
	} `json:"summary"`
	WithheldIDs []string `json:"withheldIds"`
}

func TestResultShareShowsTheFrozenSummaryWithoutRespondents(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{name: "CSAT 2026", blocks: shareBlocks})
	e.field(ws, sv, "utm", "utm", "ingested", "string")
	ada := e.contact(ws, "email", "ada@example.com", "userId", "ada-1")
	r1 := e.response(sv, row{at: at(1), finished: true, contact: &ada, contactAttributes: ptr(`{"email":"ada@example.com"}`),
		data: `{"q1":"Faster support","nps":10,"pick":"Zed","ci":["Ada","Lovelace","ada@example.com","",""],"cv":["https://files.example.com/cv.pdf"],"utm":"newsletter"}`})
	e.response(sv, row{at: at(2), finished: true, data: `{"q1":"Cheaper","nps":3,"pick":"A","utm":"ads"}`})
	unfinished := e.response(sv, row{at: at(3), data: `{"q1":"Unfinished answer","nps":0}`})

	// The link freezes the finished-only filter and reveals the open text answers alone.
	var share ResultShare
	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/result-shares", map[string]any{
		"name": "  Board  ", "filterCriteria": map[string]any{"finished": true}, "revealedElementIds": []string{"q1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &share); err != nil {
		t.Fatal(err)
	}
	if share.Name != "Board" || len(share.Key) != 43 || !reflect.DeepEqual(share.RevealedElementIDs, []string{"q1"}) || share.Creator == nil || share.Creator.ID != owner || share.ExpiresAt != nil {
		t.Errorf("created %+v", share)
	}

	var listed ResultShares
	decode(t, e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/result-shares", nil), &listed)
	wantRevealable := []Revealable{
		{ID: "q1", Kind: RevealKindAnswers, Type: "openText"},
		{ID: "pick", Kind: RevealKindOtherValues, Type: "multipleChoiceSingle"},
		{ID: "ci", Kind: RevealKindAnswers, Type: "contactInfo"},
		{ID: "utm", Kind: RevealKindHiddenField},
	}
	if len(listed.Shares) != 1 || listed.Shares[0].Key != share.Key || !reflect.DeepEqual(listed.Revealable, wantRevealable) {
		t.Errorf("listed %+v", listed)
	}

	rec = e.public("/api/app/shared-results/summary", map[string]any{"token": share.Key})
	var got sharedResultsBody
	decode(t, rec, &got)
	body := rec.Body.String()
	for _, leak := range []string{ada.String(), r1.String(), unfinished.String(), "ada@example.com", "ada-1", "Lovelace", "cv.pdf", "Zed", "newsletter", "Unfinished answer", ws.String(), org.String()} {
		if strings.Contains(body, leak) {
			t.Errorf("the shared summary shows %q: %s", leak, body)
		}
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache control %q", rec.Header().Get("Cache-Control"))
	}
	if got.Link != (SharedResultsLink{Name: "Board", Filtered: true}) || got.Survey.ID != sv || got.Survey.Name != "CSAT 2026" || got.Survey.Type != "link" {
		t.Errorf("link %+v survey %+v", got.Link, got.Survey)
	}
	if got.Summary.Meta.TotalResponses != 2 || got.Summary.Meta.CompletedResponses != 2 {
		t.Errorf("meta under the frozen filter %+v", got.Summary.Meta)
	}
	if want := []string{"ci", "cv", "utm"}; !reflect.DeepEqual(got.WithheldIDs, want) {
		t.Errorf("withheld %v, want %v", got.WithheldIDs, want)
	}
	var text SamplesSummary
	if err := json.Unmarshal(got.Summary.Summary[0], &text); err != nil {
		t.Fatal(err)
	}
	if text.ResponseCount != 2 || len(text.Samples) != 2 || text.Samples[0].Value != "Cheaper" || text.Samples[0].ID != nil || text.Samples[1].Contact != nil || string(text.Samples[1].ContactAttributes) != "null" {
		t.Errorf("revealed open text %s", got.Summary.Summary[0])
	}
	var pick MultipleChoiceSummary
	if err := json.Unmarshal(got.Summary.Summary[2], &pick); err != nil {
		t.Fatal(err)
	}
	if other := pick.Choices[2]; other.Value != "Other" || other.Count != 1 || other.Others != nil || other.OthersBreakdown != nil {
		t.Errorf("withheld other values %s", got.Summary.Summary[2])
	}
	var ci SamplesSummary
	if err := json.Unmarshal(got.Summary.Summary[3], &ci); err != nil {
		t.Fatal(err)
	}
	if ci.ResponseCount != 1 || len(ci.Samples) != 0 {
		t.Errorf("withheld contact info %s", got.Summary.Summary[3])
	}
	var hidden HiddenFieldSummary
	if err := json.Unmarshal(got.Summary.Summary[5], &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.ID != "utm" || hidden.ResponseCount != 2 || len(hidden.Samples) != 0 || len(hidden.Breakdown.Values) != 0 {
		t.Errorf("withheld hidden field %s", got.Summary.Summary[5])
	}

	// The trend reads through the link under the same filter.
	var trends Trends
	decode(t, e.public("/api/app/shared-results/trends", map[string]any{"token": share.Key, "interval": "day"}), &trends)
	if len(trends.Buckets) != 1 || trends.Buckets[0].Responses != 2 {
		t.Errorf("shared trend %+v", trends)
	}

	// Revealing the Other values and the hidden field shows their values, still without who gave them; the filter stays frozen.
	rec = e.request(owner, http.MethodPut, "/api/app/surveys/"+sv.String()+"/result-shares/"+share.ID.String(), map[string]any{
		"name": "Board", "revealedElementIds": []string{"utm", "pick", "q1"}, "filterCriteria": map[string]any{},
	})
	decode(t, rec, &share)
	var frozen map[string]any
	if err := json.Unmarshal(share.FilterCriteria, &frozen); err != nil {
		t.Fatal(err)
	}
	if want := []string{"q1", "pick", "utm"}; !reflect.DeepEqual(share.RevealedElementIDs, want) || !reflect.DeepEqual(frozen, map[string]any{"finished": true}) {
		t.Errorf("updated %+v, filter %s", share, share.FilterCriteria)
	}
	rec = e.public("/api/app/shared-results/summary", map[string]any{"token": share.Key})
	decode(t, rec, &got)
	if strings.Contains(rec.Body.String(), ada.String()) || !strings.Contains(rec.Body.String(), "Zed") || !strings.Contains(rec.Body.String(), "newsletter") {
		t.Errorf("revealed summary %s", rec.Body)
	}
	if want := []string{"ci", "cv"}; !reflect.DeepEqual(got.WithheldIDs, want) {
		t.Errorf("withheld after revealing %v", got.WithheldIDs)
	}

	// An expired link, a revoked one and a key that names none answer alike.
	e.exec(`UPDATE survey_result_shares SET expires_at = now() - interval '1 minute' WHERE id = ?`, share.ID)
	for _, token := range []string{share.Key, strings.Repeat("a", 43), "short", ""} {
		rec := e.public("/api/app/shared-results/summary", map[string]any{"token": token})
		if token == "" {
			if rec.Code != http.StatusBadRequest {
				t.Errorf("no token: %d %s", rec.Code, rec.Body)
			}
			continue
		}
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), CodeResultShareNotFound) {
			t.Errorf("token %q: %d %s", token, rec.Code, rec.Body)
		}
	}
	e.exec(`UPDATE survey_result_shares SET expires_at = NULL WHERE id = ?`, share.ID)
	if rec := e.request(owner, http.MethodDelete, "/api/app/surveys/"+sv.String()+"/result-shares/"+share.ID.String(), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
	if rec := e.public("/api/app/shared-results/summary", map[string]any{"token": share.Key}); rec.Code != http.StatusNotFound {
		t.Errorf("revoked link: %d %s", rec.Code, rec.Body)
	}
	if rec := e.public("/api/app/shared-results/trends", map[string]any{"token": share.Key}); rec.Code != http.StatusNotFound {
		t.Errorf("revoked link trend: %d %s", rec.Code, rec.Body)
	}
	if rec := e.request(owner, http.MethodDelete, "/api/app/surveys/"+sv.String()+"/result-shares/"+share.ID.String(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoking twice: %d %s", rec.Code, rec.Body)
	}
}

func TestResultSharesAreManagedByThoseWhoMayPublish(t *testing.T) {
	e := newEnv(t)
	owner, writer, reader, stranger := e.user(), e.user(), e.user(), e.user()
	org, ws := e.organization(owner, nil)
	e.grant(org, ws, writer, "readWrite")
	e.grant(org, ws, reader, "read")
	sv := e.survey(ws, surveyOpts{blocks: shareBlocks})
	path := "/api/app/surveys/" + sv.String() + "/result-shares"

	var share ResultShare
	rec := e.request(writer, http.MethodPost, path, map[string]any{"expiresAt": time.Now().Add(24 * time.Hour)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("writer creates: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &share); err != nil {
		t.Fatal(err)
	}
	if share.ExpiresAt == nil || string(share.FilterCriteria) != "{}" || share.Name != "" {
		t.Errorf("created %+v", share)
	}
	// Readers see the summary but cannot publish it, and nobody else may learn the links exist.
	for _, user := range []uuid.UUID{reader, stranger} {
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, path}, {http.MethodPost, path},
			{http.MethodPut, path + "/" + share.ID.String()}, {http.MethodDelete, path + "/" + share.ID.String()},
		} {
			rec := e.request(user, c.method, c.path, map[string]any{"revealedElementIds": []string{}})
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s by %s: %d %s", c.method, c.path, user, rec.Code, rec.Body)
			}
		}
	}
	// Another survey's link is not this survey's.
	other := e.survey(ws, surveyOpts{blocks: shareBlocks})
	if rec := e.request(owner, http.MethodDelete, "/api/app/surveys/"+other.String()+"/result-shares/"+share.ID.String(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoking through another survey: %d %s", rec.Code, rec.Body)
	}

	// Inputs the link cannot keep are refused field by field.
	for name, body := range map[string]map[string]any{
		"past expiry":         {"expiresAt": time.Now().Add(-time.Hour)},
		"long name":           {"name": strings.Repeat("n", maxResultShareNameLength+1)},
		"unrevealable card":   {"revealedElementIds": []string{"nps"}},
		"unknown card":        {"revealedElementIds": []string{"nope"}},
		"file upload":         {"revealedElementIds": []string{"cv"}},
		"malformed condition": {"filterCriteria": map[string]any{"data": map[string]any{"q1": map[string]any{"op": "nope"}}}},
	} {
		if rec := e.request(owner, http.MethodPost, path, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// A survey keeps at most maxResultShares links.
	for i := e.count(`SELECT count(*) FROM survey_result_shares WHERE survey_id = ?`, sv); i < maxResultShares; i++ {
		e.exec(`INSERT INTO survey_result_shares (survey_id, share_key) VALUES (?, ?)`, sv, strings.Repeat("k", 33)+uuid.NewString()[:8]+"ab")
	}
	if rec := e.request(owner, http.MethodPost, path, map[string]any{}); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "at most") {
		t.Errorf("over the bound: %d %s", rec.Code, rec.Body)
	}
	// Deleting the survey deletes its links.
	e.exec(`DELETE FROM surveys WHERE id = ?`, sv)
	if n := e.count(`SELECT count(*) FROM survey_result_shares WHERE survey_id = ?`, sv); n != 0 {
		t.Errorf("%d links left after the survey was deleted", n)
	}
}
