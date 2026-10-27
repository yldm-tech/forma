package contact

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Ported from forma2's segments/actions.test.ts, segments.test.ts and helper.test.ts.

func segmentPath(id uuid.UUID, rest string) string {
	return "/api/app/segments/" + id.String() + rest
}

func resetSegmentPath(surveyID uuid.UUID) string {
	return "/api/app/surveys/" + surveyID.String() + "/segment/reset"
}

var planIsPro = tree(node(nil, attributeLeaf("plan", "equals", "pro")))

func segmentRef(id uuid.UUID) []map[string]any {
	return tree(node(nil, map[string]any{"id": "segref", "root": map[string]any{"type": "segment", "segmentId": id.String()}, "value": "x", "qualifier": map[string]any{"operator": "userIsIn"}}))
}

func TestCreateSegment(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	survey := e.survey(ws, "inProgress")

	var seg Segment
	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Pro users", "description": "Paying", "isPrivate": false, "filters": planIsPro, "surveyId": ""}), http.StatusCreated, &seg)
	if seg.Title != "Pro users" || *seg.Description != "Paying" || seg.IsPrivate || seg.WorkspaceID != ws || len(seg.Surveys) != 0 {
		t.Fatalf("created %+v", seg)
	}
	var stored []map[string]any
	if err := json.Unmarshal(seg.Filters, &stored); err != nil || len(stored) != 1 {
		t.Fatalf("filters %s", seg.Filters)
	}
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Pro users", "isPrivate": false, "filters": planIsPro, "surveyId": ""}), http.StatusBadRequest, CodeSegmentTitleTaken, "")

	// A private segment is upserted by title: the survey editor saves its own segment again and again.
	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": survey.String(), "filters": planIsPro, "surveyId": survey.String()}), http.StatusCreated, &seg)
	if !seg.IsPrivate || len(seg.ActiveSurveys) != 1 || seg.ActiveSurveys[0].ID != survey {
		t.Fatalf("private %+v", seg)
	}
	first := seg.ID
	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": survey.String(), "filters": tree(node(nil, attributeLeaf("plan", "equals", "team"))), "surveyId": survey.String()}), http.StatusCreated, &seg)
	if seg.ID != first || !strings.Contains(string(seg.Filters), "team") {
		t.Fatalf("upsert %+v", seg)
	}

	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Empty", "filters": tree(), "surveyId": ""}), http.StatusBadRequest, codeInvalidFilters, "At least one filter is required")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Bad", "filters": tree(node(nil, attributeLeaf("age", "greaterThan", "x"))), "surveyId": ""}), http.StatusBadRequest, codeInvalidFilters, filterValueTypeMessage)
	e.fails(e.do(reader, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Mine", "filters": planIsPro, "surveyId": ""}), http.StatusForbidden, "forbidden", "")

	otherOwner := e.user()
	_, other := e.organization(otherOwner)
	foreignSurvey := e.survey(other, "draft")
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "X", "filters": planIsPro, "surveyId": foreignSurvey.String()}), http.StatusBadRequest, CodeWorkspaceMismatch, "Survey and segment are not in the same workspace")
	interaction := tree(node(nil, interactionLeaf("haveSeen", "specific", []string{foreignSurvey.String()}, 1, "days")))
	e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Seen", "isPrivate": false, "filters": interaction, "surveyId": ""}), http.StatusBadRequest, CodeSurveyNotFound, "Survey not found in workspace: "+foreignSurvey.String())
	interaction = tree(node(nil, interactionLeaf("haveSeen", "specific", []string{survey.String()}, 1, "days")))
	e.ok(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), map[string]any{"title": "Seen", "isPrivate": false, "filters": interaction, "surveyId": ""}), http.StatusCreated, nil)

	var all []Segment
	e.ok(e.do(reader, http.MethodGet, wsPath(ws, "/segments"), nil), http.StatusOK, &all)
	if len(all) != 3 {
		t.Fatalf("segments %+v", all)
	}
}

