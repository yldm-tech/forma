package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/yldm-tech/forma/server/internal/platform/jsvalue"
)

// notionVersion is the Notion API version forma2 spoke.
const notionVersion = "2022-06-28"

// notionAuthorizeURL is forma2's NOTION_AUTH_URL with the state: a user-owned (owner=user) authorization, as the Notion app registration expects.
func (p *providers) notionAuthorizeURL(state string) string {
	q := []string{
		"client_id=" + url.QueryEscape(p.cfg.NotionClientID),
		"response_type=code",
		"owner=user",
		"redirect_uri=" + url.QueryEscape(p.notionRedirectURI()),
		"state=" + url.QueryEscape(state),
	}
	return p.notion + "/v1/oauth/authorize?" + strings.Join(q, "&")
}

// errNotionExchange is Notion refusing the code, or answering without a usable credential (an access_token and a bot_id), which the callback reports as oauth_error rather than replace a working connection.
var errNotionExchange = errors.New("notion OAuth token exchange failed")

// notionExchange is forma2's Notion callback token exchange. It returns Notion's token answer whole, which is what forma2 stored.
func (p *providers) notionExchange(ctx context.Context, code string) (json.RawMessage, error) {
	body, err := encodeJS(map[string]string{"grant_type": "authorization_code", "code": code, "redirect_uri": p.notionRedirectURI()})
	if err != nil {
		return nil, err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(p.cfg.NotionClientID + ":" + p.cfg.NotionClientSecret))
	res, err := p.do(ctx, http.MethodPost, p.notion+"/v1/oauth/token", http.Header{
		"Accept": {"application/json"}, "Content-Type": {"application/json"}, "Authorization": {"Basic " + basic},
	}, body)
	if err != nil {
		return nil, fmt.Errorf("notion token request: %w", err)
	}
	if !res.ok() {
		return nil, fmt.Errorf("%w: status %d", errNotionExchange, res.Status)
	}
	var k notionKey
	if json.Unmarshal(res.Body, &k) != nil || k.AccessToken == "" || k.BotID == "" {
		return nil, fmt.Errorf("%w: no access_token and bot_id", errNotionExchange)
	}
	return json.RawMessage(res.Body), nil
}

func notionHeaders(accessToken string) http.Header {
	return http.Header{
		"Accept": {"application/json"}, "Content-Type": {"application/json"},
		"Authorization": {bearer(accessToken)}, "Notion-Version": {notionVersion},
	}
}

// notionError is forma2's readNotionError: the code and message of Notion's error body, when it has them.
func notionError(body []byte) string {
	var e map[string]any
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	var parts []string
	for _, k := range []string{"code", "message"} {
		if s, ok := e[k].(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// NotionDatabase is a database the connected Notion workspace shares with the integration: its id, its title, and its properties in Notion's order.
type NotionDatabase struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Properties []NotionRef `json:"properties"`
}

// notionDatabases is forma2's fetchPages: the first hundred databases the search finds.
func (p *providers) notionDatabases(ctx context.Context, accessToken string) ([]NotionDatabase, error) {
	body, err := encodeJS(map[string]any{"page_size": 100, "filter": map[string]string{"value": "database", "property": "object"}})
	if err != nil {
		return nil, err
	}
	res, err := p.do(ctx, http.MethodPost, p.notion+"/v1/search", notionHeaders(accessToken), body)
	if err != nil {
		return nil, fmt.Errorf("notion search: %w", err)
	}
	if !res.ok() {
		return nil, fmt.Errorf("Notion API error fetching databases: %d %s %s", res.Status, res.StatusText, notionError(res.Body)) //nolint:staticcheck // forma2's message
	}
	var answer struct {
		Results []struct {
			ID    string `json:"id"`
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
			Properties json.RawMessage `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &answer); err != nil {
		return nil, fmt.Errorf("decode notion search: %w", err)
	}
	out := make([]NotionDatabase, 0, len(answer.Results))
	for _, r := range answer.Results {
		db := NotionDatabase{ID: r.ID, Properties: []NotionRef{}}
		if len(r.Title) > 0 {
			db.Name = r.Title[0].PlainText
		}
		if len(r.Properties) > 0 {
			members, err := jsvalue.DecodeObject(r.Properties)
			if err != nil {
				return nil, fmt.Errorf("decode notion database %s properties: %w", r.ID, err)
			}
			for _, m := range members {
				prop, _ := m.Value.(map[string]any)
				ref := NotionRef{}
				ref.ID, _ = prop["id"].(string)
				ref.Name, _ = prop["name"].(string)
				ref.Type, _ = prop["type"].(string)
				db.Properties = append(db.Properties, ref)
			}
		}
		out = append(out, db)
	}
	return out, nil
}

// writeNotion is forma2's notion writeData: one page in the database with the properties.
func (p *providers) writeNotion(ctx context.Context, accessToken, databaseID string, properties map[string]any) error {
	body, err := encodeJS(map[string]any{"parent": map[string]string{"database_id": databaseID}, "properties": properties})
	if err != nil {
		return err
	}
	res, err := p.do(ctx, http.MethodPost, p.notion+"/v1/pages", notionHeaders(accessToken), body)
	if err != nil {
		return fmt.Errorf("notion create page: %w", err)
	}
	if !res.ok() {
		return fmt.Errorf("Notion API error creating page: %d %s %s", res.Status, res.StatusText, notionError(res.Body)) //nolint:staticcheck // forma2's message
	}
	return nil
}
