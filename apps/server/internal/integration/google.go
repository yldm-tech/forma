package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
	oauth2api "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// googleScopes are what the Google Sheets integration asks for, forma2's scopes.
var googleScopes = []string{
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/userinfo.email",
}

// Error codes of the Google Sheets integration, forma2's GOOGLE_SHEET_INTEGRATION_INVALID_GRANT and _INSUFFICIENT_PERMISSION, which the page maps to its reconnect and permission messages.
const (
	CodeInvalidGrant           = "invalid_grant"
	CodeInsufficientPermission = "insufficient_permission"
)

// errInvalidGrant is a Google credential that cannot be refreshed: no refresh token, or Google refused it. Only reconnecting helps.
var errInvalidGrant = errors.New(CodeInvalidGrant)

// errInsufficientPermission is a spreadsheet the connected account may not open.
var errInsufficientPermission = errors.New(CodeInsufficientPermission)

func (p *providers) googleOAuth() *oauth2.Config {
	return &oauth2.Config{
		ClientID: p.cfg.GoogleSheetsClientID, ClientSecret: p.cfg.GoogleSheetsClientSecret, RedirectURL: p.cfg.GoogleSheetsRedirectURL,
		Scopes:   googleScopes,
		Endpoint: oauth2.Endpoint{AuthURL: p.googleAuthURL, TokenURL: p.googleTokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// withClient makes x/oauth2 send its token requests through the providers' client.
func (p *providers) withClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, p.http)
}

// googleAuthorizeURL is googleapis' generateAuthUrl({access_type: "offline", prompt: "consent", scope, state}): offline access, so Google returns a refresh token, and the consent screen every time, so it returns one on a reconnect too.
func (p *providers) googleAuthorizeURL(state string) string {
	return p.googleOAuth().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
}

// googleKey is the stored form of a token Google issued, as google-auth-library kept it: expires_in turned into expiry_date in epoch milliseconds.
func googleKey(t *oauth2.Token, fallback GoogleKey) GoogleKey {
	k := fallback
	k.TokenType = "Bearer"
	if scope, ok := t.Extra("scope").(string); ok && scope != "" {
		k.Scope = scope
	}
	if !t.Expiry.IsZero() {
		k.ExpiryDate = t.Expiry.UnixMilli()
	}
	if t.AccessToken != "" {
		k.AccessToken = t.AccessToken
	}
	if t.RefreshToken != "" {
		k.RefreshToken = t.RefreshToken
	}
	return k
}

// googleExchange is oAuth2Client.getToken(code).
func (p *providers) googleExchange(ctx context.Context, code string) (*GoogleKey, error) {
	t, err := p.googleOAuth().Exchange(p.withClient(ctx), code)
	if err != nil {
		return nil, fmt.Errorf("exchange Google authorization code: %w", err)
	}
	k := googleKey(t, GoogleKey{})
	return &k, nil
}

// googleEmail is the connected account's address, from the OAuth2 userinfo API.
func (p *providers) googleEmail(ctx context.Context, accessToken string) (string, error) {
	svc, err := oauth2api.NewService(ctx, option.WithHTTPClient(p.googleClient(ctx, accessToken)), option.WithEndpoint(p.googleAPIs))
	if err != nil {
		return "", fmt.Errorf("google userinfo client: %w", err)
	}
	info, err := svc.Userinfo.Get().Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("google userinfo: %w", err)
	}
	return info.Email, nil
}

// googleClient sends requests authorized with accessToken.
func (p *providers) googleClient(ctx context.Context, accessToken string) *http.Client {
	return oauth2.NewClient(p.withClient(ctx), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken, TokenType: "Bearer"}))
}

// googleTokenValid is forma2's isAccessTokenValid: Google's tokeninfo accepts the token, so it was not revoked (the user removed the app's access). Any failure counts as invalid.
func (p *providers) googleTokenValid(ctx context.Context, accessToken string) bool {
	res, err := p.do(ctx, http.MethodGet, p.googleTokenInfo+"?access_token="+url.QueryEscape(accessToken), nil, nil)
	return err == nil && res.ok()
}

// googleRefresh is oAuth2Client.refreshAccessToken() with the stored refresh token. Google refusing it is errInvalidGrant.
func (p *providers) googleRefresh(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	t, err := p.googleOAuth().TokenSource(p.withClient(ctx), &oauth2.Token{RefreshToken: refreshToken}).Token()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), CodeInvalidGrant) {
			return nil, errInvalidGrant
		}
		return nil, fmt.Errorf("refresh Google token: %w", err)
	}
	return t, nil
}

func (p *providers) sheetsService(ctx context.Context, accessToken string) (*sheets.Service, error) {
	svc, err := sheets.NewService(ctx, option.WithHTTPClient(p.googleClient(ctx, accessToken)), option.WithEndpoint(p.sheets))
	if err != nil {
		return nil, fmt.Errorf("google sheets client: %w", err)
	}
	return svc, nil
}

// writeSheet is forma2's googleSheet writeData: the labels rewritten as the first row of the first sheet on every response, then the values appended below, each truncated to the Sheets cell limit, both as RAW input.
func (p *providers) writeSheet(ctx context.Context, accessToken, spreadsheetID string, values, labels []string) error {
	svc, err := p.sheetsService(ctx, accessToken)
	if err != nil {
		return err
	}
	header := make([]any, len(labels))
	for i, l := range labels {
		header[i] = l
	}
	row := make([]any, len(values))
	for i, v := range values {
		row[i] = truncate(v, googleSheetMessageLimit)
	}
	if _, err := svc.Spreadsheets.Values.Update(spreadsheetID, "A1", &sheets.ValueRange{Values: [][]any{header}}).ValueInputOption("RAW").Context(ctx).Do(); err != nil {
		return fmt.Errorf("Error while appending data: %w", err) //nolint:staticcheck // forma2's message, which the log keeps
	}
	if _, err := svc.Spreadsheets.Values.Append(spreadsheetID, "A2", &sheets.ValueRange{Values: [][]any{row}}).ValueInputOption("RAW").Context(ctx).Do(); err != nil {
		return fmt.Errorf("Error while appending data: %w", err) //nolint:staticcheck // forma2's message, which the log keeps
	}
	return nil
}

// spreadsheetName is forma2's getSpreadsheetNameById: the spreadsheet's title. A permission refusal is errInsufficientPermission; any other failure, or a spreadsheet without a title, an error with forma2's message.
func (p *providers) spreadsheetName(ctx context.Context, accessToken, spreadsheetID string) (string, error) {
	svc, err := p.sheetsService(ctx, accessToken)
	if err != nil {
		return "", err
	}
	sheet, err := svc.Spreadsheets.Get(spreadsheetID).Fields("properties.title").Context(ctx).Do()
	if err != nil {
		msg := strings.ToLower(err.Error())
		for _, s := range []string{"permission", "caller does not have", "insufficient permission", "access denied"} {
			if strings.Contains(msg, s) {
				return "", errInsufficientPermission
			}
		}
		return "", fmt.Errorf("Error while fetching spreadsheet data: %w", err) //nolint:staticcheck // forma2's message, which the page shows
	}
	if sheet.Properties == nil || sheet.Properties.Title == "" {
		return "", errors.New("Error while fetching spreadsheet data: no title on the response") //nolint:staticcheck // forma2's message, which the page shows
	}
	return sheet.Properties.Title, nil
}
