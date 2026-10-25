package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/yldm-tech/forma/apps/server/internal/oauthserver"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// forma2's tools in its order: surveys, workflows, then list_workspaces.
var toolNames = []string{
	"list_surveys", "get_survey", "create_survey", "validate_survey", "patch_survey", "delete_survey", "archive_survey", "restore_survey",
	"list_workflows", "get_workflow", "list_workflow_runs", "get_workflow_run", "test_workflow", "create_workflow", "patch_workflow",
	"duplicate_workflow", "delete_workflow", "enable_workflow", "disable_workflow", "archive_workflow", "unarchive_workflow", "list_workspaces",
}

// A real MCP client finds the authorization server through the 401 challenge and the protected resource metadata, registers itself, sends the user through sign-in and consent, redeems the code with PKCE and calls tools with the access token.
func TestClientSignsInThroughOAuthAndCallsTools(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	ctx := t.Context()
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName: "Test client", RedirectURIs: []string{redirectURI}, GrantTypes: []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
		}},
		AuthorizationCodeFetcher: e.signIn(f.owner),
		RequestRefreshToken:      true,
		Client:                   e.server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: e.url("/api/mcp"), OAuthHandler: handler, HTTPClient: e.server.Client()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if n := e.count(`SELECT count(*) FROM oauth_clients WHERE name = 'Test client' AND application_type = 'native' AND token_endpoint_auth_method = 'none'`); n != 1 {
		t.Fatalf("%d registered clients", n)
	}
	if n := e.count(`SELECT count(*) FROM oauth_consents WHERE user_id = ? AND 'surveys:write' = ANY(scopes) AND 'offline_access' = ANY(scopes)`, f.owner); n != 1 {
		t.Fatalf("%d consents", n)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(tools.Tools))
	for i, tool := range tools.Tools {
		names[i] = tool.Name
	}
	if !slices.Equal(names, toolNames) {
		t.Fatalf("tools %v", names)
	}
	for _, tool := range tools.Tools {
		schema, _ := tool.InputSchema.(map[string]any)
		if schema["additionalProperties"] != false || tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Errorf("%s: schema %v annotations %+v", tool.Name, schema["additionalProperties"], tool.Annotations)
		}
	}

	workspaces := call(t, session, "list_workspaces", map[string]any{})
	data := workspaces["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["name"] != "Beta" || data[1].(map[string]any)["id"] != f.ws.String() ||
		data[1].(map[string]any)["organizationId"] != f.org.String() || workspaces["meta"].(map[string]any)["totalCount"] != float64(2) || workspaces["requestId"] == "" {
		t.Fatalf("list_workspaces %v", workspaces)
	}
	surveys := call(t, session, "list_surveys", map[string]any{"workspaceId": f.ws.String(), "filter": map[string]any{"status": map[string]any{"in": []string{"inProgress"}}}})
	if list := surveys["data"].([]any); len(list) != 1 || list[0].(map[string]any)["id"] != f.survey.String() || surveys["meta"].(map[string]any)["limit"] != float64(20) {
		t.Fatalf("list_surveys %v", surveys)
	}
	archived := call(t, session, "archive_survey", map[string]any{"surveyId": f.survey.String()})
	if archived["data"].(map[string]any)["id"] != f.survey.String() {
		t.Fatalf("archive_survey %v", archived)
	}
	// The mutation is audited as the user, through /api/mcp.
	var found bool
	for _, rec := range e.logs.records(t, "audit") {
		if rec["action"] == "archived" && rec["apiUrl"] == e.url("/api/mcp") && rec["actor"].(map[string]any)["id"] == f.owner.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("archive_survey was not audited through /api/mcp")
	}
}

// call runs a tool that must succeed and returns its structured content.
func call(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*sdk.TextContent).Text
	if res.IsError {
		t.Fatalf("%s failed: %s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	structured, _ := json.Marshal(res.StructuredContent)
	if !jsonEqual(structured, []byte(text)) {
		t.Fatalf("%s: structured content %s differs from text %s", name, structured, text)
	}
	return out
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && string(mustJSON(x)) == string(mustJSON(y))
}

func mustJSON(v any) []byte {
	out, _ := json.Marshal(v)
	return out
}

// rpc posts one JSON-RPC message to /api/mcp as a 2025-06-18 client without a session would, and decodes the answer, JSON or an event stream.
func (e *env) rpc(headers map[string]string, method string, params any) (reply, map[string]any) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	h := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Mcp-Protocol-Version": "2025-06-18"}
	for k, v := range headers {
		h[k] = v
	}
	res := e.send(e.server.Client(), http.MethodPost, e.url("/api/mcp"), string(body), h)
	raw := res.body
	for _, line := range strings.Split(string(raw), "\n") {
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			raw = []byte(data)
		}
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return res, out
}

