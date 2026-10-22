package response

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// The cases of forma2's modules/quotas/lib/utils.test.ts and evaluation-service.test.ts, and the quota paths of the client create and update tests, against a real database.

func linkStatus(e *env, responseID, quotaID uuid.UUID) string {
	e.t.Helper()
	var status []string
	if err := e.db.Raw(`SELECT status FROM response_quota_links WHERE response_id = ? AND quota_id = ?`, responseID, quotaID).Scan(&status).Error; err != nil {
		e.t.Fatal(err)
	}
	if len(status) == 0 {
		return ""
	}
	return status[0]
}

func TestAQuotaScreensResponsesInUntilItIsFull(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	q := e.quotaOn(sv, "Q", "yes", 2, actionEndSurvey, ptr("end1"), false)
	ctx := context.Background()

	for range 2 {
		res, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{Finished: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.QuotaFull.QuotaFull {
			t.Fatalf("quota full before its limit: %+v", res)
		}
		if s := linkStatus(e, res.ID, q); s != statusScreenedIn {
			t.Fatalf("link %q, want screenedIn", s)
		}
	}
	// The count equals the limit: full.
	res, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{Finished: false, TTC: map[string]float64{"q1": 300}})
	if err != nil {
		t.Fatal(err)
	}
	want := QuotaFull{QuotaFull: true, Quota: &FullQuota{ID: q, Action: actionEndSurvey, EndingCardID: "end1"}}
	if !reflect.DeepEqual(res.QuotaFull, want) {
		t.Fatalf("quota %+v, want %+v\n%s", res.QuotaFull, want, e.logs.String())
	}
	if s := linkStatus(e, res.ID, q); s != statusScreenedOut {
		t.Fatalf("link %q, want screenedOut", s)
	}
	// endSurvey finishes the response on the quota's ending, with its total.
	row := stored(e, res.ID)
	if row["finished"] != true || row["ending_id"] != "end1" || !reflect.DeepEqual(row["ttc"], map[string]any{"q1": 300.0, "_total": 300.0}) {
		t.Fatalf("row %v", row)
	}
	// As in forma2, the pipeline saw the response before the quota finished it.
	if got := pipelineEvents(e, res.ID); !reflect.DeepEqual(got, []string{EventResponseCreated}) {
		t.Fatalf("pipeline events %v", got)
	}
	// A response that does not match is not linked.
	other, err := e.submit(ctx, ws, sv, `{"q1":"no"}`, CreateInput{Finished: true})
	if err != nil {
		t.Fatal(err)
	}
	if other.QuotaFull.QuotaFull || linkStatus(e, other.ID, q) != "" {
		t.Fatalf("an unmatched response was screened: %+v", other)
	}
}

func TestPartialResponsesCountOnlyWhereTheQuotaSaysSo(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	finishedOnly := e.quotaOn(sv, "finished only", "yes", 1, actionContinueSurvey, nil, false)
	ctx := context.Background()

	partial, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{Finished: false})
	if err != nil {
		t.Fatal(err)
	}
	// A partial response is not linked to a quota that does not count partial submissions.
	if s := linkStatus(e, partial.ID, finishedOnly); s != "" {
		t.Fatalf("partial response linked %q", s)
	}
	// Finishing it screens it in.
	res, err := e.update(ctx, ws, partial.ID.String(), `{}`, UpdateInput{Finished: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if res.QuotaFull.QuotaFull || linkStatus(e, partial.ID, finishedOnly) != statusScreenedIn {
		t.Fatalf("finished response not screened in: %+v", res)
	}
	// A partial link only counts towards a quota counting partial submissions.
	counting := e.quotaOn(sv, "counting partial", "yes", 1, actionContinueSurvey, nil, true)
	second, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{Finished: false})
	if err != nil {
		t.Fatal(err)
	}
	if s := linkStatus(e, second.ID, counting); s != statusScreenedIn {
		t.Fatalf("partial response on a counting quota: %q", s)
	}
	// The first full quota by creation, newest first, is reported; continueSurvey leaves the response open.
	third, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{Finished: true})
	if err != nil {
		t.Fatal(err)
	}
	if third.Quota == nil || third.Quota.ID != counting || third.Quota.EndingCardID != "" {
		t.Fatalf("quota %+v, want the newest full quota", third.QuotaFull)
	}
	if stored(e, third.ID)["finished"] != true || linkStatus(e, third.ID, finishedOnly) != statusScreenedOut || linkStatus(e, third.ID, counting) != statusScreenedOut {
		t.Fatal("a finished response over both limits was not screened out of both")
	}
}