// forma2's ZSegmentCreateInput and ZSegmentUpdateInput take ZRequiredSegmentFilters whatever isPrivate says: an empty tree is refused for a private segment as for a public one, and nothing is written. The survey editor never sends one: an app survey without targeting is saved without a segment and the survey save gives it its empty private segment (survey saveSegment).
func TestSegmentFiltersAreRequiredPrivateOrPublic(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)

	for _, tc := range []struct {
		name      string
		isPrivate any
	}{
		{"private by default", nil},
		{"private", true},
		{"public", false},
	} {
		survey := e.survey(ws, "draft")
		body := map[string]any{"title": survey.String(), "filters": tree(), "surveyId": survey.String()}
		if tc.isPrivate != nil {
			body["isPrivate"] = tc.isPrivate
		}
		e.fails(e.do(owner, http.MethodPost, wsPath(ws, "/segments"), body), http.StatusBadRequest, codeInvalidFilters, atLeastOneFilterMessage)
		if n := e.count(`SELECT count(*) FROM segments WHERE workspace_id = ? AND title = ?`, ws, survey.String()); n != 0 {
			t.Fatalf("%s: a refused create wrote %d segments", tc.name, n)
		}
		if n := e.count(`SELECT count(*) FROM surveys WHERE id = ? AND segment_id IS NOT NULL`, survey); n != 0 {
			t.Fatalf("%s: a refused create linked the survey", tc.name)
		}
	}

	for _, isPrivate := range []bool{true, false} {
		seg := e.segmentRow(ws, uuid.NewString(), isPrivate, `[{"id":"n1","connector":null,"resource":{"id":"f1","root":{"type":"attribute","contactAttributeKey":"plan"},"value":"pro","qualifier":{"operator":"equals"}}}]`)
		e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": tree()}), http.StatusBadRequest, codeInvalidFilters, atLeastOneFilterMessage)
		if n := e.count(`SELECT jsonb_array_length(filters) FROM segments WHERE id = ?`, seg); n != 1 {
			t.Fatalf("a refused update of a segment (private %v) left %d filters", isPrivate, n)
		}
	}
}

func TestUpdateSegment(t *testing.T) {
	e := newEnv(t)
	owner, outsider := e.user(), e.user()
	_, ws := e.organization(owner)
	_, other := e.organization(outsider)
	active, draft := e.survey(ws, "inProgress"), e.survey(ws, "draft")
	seg := e.segmentRow(ws, "Pro", false, `[]`)
	ref := e.segmentRow(ws, "Ref", false, `[]`)

	var got Segment
	e.ok(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"title": "Pro users", "description": "x", "surveys": []uuid.UUID{active, draft}}), http.StatusOK, &got)
	if got.Title != "Pro users" || *got.Description != "x" || len(got.ActiveSurveys) != 1 || len(got.InactiveSurveys) != 1 || got.ActiveSurveys[0].ID != active {
		t.Fatalf("updated %+v", got)
	}
	e.ok(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"description": nil, "surveys": []uuid.UUID{}}), http.StatusOK, &got)
	if got.Description != nil || got.Title != "Pro users" || len(got.Surveys) != 2 {
		t.Fatalf("description cleared, surveys are only ever linked: %+v", got)
	}

	foreign := e.survey(other, "draft")
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"surveys": []uuid.UUID{foreign}}), http.StatusBadRequest, CodeWorkspaceMismatch, "Survey and segment are not in the same workspace")
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"surveys": []uuid.UUID{uuid.New()}}), http.StatusBadRequest, CodeWorkspaceMismatch, "")
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": tree()}), http.StatusBadRequest, codeInvalidFilters, "At least one filter is required")

	// Recursion: the segment itself, or through a segment that references it.
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": segmentRef(seg)}), http.StatusBadRequest, CodeRecursiveSegment, "Recursive segment filter is not allowed")
	raw, _ := json.Marshal(segmentRef(seg))
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(raw), ref)
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": segmentRef(ref)}), http.StatusBadRequest, CodeRecursiveSegment, "")
	missing := uuid.New()
	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": segmentRef(missing)}), http.StatusNotFound, "not_found", "segment with ID "+missing.String()+" not found")
	// A cycle among other segments ends the check instead of looping.
	a, b := e.segmentRow(ws, "A", false, `[]`), e.segmentRow(ws, "B", false, `[]`)
	rawA, _ := json.Marshal(segmentRef(b))
	rawB, _ := json.Marshal(segmentRef(a))
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(rawA), a)
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(rawB), b)
	e.ok(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"filters": segmentRef(a)}), http.StatusOK, nil)

	e.fails(e.do(owner, http.MethodPatch, segmentPath(seg, ""), map[string]any{"title": "Ref"}), http.StatusBadRequest, CodeSegmentTitleTaken, "")
	e.fails(e.do(outsider, http.MethodPatch, segmentPath(seg, ""), map[string]any{"title": "Mine"}), http.StatusForbidden, "forbidden", "")
	e.fails(e.do(owner, http.MethodPatch, segmentPath(uuid.New(), ""), map[string]any{"title": "Mine"}), http.StatusNotFound, "not_found", "")
}