// tool calls a tool over raw JSON-RPC and returns whether it failed and its payload.
func (e *env) tool(headers map[string]string, name string, args any) (bool, map[string]any, string) {
	e.t.Helper()
	res, out := e.rpc(headers, "tools/call", map[string]any{"name": name, "arguments": args})
	result, ok := out["result"].(map[string]any)
	if res.StatusCode != http.StatusOK || !ok {
		e.t.Fatalf("%s: %d %v", name, res.StatusCode, out)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	payload, _ := result["structuredContent"].(map[string]any)
	return result["isError"] == true, payload, text
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// The scope gate runs before every tool: a token without the tool's scope gets a 403 result naming the scopes it needs, and list_workspaces admits either read scope.
func TestToolScopes(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	read := bearer(e.accessToken(f.owner, "surveys:read"))
	failed, payload, _ := e.tool(read, "create_survey", map[string]any{"workspaceId": f.ws.String(), "name": "x", "blocks": []any{map[string]any{}}})
	errObj, _ := payload["error"].(map[string]any)
	if !failed || errObj["status"] != float64(403) || errObj["code"] != "forbidden" || errObj["title"] != "Forbidden" ||
		errObj["detail"] != "OAuth token does not include the required MCP scope: surveys:write" || errObj["requestId"] == "" {
		t.Fatalf("create_survey with surveys:read: %v", payload)
	}
	if failed, payload, _ := e.tool(read, "list_workflows", map[string]any{"workspaceId": f.ws.String()}); !failed || payload["error"].(map[string]any)["detail"] != "OAuth token does not include the required MCP scope: workflows:read" {
		t.Fatalf("list_workflows with surveys:read: %v", payload)
	}
	// validate_survey is a dry run and needs read access only.
	if failed, payload, text := e.tool(read, "validate_survey", map[string]any{"operation": "patch", "surveyId": f.survey.String(), "data": map[string]any{"name": "Renamed"}}); failed {
		t.Fatalf("validate_survey: %v %s", payload, text)
	}
	if n := e.count(`SELECT count(*) FROM surveys WHERE name = 'Renamed'`); n != 0 {
		t.Fatal("validate_survey wrote")
	}
	workflowsOnly := bearer(e.accessToken(f.owner, "workflows:read"))
	if failed, payload, _ := e.tool(workflowsOnly, "list_workspaces", map[string]any{}); failed || len(payload["data"].([]any)) != 2 {
		t.Fatalf("list_workspaces with workflows:read: %v", payload)
	}
	if failed, payload, _ := e.tool(workflowsOnly, "list_workflows", map[string]any{"workspaceId": f.ws.String()}); failed || payload["meta"] == nil {
		t.Fatalf("list_workflows: %v", payload)
	}
}

// A token holding no resource scope, offline_access alone, is refused at the transport with the insufficient_scope challenge.
func TestTokenWithoutResourceScope(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	res, out := e.rpc(bearer(e.accessToken(f.owner, "offline_access")), "tools/list", map[string]any{})
	if res.StatusCode != http.StatusForbidden || out["detail"] != "OAuth token does not include the required MCP scope" ||
		res.Header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope", scope="surveys:read surveys:write workflows:read workflows:write", resource_metadata="`+e.url("/.well-known/oauth-protected-resource/api/mcp")+`", error_description="The OAuth access token does not include the required MCP scope."` {
		t.Fatalf("%d %v %v", res.StatusCode, out, res.Header)
	}
}

// The workspace checks of the v3 API still apply: a user outside the survey's organization gets the 403 that hides whether it exists.
func TestToolsAreAuthorizedPerWorkspace(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	outsider := bearer(e.accessToken(f.outsider, "surveys:read surveys:write"))
	failed, payload, _ := e.tool(outsider, "get_survey", map[string]any{"surveyId": f.survey.String()})
	if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(403) || errObj["code"] != "forbidden" {
		t.Fatalf("get_survey of another organization: %v", payload)
	}
	failed, payload, _ = e.tool(outsider, "delete_survey", map[string]any{"surveyId": f.survey.String()})
	if errObj, _ := payload["error"].(map[string]any); !failed || errObj["status"] != float64(403) {
		t.Fatalf("delete_survey of another organization: %v", payload)
	}
	if e.count(`SELECT count(*) FROM surveys WHERE id = ?`, f.survey) != 1 {
		t.Fatal("the survey was deleted")
	}
	if failed, payload, _ := e.tool(outsider, "list_workspaces", map[string]any{}); failed || len(payload["data"].([]any)) != 1 || payload["data"].([]any)[0].(map[string]any)["name"] != "Elsewhere" {
		t.Fatalf("list_workspaces of the outsider: %v", payload)
	}
}

// Tokens /api/mcp must not honour: minted for another resource server (alone or besides this one), without a subject, of an inactive user, expired. All get the same 401 and challenge; the UserInfo audience the server adds for openid is accepted.
func TestOAuthTokenChecks(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	e.accessToken(f.owner, "surveys:read") // creates the signing key
	issuer := e.url("/api/auth")
	resource := e.url("/api/mcp")
	claims := func(change func(map[string]any)) string {
		c := map[string]any{"iss": issuer, "sub": f.owner.String(), "aud": []string{resource}, "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "scope": "surveys:read", "azp": "client"}
		change(c)
		return e.sign(c)
	}
	challenge := `Bearer resource_metadata="` + e.url("/.well-known/oauth-protected-resource/api/mcp") + `", scope="surveys:read surveys:write workflows:read workflows:write offline_access"`
	if res, out := e.rpc(bearer(claims(func(c map[string]any) { c["aud"] = []string{resource, issuer + "/oauth2/userinfo"} })), "tools/list", map[string]any{}); res.StatusCode != http.StatusOK || out["result"] == nil {
		t.Fatalf("the UserInfo audience was refused: %d %v", res.StatusCode, out)
	}
	if res, _ := e.rpc(bearer(claims(func(c map[string]any) { c["aud"] = resource })), "tools/list", map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("a single-string audience was refused: %d", res.StatusCode)
	}
	cases := map[string]struct {
		token  string
		detail string
	}{
		"another resource":         {claims(func(c map[string]any) { c["aud"] = []string{"https://other.example.com/api"} }), "Invalid OAuth access token"},
		"this and another":         {claims(func(c map[string]any) { c["aud"] = []string{resource, "https://other.example.com/api"} }), "Invalid OAuth access token"},
		"no audience":              {claims(func(c map[string]any) { delete(c, "aud") }), "Invalid OAuth access token"},
		"no subject":               {claims(func(c map[string]any) { delete(c, "sub") }), "User OAuth access token required"},
		"an unknown subject":       {claims(func(c map[string]any) { c["sub"] = uuid.NewString() }), "Invalid OAuth access token"},
		"expired":                  {claims(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Second).Unix() }), "Invalid OAuth access token"},
		"another issuer":           {claims(func(c map[string]any) { c["iss"] = "https://evil.example.com/api/auth" }), "Invalid OAuth access token"},
		"garbage":                  {"not-a-token", "Invalid OAuth access token"},
		"a key that is not issued": {"fbk_" + strings.Repeat("a", 43), "API key or OAuth access token required"},
	}
	for name, tc := range cases {
		res, out := e.rpc(bearer(tc.token), "tools/list", map[string]any{})
		if res.StatusCode != http.StatusUnauthorized || out["detail"] != tc.detail || res.Header.Get("WWW-Authenticate") != challenge || res.Header.Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %v %v", name, res.StatusCode, out, res.Header)
		}
	}
	e.exec(`UPDATE users SET is_active = false WHERE id = ?`, f.owner)
	if res, out := e.rpc(bearer(claims(func(map[string]any) {})), "tools/list", map[string]any{}); res.StatusCode != http.StatusUnauthorized || out["detail"] != "Invalid OAuth access token" {
		t.Fatalf("inactive user: %d %v", res.StatusCode, out)
	}
	// The reason is logged by its source only.
	if recs := e.logs.records(t, "MCP OAuth authentication failed"); len(recs) == 0 || recs[0]["failure_source"] != "token_verification" {
		t.Fatalf("log %v", recs)
	}
}

