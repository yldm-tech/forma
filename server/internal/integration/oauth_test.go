package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// oauthCaller is a signed-in user with one session, the owner of a workspace.
type oauthCaller struct {
	user, session, ws uuid.UUID
}

func (o oauthCaller) headers() reqHeaders {
	return reqHeaders{testUserHeader: o.user.String(), testSessionHeader: o.session.String(), "workspaceId": o.ws.String()}
}

func (e *env) owner() oauthCaller {
	u := e.user()
	_, ws := e.organization(u)
	return oauthCaller{user: u, session: uuid.New(), ws: ws}
}

type authorizeBody struct {
	Data struct {
		AuthURL string `json:"authUrl"`
	} `json:"data"`
}

// authorize starts the flow at path and returns the provider's authorize URL.
func (e *env) authorize(path string, o oauthCaller) *url.URL {
	e.t.Helper()
	rec := e.do(http.MethodGet, path, o.headers(), nil)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("authorize %s = %d %s", path, rec.Code, rec.Body)
	}
	u, err := url.Parse(decode[authorizeBody](e.t, rec).Data.AuthURL)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

func (e *env) callback(path string, o oauthCaller, query url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.do(http.MethodGet, path+"?"+query.Encode(), o.headers(), nil)
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	b := decode[struct{ Code, Message string }](t, rec)
	return b.Code, b.Message
}

