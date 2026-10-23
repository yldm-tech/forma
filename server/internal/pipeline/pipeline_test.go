package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river/rivertype"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"sync/atomic"

	"github.com/yldm-tech/forma/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/server/internal/platform/analytics/analyticstest"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/server/internal/response"
	"github.com/yldm-tech/forma/server/internal/webhook"
)

func TestJobKey(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("0192f5c8-0000-7000-8000-000000000001")
	args := response.PipelineArgs{Event: "responseFinished", Response: response.Response{ID: id, UpdatedAt: time.UnixMilli(1700000000123).Add(456 * time.Microsecond)}}
	if got := JobKey(args); got != "response-pipeline-responseFinished-"+id.String()+"-1700000000123" {
		t.Fatalf("JobKey = %s", got)
	}
}

// forma2's getWebhooksForPipeline: the event's subscribers in the response's workspace that list its survey or cover every survey, and nothing else.
func TestWebhookMatching(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	otherSurvey := e.insert(`INSERT INTO surveys (workspace_id, name) VALUES (?, 'Other') RETURNING id`, f.ws)
	otherWS := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, f.org)
	all, listed, wrongSurvey, wrongTrigger, wrongWorkspace := newReceiver(t), newReceiver(t), newReceiver(t), newReceiver(t), newReceiver(t)
	e.webhook(f.ws, all.URL, "", "{responseCreated}")
	e.webhook(f.ws, listed.URL, "", "{responseFinished,responseCreated}", f.survey)
	e.webhook(f.ws, wrongSurvey.URL, "", "{responseCreated}", otherSurvey)
	e.webhook(f.ws, wrongTrigger.URL, "", "{responseUpdated}")
	e.webhook(otherWS, wrongWorkspace.URL, "", "{responseCreated}")

	r := e.respond(f, false, `{"q1":"a"}`)
	if err := e.work(job(response.EventResponseCreated, f, r, 1)); err != nil {
		t.Fatal(err)
	}
	if len(all.received()) != 1 || len(listed.received()) != 1 {
		t.Errorf("subscribers got %d and %d requests", len(all.received()), len(listed.received()))
	}
	for name, rec := range map[string]*receiver{"other survey": wrongSurvey, "other trigger": wrongTrigger, "other workspace": wrongWorkspace} {
		if len(rec.received()) != 0 {
			t.Errorf("%s was delivered to", name)
		}
	}

	if err := e.work(job(response.EventResponseUpdated, f, r, 1)); err != nil {
		t.Fatal(err)
	}
	if len(wrongTrigger.received()) != 1 || len(all.received()) != 1 {
		t.Error("responseUpdated must reach its subscriber only")
	}

	// responseUpdated without a subscriber does nothing at all.
	g := e.fixture()
	if err := e.work(job(response.EventResponseUpdated, g, e.respond(g, false, `{}`), 1)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.logs.String(), "no webhooks subscribed to responseUpdated") {
		t.Error("responseUpdated without subscribers was not skipped")
	}
}

