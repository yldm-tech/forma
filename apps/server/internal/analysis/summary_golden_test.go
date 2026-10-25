package analysis

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/response"
)

// goldenCase is a dataset of testdata/summary: a survey, its responses newest first, the display count and quotas, and the summary (meta, drop-off, quotas and one entry per element) expected for them.
type goldenCase struct {
	Survey struct {
		Blocks    json.RawMessage `json:"blocks"`
		Languages []struct {
			Language struct {
				Code string `json:"code"`
			} `json:"language"`
			Default bool `json:"default"`
		} `json:"languages"`
		WelcomeCard struct {
			Enabled bool `json:"enabled"`
		} `json:"welcomeCard"`
		EmbeddedFields []struct {
			StorageKey string `json:"storageKey"`
			Name       string `json:"name"`
			Source     string `json:"source"`
			DataType   string `json:"dataType"`
		} `json:"embeddedFields"`
	} `json:"survey"`
	Responses []struct {
		ID                uuid.UUID         `json:"id"`
		Data              map[string]any    `json:"data"`
		UpdatedAt         time.Time         `json:"updatedAt"`
		Contact           *response.Contact `json:"contact"`
		ContactAttributes json.RawMessage   `json:"contactAttributes"`
		Language          *string           `json:"language"`
		TTC               map[string]any    `json:"ttc"`
		Finished          bool              `json:"finished"`
	} `json:"responses"`
	DisplayCount int             `json:"displayCount"`
	Quotas       []QuotaProgress `json:"quotas"`
	Expected     json.RawMessage `json:"expected"`
}

func (g *goldenCase) doc(t *testing.T) *surveyDoc {
	t.Helper()
	sv := &surveyDoc{Survey: &response.Survey{}, WelcomeCardEnabled: g.Survey.WelcomeCard.Enabled}
	if err := sv.readBlocks(g.Survey.Blocks); err != nil {
		t.Fatal(err)
	}
	for _, l := range g.Survey.Languages {
		sv.Languages = append(sv.Languages, surveyLanguage{Code: l.Language.Code, Default: l.Default})
	}
	for _, f := range g.Survey.EmbeddedFields {
		sv.Fields = append(sv.Fields, embeddeddata.Linked{Field: embeddeddata.Field{StorageKey: f.StorageKey, Name: f.Name, Source: f.Source, DataType: f.DataType}})
	}
	return sv
}

// TestSummaryMatchesGolden runs the summary over seeded datasets and compares every number, sample and ordering with the expected summary, which the analysis pages show: unexpected rating and NPS values opening NaN buckets, numeric choice labels sorted first, answers in a second language, dismissals, "Other" and "None", matrix answers in either language, ranking with unknown items, CTA impressions, hidden fields, truncated samples and the drop-off table with the welcome card on and off.
func TestSummaryMatchesGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/summary/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden datasets: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var g goldenCase
			if err := json.Unmarshal(raw, &g); err != nil {
				t.Fatal(err)
			}
			agg := newSummarizer(g.doc(t))
			for _, r := range g.Responses {
				attributes := r.ContactAttributes
				if attributes == nil {
					attributes = json.RawMessage(`{}`)
				}
				agg.add(&summaryResponse{ID: r.ID, UpdatedAt: r.UpdatedAt, Data: r.Data, TTC: r.TTC, Contact: r.Contact, ContactAttributes: attributes, Language: r.Language, Finished: r.Finished})
			}
			got, err := json.Marshal(agg.result(g.DisplayCount, g.Quotas))
			if err != nil {
				t.Fatal(err)
			}
			var gotValue, wantValue map[string]any
			if err := json.Unmarshal(got, &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(g.Expected, &wantValue); err != nil {
				t.Fatal(err)
			}
			for _, section := range []string{"meta", "dropOff", "quotas"} {
				if !reflect.DeepEqual(gotValue[section], wantValue[section]) {
					t.Errorf("%s:\n got %s\nwant %s", section, mustJSON(gotValue[section]), mustJSON(wantValue[section]))
				}
			}
			gotSummary, _ := gotValue["summary"].([]any)
			wantSummary, _ := wantValue["summary"].([]any)
			if len(gotSummary) != len(wantSummary) {
				t.Fatalf("summary has %d entries, want %d", len(gotSummary), len(wantSummary))
			}
			for i := range wantSummary {
				if !reflect.DeepEqual(gotSummary[i], wantSummary[i]) {
					t.Errorf("summary[%d] (%s):\n got %s\nwant %s", i, entryName(wantSummary[i]), mustJSON(gotSummary[i]), mustJSON(wantSummary[i]))
				}
			}
		})
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func entryName(v any) string {
	m, _ := v.(map[string]any)
	if el, ok := m["element"].(map[string]any); ok {
		return fmt.Sprint(el["id"])
	}
	return fmt.Sprint(m["type"], " ", m["id"])
}