// Requests are refused before authentication for credentials in the query and for a foreign Origin; a missing credential is a 401 with the discovery challenge.
func TestRequestChecks(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	key := e.legacyKey(f.org, f.ws, "read")

	res, out := e.rpc(nil, "tools/list", map[string]any{})
	if res.StatusCode != http.StatusUnauthorized || out["detail"] != "API key or OAuth access token required" || out["instance"] != "/api/mcp" || out["code"] != "not_authenticated" ||
		res.Header.Get("WWW-Authenticate") != `Bearer resource_metadata="`+e.url("/.well-known/oauth-protected-resource/api/mcp")+`", scope="surveys:read surveys:write workflows:read workflows:write offline_access"` {
		t.Fatalf("no credentials: %d %v %v", res.StatusCode, out, res.Header)
	}
	for _, param := range []string{"api_key", "Access_Token", "X-API-KEY"} {
		res := e.send(e.server.Client(), http.MethodPost, e.url("/api/mcp?"+param+"=x"), `{}`, map[string]string{"x-api-key": key, "Content-Type": "application/json"})
		out := readJSON(t, res)
		if res.StatusCode != http.StatusBadRequest || out["detail"] != "API keys must be sent in headers, not query parameters" || !strings.Contains(mustString(out["invalid_params"]), "Send the API key with x-api-key or Authorization: Bearer.") {
			t.Errorf("%s: %d %v", param, res.StatusCode, out)
		}
	}
	for _, origin := range []string{"https://evil.example.com", "null", strings.Replace(e.server.URL, "127.0.0.1", "localhost", 1)} {
		res, out := e.rpc(map[string]string{"x-api-key": key, "Origin": origin}, "tools/list", map[string]any{})
		if res.StatusCode != http.StatusForbidden || out["detail"] != "Cross-origin MCP requests are not allowed" {
			t.Errorf("origin %s: %d %v", origin, res.StatusCode, out)
		}
	}
	res, out = e.rpc(map[string]string{"x-api-key": key, "Origin": e.server.URL, "X-Forwarded-Host": "evil.example.com", "X-Request-Id": "req_mcp"}, "tools/list", map[string]any{})
	if res.StatusCode != http.StatusOK || len(out["result"].(map[string]any)["tools"].([]any)) != len(toolNames) ||
		res.Header.Get("X-Request-Id") != "req_mcp" || res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("own origin: %d %v %v", res.StatusCode, out, res.Header)
	}
	if res := e.send(e.server.Client(), http.MethodGet, e.url("/api/mcp"), "", map[string]string{"x-api-key": key}); res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", res.StatusCode)
	}
}