// The delivered body is forma2's {webhookId, event, data: {...response, data with absolute file URLs, survey}}, with toISOString timestamps, signed with the webhook's secret under a webhook-id derived from the job.
func TestWebhookPayload(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	rec := newReceiver(t)
	secret := webhook.GenerateSecret()
	hook := e.webhook(f.ws, rec.URL+"/hook", secret, "{responseFinished}")
	tag := e.insert(`INSERT INTO tags (workspace_id, name) VALUES (?, 'vip') RETURNING id`, f.ws)
	r := e.respond(f, true, `{"q1":"<b>a</b>","q2":["/storage/ws1/private/a%20b.pdf","/storage/ws1/public/c.png","https://elsewhere.example/x.pdf"],"nested":{"f":["/storage/ws1/public/d.png"]}}`)
	e.exec(`INSERT INTO response_tags (response_id, tag_id) VALUES (?, ?)`, r.ID, tag)
	r2, _ := response.Load(context.Background(), e.db, r.ID)
	j := job(response.EventResponseFinished, f, *r2, 1)
	if err := e.work(j); err != nil {
		t.Fatal(err)
	}
	got := rec.received()
	if len(got) != 1 {
		t.Fatalf("%d deliveries", len(got))
	}
	var body struct {
		WebhookID string          `json:"webhookId"`
		Event     string          `json:"event"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(got[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.WebhookID != hook.String() || body.Event != "responseFinished" {
		t.Fatalf("envelope = %s", got[0].Body)
	}
	var sv struct{ CreatedAt time.Time }
	if err := e.db.Raw(`SELECT created_at FROM surveys WHERE id = ?`, f.survey).Scan(&sv).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"id":"` + r.ID.String() + `"`, `"createdAt":"` + jsvalue.ISOString(r.CreatedAt) + `"`, `"surveyId":"` + f.survey.String() + `"`,
		`"displayId":null`, `"contact":null`, `"contactAttributes":{"email":"respondent@example.com"}`, `"finished":true`, `"endingId":null`,
		`"q1":"<b>a</b>"`, `"https://app.example.com/storage/ws1/private/a%20b.pdf"`, `"https://surveys.example.com/storage/ws1/public/c.png"`,
		`"https://elsewhere.example/x.pdf"`, `"https://surveys.example.com/storage/ws1/public/d.png"`,
		`"tags":[{"id":"` + tag.String() + `","createdAt":"`, `"name":"vip","workspaceId":"` + f.ws.String() + `"}]`,
		`"meta":{"url":"https://example.com/?a=1"}`, `"singleUseId":null`, `"language":null`,
		`"survey":{"title":"Feedback","type":"app","status":"inProgress","createdAt":"` + jsvalue.ISOString(sv.CreatedAt) + `","updatedAt":"`,
	} {
		if !strings.Contains(string(body.Data), want) {
			t.Errorf("data lacks %s:\n%s", want, body.Data)
		}
	}
	if !strings.HasPrefix(string(body.Data), `{"id":`) || !strings.HasSuffix(string(body.Data), `"}}`) {
		t.Errorf("data is not ordered as forma2's: %s", body.Data)
	}
	h := got[0].Header
	if h.Get("Webhook-Id") != webhook.MessageID(JobKey(j.Args), hook, "responseFinished") || h.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", h)
	}
	wh, _ := standardwebhooks.NewWebhook(secret)
	if err := wh.Verify(got[0].Body, h); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

// forma2's retry rule: a failing webhook fails every attempt but the last, before any side effect, and the retry delivers again to every subscriber with the same webhook-id; the last attempt logs the failure and runs the side effects.
func TestWebhookFailureRetriesTheJob(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	owner := e.user("owner@example.com", f.survey)
	e.member(f.org, owner, "owner")
	ok, failing := newReceiver(t), newReceiver(t)
	e.webhook(f.ws, ok.URL, "", "{responseFinished}")
	e.webhook(f.ws, failing.URL, "", "{responseFinished}")
	failing.failNext(10)
	r := e.respond(f, true, `{"q1":"a"}`)

	if err := e.work(job(response.EventResponseFinished, f, r, 1)); err == nil {
		t.Fatal("a failed webhook must fail a non-final attempt")
	}
	if err := e.work(job(response.EventResponseFinished, f, r, 2)); err == nil {
		t.Fatal("a failed webhook must fail a non-final attempt")
	}
	if n := len(e.mail.Messages()); n != 0 {
		t.Fatalf("%d emails sent on attempts that will be retried", n)
	}
	if err := e.work(job(response.EventResponseFinished, f, r, 3)); err != nil {
		t.Fatalf("the last attempt must carry on: %v", err)
	}
	if n := len(e.mail.Messages()); n != 1 {
		t.Fatalf("%d emails after the last attempt", n)
	}
	got := ok.received()
	if len(got) != 3 || got[0].Header.Get("Webhook-Id") != got[2].Header.Get("Webhook-Id") {
		t.Fatalf("the healthy webhook got %d deliveries with ids that differ", len(got))
	}
	if !strings.Contains(e.logs.String(), "exhausted retries") {
		t.Error("the exhausted delivery was not logged")
	}
}

