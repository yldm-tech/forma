package webhook

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ssrf"
)

func (r *receiver) setStatus(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.Raw(query, args...).Row().Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// response adds a response to the survey and returns its id.
func (e *env) response(survey uuid.UUID) uuid.UUID {
	return e.insert(`INSERT INTO responses (survey_id, finished) VALUES (?, true) RETURNING id`, survey)
}

// worker is the webhook.deliver worker over the env's database, sending through a guard that reaches local receivers.
func (e *env) worker() *deliverWorker {
	guard := ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true})
	return &deliverWorker{deliverer: NewDeliverer(NewRepository(e.db), guard), log: slog.New(slog.NewJSONHandler(e.logs, nil))}
}

// work runs the attempt-th attempt of the delivery job.
func (e *env) work(w *deliverWorker, args DeliverArgs, attempt int) error {
	e.t.Helper()
	return w.Work(context.Background(), &river.Job[DeliverArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: deliveryAttempts, Kind: DeliverArgs{}.Kind()},
		Args:   args,
	})
}

func (e *env) deliveryLog(user, hook uuid.UUID) []DeliveryView {
	e.t.Helper()
	res := e.do(http.MethodGet, "/api/app/webhooks/"+hook.String()+"/deliveries", asUser(user), nil)
	if res.Code != http.StatusOK {
		e.t.Fatalf("deliveries = %d %s", res.Code, res.Body)
	}
	return decode[DeliveriesResponse](e.t, res).Deliveries
}

func (e *env) healthOf(user, ws, hook uuid.UUID) *Health {
	e.t.Helper()
	res := e.do(http.MethodGet, "/api/app/workspaces/"+ws.String()+"/webhooks", asUser(user), nil)
	if res.Code != http.StatusOK {
		e.t.Fatalf("list = %d %s", res.Code, res.Body)
	}
	for _, w := range decode[ListResponse](e.t, res).Webhooks {
		if w.ID == hook {
			return w.Health
		}
	}
	e.t.Fatalf("webhook %s is not listed", hook)
	return nil
}

