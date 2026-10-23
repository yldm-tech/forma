package survey

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/analytics/analyticstest"
)

// TestEditorSaveEvents ports forma2's captureSurveyEditDiffEvents cases: what a save added is reported once per addition, the first added language makes a single-language survey multilingual, and only a survey that is not a draft reports survey_updated, with survey_published when it just went live.
func TestEditorSaveEvents(t *testing.T) {
	rec := analyticstest.Start(t)
	user, org := uuid.New(), uuid.New()
	lang := func(code string) SurveyLanguage { return SurveyLanguage{Language: Language{Code: code}} }
	old := &Survey{
		ID: uuid.New(), WorkspaceID: uuid.New(), Type: "link", Status: StatusDraft,
		HiddenFields: json.RawMessage(`{"enabled":true,"fieldIds":["a"]}`),
		Blocks:       json.RawMessage(`[{"id":"b1","elements":[{"id":"q1"}],"logic":[]},{"id":"b2","elements":[{"id":"q2"}],"logic":[{"id":"l1"}]}]`),
		Variables:    json.RawMessage(`[{"id":"v1","type":"number"}]`),
		Languages:    []SurveyLanguage{lang("en")},
		FollowUps:    []FollowUp{{ID: uuid.New()}},
	}
	updated := *old
	updated.Status = StatusInProgress
	updated.HiddenFields = json.RawMessage(`{"enabled":true,"fieldIds":["a","b"]}`)
	updated.Blocks = json.RawMessage(`[{"id":"b1","elements":[{"id":"q1"},{"id":"q3"}],"logic":[{"id":"l2"}]},{"id":"b2","elements":[{"id":"q2"}],"logic":[{"id":"l1"}]}]`)
	updated.Variables = json.RawMessage(`[{"id":"v1","type":"number"},{"id":"v2","type":"text"}]`)
	updated.Languages = []SurveyLanguage{lang("en"), lang("de"), lang("fr")}
	updated.FollowUps = append([]FollowUp{{ID: uuid.New()}}, old.FollowUps...)
	updated.Segment = &Segment{IsPrivate: false}

	captureEditorSave(UserCaller(user), &org, old, &updated)
	// An API key's save is not an editor save.
	captureEditorSave(APIKeyCaller(uuid.New()), &org, old, &updated)

	// Everything was captured above; once flushed, all of it has arrived, whatever order the concurrent sends finished in.
	rec.Flush()
	mine := func(event string, n int) []analyticstest.Message {
		t.Helper()
		var out []analyticstest.Message
		for _, m := range rec.All() {
			if m.Event == event && m.DistinctID == user.String() {
				out = append(out, m)
			}
		}
		if len(out) != n {
			t.Fatalf("%s sent %d times, want %d", event, len(out), n)
		}
		return out
	}
	if p := mine("hidden_field_added", 1)[0].Properties; p["field_count"] != float64(2) || p["survey_id"] != old.ID.String() || p["organization_id"] != org.String() {
		t.Errorf("hidden_field_added = %v", p)
	}
	if p := mine("conditional_logic_added", 1)[0].Properties; p["question_id"] != "b1" {
		t.Errorf("conditional_logic_added = %v", p)
	}
	if p := mine("variable_created", 1)[0].Properties; p["variable_type"] != "text" {
		t.Errorf("variable_created = %v", p)
	}
	if p := mine("survey_language_enabled", 1)[0].Properties; p["language_code"] != "de" || p["existing_language_count"] != float64(1) {
		t.Errorf("survey_language_enabled = %v", p)
	}
	if p := mine("survey_language_added", 1)[0].Properties; p["language_code"] != "fr" || p["existing_language_count"] != float64(2) {
		t.Errorf("survey_language_added = %v", p)
	}
	if p := mine("follow_up_added", 1)[0].Properties; p["follow_up_id"] != updated.FollowUps[0].ID.String() {
		t.Errorf("follow_up_added = %v", p)
	}
	published := mine("survey_published", 1)[0].Properties
	if published["question_count"] != float64(3) || published["has_targeting"] != true || published["language_count"] != float64(3) || published["survey_type"] != "link" {
		t.Errorf("survey_published = %v", published)
	}
	mine("survey_updated", 1)
}
