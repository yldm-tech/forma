package analysis

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// The filter offers the workspace's public segments by title, and the summary, its impressions and the export follow a segment filter: only the responses of the segment's contacts, and only the displays they answered.
func TestSegmentFilterNarrowsTheSummaryAndTheExport(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	org, ws := e.organization(owner, nil)
	sv := e.survey(ws, surveyOpts{blocks: summaryBlocks})
	pro, free := e.contact(ws, "plan", "pro"), e.contact(ws, "plan", "free")
	proTree := `[{"id":"n1","connector":null,"resource":{"id":"l1","root":{"type":"attribute","contactAttributeKey":"plan"},"value":"pro","qualifier":{"operator":"equals"}}}]`
	proSegment := e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, 'Pro customers', false, ?::jsonb) RETURNING id`, ws, proTree)
	allSegment := e.insert(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, 'All contacts', false, '[]'::jsonb) RETURNING id`, ws)
	e.exec(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, ?, true, '[]'::jsonb)`, ws, sv.String())
	e.exec(`INSERT INTO segments (workspace_id, title, is_private, filters) VALUES (?, 'Elsewhere', false, '[]'::jsonb)`, e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, org))

	d1, d2, d3 := e.display(sv, &pro, at(1)), e.display(sv, &free, at(2)), e.display(sv, nil, at(3))
	e.display(sv, &pro, at(4))
	e.response(sv, row{at: at(1), finished: true, display: &d1, contact: &pro, data: `{"q1":"Ada","nps":10}`})
	e.response(sv, row{at: at(2), finished: true, display: &d2, contact: &free, data: `{"q1":"Bob","nps":2}`})
	e.response(sv, row{at: at(3), finished: true, display: &d3, data: `{"q1":"Cy","nps":9}`})

	var data FilterData
	decode(t, e.request(owner, http.MethodGet, "/api/app/surveys/"+sv.String()+"/filter-data", nil), &data)
	if want := []FilterSegment{{ID: allSegment, Title: "All contacts"}, {ID: proSegment, Title: "Pro customers"}}; !reflect.DeepEqual(data.Segments, want) {
		t.Errorf("segments %+v, want %+v", data.Segments, want)
	}

	var s struct {
		Meta Meta `json:"meta"`
	}
	in := map[string]any{"filterCriteria": map[string]any{"segments": map[string]any{proSegment.String(): map[string]any{"op": "userIsIn"}}}}
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", in), &s)
	if s.Meta.TotalResponses != 1 || s.Meta.CompletedResponses != 1 || s.Meta.DisplayCount != 1 {
		t.Errorf("in segment meta %+v", s.Meta)
	}
	out := map[string]any{"filterCriteria": map[string]any{"segments": map[string]any{proSegment.String(): map[string]any{"op": "userIsNotIn"}}}}
	decode(t, e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", out), &s)
	if s.Meta.TotalResponses != 2 || s.Meta.DisplayCount != 2 {
		t.Errorf("not in segment meta %+v", s.Meta)
	}

	rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/responses/export", map[string]any{"format": "csv", "filterCriteria": in["filterCriteria"]})
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	records, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(rec.Body.Bytes(), []byte("\ufeff")))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("export rows %d, want a header and one response: %v", len(records), records)
	}
	found := false
	for _, cell := range records[1] {
		found = found || cell == "Ada"
	}
	if !found {
		t.Errorf("exported row %v is not Ada's", records[1])
	}

	// A malformed segment condition is refused.
	bad := map[string]any{"filterCriteria": map[string]any{"segments": map[string]any{uuid.NewString(): map[string]any{"op": "includes"}}}}
	if rec := e.request(owner, http.MethodPost, "/api/app/surveys/"+sv.String()+"/summary", bad); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed segment filter: %d %s", rec.Code, rec.Body)
	}
}