// Every attempt of a delivery lands in the webhook's log: a failure while retries remain is retrying with the time of the next one, a failure on the last attempt is failed, and the webhook's health follows (none, retrying, failing). A resend sends the same body with the same webhook-id, signed with the current secret, and a success makes the webhook healthy again.
func TestDeliveryLogAndResend(t *testing.T) {
	e := newEnv(t, true)
	owner, reader, stranger := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	e.organization(stranger)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusMethodNotAllowed)
	secret := GenerateSecret()
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers, secret) VALUES (?, ?, '{responseFinished}', ?) RETURNING id`, ws, rec.URL+"/hook?token=abc", secret)
	if h := e.healthOf(owner, ws, hook); h != nil {
		t.Fatalf("a webhook nothing was sent to has health %+v", h)
	}

	args := DeliverArgs{WebhookID: hook, JobKey: "job-1", Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{"id":"r1","data":{"q":"<b>&</b>"}}`}
	w := e.worker()
	before := time.Now()
	if err := e.work(w, args, 1); err == nil || !strings.Contains(err.Error(), "status 405") {
		t.Fatalf("first attempt = %v", err)
	}
	log := e.deliveryLog(owner, hook)
	if len(log) != 1 {
		t.Fatalf("log = %+v", log)
	}
	d := log[0]
	if d.Status != DeliveryRetrying || d.Attempts != 1 || d.StatusCode == nil || *d.StatusCode != 405 || d.Failure == nil || *d.Failure != FailureStatus ||
		d.NextAttemptAt == nil || d.ResponseID != args.ResponseID || d.SurveyID != sv || d.SurveyName == nil || *d.SurveyName != "Survey" ||
		d.MessageID != MessageID("job-1", hook, "responseFinished") || d.Event != "responseFinished" {
		t.Fatalf("after the first attempt = %+v", d)
	}
	if next, _ := time.Parse(time.RFC3339, *d.NextAttemptAt); next.Before(before.Add(4*time.Second)) || next.After(time.Now().Add(6*time.Second)) {
		t.Errorf("next attempt at %s, want in 5 s", *d.NextAttemptAt)
	}
	if h := e.healthOf(reader, ws, hook); h == nil || h.Status != HealthRetrying || h.FailingSince == nil || h.LastSuccessAt != nil || h.NextAttemptAt == nil || h.FailedCount != 0 {
		t.Fatalf("health while retrying = %+v", h)
	}

	if err := e.work(w, args, deliveryAttempts); err == nil {
		t.Fatal("the last attempt must fail")
	}
	d = e.deliveryLog(owner, hook)[0]
	if d.Status != DeliveryFailed || d.Attempts != 2 || d.NextAttemptAt != nil {
		t.Fatalf("after the last attempt = %+v", d)
	}
	if h := e.healthOf(owner, ws, hook); h == nil || h.Status != HealthFailing || h.FailedCount != 1 || h.FailingSince == nil {
		t.Fatalf("health after a lost delivery = %+v", h)
	}

	// The receiver is fixed and the secret was rotated: the resend reaches it with the original body and webhook-id, signed with the new secret.
	rec.setStatus(http.StatusOK)
	rotated := GenerateSecret()
	e.exec(`UPDATE webhooks SET secret = ? WHERE id = ?`, rotated, hook)
	resend := "/api/app/webhooks/" + hook.String() + "/deliveries/" + d.ID.String() + "/resend"
	if res := e.do(http.MethodPost, resend, asUser(reader), nil); res.Code != http.StatusForbidden {
		t.Fatalf("reader resend = %d", res.Code)
	}
	if res := e.do(http.MethodGet, "/api/app/webhooks/"+hook.String()+"/deliveries", asUser(stranger), nil); res.Code != http.StatusForbidden {
		t.Fatalf("another organization reads the log: %d", res.Code)
	}
	res := e.do(http.MethodPost, resend, asUser(owner), nil)
	if res.Code != http.StatusOK {
		t.Fatalf("resend = %d %s", res.Code, res.Body)
	}
	if got := decode[DeliveryView](t, res); got.Status != DeliverySucceeded || got.Attempts != 3 || got.Failure != nil || got.FailureMessage != nil || *got.StatusCode != 200 {
		t.Fatalf("after the resend = %+v", got)
	}
	got := rec.received()
	if len(got) != 3 || string(got[2].Body) != string(got[0].Body) || got[2].Header.Get("Webhook-Id") != got[0].Header.Get("Webhook-Id") {
		t.Fatalf("the resend differs from the original delivery: %d requests", len(got))
	}
	wantBody := `{"webhookId":"` + hook.String() + `","event":"responseFinished","data":{"id":"r1","data":{"q":"<b>&</b>"}}}`
	if string(got[2].Body) != wantBody {
		t.Fatalf("resent body = %s", got[2].Body)
	}
	wh, _ := standardwebhooks.NewWebhook(rotated)
	if err := wh.Verify(got[2].Body, got[2].Header); err != nil {
		t.Fatalf("the resend is not signed with the current secret: %v", err)
	}
	if h := e.healthOf(owner, ws, hook); h == nil || h.Status != HealthHealthy || h.LastSuccessAt == nil || h.FailingSince != nil || h.FailedCount != 0 {
		t.Fatalf("health after the resend = %+v", h)
	}

	for name, path := range map[string]string{
		"unknown delivery":           "/api/app/webhooks/" + hook.String() + "/deliveries/" + uuid.NewString() + "/resend",
		"another webhook's delivery": "/api/app/webhooks/" + e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL).String() + "/deliveries/" + d.ID.String() + "/resend",
	} {
		if res := e.do(http.MethodPost, path, asUser(owner), nil); res.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, res.Code, res.Body)
		}
	}
	if strings.Contains(e.do(http.MethodGet, "/api/app/webhooks/"+hook.String()+"/deliveries", asUser(owner), nil).Body.String(), "token=abc") {
		t.Error("the log shows the URL's query")
	}
}

// A resend that delivers an event while its job waits to retry settles it: the job's next attempt sends nothing. A resend that fails leaves the delivery to its job.
func TestResendSettlesTheRetries(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusInternalServerError)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL)
	args := DeliverArgs{WebhookID: hook, JobKey: "job-1", Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
	w := e.worker()
	if err := e.work(w, args, 1); err == nil {
		t.Fatal("the first attempt must fail")
	}
	d := e.deliveryLog(owner, hook)[0]
	resend := "/api/app/webhooks/" + hook.String() + "/deliveries/" + d.ID.String() + "/resend"

	res := e.do(http.MethodPost, resend, asUser(owner), nil)
	if got := decode[DeliveryView](t, res); res.Code != http.StatusOK || got.Status != DeliveryRetrying || got.NextAttemptAt == nil || got.Attempts != 2 || *got.StatusCode != 500 {
		t.Fatalf("a failed resend while retrying = %d %+v", res.Code, got)
	}

	rec.setStatus(http.StatusNoContent)
	if got := decode[DeliveryView](t, e.do(http.MethodPost, resend, asUser(owner), nil)); got.Status != DeliverySucceeded || got.NextAttemptAt != nil {
		t.Fatalf("a successful resend = %+v", got)
	}
	if err := e.work(w, args, 2); err != nil {
		t.Fatalf("the retry after a resend delivered the event = %v", err)
	}
	if n := len(rec.received()); n != 3 {
		t.Fatalf("%d requests, want the retry to send nothing", n)
	}
	if d := e.deliveryLog(owner, hook)[0]; d.Attempts != 3 || d.Status != DeliverySucceeded {
		t.Fatalf("after the skipped retry = %+v", d)
	}
}

