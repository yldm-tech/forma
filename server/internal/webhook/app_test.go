package webhook

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// The settings page's create flow (forma2's add-webhook-modal): the endpoint test signs with a new secret and returns it, the create stores that secret and returns it once, and the list never shows it.
func TestAppCreateFlow(t *testing.T) {
	e := newEnv(t, true)
	owner := e.user()
	_, ws := e.organization(owner)
	s1, s2 := e.survey(ws), e.survey(ws)
	rec := newReceiver(t, http.StatusTeapot)

	res := e.do(http.MethodPost, "/api/app/workspaces/"+ws.String()+"/webhooks/test", asUser(owner), map[string]any{"url": rec.URL + "/hook"})
	if res.Code != http.StatusOK {
		t.Fatalf("test endpoint = %d %s", res.Code, res.Body)
	}
	test := decode[TestResult](t, res)
	if !test.Success || test.Secret == nil || !validSecret(*test.Secret) {
		t.Fatalf("test result = %+v", test)
	}
	got := rec.received()
	if len(got) != 1 || string(got[0].Body) != `{"event":"testEndpoint"}` {
		t.Fatalf("test request = %+v", got)
	}
	if _, err := uuid.Parse(got[0].Header.Get("Webhook-Id")); err != nil {
		t.Errorf("webhook-id %q is not a UUIDv7", got[0].Header.Get("Webhook-Id"))
	}
	wh, _ := standardwebhooks.NewWebhook(*test.Secret)
	if err := wh.Verify(got[0].Body, got[0].Header); err != nil {
		t.Fatalf("the test request does not verify with the returned secret: %v", err)
	}

	name := "Orders"
	res = e.do(http.MethodPost, "/api/app/workspaces/"+ws.String()+"/webhooks", asUser(owner), map[string]any{
		"name": name, "url": rec.URL + "/hook", "source": "user", "triggers": []string{"responseFinished", "responseCreated"},
		"surveyIds": []string{s1.String(), s2.String()}, "webhookSecret": *test.Secret,
	})
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", res.Code, res.Body)
	}
	created := decode[View](t, res)
	if created.Secret == nil || *created.Secret != *test.Secret || *created.Name != name || created.Source != SourceUser || len(created.SurveyIDs) != 2 || created.Triggers[0] != "responseFinished" {
		t.Fatalf("created = %+v", created)
	}
	if ev := e.lastAudit(); ev.Action != "created" || ev.Target.Type != "webhook" || ev.Target.ID != created.ID.String() || ev.Status != "success" || ev.Actor.ID != owner.String() {
		t.Errorf("audit = %+v", ev)
	}

	res = e.do(http.MethodPost, "/api/app/workspaces/"+ws.String()+"/webhooks", asUser(owner), map[string]any{"url": rec.URL, "triggers": []string{"responseUpdated"}})
	if res.Code != http.StatusCreated {
		t.Fatalf("second create = %d %s", res.Code, res.Body)
	}
	all := decode[View](t, res)
	if all.Source != SourceUser || len(all.SurveyIDs) != 0 || all.Secret == nil || !validSecret(*all.Secret) || all.Name != nil {
		t.Fatalf("defaults = %+v", all)
	}

	res = e.do(http.MethodGet, "/api/app/workspaces/"+ws.String()+"/webhooks", asUser(owner), nil)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "secret") || strings.Contains(res.Body.String(), "whsec_") {
		t.Fatalf("list = %d %s", res.Code, res.Body)
	}
	list := decode[[]View](t, res)
	if len(list) != 2 || list[0].ID != all.ID || list[1].ID != created.ID {
		t.Fatalf("list is not newest first: %+v", list)
	}
	var allSurveys bool
	if err := e.db.Raw(`SELECT all_surveys FROM webhooks WHERE id = ?`, all.ID).Row().Scan(&allSurveys); err != nil || !allSurveys {
		t.Errorf("an empty survey list must scope the webhook to every survey: %v %v", allSurveys, err)
	}
}

