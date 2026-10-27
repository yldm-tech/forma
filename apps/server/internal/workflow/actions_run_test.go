package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/response"
)

// contactFixture is a contact of the workspace with a plan attribute, a number attribute and the unique email key.
type contactFixture struct {
	contact, plan, seats uuid.UUID
}

func (e *env) contactFixture(f fixture) contactFixture {
	e.t.Helper()
	c := e.insert(`INSERT INTO contacts (workspace_id) VALUES (?) RETURNING id`, f.ws)
	plan := e.insert(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'plan', 'Plan', 'custom', 'string') RETURNING id`, f.ws)
	seats := e.insert(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'seats', 'Seats', 'custom', 'number') RETURNING id`, f.ws)
	e.exec(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, is_unique) VALUES (?, 'email', 'Email', 'default', true)`, f.ws)
	e.exec(`INSERT INTO contact_attributes (contact_id, attribute_key_id, value_text) VALUES (?, ?, 'free')`, c, plan)
	return contactFixture{contact: c, plan: plan, seats: seats}
}

// respondAs stores a finished response of the contact.
func (e *env) respondAs(f fixture, contactID *uuid.UUID, data string) uuid.UUID {
	e.t.Helper()
	return e.insert(`INSERT INTO responses (survey_id, finished, data, contact_id, contact_attributes) VALUES (?, true, ?::jsonb, ?, '{"email":"jane@example.com"}') RETURNING id`, f.survey, data, contactID)
}

// startRun enqueues the response's runs and returns the one of the workflow.
func (e *env) startRun(f fixture, workflowID, responseID uuid.UUID) uuid.UUID {
	e.t.Helper()
	r, err := response.Load(context.Background(), e.db, responseID)
	if err != nil || r == nil {
		e.t.Fatalf("load response: %v", err)
	}
	if err := e.enqueuer.EnqueueResponseCompleted(context.Background(), e.event(f, *r)); err != nil {
		e.t.Fatal(err)
	}
	return e.runFor(workflowID, responseID)
}

func TestRunActions(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	cf := e.contactFixture(f)
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'VIP') RETURNING id`, f.ws)
	rec, url := serve(t, http.StatusOK)
	def := chain(
		tagNode("tag", tag.String()),
		attributeNode("plan", "plan", "{{answers.q1}}"),
		attributeNode("seats", "seats", "{{answers.seats}}"),
		httpNode("notify", url+"/hook", map[string]any{"name": "Authorization", "secret": map[string]any{"value": "Bearer abcdefghijkl"}}),
	)
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	wf := e.enabled(f, "Act", def)

	responseID := e.respondAs(f, &cf.contact, `{"q1":"enterprise","seats":12}`)
	run := e.startRun(f, wf, responseID)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatalf("%v\n%s", err, e.errorLogs())
	}
	if status := e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run); status != RunCompleted {
		t.Fatalf("run %s: %s\n%s", status, e.text(`SELECT error FROM workflow_runs WHERE id = ?`, run), e.errorLogs())
	}
	if e.count(`SELECT count(*) FROM response_tags WHERE response_id = ? AND tag_id = ?`, responseID, tag) != 1 {
		t.Fatal("the response was not tagged")
	}
	if got := e.text(`SELECT value_text FROM contact_attributes WHERE contact_id = ? AND attribute_key_id = ?`, cf.contact, cf.plan); got != "enterprise" {
		t.Fatalf("plan %q", got)
	}
	if got := e.text(`SELECT value_number::text FROM contact_attributes WHERE contact_id = ? AND attribute_key_id = ?`, cf.contact, cf.seats); got != "12" {
		t.Fatalf("seats %q", got)
	}
	if rec.count() != 1 || rec.bodies[0] != `{"answer":"enterprise"}` || rec.requests[0].Header.Get("Authorization") != "Bearer abcdefghijkl" ||
		rec.requests[0].Header.Get("Webhook-Id") != stepMessageID(run, "notify") {
		t.Fatalf("request %d %v %v", rec.count(), rec.bodies, rec.requests)
	}
	var logs []RunLog
	e.db.Raw(`SELECT sequence, step_id, step_type, status, input, output FROM workflow_run_logs WHERE run_id = ? ORDER BY sequence`, run).Scan(&logs)
	if len(logs) != 4 || logs[0].StepType != ActionTagResponse || logs[3].StepType != ActionHTTPRequest || !strings.Contains(string(logs[3].Output), `"status": 200`) ||
		strings.Contains(string(logs[3].Input), "/hook") || !strings.Contains(string(logs[0].Output), `"added": true`) {
		t.Fatalf("logs %+v", logs)
	}
	for _, l := range logs {
		if l.Status != StepSucceeded {
			t.Fatalf("step %s %s", l.StepID, l.Status)
		}
	}

	// Another delivery of the finished run does nothing again.
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil || rec.count() != 1 {
		t.Fatalf("a redelivery acted again: %v, %d requests", err, rec.count())
	}

	// A response without a contact skips the attribute steps and the run still completes.
	anonymous := e.respondAs(f, nil, `{"q1":"x","seats":1}`)
	run2 := e.startRun(f, wf, anonymous)
	if err := e.runner.Work(context.Background(), runJob(run2, wf, f.ws, 1)); err != nil {
		t.Fatal(err)
	}
	if e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run2) != RunCompleted || e.text(`SELECT status FROM workflow_run_logs WHERE run_id = ? AND step_id = 'plan'`, run2) != StepSkipped {
		t.Fatalf("anonymous run %s", e.text(`SELECT data::text FROM workflow_runs WHERE id = ?`, run2))
	}
}

