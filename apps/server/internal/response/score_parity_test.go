package response

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// scoreParity is testdata/score_parity.json, the table packages/surveys/src/lib/score.parity.test.ts runs the survey runtime over.
type scoreParity struct {
	Elements []struct {
		Name    string            `json:"name"`
		Element surveydoc.Element `json:"element"`
		Answer  any               `json:"answer"`
		Score   *float64          `json:"score"`
	} `json:"elements"`
	Surveys []struct {
		Name           string               `json:"name"`
		Blocks         []surveydoc.Block    `json:"blocks"`
		Variables      []surveydoc.Variable `json:"variables"`
		EmbeddedFields []struct {
			Field struct {
				Name         string `json:"name"`
				Source       string `json:"source"`
				DataType     string `json:"dataType"`
				DefaultValue any    `json:"defaultValue"`
			} `json:"field"`
			Link struct {
				StorageKey string `json:"storageKey"`
			} `json:"link"`
		} `json:"embeddedFields"`
		Data  map[string]any     `json:"data"`
		TTC   map[string]float64 `json:"ttc"`
		Meta  map[string]any     `json:"meta"`
		Path  []string           `json:"path"`
		Score float64            `json:"score"`
	} `json:"surveys"`
}

func loadScoreParity(t *testing.T) scoreParity {
	t.Helper()
	raw, err := os.ReadFile("testdata/score_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var p scoreParity
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Elements) == 0 || len(p.Surveys) == 0 {
		t.Fatal("empty parity table")
	}
	return p
}

func TestElementScoreParity(t *testing.T) {
	for _, c := range loadScoreParity(t).Elements {
		t.Run(c.Name, func(t *testing.T) {
			score, ok := elementScore(c.Element, c.Answer)
			switch {
			case c.Score == nil && ok:
				t.Fatalf("scored %v, want not scored", score)
			case c.Score != nil && (!ok || score != *c.Score):
				t.Fatalf("score %v (scored %v), want %v", score, ok, *c.Score)
			}
		})
	}
}

func TestRecomputeScoreParity(t *testing.T) {
	for _, c := range loadScoreParity(t).Surveys {
		t.Run(c.Name, func(t *testing.T) {
			sv := &Survey{blocks: c.Blocks, variables: c.Variables}
			for _, f := range c.EmbeddedFields {
				sv.Fields = append(sv.Fields, embeddeddata.Linked{Field: embeddeddata.Field{
					StorageKey: f.Link.StorageKey, Name: f.Field.Name, Source: f.Field.Source, DataType: f.Field.DataType, DefaultValue: f.Field.DefaultValue,
				}})
			}
			id, score, path, ok := sv.RecomputeScore(ScoreInput{Data: c.Data, TTC: c.TTC, Language: "default", Reserved: embeddeddata.ClientValues(embeddeddata.ValueResponse{Data: c.Data, Meta: c.Meta}, sv.Fields, sv.ElementIDs())})
			if !ok || id != surveydoc.ScoreVariable(c.Variables).ID {
				t.Fatalf("recomputed %q %v", id, ok)
			}
			if !slices.Equal(path, c.Path) {
				t.Errorf("path %v, want %v", path, c.Path)
			}
			if score != c.Score {
				t.Errorf("score %v, want %v", score, c.Score)
			}
		})
	}
}