// A resend of an event that was delivered already, to an endpoint that fails now, does not undo the delivery: the event stays delivered, the webhook keeps its last success and loses nothing, and the failed attempt shows as the latest one, which makes the webhook failing.
func TestFailedResendKeepsTheDelivery(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusOK)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL)
	args := DeliverArgs{WebhookID: hook, JobKey: "job-1", Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
	if err := e.work(e.worker(), args, 1); err != nil {
		t.Fatal(err)
	}
	delivered := e.healthOf(owner, ws, hook)
	if delivered == nil || delivered.Status != HealthHealthy || delivered.LastSuccessAt == nil {
		t.Fatalf("health after the delivery = %+v", delivered)
	}

	rec.setStatus(http.StatusServiceUnavailable)
	d := e.deliveryLog(owner, hook)[0]
	res := e.do(http.MethodPost, "/api/app/webhooks/"+hook.String()+"/deliveries/"+d.ID.String()+"/resend", asUser(owner), nil)
	got := decode[DeliveryView](t, res)
	if res.Code != http.StatusOK || got.Status != DeliverySucceeded || got.Attempts != 2 || got.NextAttemptAt != nil || got.Failure == nil || *got.Failure != FailureStatus || *got.StatusCode != 503 {
		t.Fatalf("a failed resend of a delivered event = %d %+v", res.Code, got)
	}
	h := e.healthOf(owner, ws, hook)
	if h == nil || h.LastSuccessAt == nil || *h.LastSuccessAt != *delivered.LastSuccessAt || h.FailedCount != 0 || h.Status != HealthFailing || h.Failure == nil {
		t.Fatalf("health after a failed resend of a delivered event = %+v", h)
	}
}

// Deliveries lost during an outage stay counted after the endpoint recovers: a later success makes the webhook lost, not healthy, until each of them is resent.
func TestLostDeliveriesOutliveALaterSuccess(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusBadGateway)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL)
	w := e.worker()
	for _, key := range []string{"job-1", "job-2"} {
		args := DeliverArgs{WebhookID: hook, JobKey: key, Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
		if err := e.work(w, args, deliveryAttempts); err == nil {
			t.Fatal("the last attempt must fail")
		}
	}
	if h := e.healthOf(owner, ws, hook); h == nil || h.Status != HealthFailing || h.FailedCount != 2 {
		t.Fatalf("health after two lost deliveries = %+v", h)
	}

	rec.setStatus(http.StatusOK)
	newest := e.deliveryLog(owner, hook)[0]
	if res := e.do(http.MethodPost, "/api/app/webhooks/"+hook.String()+"/deliveries/"+newest.ID.String()+"/resend", asUser(owner), nil); res.Code != http.StatusOK {
		t.Fatalf("resend = %d %s", res.Code, res.Body)
	}
	h := e.healthOf(owner, ws, hook)
	if h == nil || h.Status != HealthLost || h.FailedCount != 1 || h.LastSuccessAt == nil || h.FailingSince != nil || h.Failure != nil {
		t.Fatalf("health with one lost delivery left after a success = %+v", h)
	}

	oldest := e.deliveryLog(owner, hook)[1]
	if res := e.do(http.MethodPost, "/api/app/webhooks/"+hook.String()+"/deliveries/"+oldest.ID.String()+"/resend", asUser(owner), nil); res.Code != http.StatusOK {
		t.Fatalf("resend = %d %s", res.Code, res.Body)
	}
	if h := e.healthOf(owner, ws, hook); h == nil || h.Status != HealthHealthy || h.FailedCount != 0 {
		t.Fatalf("health once every lost delivery was resent = %+v", h)
	}
}

