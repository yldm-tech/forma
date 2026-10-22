package response

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/survey"
)

// The cases of forma2's put-response-handler.test.ts, V1R/[responseId]/lib/response.test.ts and lib/response/service.test.ts (updateResponse), against a real database.

func TestUpdateMergesOverWhatIsStored(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{hiddenFieldIDs: []string{"seats", "plan"}})
	e.field(ws, sv, "seats", "ingested", "number", false)
	e.field(ws, sv, "plan", "ingested", "string", false)
	e.contact(ws, "u1")
	ctx := context.Background()
	created, err := e.submit(ctx, ws, sv, `{"q1":"first","seats":"many","plan":"gold"}`, CreateInput{
		UserID: ptr("u1"), Variables: map[string]any{"a": 1.0}, TTC: map[string]float64{"q1": 100}, Language: ptr("en"),
		EndingID: ptr("end1"), Meta: ClientMeta{Source: ptr("link")},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := stored(e, created.ID)
	if !reflect.DeepEqual(before["ingest_flags"], []any{map[string]any{"key": "seats", "reason": "coercion_failed"}}) {
		t.Fatalf("ingest flags %v", before["ingest_flags"])
	}

	res, err := e.update(ctx, ws, created.ID.String(), `{"q2":"One","seats":"12"}`, UpdateInput{
		Finished: ptr(true), EndingIDSet: true, Variables: map[string]any{"b": "x"}, TTC: map[string]float64{"q2": 50}, Language: ptr("de"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != created.ID || res.QuotaFull.QuotaFull {
		t.Fatalf("result %+v", res)
	}
	row := stored(e, created.ID)
	want := map[string]any{
		"finished":     true,
		"ending_id":    nil,
		"data":         map[string]any{"q1": "first", "q2": "One", "seats": 12.0, "plan": "gold"},
		"variables":    map[string]any{"a": 1.0, "b": "x"},
		"ttc":          map[string]any{"q1": 100.0, "q2": 50.0, "_total": 150.0},
		"language":     "de-DE",
		"ingest_flags": []any{},
		"meta":         before["meta"],
		"contact_id":   before["contact_id"],
	}
	for k, v := range want {
		if !reflect.DeepEqual(row[k], v) {
			t.Errorf("%s = %#v, want %#v", k, row[k], v)
		}
	}
	if got := pipelineEvents(e, created.ID); !reflect.DeepEqual(got, []string{EventResponseCreated, EventResponseUpdated, EventResponseFinished}) {
		t.Fatalf("pipeline events %v", got)
	}
}

func TestAPartialUpdateLeavesTheRestAlone(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{hiddenFieldIDs: []string{"seats"}})
	e.field(ws, sv, "seats", "ingested", "number", false)
	ctx := context.Background()
	created, err := e.submit(ctx, ws, sv, `{"q1":"a","seats":"many"}`, CreateInput{TTC: map[string]float64{"q1": 10}, Language: ptr("fr"), EndingID: ptr("end1")})
	if err != nil {
		t.Fatal(err)
	}
	// No data: the flags of untouched keys stay; no finished: the ttc keeps no total; no endingId: the ending stays.
	if _, err := e.update(ctx, ws, created.ID.String(), "", UpdateInput{TTC: map[string]float64{"q2": 5}}); err != nil {
		t.Fatal(err)
	}
	row := stored(e, created.ID)
	if !reflect.DeepEqual(row["ttc"], map[string]any{"q1": 10.0, "q2": 5.0}) || row["ending_id"] != "end1" || row["language"] != "fr-FR" || row["finished"] != false {
		t.Fatalf("row %v", row)
	}
	if !reflect.DeepEqual(row["ingest_flags"], []any{map[string]any{"key": "seats", "reason": "coercion_failed"}}) {
		t.Fatalf("ingest flags %v", row["ingest_flags"])
	}
	if got := pipelineEvents(e, created.ID); !reflect.DeepEqual(got, []string{EventResponseCreated, EventResponseUpdated}) {
		t.Fatalf("pipeline events %v", got)
	}
}

func TestUpdateRefusals(t *testing.T) {
	e := newEnv(t)
	org, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	ctx := context.Background()

	_, _, err := e.svc.ResponseForUpdate(ctx, ws, "not-a-cuid")
	appErr(t, err, http.StatusBadRequest, "Validation failed: Invalid cuid2", map[string]any{})
	_, _, err = e.svc.ResponseForUpdate(ctx, ws, "clunknownresponseaaaaaaa1")
	appErr(t, err, http.StatusNotFound, "Response not found", map[string]any{"resource_id": "clunknownresponseaaaaaaa1", "resource_type": "Response"})

	created, err := e.submit(ctx, ws, sv, `{}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = e.svc.ResponseForUpdate(ctx, e.workspace(org), created.ID.String())
	appErr(t, err, http.StatusNotFound, "Response not found", map[string]any{"resource_id": created.ID.String(), "resource_type": "Response"})

	e.exec(`UPDATE surveys SET status = ? WHERE id = ?`, survey.StatusCompleted, sv)
	_, err = e.update(ctx, ws, created.ID.String(), `{}`, UpdateInput{Finished: ptr(true)})
	appErr(t, err, http.StatusForbidden, "Survey is not accepting submissions", map[string]any{"surveyId": sv.String()})

	e.exec(`UPDATE surveys SET status = ? WHERE id = ?`, survey.StatusInProgress, sv)
	if _, err := e.update(ctx, ws, created.ID.String(), `{}`, UpdateInput{Finished: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	_, err = e.update(ctx, ws, created.ID.String(), `{}`, UpdateInput{Finished: ptr(true)})
	appErr(t, err, http.StatusBadRequest, "Response is already finished", map[string]any{"code": CodeResponseAlreadyFinished})

	gone := uuid.New()
	sv2, err := e.svc.Survey(ctx, sv.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Update(ctx, sv2, gone.String(), UpdateInput{})
	appErr(t, err, http.StatusNotFound, "Response not found", map[string]any{"resource_id": gone.String(), "resource_type": "Response"})
}

func TestAMigratedResponseIsFoundByItsForma2ID(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	legacy := "clmigratedresponseaaaaa1"
	e.exec(`INSERT INTO responses (id, survey_id) VALUES (?, ?)`, legacyid.ID(legacy), sv)
	res, err := e.update(context.Background(), ws, legacy, `{"q1":"x"}`, UpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != legacyid.ID(legacy) {
		t.Fatalf("updated %s", res.ID)
	}
}

func TestUpdateDropsAClientSentVerifiedEmail(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{verifyEmail: true})
	ctx := context.Background()
	created, err := e.submit(ctx, ws, sv, `{}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	// The create path's email gate writes verifiedEmail after the ingest; stand in for it.
	e.exec(`UPDATE responses SET data = '{"verifiedEmail":"real@example.com"}' WHERE id = ?`, created.ID)
	if _, err := e.update(ctx, ws, created.ID.String(), `{"verifiedEmail":"forged@example.com","q1":"a"}`, UpdateInput{}); err != nil {
		t.Fatal(err)
	}
	if data := stored(e, created.ID)["data"]; !reflect.DeepEqual(data, map[string]any{"verifiedEmail": "real@example.com", "q1": "a"}) {
		t.Fatalf("data %v", data)
	}
}