// forma2's integration-state tests: the state is opaque (the workspace never appears in it), stored hashed for ten minutes, and consumed once by the provider, user and session it was issued to.
func TestOAuthState(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user, session, ws := uuid.New(), uuid.New(), uuid.New()
	st := e.svc.states
	state, err := st.create(ctx, oauthState{Provider: TypeSlack, UserID: user, SessionID: session, WorkspaceID: ws})
	if err != nil {
		t.Fatal(err)
	}
	if !stateToken.MatchString(state) || len(state) != 43 || strings.Contains(state, ws.String()) {
		t.Fatalf("state = %q", state)
	}
	ttl, err := e.rdb.TTL(ctx, "oauth:state:"+sha256Hex(state)).Result()
	if err != nil || ttl <= 9*time.Minute || ttl > 10*time.Minute {
		t.Fatalf("stored ttl = %v, %v", ttl, err)
	}
	if _, err := st.consume(ctx, TypeNotion, user, session, state); !errors.Is(err, errInvalidState) {
		t.Errorf("another provider: %v", err)
	}
	// The refused attempt used the state up.
	if _, err := st.consume(ctx, TypeSlack, user, session, state); !errors.Is(err, errInvalidState) {
		t.Errorf("a consumed state was accepted: %v", err)
	}

	for name, consume := range map[string]func(state string) error{
		"another user":    func(s string) error { _, err := st.consume(ctx, TypeSlack, uuid.New(), session, s); return err },
		"another session": func(s string) error { _, err := st.consume(ctx, TypeSlack, user, uuid.New(), s); return err },
	} {
		s, _ := st.create(ctx, oauthState{Provider: TypeSlack, UserID: user, SessionID: session, WorkspaceID: ws})
		if err := consume(s); !errors.Is(err, errInvalidState) {
			t.Errorf("%s: %v", name, err)
		}
	}

	s, _ := st.create(ctx, oauthState{Provider: TypeSlack, UserID: user, SessionID: session, WorkspaceID: ws})
	got, err := st.consume(ctx, TypeSlack, user, session, s)
	if err != nil || got.WorkspaceID != ws {
		t.Fatalf("consume = %+v, %v", got, err)
	}
	if _, err := st.consume(ctx, TypeSlack, user, session, s); !errors.Is(err, errInvalidState) {
		t.Errorf("a reused state was accepted: %v", err)
	}
	for _, malformed := range []string{"", "short", strings.Repeat("a", 42), strings.Repeat("a", 43) + "!", strings.Repeat("a", 129)} {
		if _, err := st.consume(ctx, TypeSlack, user, session, malformed); !errors.Is(err, errInvalidState) {
			t.Errorf("malformed %q: %v", malformed, err)
		}
	}
	if err := e.rdb.Set(ctx, "oauth:state:"+sha256Hex(strings.Repeat("b", 43)), "not json", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.consume(ctx, TypeSlack, user, session, strings.Repeat("b", 43)); !errors.Is(err, errInvalidState) {
		t.Errorf("a malformed stored state was accepted: %v", err)
	}

	// Without Valkey no flow starts and no callback is accepted.
	down := &stateStore{rdb: redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 100 * time.Millisecond}), now: time.Now}
	if _, err := down.create(ctx, oauthState{Provider: TypeSlack}); err == nil {
		t.Error("a state was created without Valkey")
	}
	if got, err := down.consume(ctx, TypeSlack, user, session, s); err == nil || got != nil {
		t.Error("a state was accepted without Valkey")
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// forma2's authorize routes: the workspace header, a session, workspace.write (401 unauthorized, as the v1 wrapper answered), the provider configured.
func TestAuthorizeRefusals(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	org := e.insert(`SELECT organization_id FROM workspaces WHERE id = ?`, o.ws)
	reader := e.user()
	e.grant(org, o.ws, reader, "read")

	for _, path := range []string{"/api/google-sheet", "/api/v1/integrations/airtable", "/api/v1/integrations/notion", "/api/v1/integrations/slack"} {
		h := o.headers()
		delete(h, "workspaceId")
		if rec := e.do(http.MethodGet, path, h, nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"message":"workspaceId is missing"`) {
			t.Errorf("%s without a workspace: %d %s", path, rec.Code, rec.Body)
		}
		if rec := e.do(http.MethodGet, path, reqHeaders{"workspaceId": o.ws.String()}, nil); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"not_authenticated"`) {
			t.Errorf("%s without a session: %d %s", path, rec.Code, rec.Body)
		}
		readOnly := reqHeaders{testUserHeader: reader.String(), "workspaceId": o.ws.String()}
		if rec := e.do(http.MethodGet, path, readOnly, nil); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"code":"unauthorized"`) {
			t.Errorf("%s as a read-only member: %d %s", path, rec.Code, rec.Body)
		}
	}

	e.svc.cfg.Integrations.GoogleSheetsClientSecret = ""
	e.svc.cfg.Integrations.SlackClientID = ""
	for path, message := range map[string]string{"/api/google-sheet": "Google client secret is missing", "/api/v1/integrations/slack": "Slack client id is missing"} {
		rec := e.do(http.MethodGet, path, o.headers(), nil)
		if code, msg := errorBody(t, rec); rec.Code != http.StatusInternalServerError || code != "internal_server_error" || msg != message {
			t.Errorf("%s unconfigured: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func (e *env) linksOf(id uuid.UUID) string {
	e.t.Helper()
	in, err := e.repo.ByID(context.Background(), id)
	if err != nil || in == nil {
		e.t.Fatalf("integration %s: %v", id, err)
	}
	return string(in.Config)
}

// forma2's Google Sheets OAuth flow: offline access with consent, the code exchanged and the account's email read, the credential stored with the integration's links kept, and the browser sent back to the page; provider errors reach the page sanitized, and a state works once.
func TestGoogleSheetsOAuthFlow(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	sv := e.survey(o.ws)
	e.fake.reply("POST /google/token", http.StatusOK, `{"access_token":"ya29.new","refresh_token":"1//refresh","expires_in":3599,"scope":"https://www.googleapis.com/auth/spreadsheets https://www.googleapis.com/auth/userinfo.email","token_type":"Bearer"}`)
	e.fake.reply("GET /googleapis/oauth2/v2/userinfo", http.StatusOK, `{"email":"sheets@example.com"}`)
	id := e.connect(o.ws, TypeGoogleSheets, GoogleKey{AccessToken: "old"}, "old@example.com", []SheetLink{{LinkBase: base(sv), Elements: elements("q1"), SpreadsheetID: "sheet1"}})
	links := e.linksOf(id)

	u := e.authorize("/api/google-sheet", o)
	q := u.Query()
	if u.Path != "/google/auth" || q.Get("client_id") != "google-id" || q.Get("redirect_uri") != webApp+"/api/google-sheet/callback" || q.Get("access_type") != "offline" ||
		q.Get("prompt") != "consent" || q.Get("response_type") != "code" || q.Get("scope") != strings.Join(googleScopes, " ") || len(q.Get("state")) != 43 {
		t.Fatalf("authorize URL = %s", u)
	}
	if strings.Contains(u.String(), o.ws.String()) {
		t.Error("the authorize URL names the workspace")
	}

	rec := e.callback("/api/google-sheet/callback", o, url.Values{"state": {q.Get("state")}, "code": {"auth-code"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != webApp+"/workspaces/"+o.ws.String()+"/integrations/google-sheets" {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	exchange := e.fake.to(http.MethodPost, "/google/token")
	form, _ := url.ParseQuery(string(exchange[0].Body))
	if form.Get("code") != "auth-code" || form.Get("grant_type") != "authorization_code" || form.Get("client_secret") != "google-secret" || form.Get("redirect_uri") != webApp+"/api/google-sheet/callback" {
		t.Errorf("exchange form = %v", form)
	}
	if got := e.fake.to(http.MethodGet, "/googleapis/oauth2/v2/userinfo"); len(got) != 1 || got[0].Header.Get("Authorization") != "Bearer ya29.new" {
		t.Errorf("userinfo = %+v", got)
	}
	stored, _ := e.repo.ByID(context.Background(), id)
	k, _ := key[GoogleKey](stored)
	if k.AccessToken != "ya29.new" || k.RefreshToken != "1//refresh" || k.TokenType != "Bearer" || k.ExpiryDate < e.now.Add(59*time.Minute).UnixMilli() || stored.Credentials.Email != "sheets@example.com" {
		t.Errorf("stored = %+v %q", k, stored.Credentials.Email)
	}
	if e.linksOf(id) != links {
		t.Errorf("links changed: %s", e.linksOf(id))
	}

	if rec := e.callback("/api/google-sheet/callback", o, url.Values{"state": {q.Get("state")}, "code": {"auth-code"}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"message":"Invalid OAuth state"`) {
		t.Errorf("reused state: %d %s", rec.Code, rec.Body)
	}

	for given, want := range map[string]string{"access_denied": "access_denied", "javascript:alert(1)": "oauth_error"} {
		state := e.authorize("/api/google-sheet", o).Query().Get("state")
		rec := e.callback("/api/google-sheet/callback", o, url.Values{"state": {state}, "error": {given}, "code": {"c"}})
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != webApp+"/workspaces/"+o.ws.String()+"/integrations/google-sheets?error="+want {
			t.Errorf("error %q: %d %s", given, rec.Code, rec.Header().Get("Location"))
		}
	}

	state := e.authorize("/api/google-sheet", o).Query().Get("state")
	otherSession := o
	otherSession.session = uuid.New()
	if rec := e.callback("/api/google-sheet/callback", otherSession, url.Values{"state": {state}, "code": {"c"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("a state from another session: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, "/api/google-sheet/callback?state="+state, nil, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a callback without a session: %d", rec.Code)
	}
}

// forma2's Airtable OAuth flow: PKCE with S256 and no secret, the verifier kept in the state, the token's expires_in stored as an ISO expiry, the email from whoami; a callback without a code, or whose exchange fails, is refused.
func TestAirtableOAuthFlow(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	failing := false
	e.fake.handle("POST /airtable/oauth2/v1/token", func(w http.ResponseWriter, _ *http.Request) {
		if failing {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600,"token_type":"Bearer"}`)
	})
	e.fake.reply("GET /airtableapi/v0/meta/whoami", http.StatusOK, `{"id":"usr1","email":"air@example.com"}`)

	u := e.authorize("/api/v1/integrations/airtable", o)
	q := u.Query()
	if u.Path != "/airtable/oauth2/v1/authorize" || q.Get("client_id") != "airtable-id" || q.Get("redirect_uri") != webApp+"/api/v1/integrations/airtable/callback" ||
		q.Get("scope") != airtableScope || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorize URL = %s", u)
	}
	rec := e.callback("/api/v1/integrations/airtable/callback", o, url.Values{"state": {q.Get("state")}, "code": {"air-code"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != webApp+"/workspaces/"+o.ws.String()+"/settings/workspace/integrations/airtable" {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	form, _ := url.ParseQuery(string(e.fake.to(http.MethodPost, "/airtable/oauth2/v1/token")[0].Body))
	verifier := form.Get("code_verifier")
	challenge := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(challenge[:]) != q.Get("code_challenge") || len(verifier) != 43 {
		t.Errorf("the verifier %q does not match the challenge %q", verifier, q.Get("code_challenge"))
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "air-code" || form.Get("client_id") != "airtable-id" || form.Has("client_secret") {
		t.Errorf("exchange form = %v", form)
	}
	in, _ := e.repo.ByType(context.Background(), o.ws, TypeAirtable)
	k, _ := key[AirtableKey](in)
	if k.AccessToken != "at" || k.RefreshToken != "rt" || !strings.HasSuffix(k.ExpiryDate, "Z") || in.Credentials.Email != "air@example.com" || string(in.Config) != `{"data": []}` {
		t.Errorf("stored = %+v %q %s", k, in.Credentials.Email, in.Config)
	}

	state := e.authorize("/api/v1/integrations/airtable", o).Query().Get("state")
	if rec := e.callback("/api/v1/integrations/airtable/callback", o, url.Values{"state": {state}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "`code` is missing") {
		t.Errorf("no code: %d %s", rec.Code, rec.Body)
	}
	failing = true
	state = e.authorize("/api/v1/integrations/airtable", o).Query().Get("state")
	rec = e.callback("/api/v1/integrations/airtable/callback", o, url.Values{"state": {state}, "code": {"c"}})
	if code, msg := errorBody(t, rec); rec.Code != http.StatusInternalServerError || code != "internal_server_error" || msg != "Unable to complete Airtable OAuth flow" {
		t.Errorf("failed exchange: %d %s", rec.Code, rec.Body)
	}
}

// forma2's Notion callback tests: the token answer is stored whole; a refused exchange, a 2xx without a credential or a body that is not JSON keep the existing credential and tell the page oauth_error.
func TestNotionOAuthFlow(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	answer := struct {
		status int
		body   string
	}{http.StatusOK, `{"access_token":"ntn_new","token_type":"bearer","bot_id":"bot-2","workspace_name":"Acme Notion","workspace_icon":null,"workspace_id":"w2","owner":{"type":"workspace","workspace":true},"duplicated_template_id":null}`}
	e.fake.handle("POST /notion/v1/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(answer.status)
		_, _ = io.WriteString(w, answer.body)
	})

	u := e.authorize("/api/v1/integrations/notion", o)
	want := e.fake.srv.URL + "/notion/v1/oauth/authorize?client_id=notion-id&response_type=code&owner=user&redirect_uri=" + url.QueryEscape(webApp+"/api/v1/integrations/notion/callback") + "&state=" + u.Query().Get("state")
	if u.String() != want {
		t.Fatalf("authorize URL = %s, want %s", u, want)
	}
	page := webApp + "/workspaces/" + o.ws.String() + "/settings/workspace/integrations/notion"
	rec := e.callback("/api/v1/integrations/notion/callback", o, url.Values{"state": {u.Query().Get("state")}, "code": {"n-code"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != page {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	exchange := e.fake.to(http.MethodPost, "/notion/v1/oauth/token")[0]
	if exchange.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("notion-id:notion-secret")) ||
		string(exchange.Body) != `{"code":"n-code","grant_type":"authorization_code","redirect_uri":"`+webApp+`/api/v1/integrations/notion/callback"}` {
		t.Errorf("exchange = %v %s", exchange.Header, exchange.Body)
	}
	in, _ := e.repo.ByType(context.Background(), o.ws, TypeNotion)
	var stored map[string]any
	_ = json.Unmarshal(in.Credentials.Key, &stored)
	if stored["access_token"] != "ntn_new" || stored["workspace_id"] != "w2" || stored["owner"] == nil {
		t.Errorf("stored = %v", stored)
	}

	for name, a := range map[string]struct {
		status int
		body   string
	}{
		"refused":      {http.StatusBadRequest, `{"error":"invalid_grant"}`},
		"no bot id":    {http.StatusOK, `{"access_token":"x"}`},
		"not JSON":     {http.StatusOK, `<html>`},
		"empty tokens": {http.StatusOK, `{"access_token":"","bot_id":""}`},
	} {
		answer.status, answer.body = a.status, a.body
		state := e.authorize("/api/v1/integrations/notion", o).Query().Get("state")
		rec := e.callback("/api/v1/integrations/notion/callback", o, url.Values{"state": {state}, "code": {"c"}})
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != page+"?error=oauth_error" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Header().Get("Location"))
		}
		in, _ := e.repo.ByType(context.Background(), o.ws, TypeNotion)
		if k, _ := key[notionKey](in); k.AccessToken != "ntn_new" {
			t.Errorf("%s replaced the credential", name)
		}
	}
	state := e.authorize("/api/v1/integrations/notion", o).Query().Get("state")
	if rec := e.callback("/api/v1/integrations/notion/callback", o, url.Values{"state": {state}}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Missing code or error parameter") {
		t.Errorf("no code: %d %s", rec.Code, rec.Body)
	}
}

// forma2's Slack OAuth flow: the registered scope, the bot credential stored as forma2 kept it with the links kept on reconnect, and Slack refusing the code answered with Slack's error.
func TestSlackOAuthFlow(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	sv := e.survey(o.ws)
	refuse := false
	e.fake.handle("POST /slack/api/oauth.v2.access", func(w http.ResponseWriter, _ *http.Request) {
		if refuse {
			_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_code"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"app_id":"A1","authed_user":{"id":"U1"},"scope":"x","token_type":"bot","access_token":"xoxb-new","bot_user_id":"B1","team":{"id":"T1","name":"Acme Slack"}}`)
	})
	id := e.connect(o.ws, TypeSlack, SlackKey{AccessToken: "xoxb-old"}, "", []SlackLink{{LinkBase: base(sv), Elements: elements("q1"), ChannelID: "C1"}})
	links := e.linksOf(id)

	u := e.authorize("/api/v1/integrations/slack", o)
	want := e.fake.srv.URL + "/slack/oauth/v2/authorize?client_id=slack-id&scope=channels%3Aread%2Cchat%3Awrite%2Cchat%3Awrite.public%2Cchat%3Awrite.customize%2Cgroups%3Aread&redirect_uri=" + url.QueryEscape(webApp+"/api/v1/integrations/slack/callback") + "&state=" + u.Query().Get("state")
	if u.String() != want {
		t.Fatalf("authorize URL = %s, want %s", u, want)
	}
	rec := e.callback("/api/v1/integrations/slack/callback", o, url.Values{"state": {u.Query().Get("state")}, "code": {"s-code"}})
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != webApp+"/workspaces/"+o.ws.String()+"/settings/workspace/integrations/slack" {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	form, _ := url.ParseQuery(string(e.fake.to(http.MethodPost, "/slack/api/oauth.v2.access")[0].Body))
	if form.Get("code") != "s-code" || form.Get("client_id") != "slack-id" || form.Get("client_secret") != "slack-secret" || form.Get("redirect_uri") != webApp+"/api/v1/integrations/slack/callback" {
		t.Errorf("exchange form = %v", form)
	}
	in, _ := e.repo.ByID(context.Background(), id)
	if string(in.Credentials.Key) != `{"app_id":"A1","authed_user":{"id":"U1"},"token_type":"bot","access_token":"xoxb-new","bot_user_id":"B1","team":{"id":"T1","name":"Acme Slack"}}` {
		t.Errorf("stored = %s", in.Credentials.Key)
	}
	if e.linksOf(id) != links {
		t.Error("reconnecting changed the links")
	}

	refuse = true
	state := e.authorize("/api/v1/integrations/slack", o).Query().Get("state")
	rec = e.callback("/api/v1/integrations/slack/callback", o, url.Values{"state": {state}, "code": {"bad"}})
	if code, msg := errorBody(t, rec); rec.Code != http.StatusBadRequest || code != "bad_request" || msg != "invalid_code" {
		t.Errorf("refused code: %d %s", rec.Code, rec.Body)
	}
}

// forma2's GET /api/v1/integrations/airtable/tables: the base id, then the workspace header, workspace.read, the integration, the tables.
func TestAirtableTablesRoute(t *testing.T) {
	e := newEnv(t)
	o := e.owner()
	if rec := e.do(http.MethodGet, "/api/v1/integrations/airtable/tables", o.headers(), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Base Id is Required") {
		t.Errorf("no base: %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodGet, "/api/v1/integrations/airtable/tables?baseId=app1", o.headers(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("no integration: %d %s", rec.Code, rec.Body)
	}
	e.airtableFake()
	e.connect(o.ws, TypeAirtable, freshAirtableKey(e.now), "a@example.com", nil)
	rec := e.do(http.MethodGet, "/api/v1/integrations/airtable/tables?baseId=app1", o.headers(), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != `{"data":{"tables":[{"id":"table1","name":"Table 1"},{"id":"table2","name":"Table 2"}]}}` {
		t.Errorf("tables: %d %s", rec.Code, rec.Body)
	}
}