func TestDeleteSegment(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	seg := e.segmentRow(ws, "Linked", false, `[]`)
	e.exec(`UPDATE surveys SET segment_id = ? WHERE id = ?`, seg, e.survey(ws, "draft"))
	free := e.segmentRow(ws, "Free", false, `[]`)
	unlinkedPrivate := e.segmentRow(ws, uuid.NewString(), true, `[]`)

	e.fails(e.do(owner, http.MethodDelete, segmentPath(seg, ""), nil), http.StatusForbidden, "operation_not_allowed", "Cannot delete a segment that is associated with a survey")
	for _, id := range []uuid.UUID{free, unlinkedPrivate} {
		e.ok(e.do(owner, http.MethodDelete, segmentPath(id, ""), nil), http.StatusOK, nil)
		if e.count(`SELECT count(*) FROM segments WHERE id = ?`, id) != 0 {
			t.Fatal("not deleted")
		}
	}
}

// Deleting a segment is not refused for the results links whose frozen filter names it, but the people who may delete it can list them first, so the dialog can say which links stop showing what they were created for.
func TestSegmentResultSharesListTheLinksFilteringByIt(t *testing.T) {
	e := newEnv(t)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader)
	seg, other := e.segmentRow(ws, "Pro", false, `[]`), e.segmentRow(ws, "Free", false, `[]`)
	alpha := e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Alpha', 'inProgress') RETURNING id`, ws)
	beta := e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Beta', 'completed') RETURNING id`, ws)
	link := func(survey uuid.UUID, name, criteria string) uuid.UUID {
		return e.insert(`INSERT INTO survey_result_shares (survey_id, share_key, name, filter_criteria) VALUES (?, ?, ?, ?::jsonb) RETURNING id`,
			survey, strings.Repeat(string(name[0]), 43), name, criteria)
	}
	board := link(beta, "Board", `{"segments":{"`+seg.String()+`":{"op":"userIsNotIn"}}}`)
	team := link(alpha, "Team", `{"finished":true,"segments":{"`+seg.String()+`":{"op":"userIsIn"},"`+other.String()+`":{"op":"userIsIn"}}}`)
	link(alpha, "Everyone", `{}`)
	link(alpha, "Free", `{"segments":{"`+other.String()+`":{"op":"userIsIn"}}}`)

	var got []SegmentResultShare
	e.ok(e.do(owner, http.MethodGet, segmentPath(seg, "/result-shares"), nil), http.StatusOK, &got)
	want := []SegmentResultShare{{ID: team, Name: "Team", SurveyID: alpha, SurveyName: "Alpha"}, {ID: board, Name: "Board", SurveyID: beta, SurveyName: "Beta"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links %+v, want %+v", got, want)
	}
	e.fails(e.do(reader, http.MethodGet, segmentPath(seg, "/result-shares"), nil), http.StatusForbidden, "forbidden", "")
	e.fails(e.do(owner, http.MethodGet, segmentPath(uuid.New(), "/result-shares"), nil), http.StatusNotFound, "not_found", "")

	// The links stay when the segment goes.
	e.ok(e.do(owner, http.MethodDelete, segmentPath(seg, ""), nil), http.StatusOK, nil)
	if n := e.count(`SELECT count(*) FROM survey_result_shares WHERE id IN (?, ?)`, board, team); n != 2 {
		t.Errorf("%d links left", n)
	}
}