func TestUnrecoverableJobs(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	r := e.respond(f, true, `{}`)
	j := job(response.EventResponseFinished, f, r, 1)
	j.Args.SurveyID = uuid.New()
	if err := e.work(j); !isCancel(err) {
		t.Errorf("missing survey = %v, want a cancel", err)
	}
	otherOrg := e.insert(`INSERT INTO organizations (name) VALUES ('Other') RETURNING id`)
	otherWS := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Other') RETURNING id`, otherOrg)
	j = job(response.EventResponseFinished, f, r, 1)
	j.Args.WorkspaceID = otherWS
	if err := e.work(j); !isCancel(err) {
		t.Errorf("foreign workspace = %v, want a cancel", err)
	}
	j.Args.WorkspaceID = uuid.New()
	if err := e.work(j); !isCancel(err) {
		t.Errorf("missing organization = %v, want a cancel", err)
	}
}

// forma2's getUsersWithNotifications and sendResponseFinishedEmail: owners and managers, and members on a team with access to the workspace, whose alert for the survey is on; one email each, in their locale, replying to the respondent.
func TestNotificationEmails(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	owner := e.user("owner@example.com", f.survey)
	manager := e.user("manager@example.com", f.survey)
	teamMember := e.user("team@example.com", f.survey)
	noTeam := e.user("noteam@example.com", f.survey)
	alertOff := e.user("off@example.com")
	outsider := e.user("outsider@example.com", f.survey)
	unsubscribedOrg := e.user("unsubscribed@example.com", f.survey)
	e.member(f.org, owner, "owner")
	e.member(f.org, manager, "manager")
	e.member(f.org, teamMember, "member")
	e.teamAccess(f.org, f.ws, teamMember)
	e.member(f.org, noTeam, "member")
	e.member(f.org, alertOff, "owner")
	e.member(f.org, unsubscribedOrg, "owner")
	e.exec(`UPDATE users SET locale = 'ja-JP', notification_settings = jsonb_set(notification_settings, '{unsubscribedOrganizationIds}', to_jsonb(ARRAY[?]::text[])) WHERE id = ?`, f.org.String(), unsubscribedOrg)
	_ = outsider

	e.respond(f, false, `{}`)
	r := e.respond(f, true, `{"q1":"jane@example.com"}`)
	if err := e.work(job(response.EventResponseFinished, f, r, 1)); err != nil {
		t.Fatal(err)
	}
	sent := map[string]string{}
	for _, m := range e.mail.Messages() {
		sent[m.To] = m.Subject
		if m.ReplyTo != "respondent@example.com" {
			t.Errorf("reply-to = %q", m.ReplyTo)
		}
		if (m.To != "unsubscribed@example.com" && !strings.Contains(m.HTML, "View 1 more responses")) || !strings.Contains(m.HTML, "/workspaces/"+f.ws.String()+"/settings/notifications?type=alert&amp;elementId="+f.survey.String()) {
			t.Errorf("email to %s lacks the count or the unsubscribe link", m.To)
		}
		if !strings.Contains(m.HTML, `src="https://surveys.example.com/storage/ws/public/logo.png"`) {
			t.Errorf("email to %s lacks the organization logo", m.To)
		}
	}
	want := []string{"owner@example.com", "manager@example.com", "team@example.com", "unsubscribed@example.com"}
	if len(sent) != len(want) {
		t.Fatalf("sent to %v, want %v", sent, want)
	}
	for _, to := range want {
		if _, ok := sent[to]; !ok {
			t.Errorf("no email to %s", to)
		}
	}
	if sent["owner@example.com"] != "A response for Feedback was completed ✅" || sent["unsubscribed@example.com"] == sent["owner@example.com"] {
		t.Errorf("subjects = %v", sent)
	}
	if strings.Contains(sent["owner@example.com"], "jane@example.com") {
		t.Error("the respondent's address is in the subject")
	}
}

