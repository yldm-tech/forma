package followup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/server/internal/platform/pgtest"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/valkey/valkeytest"
)

type env struct {
	t      *testing.T
	db     *gorm.DB
	mail   *mailertest.Transport
	sender *Sender
	survey uuid.UUID
}

// newEnv is a sender on a fresh database with a survey of one email question, a mail transport that records (nil for an instance without SMTP), and the real follow-up limit.
func newEnv(t *testing.T, smtp bool) *env {
	t.Helper()
	app, _ := url.Parse("https://app.example.com")
	cfg := config.Config{WebAppURL: app, MailFrom: "noreply@forma.ylam.ai", MailFromName: "Forma"}
	db := pgtest.GORM(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &env{t: t, db: db}
	var transport mailer.Transport
	if smtp {
		e.mail = &mailertest.Transport{}
		transport = e.mail
	}
	m, err := mailer.NewWithTransport(cfg, transport, log)
	if err != nil {
		t.Fatal(err)
	}
	frozen := time.Now()
	limiter := ratelimit.New(cfg, valkeytest.Client(t), log).WithClock(func() time.Time { return frozen })
	e.sender = NewSender(cfg, db, m, limiter, log)
	org := e.insert(`INSERT INTO organizations (name) VALUES ('Acme') RETURNING id`)
	ws := e.insert(`INSERT INTO workspaces (organization_id, name) VALUES (?, 'Main') RETURNING id`, org)
	e.survey = e.insert(`INSERT INTO surveys (workspace_id, name, blocks) VALUES (?, 'Feedback', ?::jsonb) RETURNING id`, ws,
		`[{"id":"b1","name":"B","elements":[{"id":"email","type":"openText","headline":{"default":"Email"},"required":false,"inputType":"email"},{"id":"contact","type":"contactInfo","headline":{"default":"Contact"},"required":false}]}]`)
	return e
}

func (e *env) insert(query string, args ...any) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	if err := e.db.Raw(query, args...).Row().Scan(&id); err != nil {
		e.t.Fatalf("insert %q: %v", query, err)
	}
	return id
}

// followUp adds a follow-up sending to `to`; endings non-nil makes it an endings trigger.
func (e *env) followUp(to string, endings []string, body string) uuid.UUID {
	e.t.Helper()
	trigger := `{"type":"response","properties":null}`
	if endings != nil {
		trigger = `{"type":"endings","properties":{"endingIds":["` + strings.Join(endings, `","`) + `"]}}`
	}
	action := `{"type":"send-email","properties":{"to":"` + to + `","from":"author@example.com","replyTo":["a@example.com","b@example.com"],"subject":"Hello #recall:email/fallback:x#","body":` + body + `,"attachResponseData":true,"includeVariables":false,"includeHiddenFields":false}}`
	return e.insert(`INSERT INTO survey_follow_ups (survey_id, name, trigger, action) VALUES (?, 'F', ?::jsonb, ?::jsonb) RETURNING id`, e.survey, trigger, action)
}

func (e *env) respond(data string, ending *string) uuid.UUID {
	return e.insert(`INSERT INTO responses (survey_id, finished, data, ending_id) VALUES (?, true, ?::jsonb, ?) RETURNING id`, e.survey, data, ending)
}

func byID(results []Result) map[string]Result {
	out := map[string]Result{}
	for _, r := range results {
		out[r.FollowUpID] = r
	}
	return out
}