func TestAppCreateRules(t *testing.T) {
	e := newEnv(t, false)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	foreign := e.survey(e.workspace(org))
	path := "/api/app/workspaces/" + ws.String() + "/webhooks"
	valid := func(extra map[string]any) map[string]any {
		body := map[string]any{"url": "https://93.184.216.34/hook", "triggers": []string{"responseFinished"}}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}
	cases := []struct {
		name string
		as   reqHeaders
		body map[string]any
		code int
		want string
	}{
		{"read only", asUser(reader), valid(nil), http.StatusForbidden, "forbidden"},
		{"no triggers", asUser(owner), valid(map[string]any{"triggers": []string{}}), http.StatusBadRequest, "At least one trigger is required"},
		{"unknown trigger", asUser(owner), valid(map[string]any{"triggers": []string{"responseDeleted"}}), http.StatusBadRequest, "Invalid webhook trigger"},
		{"unknown source", asUser(owner), valid(map[string]any{"source": "carrier-pigeon"}), http.StatusBadRequest, "Invalid webhook source"},
		{"foreign survey", asUser(owner), valid(map[string]any{"surveyIds": []string{foreign.String()}}), http.StatusBadRequest, CodeForeignSurveys},
		{"private address", asUser(owner), valid(map[string]any{"url": "http://169.254.169.254/latest"}), http.StatusBadRequest, "Webhook URL must not point to private or internal IP addresses"},
		{"localhost", asUser(owner), valid(map[string]any{"url": "http://localhost:3000/"}), http.StatusBadRequest, "Webhook URL must not point to localhost or internal services"},
		{"bad secret", asUser(owner), valid(map[string]any{"webhookSecret": "hunter2"}), http.StatusBadRequest, CodeInvalidSecret},
	}
	for _, tc := range cases {
		res := e.do(http.MethodPost, path, tc.as, tc.body)
		if res.Code != tc.code || (tc.want != "" && !strings.Contains(res.Body.String(), tc.want)) {
			t.Errorf("%s: %d %s, want %d %q", tc.name, res.Code, res.Body, tc.code, tc.want)
		}
	}
}

func TestAppUpdateAndDelete(t *testing.T) {
	e := newEnv(t, false)
	owner, writer, reader := e.user(), e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, writer, "readWrite")
	e.grant(org, ws, reader, "read")
	sv := e.survey(ws)
	id := e.insert(`INSERT INTO webhooks (workspace_id, name, url, source, triggers, secret) VALUES (?, 'Zap', 'https://93.184.216.34/zap', 'zapier', '{responseFinished}', 'whsec_x') RETURNING id`, ws)
	path := "/api/app/webhooks/" + id.String()

	if res := e.do(http.MethodPatch, path, asUser(reader), map[string]any{"url": "https://93.184.216.34/new", "triggers": []string{"responseCreated"}}); res.Code != http.StatusForbidden {
		t.Fatalf("reader update = %d", res.Code)
	}
	res := e.do(http.MethodPatch, path, asUser(writer), map[string]any{"name": "", "url": "https://93.184.216.34/new", "source": "user", "triggers": []string{"responseCreated"}, "surveyIds": []string{sv.String()}})
	if res.Code != http.StatusOK {
		t.Fatalf("update = %d %s", res.Code, res.Body)
	}
	updated := decode[View](t, res)
	if *updated.Name != "" || updated.URL != "https://93.184.216.34/new" || updated.Source != SourceZapier || updated.Triggers[0] != "responseCreated" || len(updated.SurveyIDs) != 1 || updated.Secret != nil {
		t.Fatalf("updated = %+v", updated)
	}
	ev := e.lastAudit()
	if ev.Action != "updated" || ev.Target.ID != id.String() || ev.Changes == nil {
		t.Errorf("update audit = %+v", ev)
	}
	if res := e.do(http.MethodPatch, path, asUser(writer), map[string]any{"url": "http://127.0.0.1/", "triggers": []string{"responseCreated"}}); res.Code != http.StatusBadRequest {
		t.Errorf("update to a private URL = %d", res.Code)
	}
	if res := e.do(http.MethodPatch, "/api/app/webhooks/"+uuid.NewString(), asUser(writer), map[string]any{"url": "https://93.184.216.34/", "triggers": []string{"responseCreated"}}); res.Code != http.StatusNotFound {
		t.Errorf("update of an unknown webhook = %d", res.Code)
	}

	if res := e.do(http.MethodDelete, path, asUser(reader), nil); res.Code != http.StatusForbidden {
		t.Fatalf("reader delete = %d", res.Code)
	}
	if res := e.do(http.MethodDelete, path, asUser(writer), nil); res.Code != http.StatusNoContent {
		t.Fatalf("writer delete = %d %s", res.Code, res.Body)
	}
	if ev := e.lastAudit(); ev.Action != "deleted" || ev.Status != "success" || ev.Target.ID != id.String() {
		t.Errorf("delete audit = %+v", ev)
	}
	if res := e.do(http.MethodDelete, path, asUser(writer), nil); res.Code != http.StatusNotFound {
		t.Errorf("second delete = %d", res.Code)
	}
}