// forma2's auto-complete: the survey closes once its finished responses reach the limit, guarded on the status this job read, and the change is audited as the system's.
func TestAutoComplete(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	e.exec(`UPDATE surveys SET auto_complete = 2 WHERE id = ?`, f.survey)
	status := func() (s string) {
		_ = e.db.Raw(`SELECT status FROM surveys WHERE id = ?`, f.survey).Row().Scan(&s)
		return s
	}
	e.respond(f, false, `{}`)
	first := e.respond(f, true, `{}`)
	if err := e.work(job(response.EventResponseFinished, f, first, 1)); err != nil {
		t.Fatal(err)
	}
	if status() != "inProgress" {
		t.Fatal("a partial response must not count towards the limit")
	}
	second := e.respond(f, true, `{}`)
	if err := e.work(job(response.EventResponseFinished, f, second, 1)); err != nil {
		t.Fatal(err)
	}
	if status() != "completed" {
		t.Fatal("the survey was not completed at its limit")
	}
	if !strings.Contains(e.logs.String(), `"msg":"audit","actor":{"id":"unknown","type":"system"},"action":"updated","target":{"id":"`+f.survey.String()+`","type":"survey"},"status":"success"`) {
		t.Errorf("auto-complete was not audited:\n%s", e.logs.String())
	}

	// The status moved after the snapshot: the guarded write does nothing.
	g := e.fixture()
	e.exec(`UPDATE surveys SET auto_complete = 1 WHERE id = ?`, g.survey)
	r := e.respond(g, true, `{}`)
	sv, err := e.worker.loadSurvey(context.Background(), g.survey)
	if err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE surveys SET status = 'paused' WHERE id = ?`, g.survey)
	e.worker.autoComplete(context.Background(), &Event{Survey: sv, OrganizationID: g.org, Response: r}, e.log)
	var s string
	_ = e.db.Raw(`SELECT status FROM surveys WHERE id = ?`, g.survey).Row().Scan(&s)
	if s != "paused" {
		t.Errorf("auto-complete overwrote a status that changed: %s", s)
	}
}

// recorder stands in for the integration and workflow modules, recording what the pipeline hands them.
type recorder struct {
	mu        sync.Mutex
	order     []string
	events    []*Event
	workflowE error
	metered   []string
}

func (r *recorder) ResponseCreated(_ context.Context, stripeCustomerID *string, responseID uuid.UUID, createdAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	customer := "<nil>"
	if stripeCustomerID != nil {
		customer = *stripeCustomerID
	}
	r.metered = append(r.metered, fmt.Sprintf("response %s %s %d", customer, responseID, createdAt.Unix()))
}

func (r *recorder) WorkflowRunCreated(context.Context, *string, uuid.UUID, time.Time) {}

func (r *recorder) HandleResponse(_ context.Context, ev *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, "integrations")
	r.events = append(r.events, ev)
	return errors.New("sheet unavailable")
}

func (r *recorder) EnqueueResponseCompleted(_ context.Context, ev *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, "workflows")
	r.events = append(r.events, ev)
	return r.workflowE
}

// The hand-offs run on responseFinished only, workflows first; an integration failure is logged and the job goes on, and a workflow failure that asks for a retry fails the job before follow-ups and emails.
func TestHandoffs(t *testing.T) {
	rec := &recorder{}
	e := newEnv(t, Handoffs{Integrations: rec, Workflows: rec, Meter: rec})
	f := e.fixture()
	e.exec(`UPDATE organizations SET display_time_zone = 'Asia/Tokyo' WHERE id = ?`, f.org)
	e.exec(`INSERT INTO organization_billing (organization_id, stripe_customer_id, limits) VALUES (?, 'cus_123', '{}')`, f.org)
	owner := e.user("owner@example.com", f.survey)
	e.member(f.org, owner, "owner")
	r := e.respond(f, true, `{}`)
	if err := e.work(job(response.EventResponseCreated, f, r, 1)); err != nil {
		t.Fatal(err)
	}
	if len(rec.order) != 0 {
		t.Fatalf("hand-offs ran on responseCreated: %v", rec.order)
	}
	// forma2's runResponseCreatedSideEffects: the created response is metered for the organization's Stripe customer, once per responseCreated event.
	if want := fmt.Sprintf("[response cus_123 %s %d]", r.ID, r.CreatedAt.Unix()); fmt.Sprint(rec.metered) != want {
		t.Fatalf("metered %v, want %s", rec.metered, want)
	}
	j := job(response.EventResponseFinished, f, r, 1)
	j.Args.Locale = "ja-JP"
	if err := e.work(j); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rec.order) != "[workflows integrations]" {
		t.Fatalf("order = %v", rec.order)
	}
	ev := rec.events[0]
	if ev.JobKey != JobKey(j.Args) || ev.OrganizationID != f.org || ev.DisplayTimeZone != "Asia/Tokyo" || ev.Locale != "ja-JP" || ev.Survey.Name != "Feedback" || ev.Response.ID != r.ID {
		t.Errorf("event = %+v", ev)
	}
	if !strings.Contains(e.logs.String(), "sheet unavailable") || len(e.mail.Messages()) != 1 {
		t.Error("an integration failure must be logged and stop nothing")
	}

	rec.workflowE = fmt.Errorf("pool exhausted: %w", ErrRetry)
	if err := e.work(job(response.EventResponseFinished, f, r, 1)); !errors.Is(err, ErrRetry) {
		t.Fatalf("a workflow retry request = %v", err)
	}
	if len(e.mail.Messages()) != 1 {
		t.Error("emails were sent on an attempt that will be retried")
	}
	rec.workflowE = errors.New("bad definition")
	if err := e.work(job(response.EventResponseFinished, f, r, 1)); err != nil {
		t.Fatalf("any other workflow failure is logged: %v", err)
	}
	if len(rec.metered) != 1 {
		t.Errorf("responseFinished was metered: %v", rec.metered)
	}
}

// The worker runs on River: the job the response module enqueues is delivered, a failing webhook is retried after River's backoff with the same webhook-id, and the follow-up and notification side effects run once, after the delivery that succeeded.
func TestRiverEndToEnd(t *testing.T) {
	e := newEnv(t, Handoffs{})
	f := e.fixture()
	owner := e.user("owner@example.com", f.survey)
	e.member(f.org, owner, "owner")
	e.exec(`INSERT INTO survey_follow_ups (survey_id, name, trigger, action) VALUES (?, 'Thanks', '{"type":"response","properties":null}', ?::jsonb)`, f.survey,
		`{"type":"send-email","properties":{"to":"q1","from":"x@example.com","replyTo":["owner@example.com"],"subject":"Thanks","body":"<p>Thanks</p>","attachResponseData":false}}`)
	rec := newReceiver(t)
	rec.failNext(1)
	e.webhook(f.ws, rec.URL, webhook.GenerateSecret(), "{responseFinished}")
	r := e.respond(f, true, `{"q1":"jane@example.com"}`)

	pool, cleanup, err := jobs.OpenPool(context.Background(), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	reg := jobs.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterJobs(reg, e.worker)
	worker, err := jobs.NewWorker(e.cfg, pool, reg, e.log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = worker.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	inserter, err := jobs.NewInserter(pool, e.log)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inserter.Insert(context.Background(), response.PipelineArgs{Event: response.EventResponseFinished, WorkspaceID: f.ws, SurveyID: f.survey, Response: r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the job to complete", 30*time.Second, func() bool {
		var state string
		_ = pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE id = $1`, res.Job.ID).Scan(&state)
		return state == string(rivertype.JobStateCompleted)
	})
	got := rec.received()
	if len(got) != 2 || got[0].Header.Get("Webhook-Id") != got[1].Header.Get("Webhook-Id") {
		t.Fatalf("%d deliveries, ids %q", len(got), got)
	}
	var followUps, notifications int
	for _, m := range e.mail.Messages() {
		switch m.To {
		case "jane@example.com":
			followUps++
			if m.Subject != "Thanks" || m.ReplyTo != "owner@example.com" {
				t.Errorf("follow-up = %+v", m)
			}
		case "owner@example.com":
			notifications++
		}
	}
	if followUps != 1 || notifications != 1 {
		t.Fatalf("follow-ups %d, notifications %d; want one each", followUps, notifications)
	}
}