// forma2's follow-ups.test.ts: the recipient rules, the endings trigger, the From the author typed being ignored, and the subject sent as written.
func TestSend(t *testing.T) {
	t.Parallel()
	e := newEnv(t, true)
	body := `"<p>Thanks #recall:email/fallback:friend#!</p><script>x()</script>"`
	literal := e.followUp("teammate@example.com", nil, body)
	fromAnswer := e.followUp("email", nil, body)
	fromContact := e.followUp("contact", nil, body)
	missing := e.followUp("nothing", nil, body)
	ending := e.followUp("teammate@example.com", []string{"end2"}, body)
	end1 := "end1"
	r := e.respond(`{"email":"jane@example.com","contact":["Jane","Doe","jane.doe@example.com","+1"]}`, &end1)

	results, err := e.sender.Send(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	got := byID(results)
	for id, want := range map[uuid.UUID]Result{
		literal:     {Status: StatusSuccess},
		fromAnswer:  {Status: StatusSuccess},
		fromContact: {Status: StatusSuccess},
		missing:     {Status: StatusError, Error: "To value not found in response data for followup: " + missing.String()},
		ending:      {Status: StatusSkipped},
	} {
		res := got[id.String()]
		if res.Status != want.Status || res.Error != want.Error {
			t.Errorf("follow-up %s = %+v, want %+v", id, res, want)
		}
	}
	sent := map[string]mailer.Message{}
	for _, m := range e.mail.Messages() {
		sent[m.To] = m
	}
	if len(sent) != 3 {
		t.Fatalf("sent to %v", sent)
	}
	m := sent["jane@example.com"]
	if m.Subject != "Hello #recall:email/fallback:x#" || m.ReplyTo != "a@example.com, b@example.com" || m.From != `"Forma" <noreply@forma.ylam.ai>` {
		t.Errorf("message = %+v", m)
	}
	if !strings.Contains(m.HTML, "<p>Thanks jane@example.com!</p>") || strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "Response data") || strings.Contains(m.HTML, "author@example.com") {
		t.Errorf("body:\n%s", m.HTML)
	}
	if _, ok := sent["jane.doe@example.com"]; !ok {
		t.Error("a contact info answer must resolve to its email")
	}

	end2 := "end2"
	results, _ = e.sender.Send(context.Background(), e.respond(`{"email":"not-an-email"}`, &end2), "")
	got = byID(results)
	if got[ending.String()].Status != StatusSuccess || got[fromAnswer.String()].Error != "Email address is not valid for followup: "+fromAnswer.String() {
		t.Errorf("second response = %+v", results)
	}
	if got[fromContact.String()].Error != "To value not found in response data for followup: "+fromContact.String() {
		t.Errorf("missing contact = %+v", got[fromContact.String()])
	}
	results, _ = e.sender.Send(context.Background(), e.respond(`{"contact":["Jane","Doe",""]}`, nil), "")
	if res := byID(results)[fromContact.String()]; res.Error != "Email address not found in response data for followup: "+fromContact.String() {
		t.Errorf("contact without email = %+v", res)
	}
}

func TestSendFailures(t *testing.T) {
	t.Parallel()
	e := newEnv(t, false)
	id := e.followUp("teammate@example.com", nil, `"<p>Hi</p>"`)
	r := e.respond(`{}`, nil)
	results, err := e.sender.Send(context.Background(), r, "")
	if err != nil || len(results) != 1 || results[0].Error != "SMTP is not configured; follow-up email was not sent" || results[0].FollowUpID != id.String() {
		t.Fatalf("without SMTP = %+v, %v", results, err)
	}

	var codeErr *Error
	if _, err := e.sender.Send(context.Background(), uuid.New(), ""); !errors.As(err, &codeErr) || codeErr.Code != CodeResponseNotFound {
		t.Errorf("unknown response = %v", err)
	}

	withMail := newEnv(t, true)
	withMail.mail.Err = errors.New("relay refused")
	withMail.followUp("teammate@example.com", nil, `"<p>Hi</p>"`)
	results, _ = withMail.sender.Send(context.Background(), withMail.respond(`{}`, nil), "")
	if len(results) != 1 || results[0].Status != StatusError || !strings.Contains(results[0].Error, "failed to send email") {
		t.Errorf("failed delivery = %+v", results)
	}

	none := newEnv(t, true)
	if results, err := none.sender.Send(context.Background(), none.respond(`{}`, nil), ""); err != nil || len(results) != 0 {
		t.Errorf("no follow-ups = %+v, %v", results, err)
	}
}

// One unit of the organization's limit per response: over 50 an hour, every follow-up of the response is dropped.
func TestSendRateLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t, true)
	e.followUp("teammate@example.com", nil, `"<p>Hi</p>"`)
	e.followUp("other@example.com", nil, `"<p>Hi</p>"`)
	for i := range ratelimit.ActionSurveyFollowUp.Limit {
		if _, err := e.sender.Send(context.Background(), e.respond(`{}`, nil), ""); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	before := len(e.mail.Messages())
	_, err := e.sender.Send(context.Background(), e.respond(`{}`, nil), "")
	if !IsRateLimited(err) {
		t.Fatalf("over the limit = %v", err)
	}
	if len(e.mail.Messages()) != before || before != 2*ratelimit.ActionSurveyFollowUp.Limit {
		t.Errorf("%d emails before, %d after the limit", before, len(e.mail.Messages()))
	}
}