// A job attempt that was already on its way when a resend delivered the event does not undo the delivery: the row stays delivered, with no retry scheduled, so the job's next run sends nothing.
func TestJobAttemptAfterResendKeepsTheDelivery(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, "https://example.com/hook")
	repo := NewRepository(e.db)
	now := time.Now().UTC()
	next := now.Add(time.Minute)
	rec := deliveryRecord{
		WebhookID: hook, ResponseID: e.response(sv), SurveyID: sv, Event: "responseFinished", MessageID: "msg-1", Payload: `{}`,
		Status: DeliveryRetrying, At: now, NextAttemptAt: &next, Attempt: Attempt{StatusCode: 500, Failure: FailureStatus, Message: "Status 500"},
	}
	if err := repo.recordAttempt(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	d := e.deliveryLog(owner, hook)[0]
	if err := repo.recordResend(t.Context(), d.ID, now.Add(time.Second), Attempt{StatusCode: 200}); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{DeliveryRetrying, DeliveryFailed} {
		late := rec
		late.At, late.Status = now.Add(2*time.Second), status
		if status == DeliveryFailed {
			late.NextAttemptAt = nil
		}
		if err := repo.recordAttempt(t.Context(), late); err != nil {
			t.Fatalf("a %s attempt after the resend: %v", status, err)
		}
		got := e.deliveryLog(owner, hook)[0]
		if got.Status != DeliverySucceeded || got.NextAttemptAt != nil || got.Failure != nil || got.Attempts != 2 {
			t.Fatalf("after a %s job attempt that a resend overtook = %+v", status, got)
		}
	}
	if done, err := repo.deliveredAlready(t.Context(), hook, "msg-1"); err != nil || !done {
		t.Fatalf("delivered = %v, %v", done, err)
	}
	if h := e.healthOf(owner, ws, hook); h == nil || h.Status != HealthHealthy || h.FailedCount != 0 {
		t.Fatalf("health = %+v", h)
	}
}

// A webhook that stops subscribing while a delivery is being retried ends it as failed instead of leaving it retrying for good; deleting the response deletes its deliveries, payload included; the log keeps the newest deliveries of each webhook.
func TestDeliveryLogLifecycle(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusBadGateway)
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers) VALUES (?, ?, '{responseFinished}') RETURNING id`, ws, rec.URL)
	response := e.response(sv)
	args := DeliverArgs{WebhookID: hook, JobKey: "job-1", Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: response, Data: `{}`}
	w := e.worker()
	if err := e.work(w, args, 1); err == nil {
		t.Fatal("the first attempt must fail")
	}
	e.exec(`UPDATE webhooks SET triggers = '{responseCreated}' WHERE id = ?`, hook)
	if err := e.work(w, args, 2); err != nil {
		t.Fatal(err)
	}
	if d := e.deliveryLog(owner, hook)[0]; d.Status != DeliveryFailed || d.NextAttemptAt != nil || d.Attempts != 1 || len(rec.received()) != 1 {
		t.Fatalf("after the webhook stopped subscribing = %+v", d)
	}

	repo := NewRepository(e.db)
	start := time.Now().Add(-time.Hour)
	for i := range deliveriesKept + 5 {
		if err := repo.recordAttempt(t.Context(), deliveryRecord{
			WebhookID: hook, ResponseID: response, SurveyID: sv, Event: "responseFinished", MessageID: uuid.NewString(), Payload: `{}`,
			Status: DeliverySucceeded, At: start.Add(time.Duration(i) * time.Second), Attempt: Attempt{StatusCode: 200, Latency: 12 * time.Millisecond},
		}); err != nil {
			t.Fatal(err)
		}
	}
	log := e.deliveryLog(owner, hook)
	if len(log) != deliveriesKept || log[0].LatencyMs != 12 || log[0].Status != DeliverySucceeded {
		t.Fatalf("%d deliveries kept, newest %+v", len(log), log[0])
	}
	if n := e.count(`SELECT count(*) FROM webhook_deliveries WHERE webhook_id = ? AND message_id = ?`, hook, MessageID("job-1", hook, "responseFinished")); n != 0 {
		t.Error("the oldest delivery was not trimmed")
	}

	e.exec(`DELETE FROM responses WHERE id = ?`, response)
	if n := e.count(`SELECT count(*) FROM webhook_deliveries`); n != 0 {
		t.Errorf("%d deliveries outlive their response", n)
	}
}

// A new signing secret is shown once, signs every delivery from then on, and is audited without its value; a webhook an automation platform created keeps its secret, and a member who cannot write cannot change it.
func TestRegenerateSecret(t *testing.T) {
	e := newEnv(t, true)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	sv := e.survey(ws)
	rec := newReceiver(t, http.StatusOK)
	old := GenerateSecret()
	hook := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers, secret) VALUES (?, ?, '{responseFinished}', ?) RETURNING id`, ws, rec.URL, old)
	zap := e.insert(`INSERT INTO webhooks (workspace_id, url, source, triggers, secret) VALUES (?, ?, 'zapier', '{responseFinished}', ?) RETURNING id`, ws, rec.URL, old)
	path := "/api/app/webhooks/" + hook.String() + "/secret"

	if res := e.do(http.MethodPost, path, asUser(reader), nil); res.Code != http.StatusForbidden {
		t.Fatalf("reader = %d", res.Code)
	}
	if res := e.do(http.MethodPost, "/api/app/webhooks/"+zap.String()+"/secret", asUser(owner), nil); res.Code != http.StatusBadRequest || decode[errorBody](t, res).Code != CodeThirdPartySecret {
		t.Fatalf("third-party webhook = %d %s", res.Code, res.Body)
	}
	if ev := e.lastAudit(); ev.Action != "updated" || ev.Status != "failure" || ev.Target.ID != zap.String() {
		t.Errorf("refusal audit = %+v", ev)
	}
	res := e.do(http.MethodPost, path, asUser(owner), nil)
	if res.Code != http.StatusOK {
		t.Fatalf("regenerate = %d %s", res.Code, res.Body)
	}
	secret := decode[SecretView](t, res).Secret
	if !validSecret(secret) || secret == old {
		t.Fatalf("new secret %q", secret)
	}
	var stored string
	if err := e.db.Raw(`SELECT secret FROM webhooks WHERE id = ?`, hook).Row().Scan(&stored); err != nil || stored != secret {
		t.Fatalf("stored secret %q (%v)", stored, err)
	}
	ev := e.lastAudit()
	if ev.Action != "updated" || ev.Status != "success" || ev.Target.ID != hook.String() || ev.Actor.ID != owner.String() || ev.OrganizationID != org.String() {
		t.Errorf("audit = %+v", ev)
	}
	if strings.Contains(e.logs.String(), secret) || strings.Contains(e.logs.String(), old) {
		t.Error("the log carries a secret")
	}
	if strings.Contains(e.do(http.MethodGet, "/api/app/workspaces/"+ws.String()+"/webhooks", asUser(owner), nil).Body.String(), "whsec_") {
		t.Error("the list shows a secret")
	}

	args := DeliverArgs{WebhookID: hook, JobKey: "job-1", Event: "responseFinished", WorkspaceID: ws, SurveyID: sv, ResponseID: e.response(sv), Data: `{}`}
	if err := e.work(e.worker(), args, 1); err != nil {
		t.Fatal(err)
	}
	got := rec.received()
	wh, _ := standardwebhooks.NewWebhook(secret)
	if len(got) != 1 || wh.Verify(got[0].Body, got[0].Header) != nil {
		t.Fatal("a delivery after the regeneration is not signed with the new secret")
	}
}