func mustString(v any) string { return string(mustJSON(v)) }

// API keys authenticate through x-api-key or a Bearer current-format key. Every key may read; only a key with write or manage on a workspace gets the write scopes, and it sees only its own organization's workspaces it was granted.
func TestAPIKeys(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	read := e.apiKey(f.org, f.ws, "read")
	write := e.apiKey(f.org, f.ws, "write")

	if failed, payload, _ := e.tool(map[string]string{"x-api-key": e.legacyKey(f.org, f.ws, "read")}, "list_surveys", map[string]any{"workspaceId": f.ws.String()}); failed || len(payload["data"].([]any)) != 1 {
		t.Fatalf("read key: %v", payload)
	}
	if failed, payload, _ := e.tool(bearer(read), "patch_survey", map[string]any{"surveyId": f.survey.String(), "data": map[string]any{"name": "Renamed"}}); !failed || payload["error"].(map[string]any)["detail"] != "OAuth token does not include the required MCP scope: surveys:write" {
		t.Fatalf("read key writing: %v", payload)
	}
	failed, payload, text := e.tool(bearer(write), "patch_survey", map[string]any{"surveyId": f.survey.String(), "data": map[string]any{"name": "Renamed"}})
	if failed || payload["data"].(map[string]any)["name"] != "Renamed" {
		t.Fatalf("write key: %s", text)
	}
	if failed, payload, _ := e.tool(map[string]string{"x-api-key": e.legacyKey(f.org, f.ws, "manage")}, "list_workspaces", map[string]any{}); failed || len(payload["data"].([]any)) != 1 || payload["data"].([]any)[0].(map[string]any)["id"] != f.ws.String() {
		t.Fatalf("list_workspaces with a key: %v", payload)
	}
	// An empty x-api-key falls through to the Authorization header.
	if res, _ := e.rpc(map[string]string{"x-api-key": " ", "Authorization": "Bearer " + read}, "tools/list", map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("empty x-api-key: %d", res.StatusCode)
	}
	if res, out := e.rpc(map[string]string{"x-api-key": "fbk_unknown"}, "tools/list", map[string]any{}); res.StatusCode != http.StatusUnauthorized || out["detail"] != "API key or OAuth access token required" {
		t.Fatalf("unknown key: %d %v", res.StatusCode, out)
	}
}