// A step that keeps failing transiently is retried by the run, which resumes after its succeeded steps; the request carries the same message id every time, so the receiver can drop a repeat.
func TestRunActionRetriesAndPermanentFailures(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'Seen') RETURNING id`, f.ws)
	rec, url := serve(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK)
	def := chain(tagNode("tag", tag.String()), httpNode("notify", url+"/hook"))
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	wf := e.enabled(f, "Retry", def)
	responseID := e.respondAs(f, nil, `{"q1":"a"}`)
	run := e.startRun(f, wf, responseID)

	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err == nil || err.Error() != "Status 503" {
		t.Fatalf("first attempt: %v", err)
	}
	if rec.count() != 3 || e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run) != RunRunning {
		t.Fatalf("after the first attempt: %d requests, run %s", rec.count(), e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run))
	}
	e.exec(`DELETE FROM response_tags WHERE response_id = ?`, responseID)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err != nil {
		t.Fatal(err)
	}
	if e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run) != RunCompleted || rec.count() != 4 {
		t.Fatalf("after the retry: %d requests", rec.count())
	}
	if e.count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, responseID) != 0 {
		t.Fatal("the succeeded tag step ran again on the retry")
	}
	for _, r := range rec.requests {
		if r.Header.Get("Webhook-Id") != stepMessageID(run, "notify") {
			t.Fatalf("message ids differ between deliveries: %s", r.Header.Get("Webhook-Id"))
		}
	}

	// A 4xx is not retried: the first attempt fails the run.
	rejecting, rejectURL := serve(t, http.StatusUnprocessableEntity)
	def = chain(httpNode("notify", rejectURL))
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	wf2 := e.enabled(f, "Rejected", def)
	other := e.respondAs(f, nil, `{"q1":"b"}`)
	run2 := e.startRun(f, wf2, other)
	if err := e.runner.Work(context.Background(), runJob(run2, wf2, f.ws, 1)); err != nil {
		t.Fatalf("a permanent failure asked for a retry: %v", err)
	}
	if e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run2) != RunFailed || e.text(`SELECT error FROM workflow_runs WHERE id = ?`, run2) != "Status 422" || rejecting.count() != 1 {
		t.Fatalf("run %s after %d requests", e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run2), rejecting.count())
	}

	// A value the key's type does not take, or a key that became unique or was deleted after enable, fails its step for good.
	cf := e.contactFixture(f)
	later := e.insert(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'later', 'Later', 'custom', 'string') RETURNING id`, f.ws)
	gone := e.insert(`INSERT INTO contact_attribute_keys (workspace_id, key, name, type, data_type) VALUES (?, 'gone', 'Gone', 'custom', 'string') RETURNING id`, f.ws)
	for _, tc := range []struct {
		key, value, want string
		afterEnable      func()
	}{
		{"seats", "{{answers.q1}}", "Attribute 'seats' expects a number", nil},
		{"later", "x", `Attribute "later" is unique to each contact and cannot be set by a workflow`, func() { e.exec(`UPDATE contact_attribute_keys SET is_unique = true WHERE id = ?`, later) }},
		{"gone", "x", `Attribute "gone" does not exist in this workspace`, func() { e.exec(`DELETE FROM contact_attribute_keys WHERE id = ?`, gone) }},
	} {
		def = chain(attributeNode("attr", tc.key, tc.value))
		def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
		wf := e.enabled(f, "Attribute "+tc.key, def)
		if tc.afterEnable != nil {
			tc.afterEnable()
		}
		r := e.respondAs(f, &cf.contact, `{"q1":"many"}`)
		run := e.startRun(f, wf, r)
		if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
			t.Fatalf("%s: %v", tc.key, err)
		}
		if got := e.text(`SELECT error FROM workflow_runs WHERE id = ?`, run); e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run) != RunFailed || !strings.HasPrefix(got, tc.want) {
			t.Fatalf("%s: %q", tc.key, got)
		}
	}

	// A tag deleted after enable fails its step for good.
	goneTag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'Gone') RETURNING id`, f.ws)
	def = chain(tagNode("tag", goneTag.String()))
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	wf3 := e.enabled(f, "Deleted tag", def)
	e.exec(`DELETE FROM tags WHERE id = ?`, goneTag)
	third := e.respondAs(f, nil, `{}`)
	run3 := e.startRun(f, wf3, third)
	if err := e.runner.Work(context.Background(), runJob(run3, wf3, f.ws, 1)); err != nil || e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run3) != RunFailed {
		t.Fatalf("deleted tag: %v", err)
	}
}

// continueOnFailure sets an action node to let the run go on past its failure.
func continueOnFailure(n map[string]any) map[string]any {
	n["continueOnFailure"] = true
	return n
}

// A failed step set to continue on failure does not stop the steps after it: they run at once, the failed step gets the run's retries (the steps that succeeded are not repeated), and the run then completes with the failure recorded on it and on the step's log. A step without the option still fails the run, even after a continued failure.
func TestRunContinuesPastFailedStep(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'After') RETURNING id`, f.ws)
	start := func(name string, def map[string]any) (uuid.UUID, uuid.UUID, uuid.UUID) {
		t.Helper()
		def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
		wf := e.enabled(f, name, def)
		responseID := e.respondAs(f, nil, `{"q1":"a"}`)
		return wf, e.startRun(f, wf, responseID), responseID
	}
	runState := func(run uuid.UUID) (string, string) {
		return e.text(`SELECT status FROM workflow_runs WHERE id = ?`, run), e.text(`SELECT coalesce(error, '') FROM workflow_runs WHERE id = ?`, run)
	}

	// A transient failure: retried with the run, then the run completes.
	failing, failingURL := serve(t, http.StatusServiceUnavailable)
	wf, run, responseID := start("Continue transient", chain(continueOnFailure(httpNode("notify", failingURL)), tagNode("tag", tag.String())))
	const want = "Continued after a failed step: Status 503"
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err == nil || err.Error() != want {
		t.Fatalf("first attempt: %v\n%s", err, e.errorLogs())
	}
	if status, msg := runState(run); status != RunRunning || msg != want {
		t.Fatalf("after the first attempt: run %s %q", status, msg)
	}
	if e.count(`SELECT count(*) FROM response_tags WHERE response_id = ? AND tag_id = ?`, responseID, tag) != 1 {
		t.Fatal("the step after the failed one did not run")
	}
	e.exec(`DELETE FROM response_tags WHERE response_id = ?`, responseID)
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err == nil || err.Error() != want {
		t.Fatalf("second attempt: %v", err)
	}
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 3)); err != nil {
		t.Fatalf("last attempt: %v", err)
	}
	if status, msg := runState(run); status != RunCompleted || msg != want {
		t.Fatalf("after the last attempt: run %s %q", status, msg)
	}
	if failing.count() != 9 {
		t.Fatalf("the failed step was sent %d times, want 3 per attempt", failing.count())
	}
	if e.count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, responseID) != 0 {
		t.Fatal("the succeeded step ran again on a retry")
	}
	var logs []RunLog
	e.db.Raw(`SELECT step_id, status, input, error FROM workflow_run_logs WHERE run_id = ? ORDER BY sequence`, run).Scan(&logs)
	if len(logs) != 2 || logs[0].Status != StepFailed || logs[0].Error == nil || *logs[0].Error != "Status 503" || !strings.Contains(string(logs[0].Input), `"continueOnFailure": true`) ||
		logs[1].Status != StepSucceeded || strings.Contains(string(logs[1].Input), "continueOnFailure") {
		t.Fatalf("logs %+v", logs)
	}
	data := e.text(`SELECT data::text FROM workflow_runs WHERE id = ?`, run)
	if !strings.Contains(data, `"status": "failed"`) || !strings.Contains(data, `"status": "succeeded"`) {
		t.Fatalf("run data %s", data)
	}

	// A permanent failure: no retry can change it, so the run completes on the first attempt.
	_, rejectURL := serve(t, http.StatusUnprocessableEntity)
	wf, run, responseID = start("Continue permanent", chain(continueOnFailure(httpNode("notify", rejectURL)), tagNode("tag", tag.String())))
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatalf("a permanent continued failure asked for a retry: %v", err)
	}
	if status, msg := runState(run); status != RunCompleted || msg != "Continued after a failed step: Status 422" ||
		e.count(`SELECT count(*) FROM response_tags WHERE response_id = ? AND tag_id = ?`, responseID, tag) != 1 {
		t.Fatalf("permanent: run %s %q", status, msg)
	}

	// Two continued failures: the run names the first and counts the rest.
	wf, run, _ = start("Continue twice", chain(continueOnFailure(httpNode("a", rejectURL)), continueOnFailure(httpNode("b", rejectURL)), tagNode("tag", tag.String())))
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatal(err)
	}
	if status, msg := runState(run); status != RunCompleted || msg != "Continued after a failed step: Status 422 (and 1 more)" {
		t.Fatalf("twice: run %s %q", status, msg)
	}

	// A transient failure a retry clears: the run completes clean, without the error the first attempt recorded for it.
	flaky, flakyURL := serve(t, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusOK)
	wf, run, _ = start("Continue then recover", chain(continueOnFailure(httpNode("notify", flakyURL)), tagNode("tag", tag.String())))
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err == nil || err.Error() != want {
		t.Fatalf("recover, first attempt: %v", err)
	}
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 2)); err != nil {
		t.Fatalf("recover, second attempt: %v", err)
	}
	if status, msg := runState(run); status != RunCompleted || msg != "" || flaky.count() != 4 ||
		e.count(`SELECT count(*) FROM workflow_runs WHERE id = ? AND last_error_at IS NULL`, run) != 1 {
		t.Fatalf("recover: run %s %q, %d requests", status, msg, flaky.count())
	}

	// A permanent failure beside a transient one: the run's retries are for the transient step, and the step that failed for good is never sent again.
	transient, transientURL := serve(t, http.StatusServiceUnavailable)
	rejecting, rejectingURL := serve(t, http.StatusUnprocessableEntity)
	wf, run, _ = start("Continue mixed", chain(continueOnFailure(httpNode("a", rejectingURL)), continueOnFailure(httpNode("b", transientURL)), tagNode("tag", tag.String())))
	const mixed = "Continued after a failed step: Status 422 (and 1 more)"
	for attempt := 1; attempt <= 3; attempt++ {
		err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, attempt))
		if attempt < 3 && (err == nil || err.Error() != mixed) || attempt == 3 && err != nil {
			t.Fatalf("mixed, attempt %d: %v", attempt, err)
		}
	}
	if status, msg := runState(run); status != RunCompleted || msg != mixed || rejecting.count() != 1 || transient.count() != 9 {
		t.Fatalf("mixed: run %s %q, the permanent step sent %d times, the transient one %d", status, msg, rejecting.count(), transient.count())
	}
	if e.count(`SELECT count(*) FROM workflow_run_logs WHERE run_id = ? AND step_id = 'a' AND status = 'failed' AND permanent_failure`, run) != 1 ||
		e.count(`SELECT count(*) FROM workflow_run_logs WHERE run_id = ? AND step_id = 'b' AND status = 'failed' AND NOT permanent_failure`, run) != 1 {
		t.Fatal("the step logs do not record which failure was permanent")
	}

	// A step without the option still fails the run, after a continued failure too, and the steps after it never run.
	wf, run, responseID = start("Stop after continue", chain(continueOnFailure(httpNode("a", rejectURL)), httpNode("b", rejectURL), tagNode("tag", tag.String())))
	if err := e.runner.Work(context.Background(), runJob(run, wf, f.ws, 1)); err != nil {
		t.Fatal(err)
	}
	if status, msg := runState(run); status != RunFailed || msg != "Status 422" ||
		e.count(`SELECT count(*) FROM response_tags WHERE response_id = ?`, responseID) != 0 ||
		e.count(`SELECT count(*) FROM workflow_run_logs WHERE run_id = ?`, run) != 2 {
		t.Fatalf("stop: run %s %q", status, msg)
	}
}