// forma2's testEndpoint: six statuses are failures with their translated messages, every other answer is reachable; an existing webhook is tested with its stored secret, which is never sent back.
func TestAppTestEndpoint(t *testing.T) {
	e := newEnv(t, true)
	owner, reader := e.user(), e.user()
	org, ws := e.organization(owner)
	e.grant(org, ws, reader, "read")
	path := "/api/app/workspaces/" + ws.String() + "/webhooks/test"
	for status, code := range map[int]string{
		500: "endpoint_internal_server_error", 404: "endpoint_not_found_error", 405: "endpoint_method_not_allowed_error",
		502: "endpoint_bad_gateway_error", 503: "endpoint_service_unavailable_error", 504: "endpoint_gateway_timeout_error",
		400: "", 401: "", 403: "", 418: "", 204: "",
	} {
		rec := newReceiver(t, status)
		res := e.do(http.MethodPost, path, asUser(owner), map[string]any{"url": rec.URL})
		if code == "" {
			if res.Code != http.StatusOK {
				t.Errorf("status %d: %d %s", status, res.Code, res.Body)
			}
			continue
		}
		if body := decode[errorBody](t, res); res.Code != http.StatusBadRequest || body.Code != code || body.Message == "" {
			t.Errorf("status %d: %d %+v, want %s", status, res.Code, body, code)
		}
	}

	stored := GenerateSecret()
	rec := newReceiver(t, http.StatusOK)
	id := e.insert(`INSERT INTO webhooks (workspace_id, url, triggers, secret) VALUES (?, ?, '{responseFinished}', ?) RETURNING id`, ws, rec.URL, stored)
	res := e.do(http.MethodPost, path, asUser(owner), map[string]any{"url": rec.URL + "/edited", "webhookId": id.String(), "secret": GenerateSecret()})
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("existing webhook test = %d %s", res.Code, res.Body)
	}
	got := rec.received()
	wh, _ := standardwebhooks.NewWebhook(stored)
	if len(got) != 1 || wh.Verify(got[0].Body, got[0].Header) != nil {
		t.Fatal("an existing webhook must be tested with its stored secret")
	}

	if res := e.do(http.MethodPost, path, asUser(reader), map[string]any{"url": rec.URL}); res.Code != http.StatusForbidden {
		t.Errorf("reader test = %d", res.Code)
	}
	otherOwner := e.user()
	_, otherWS := e.organization(otherOwner)
	if res := e.do(http.MethodPost, "/api/app/workspaces/"+otherWS.String()+"/webhooks/test", asUser(otherOwner), map[string]any{"url": rec.URL, "webhookId": id.String()}); res.Code != http.StatusForbidden {
		t.Errorf("testing another workspace's webhook = %d", res.Code)
	}
	raw, _ := json.Marshal(map[string]any{"url": "ftp://example.com"})
	if res := e.do(http.MethodPost, path, asUser(owner), string(raw)); res.Code != http.StatusBadRequest {
		t.Errorf("ftp = %d", res.Code)
	}
}