func TestAnUpdateRescreensTheMergedResponse(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	q := e.quotaOn(sv, "Q", "yes", 1, actionContinueSurvey, nil, true)
	ctx := context.Background()

	res, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if linkStatus(e, res.ID, q) != statusScreenedIn {
		t.Fatal("not screened in")
	}
	// An answer that no longer matches removes the link.
	if _, err := e.update(ctx, ws, res.ID.String(), `{"q1":"no"}`, UpdateInput{}); err != nil {
		t.Fatal(err)
	}
	if s := linkStatus(e, res.ID, q); s != "" {
		t.Fatalf("link %q after the answer changed", s)
	}

	full, err := e.submit(ctx, ws, sv, `{"q1":"yes"}`, CreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE response_quota_links SET status = 'screenedOut' WHERE response_id = ?`, full.ID)
	e.exec(`UPDATE survey_quotas SET "limit" = 100 WHERE id = ?`, q)
	// A screened-out response is never screened back in.
	if _, err := e.update(ctx, ws, full.ID.String(), `{"q2":"One"}`, UpdateInput{}); err != nil {
		t.Fatal(err)
	}
	if s := linkStatus(e, full.ID, q); s != statusScreenedOut {
		t.Fatalf("link %q, want it to stay screenedOut", s)
	}
}

func TestQuotaLanguageAndReservedOperands(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{defaultLanguage: "de-DE"})
	// q2's choice labels are matched in the response's language; the survey's default language reads as "default".
	choice := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'choice', 10,
		'{"connector":"and","conditions":[{"id":"c","leftOperand":{"type":"element","value":"q2"},"operator":"equals","rightOperand":{"type":"static","value":"c1"}}]}'::jsonb, 'continueSurvey') RETURNING id`, sv)
	country := e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'country', 10,
		'{"connector":"and","conditions":[{"id":"c","leftOperand":{"type":"reserved","value":"country"},"operator":"equals","rightOperand":{"type":"static","value":"DE"}}]}'::jsonb, 'continueSurvey') RETURNING id`, sv)
	ctx := context.Background()

	res, err := e.submit(ctx, ws, sv, `{"q2":"One"}`, CreateInput{Finished: true, Language: ptr("de-DE"), Request: RequestInfo{Country: "DE"}})
	if err != nil {
		t.Fatal(err)
	}
	if linkStatus(e, res.ID, choice) != statusScreenedIn || linkStatus(e, res.ID, country) != statusScreenedIn {
		t.Fatal("the default-language label or the reserved country did not match")
	}
	res, err = e.submit(ctx, ws, sv, `{"q2":"Eins"}`, CreateInput{Finished: true, Language: ptr("de"), Request: RequestInfo{Country: "FR"}})
	if err != nil {
		t.Fatal(err)
	}
	if linkStatus(e, res.ID, choice) != "" || linkStatus(e, res.ID, country) != "" {
		t.Fatal("a response in the default language's code matched the de label as another language")
	}
}

func TestAQuotaFailureNeverFailsTheResponse(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	e.insert(`INSERT INTO survey_quotas (survey_id, name, "limit", logic, action) VALUES (?, 'broken', 1, '"not a group"'::jsonb, 'continueSurvey') RETURNING id`, sv)
	res, err := e.submit(context.Background(), ws, sv, `{"q1":"yes"}`, CreateInput{Finished: true})
	if err != nil {
		t.Fatalf("a broken quota failed the response: %v", err)
	}
	if res.QuotaFull.QuotaFull || e.count(`SELECT count(*) FROM responses`) != 1 || len(pipelineEvents(e, res.ID)) != 2 {
		t.Fatal("the response did not commit with its pipeline events")
	}
	if !contains(e.logs.String(), "Error evaluating quotas for response") {
		t.Fatal("the quota failure was not logged")
	}
}

const actionContinueSurvey = "continueSurvey"

// A statement that fails inside the quota evaluation aborts only its savepoint: the response and its pipeline events still commit.
func TestAFailingQuotaStatementIsRolledBackToItsSavepoint(t *testing.T) {
	e := newEnv(t)
	_, ws := e.organization(e.user())
	sv := e.survey(ws, surveyOpts{})
	e.quotaOn(sv, "Q", "yes", 5, actionContinueSurvey, nil, true)
	e.exec(`CREATE FUNCTION refuse_quota_link() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'quota link refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse_quota_link BEFORE INSERT ON response_quota_links FOR EACH ROW EXECUTE FUNCTION refuse_quota_link()`)

	res, err := e.submit(context.Background(), ws, sv, `{"q1":"yes"}`, CreateInput{Finished: true})
	if err != nil {
		t.Fatalf("a failing quota statement failed the response: %v", err)
	}
	if e.count(`SELECT count(*) FROM responses WHERE id = ?`, res.ID) != 1 || len(pipelineEvents(e, res.ID)) != 2 || e.count(`SELECT count(*) FROM response_quota_links`) != 0 {
		t.Fatal("the response did not commit without its quota links")
	}
	if !contains(e.logs.String(), "quota link refused") {
		t.Fatal("the quota failure was not logged")
	}
}