func TestCloneSegment(t *testing.T) {
	e := newEnv(t)
	owner, outsider := e.user(), e.user()
	_, ws := e.organization(owner)
	_, other := e.organization(outsider)
	raw, _ := json.Marshal(planIsPro)
	seg := e.segmentRow(ws, "Pro", false, string(raw))
	e.segmentRow(ws, "Copy of Pro (9)", false, `[]`)
	e.segmentRow(ws, "Copy of Pro (10)", false, `[]`)
	e.segmentRow(ws, "Copy of Pro", false, `[]`)
	survey := e.survey(ws, "draft")

	var clone Segment
	e.ok(e.do(owner, http.MethodPost, segmentPath(seg, "/clone"), map[string]any{"surveyId": survey}), http.StatusCreated, &clone)
	if clone.Title != "Copy of Pro (11)" || clone.IsPrivate || len(clone.Surveys) != 1 || clone.Surveys[0] != survey || !strings.Contains(string(clone.Filters), "pro") {
		t.Fatalf("clone %+v", clone)
	}
	e.fails(e.do(owner, http.MethodPost, segmentPath(seg, "/clone"), map[string]any{"surveyId": e.survey(other, "draft")}), http.StatusBadRequest, CodeWorkspaceMismatch, "Segment and survey are not in the same workspace")
}

func TestResetSurveySegment(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	survey := e.survey(ws, "draft")

	var seg Segment
	e.ok(e.do(owner, http.MethodPost, resetSegmentPath(survey), nil), http.StatusOK, &seg)
	if seg.Title != survey.String() || !seg.IsPrivate || string(seg.Filters) != "[]" || len(seg.Surveys) != 1 {
		t.Fatalf("created private segment %+v", seg)
	}
	private := seg.ID
	raw, _ := json.Marshal(planIsPro)
	e.exec(`UPDATE segments SET filters = ? WHERE id = ?`, string(raw), private)
	e.ok(e.do(owner, http.MethodPost, resetSegmentPath(survey), nil), http.StatusOK, &seg)
	if seg.ID != private || string(seg.Filters) != "[]" {
		t.Fatalf("reset %+v", seg)
	}

	outsider := e.user()
	e.organization(outsider)
	e.fails(e.do(outsider, http.MethodPost, resetSegmentPath(survey), nil), http.StatusForbidden, "forbidden", "")
	e.fails(e.do(owner, http.MethodPost, resetSegmentPath(uuid.New()), nil), http.StatusNotFound, "not_found", "")
}

func TestSurveyFilterRefs(t *testing.T) {
	e := newEnv(t)
	owner := e.user()
	_, ws := e.organization(owner)
	first := e.survey(ws, "draft")
	second := e.survey(ws, "inProgress")
	e.exec(`UPDATE surveys SET updated_at = now() + interval '1 minute' WHERE id = ?`, first)

	var refs []SurveyFilterRef
	e.ok(e.do(owner, http.MethodGet, wsPath(ws, "/segment-surveys"), nil), http.StatusOK, &refs)
	if len(refs) != 2 || refs[0].ID != first || refs[1].ID != second || refs[1].Status != "inProgress" {
		t.Fatalf("refs %+v", refs)
	}
}