// How an attempt's failure is classified and described: never with the URL's path or query.
func TestAttemptFailures(t *testing.T) {
	t.Parallel()
	d := NewDeliverer(nil, ssrf.New(config.Config{}))
	a, err := d.attempt(context.Background(), &Webhook{ID: uuid.New(), URL: "http://127.0.0.1/hook?token=abc"}, "responseCreated", "m", []byte(`{}`))
	if err != nil || a.Failure != FailureRefused || a.Message != ssrf.MessagePrivateAddress || a.StatusCode != 0 {
		t.Fatalf("private URL = %+v %v", a, err)
	}
	open := NewDeliverer(nil, ssrf.New(config.Config{DangerouslyAllowWebhookInternalURLs: true}))
	for status, want := range map[int]string{http.StatusOK: "", http.StatusAccepted: "", http.StatusNotFound: FailureStatus, http.StatusServiceUnavailable: FailureStatus} {
		rec := newReceiver(t, status)
		a, err := open.attempt(context.Background(), &Webhook{ID: uuid.New(), URL: rec.URL + "/x?token=abc"}, "responseCreated", "m", []byte(`{}`))
		if err != nil || a.Failure != want || a.StatusCode != status || a.OK() != (want == "") || strings.Contains(a.Message, "token") {
			t.Errorf("status %d = %+v %v", status, a, err)
		}
	}
	closed := newReceiver(t, http.StatusOK)
	url := closed.URL
	closed.Close()
	a, err = open.attempt(context.Background(), &Webhook{ID: uuid.New(), URL: url + "/x?token=abc"}, "responseCreated", "m", []byte(`{}`))
	if err != nil || a.Failure != FailureUnreachable || a.Message == "" || strings.Contains(a.Message, "token") {
		t.Errorf("closed endpoint = %+v %v", a, err)
	}
}