// interrupter stands in for the hand-offs while River cancels the attempt's context in the middle of one of them, as it does SHUTDOWN_TIMEOUT after the worker is asked to stop.
type interrupter struct {
	cancel context.CancelFunc
	at     string
	order  []string
}

func (i *interrupter) step(ctx context.Context, name string) error {
	i.order = append(i.order, name)
	if name != i.at {
		return nil
	}
	i.cancel()
	return fmt.Errorf("%s: %w", name, ctx.Err())
}

func (i *interrupter) EnqueueResponseCompleted(ctx context.Context, _ *Event) error {
	return i.step(ctx, "workflows")
}

func (i *interrupter) HandleResponse(ctx context.Context, _ *Event) error {
	return i.step(ctx, "integrations")
}

// An attempt cancelled in the middle of the side effects fails, so River retries it, instead of completing with the follow-ups, emails and auto-complete it never ran.
func TestInterruptedAttemptIsRetried(t *testing.T) {
	for _, at := range []string{"workflows", "integrations"} {
		t.Run(at, func(t *testing.T) {
			rec := &interrupter{at: at}
			e := newEnv(t, Handoffs{Integrations: rec, Workflows: rec})
			f := e.fixture()
			owner := e.user("owner@example.com", f.survey)
			e.member(f.org, owner, "owner")
			r := e.respond(f, true, `{}`)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec.cancel = cancel
			err := e.worker.Work(ctx, job(response.EventResponseFinished, f, r, 1))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("an interrupted attempt = %v", err)
			}
			if at == "workflows" && fmt.Sprint(rec.order) != "[workflows]" {
				t.Errorf("steps after the interruption ran: %v", rec.order)
			}
			if len(e.mail.Messages()) != 0 {
				t.Errorf("emails were sent on an attempt that will be retried")
			}
		})
	}
}