// Arguments are strict at every structured depth: an undeclared key fails before the operation and names itself. Defaults apply, and the archived pseudo-status is accepted.
func TestArgumentValidation(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	key := map[string]string{"x-api-key": e.legacyKey(f.org, f.ws, "write")}
	cases := map[string]map[string]any{
		"workspaceID":           {"workspaceID": f.ws.String()},
		"userId":                {"workspaceId": f.ws.String(), "userId": "u"},
		"include":               {"workspaceId": f.ws.String(), "filter": map[string]any{"status": map[string]any{"include": []string{"draft"}}}},
		"owner":                 {"workspaceId": f.ws.String(), "filter": map[string]any{"owner": "me"}},
		"limit must be at most": {"workspaceId": f.ws.String(), "limit": 101},
	}
	for name, args := range cases {
		failed, _, text := e.tool(key, "list_surveys", args)
		if !failed || !strings.Contains(text, `validating "arguments"`) || (!strings.Contains(text, name) && !strings.Contains(text, "maximum")) {
			t.Errorf("%s: %v %s", name, failed, text)
		}
	}
	if failed, _, text := e.tool(key, "list_workspaces", map[string]any{"placeholder": true}); !failed || !strings.Contains(text, "placeholder") {
		t.Fatalf("list_workspaces with an argument: %s", text)
	}
	failed, payload, text := e.tool(key, "list_surveys", map[string]any{"workspaceId": f.ws.String(), "filter": map[string]any{"status": map[string]any{"in": []string{"archived"}}}})
	if failed || len(payload["data"].([]any)) != 0 || payload["meta"].(map[string]any)["limit"] != float64(20) {
		t.Fatalf("archived filter: %s", text)
	}
}

