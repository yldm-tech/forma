package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// requestTimeout bounds one request to a provider. forma2 set none, so a provider that stopped answering held the pipeline job until its own timeout.
const requestTimeout = 30 * time.Second

// providers are the provider APIs, at their real addresses; a test points them at a server that speaks the provider's API. Every URL here is fixed: ids a user chose only ever appear path-escaped or in a request body.
type providers struct {
	cfg  config.IntegrationsConfig
	http *http.Client
	// webApp is WEBAPP_URL without a trailing slash, the origin of the Airtable, Notion and Slack redirect URIs.
	webApp string

	googleAuthURL   string
	googleTokenURL  string
	googleTokenInfo string
	// googleAPIs is the base of Google's OAuth2 userinfo API, sheets the base of the Sheets API.
	googleAPIs string
	sheets     string
	// airtable serves Airtable's OAuth endpoints, airtableAPI its REST API.
	airtable    string
	airtableAPI string
	notion      string
	slack       string
}

func newProviders(cfg config.Config) *providers {
	webApp := ""
	if cfg.WebAppURL != nil {
		webApp = trimSlash(cfg.WebAppURL.String())
	}
	return &providers{
		cfg:             cfg.Integrations,
		http:            &http.Client{Timeout: requestTimeout},
		webApp:          webApp,
		googleAuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		googleTokenURL:  "https://oauth2.googleapis.com/token",
		googleTokenInfo: "https://www.googleapis.com/oauth2/v1/tokeninfo",
		googleAPIs:      "https://www.googleapis.com/",
		sheets:          "https://sheets.googleapis.com/",
		airtable:        "https://airtable.com",
		airtableAPI:     "https://api.airtable.com",
		notion:          "https://api.notion.com",
		slack:           "https://slack.com",
	}
}

func trimSlash(s string) string { return strings.TrimRight(s, "/") }

// Redirect URIs of the providers whose path forma2 fixed under WEBAPP_URL; Google's is GOOGLE_SHEETS_REDIRECT_URL itself.
func (p *providers) airtableRedirectURI() string {
	return p.webApp + "/api/v1/integrations/airtable/callback"
}
func (p *providers) notionRedirectURI() string {
	return p.webApp + "/api/v1/integrations/notion/callback"
}
func (p *providers) slackRedirectURI() string {
	return p.webApp + "/api/v1/integrations/slack/callback"
}

// apiResponse is a provider's answer: its status line and body.
type apiResponse struct {
	Status     int
	StatusText string
	Body       []byte
}

func (r *apiResponse) ok() bool { return r.Status >= 200 && r.Status < 300 }

// statusText is the status line's reason as fetch's statusText gave it.
func statusText(status int) string { return http.StatusText(status) }

// do sends one request and reads the whole answer, whatever its status.
func (p *providers) do(ctx context.Context, method, url string, header http.Header, body []byte) (*apiResponse, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s %s answer: %w", method, req.URL.Host, err)
	}
	return &apiResponse{Status: res.StatusCode, StatusText: statusText(res.StatusCode), Body: raw}, nil
}

// encodeJS encodes v as JSON.stringify did: no HTML escaping and no trailing newline.
func encodeJS(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func bearer(token string) string { return "Bearer " + token }