func TestHTTPRequestPermissionsAndSecrets(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	writer := e.user("writer@example.com")
	e.member(f.org, writer, "member")
	e.grant(f.org, f.ws, writer, "readWrite")
	manager := e.user("manager@example.com")
	e.member(f.org, manager, "member")
	e.grant(f.org, f.ws, manager, "manage")
	_, url := serve(t, http.StatusOK)

	withHTTP := chain(httpNode("notify", url+"/hook", map[string]any{"name": "Authorization", "secret": map[string]any{"value": "Bearer abcdefghijkl"}}))
	e.problem(e.v3(writer, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": f.ws, "name": "W", "definition": withHTTP}), http.StatusForbidden, "forbidden", httpRequestsManaged)
	created := e.create(writer, f.ws, "Tag only", chain(tagNode("t", uuid.NewString())))
	if created.ID == uuid.Nil {
		t.Fatal("a writer cannot create a workflow without an HTTP request")
	}

	w := e.create(manager, f.ws, "With HTTP", withHTTP)
	if strings.Contains(string(w.Definition), "abcdefghijkl") || !strings.Contains(string(w.Definition), `"hint":"…ijkl"`) {
		t.Fatalf("the secret came back: %s", w.Definition)
	}
	if stored := e.text(`SELECT definition::text FROM workflows WHERE id = ?`, w.ID); strings.Contains(stored, "abcdefghijkl") {
		t.Fatalf("the secret was stored in plaintext: %s", stored)
	}
	if strings.Contains(e.logs.String(), "abcdefghijkl") {
		t.Fatal("the secret reached the logs or the audit events")
	}

	// A writer may edit around an HTTP request a manager set up, sending its sealed secret back as it was, but not change it.
	var stored map[string]any
	if err := json.Unmarshal(w.Definition, &stored); err != nil {
		t.Fatal(err)
	}
	stored["nodes"].([]any)[0].(map[string]any)["label"] = "Renamed"
	var patched envelope[resource]
	e.ok(e.v3(writer, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": stored}), http.StatusOK, &patched)
	if !strings.Contains(string(patched.Data.Definition), `"hint":"…ijkl"`) {
		t.Fatalf("the sealed secret was not kept: %s", patched.Data.Definition)
	}
	config := stored["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)
	config["body"] = `{"changed":true}`
	e.problem(e.v3(writer, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": stored}), http.StatusForbidden, "forbidden", httpRequestsManaged)

	// Moving the request to another host asks for its secrets again instead of sending them there.
	config["body"], config["url"] = `{}`, "https://collector.example/steal"
	p := e.problem(e.v3(manager, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": stored}), http.StatusBadRequest, "bad_request", "")
	if len(p.InvalidParams) != 1 || p.InvalidParams[0].Name != "definition.nodes.0.config.headers.0.secret" || p.InvalidParams[0].Reason != secretForOtherDestination {
		t.Fatalf("problem %+v", p)
	}

	// A sealed value from another workspace does not open here either.
	otherWS := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, f.org)
	slack := chain(chatNode("s", ActionSlackMessage, map[string]any{"value": testSlackURL}, "Hi"))
	sealedSlack := e.create(f.owner, f.ws, "Slack", slack)
	var copied map[string]any
	_ = json.Unmarshal(sealedSlack.Definition, &copied)
	e.problem(e.v3(f.owner, http.MethodPost, "/api/v3/workflows", map[string]any{"workspaceId": otherWS, "name": "Copy", "definition": copied}), http.StatusBadRequest, "bad_request", "")
}

func TestTestSend(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	writer := e.user("writer@example.com")
	e.member(f.org, writer, "member")
	e.grant(f.org, f.ws, writer, "readWrite")
	rec, url := serve(t, http.StatusOK)
	def := chain(httpNode("notify", url+"/hook"), tagNode("tag", uuid.NewString()))
	def["nodes"].([]any)[0].(map[string]any)["config"].(map[string]any)["body"] = `{"survey":"{{survey.name}}","answer":"{{answers.q1}}"}`
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	w := e.create(f.owner, f.ws, "Test send", def)
	send := func(user uuid.UUID, step string) (int, TestSendResult) {
		res := e.request(http.MethodPost, "/api/app/workflows/"+w.ID.String()+"/steps/"+step+"/test-send", nil, map[string]string{testUserHeader: user.String()})
		var out TestSendResult
		_ = json.Unmarshal(res.Body.Bytes(), &out)
		return res.Code, out
	}
	if code, out := send(f.owner, "notify"); code != http.StatusOK || !out.OK || out.Status != http.StatusOK {
		t.Fatalf("test send %d %+v", code, out)
	}
	if rec.count() != 1 || rec.bodies[0] != `{"answer":"{{answers.q1}}","survey":"Feedback"}` || !strings.HasPrefix(rec.requests[0].Header.Get("Webhook-Id"), "test_") {
		t.Fatalf("received %v %v", rec.bodies, rec.requests[0].Header)
	}
	if code, _ := send(writer, "notify"); code != http.StatusForbidden {
		t.Fatalf("a writer test-sent an HTTP request: %d", code)
	}
	if code, _ := send(f.owner, "tag"); code != http.StatusBadRequest {
		t.Fatalf("a tag step was test-sent: %d", code)
	}
	if code, _ := send(f.owner, "ghost"); code != http.StatusNotFound {
		t.Fatalf("an unknown step: %d", code)
	}
	if e.count(`SELECT count(*) FROM workflow_runs`) != 0 {
		t.Fatal("a test send created a run")
	}

	// A failure carries a reason code the builder shows in the member's language, besides the English detail.
	_, missingURL := serve(t, http.StatusNotFound)
	missing := chain(httpNode("notify", missingURL+"/hook"))
	missing["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	w = e.create(f.owner, f.ws, "Missing receiver", missing)
	if code, out := send(f.owner, "notify"); code != http.StatusOK || out.OK || out.Status != http.StatusNotFound || out.Reason != reasonStatus || out.Message != "Status 404" {
		t.Fatalf("failed test send %d %+v", code, out)
	}
}

// Enable and the dry run refuse a tag that is not the workspace's and an attribute key a workflow cannot write, which would otherwise fail a run for every response.
func TestEnableChecksStepTargets(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	e.contactFixture(f)
	otherWS := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, f.org)
	foreignTag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'Elsewhere') RETURNING id`, otherWS)
	ownTag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'Mine') RETURNING id`, f.ws)
	def := chain(
		tagNode("own", ownTag.String()),
		tagNode("foreign", foreignTag.String()),
		attributeNode("unique", "email", "{{answers.q1}}"),
		attributeNode("missing", "missing", "x"),
		attributeNode("plan", "plan", "x"),
	)
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	w := e.create(f.owner, f.ws, "Targets", def)

	p := e.problem(e.lifecycle(f.owner, w.ID, "enable"), http.StatusUnprocessableEntity, "workflow_not_executable", notExecutableDetail)
	want := []struct{ name, reason string }{
		{tagField, tagReason(foreignTag.String())},
		{attributeField, `Attribute "email" is unique to each contact and cannot be set by a workflow.`},
		{attributeField, `Attribute "missing" does not exist in this workspace.`},
	}
	if len(p.InvalidParams) != len(want) {
		t.Fatalf("%+v", p.InvalidParams)
	}
	for i, w := range want {
		if p.InvalidParams[i].Name != w.name || p.InvalidParams[i].Reason != w.reason {
			t.Fatalf("param %d %+v, want %+v", i, p.InvalidParams[i], w)
		}
	}
	var tested envelope[TestResult]
	e.ok(e.lifecycle(f.owner, w.ID, "test"), http.StatusOK, &tested)
	if tested.Data.OK || len(tested.Data.Problems) != 3 || tested.Data.Problems[0].Code != problemTagNotFound || tested.Data.Problems[1].Code != problemAttributeKey || tested.Data.Problems[2].Code != problemAttributeKey {
		t.Fatalf("test %+v", tested.Data)
	}
}

// Which survey's responses an HTTP request sends, and copying one into a workflow that can be retargeted, are a manager's decision like the request itself.
func TestHTTPRequestTriggerAndDuplicateNeedManage(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	writer := e.user("writer@example.com")
	e.member(f.org, writer, "member")
	e.grant(f.org, f.ws, writer, "readWrite")
	_, url := serve(t, http.StatusOK)
	def := chain(httpNode("notify", url+"/hook", map[string]any{"name": "Authorization", "secret": map[string]any{"value": "Bearer abcdefghijkl"}}))
	def["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	w := e.create(f.owner, f.ws, "With HTTP", def)
	other := e.insert(`INSERT INTO surveys (workspace_id, name, status) VALUES (?, 'Other', 'inProgress') RETURNING id`, f.ws)

	var stored map[string]any
	if err := json.Unmarshal(w.Definition, &stored); err != nil {
		t.Fatal(err)
	}
	stored["trigger"].(map[string]any)["config"].(map[string]any)["surveyId"] = other.String()
	e.problem(e.v3(writer, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": stored}), http.StatusForbidden, "forbidden", httpRequestTriggerManaged)
	e.problem(e.v3(writer, http.MethodPost, "/api/v3/workflows/"+w.ID.String()+"/duplicate", map[string]any{}), http.StatusForbidden, "forbidden", httpRequestDuplicateManaged)

	e.ok(e.v3(f.owner, http.MethodPatch, "/api/v3/workflows/"+w.ID.String(), map[string]any{"definition": stored}), http.StatusOK, nil)
	e.ok(e.v3(f.owner, http.MethodPost, "/api/v3/workflows/"+w.ID.String()+"/duplicate", map[string]any{}), http.StatusCreated, nil)

	// Without an HTTP request a writer retargets and copies freely.
	plain := chain(tagNode("t", uuid.NewString()))
	plain["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": f.survey.String(), "endingCardIds": []any{}}
	tagOnly := e.create(writer, f.ws, "Tag only", plain)
	plain["trigger"].(map[string]any)["config"] = map[string]any{"surveyId": other.String(), "endingCardIds": []any{}}
	e.ok(e.v3(writer, http.MethodPatch, "/api/v3/workflows/"+tagOnly.ID.String(), map[string]any{"definition": plain}), http.StatusOK, nil)
	e.ok(e.v3(writer, http.MethodPost, "/api/v3/workflows/"+tagOnly.ID.String()+"/duplicate", map[string]any{}), http.StatusCreated, nil)
}