// The transport: the body limit applies before authentication, whether or not the length is advertised; subscriptions/listen is refused instead of held open; an old client may list tools without initializing.
func TestTransport(t *testing.T) {
	e := newEnv(t)
	f := e.fixture()
	key := e.legacyKey(f.org, f.ws, "read")
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"padding":"` + strings.Repeat("x", 2<<20) + `"}}`

	req, _ := http.NewRequest(http.MethodPost, e.url("/api/mcp"), strings.NewReader(big))
	req.Header.Set("X-Request-Id", "req_large")
	res := e.do(e.server.Client(), req)
	out := readJSON(t, res)
	if res.StatusCode != http.StatusRequestEntityTooLarge || res.Header.Get("X-Request-Id") != "req_large" || out["detail"] != "Request body must not exceed 2097152 bytes" {
		t.Fatalf("advertised: %d %v", res.StatusCode, out)
	}
	// A body without a length is counted as it is read.
	req, _ = http.NewRequest(http.MethodPost, e.url("/api/mcp"), io.MultiReader(strings.NewReader(big)))
	req.ContentLength = -1
	req.Header.Set("X-Request-Id", "req_chunked")
	res = e.do(e.server.Client(), req)
	out = readJSON(t, res)
	if res.StatusCode != http.StatusRequestEntityTooLarge || res.Header.Get("X-Request-Id") != "req_chunked" || out["code"] != "payload_too_large" {
		t.Fatalf("streamed: %d %v", res.StatusCode, out)
	}

	_, listen := e.rpc(map[string]string{"x-api-key": key, "Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "subscriptions/listen"}, "subscriptions/listen", map[string]any{
		"notifications": map[string]any{"toolsListChanged": true},
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{},
			"io.modelcontextprotocol/clientInfo": map[string]any{"name": "route-test", "version": "1.0.0"},
		},
	})
	if errObj, _ := listen["error"].(map[string]any); errObj["code"] != float64(-32603) || !strings.Contains(mustString(errObj["message"]), "Subscription limit") {
		t.Fatalf("subscriptions/listen: %v", listen)
	}

	res, list := e.rpc(map[string]string{"x-api-key": key}, "tools/list", map[string]any{})
	if res.StatusCode != http.StatusOK || len(list["result"].(map[string]any)["tools"].([]any)) != len(toolNames) {
		t.Fatalf("tools/list without initialize: %d %v", res.StatusCode, list)
	}
	_, initialized := e.rpc(map[string]string{"x-api-key": key}, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "c", "version": "1"}})
	info := initialized["result"].(map[string]any)["serverInfo"].(map[string]any)
	if info["name"] != "forma-xm-suite" || info["version"] != "0.2.0" {
		t.Fatalf("initialize: %v", initialized)
	}
}

// The protected resource metadata is the same at the root and at /api/mcp, advertises exactly the scopes the challenge names, and nothing else is a resource.
func TestProtectedResourceMetadata(t *testing.T) {
	e := newEnv(t)
	var bodies []string
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/api/mcp"} {
		res := e.get(e.server.Client(), e.url(path), nil)
		bodies = append(bodies, string(res.body))
		var out map[string]any
		_ = json.Unmarshal(res.body, &out)
		if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "public, max-age=15, stale-while-revalidate=15, stale-if-error=86400" ||
			out["resource"] != e.url("/api/mcp") || mustString(out["authorization_servers"]) != `["`+e.url("/api/auth")+`"]` ||
			mustString(out["scopes_supported"]) != mustString(oauthserver.ProtectedResourceScopes) || mustString(out["bearer_methods_supported"]) != `["header"]` {
			t.Fatalf("%s: %d %v", path, res.StatusCode, out)
		}
	}
	if bodies[0] != bodies[1] {
		t.Fatal("the two metadata documents differ")
	}
	if res := e.get(e.server.Client(), e.url("/.well-known/oauth-protected-resource/api/other"), nil); res.StatusCode != http.StatusNotFound || mustString(readJSON(t, res)) != `{"error":"not_found"}` {
		t.Fatalf("another resource: %d", res.StatusCode)
	}
}

// Failed authentication charges the per-address limit first; an API key or OAuth caller is limited to api:v3's 100 a minute.
func TestRateLimits(t *testing.T) {
	e := newEnv(t, envOptions{rateLimits: true})
	f := e.fixture()
	key := e.legacyKey(f.org, f.ws, "read")
	ctx := t.Context()
	if err := e.limiter.ApplyN(ctx, ratelimit.APIMCPAuth, ratelimit.IPIdentifier(ctx), ratelimit.APIMCPAuth.Limit); err != nil {
		t.Fatal(err)
	}
	res, out := e.rpc(nil, "tools/list", map[string]any{})
	if res.StatusCode != http.StatusTooManyRequests || out["code"] != "too_many_requests" || res.Header.Get("Retry-After") == "" || res.Header.Get("WWW-Authenticate") != "" {
		t.Fatalf("unauthenticated: %d %v %v", res.StatusCode, out, res.Header)
	}
	// A valid key is not charged to that limit.
	if res, _ := e.rpc(map[string]string{"x-api-key": key}, "tools/list", map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("valid key: %d", res.StatusCode)
	}
	var keyID uuid.UUID
	if err := e.db.Raw(`SELECT id FROM api_keys ORDER BY created_at DESC LIMIT 1`).Row().Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := e.limiter.ApplyN(ctx, ratelimit.APIV3, keyID.String(), ratelimit.APIV3.Limit-1); err != nil {
		t.Fatal(err)
	}
	if res, out := e.rpc(map[string]string{"x-api-key": key}, "tools/list", map[string]any{}); res.StatusCode != http.StatusTooManyRequests || out["code"] != "too_many_requests" {
		t.Fatalf("api:v3: %d %v", res.StatusCode, out)
	}
}