type usageReports struct{ sent atomic.Int32 }

func (u *usageReports) Send(context.Context) { u.sent.Add(1) }

// TestResponseCreatedAnalytics: a created response sends survey_response_received at forma2's milestones (the first, every tenth to a hundred) with both groups, and asks for the usage update; a finished one does neither.
func TestResponseCreatedAnalytics(t *testing.T) {
	rec := analyticstest.Start(t)
	reports := &usageReports{}
	e := newEnv(t, Handoffs{UsageReport: reports})
	f := e.fixture()
	created := func() {
		t.Helper()
		if err := e.work(job(response.EventResponseCreated, f, e.respond(f, false, `{}`), 1)); err != nil {
			t.Fatal(err)
		}
	}
	created()
	for range 8 {
		e.exec(`INSERT INTO responses (survey_id) VALUES (?)`, f.survey)
	}
	created()
	created()
	if err := e.work(job(response.EventResponseFinished, f, e.respond(f, true, `{}`), 1)); err != nil {
		t.Fatal(err)
	}
	// Events are delivered in order, so once the sentinel arrives every event of this survey has.
	sentinel := "sentinel-" + f.survey.String()
	analytics.Capture(sentinel, "test_sentinel", nil, analytics.Groups{})
	rec.Events(t, "test_sentinel", 1)
	var milestones []string
	for _, m := range rec.All() {
		if m.Event != "survey_response_received" || m.Properties["survey_id"] != f.survey.String() {
			continue
		}
		milestones = append(milestones, m.Properties["milestone"].(string))
		groups, _ := m.Properties["$groups"].(map[string]any)
		if m.DistinctID != f.org.String() || groups["organization"] != f.org.String() || groups["workspace"] != f.ws.String() || m.Properties["survey_type"] != "app" || m.Properties["workspace_id"] != f.ws.String() {
			t.Errorf("survey_response_received = %+v", m)
		}
	}
	if fmt.Sprint(milestones) != "[first 10]" {
		t.Errorf("milestones = %v, want the first and the tenth response only", milestones)
	}
	if n := reports.sent.Load(); n != 3 {
		t.Errorf("usage update asked for %d times, want once per created response", n)
	}
}
