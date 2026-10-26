// Package linksurvey is the public survey pages respondents open: /s/{surveyId}, the personal /c/{token} links and the /p/{slug} pretty URLs, with the PIN and email verification gates in front of a survey and the OG image its link previews show. It ports forma2's modules/survey/link.
//
// The server decides everything a page may show and renders the document head; the page itself is the survey page bundle of the web app (apps/web/survey.html), which reads the state the server embeds and renders the survey with the respondent runtime (/js/surveys.umd.js).
package linksurvey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/sdkidentity"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

// Service resolves what a public survey page shows.
type Service struct {
	cfg     config.Config
	repo    *Repository
	surveys *survey.Service
	mailer  *mailer.Mailer
	log     *slog.Logger
	now     func() time.Time
}

func NewService(cfg config.Config, repo *Repository, surveys *survey.Service, m *mailer.Mailer, log *slog.Logger) *Service {
	return &Service{cfg: cfg, repo: repo, surveys: surveys, mailer: m, log: log, now: time.Now}
}

// The views of the survey page bundle.
const (
	viewNotFound    = "notFound"
	viewError       = "error"
	viewInactive    = "inactive"
	viewCompleted   = "completed"
	viewVerifyEmail = "verifyEmail"
	viewPin         = "pin"
	viewSurvey      = "survey"
)

// The statuses of forma2's SurveyInactive.
const (
	inactiveLinkInvalid       = "link invalid"
	inactiveLinkExpired       = "link expired"
	inactiveResponseSubmitted = "response submitted"
	// inactiveEmailResponded is a verification link whose address already finished a response to a survey that takes one response per verified email.
	inactiveEmailResponded = "email responded"
)

// page is one rendered survey page: its status, head and the state the bundle renders.
type page struct {
	Status int
	Head   head
	State  pageState
	// RuntimeLanguage is the survey language code the respondent runtime renders a survey view in (its getI18nLanguage): LanguageCode, with "default" read as the default language's code. The handler preloads the runtime's translations for it.
	RuntimeLanguage string
}

// pageState is the document the survey page bundle reads. Locale is the locale the page's own chrome is translated in: forma2's root layout locale, negotiated from Accept-Language.
type pageState struct {
	View        string            `json:"view"`
	Locale      string            `json:"locale"`
	Inactive    *inactiveState    `json:"inactive,omitempty"`
	Completed   *completedState   `json:"completed,omitempty"`
	VerifyEmail *verifyEmailState `json:"verifyEmail,omitempty"`
	Survey      *surveyState      `json:"survey,omitempty"`
	// BrowserLimit is set on the screens in front of and of a survey that takes one response per browser.
	BrowserLimit *browserLimitState `json:"browserLimit,omitempty"`
}

// browserLimitState is a link survey that takes one response per browser (isSingleResponsePerBrowserEnabled, owner decision D-FG03). The browser, not the server, knows whether it answered: once a response is finished the page remembers it under SurveyID in localStorage and a cookie (apps/web/src/survey-page/lib/browser-response.ts), and on a later visit shows "already responded", in Workspace's branding, instead of the screen the state names. Best effort: a private window, cleared site data or another browser answers again.
type browserLimitState struct {
	SurveyID  uuid.UUID `json:"surveyId"`
	Workspace *branding `json:"workspace"`
}

// branding is the part of a workspace the inactive screens read; nil is no workspace, which shows Forma's branding.
type branding struct {
	LinkSurveyBranding bool `json:"linkSurveyBranding"`
}

// inactiveState is forma2's SurveyInactive.
type inactiveState struct {
	Status              string          `json:"status"`
	IsScheduled         bool            `json:"isScheduled"`
	SurveyClosedMessage json.RawMessage `json:"surveyClosedMessage"`
	Workspace           *branding       `json:"workspace"`
}

// completedState is forma2's SurveyCompletedMessage: a single-use link whose response is finished.
type completedState struct {
	SingleUse json.RawMessage `json:"singleUse"`
	Workspace *branding       `json:"workspace"`
}

// verifyEmailState is forma2's VerifyEmail gate. Fishy is its error screen, for a verification link that does not verify.
type verifyEmailState struct {
	Fishy          bool            `json:"fishy"`
	Survey         json.RawMessage `json:"survey"`
	LanguageCode   string          `json:"languageCode"`
	Styling        json.RawMessage `json:"styling"`
	Locale         string          `json:"locale"`
	SingleUseID    string          `json:"singleUseId"`
	SingleUseToken *string         `json:"singleUseToken"`
}

// surveyState is forma2's SurveyClientWrapper props, and PinScreen's: behind a PIN, Survey is null until the PIN is entered and Locale is the gate's.
type surveyState struct {
	Survey                  json.RawMessage `json:"survey"`
	SurveyID                uuid.UUID       `json:"surveyId"`
	Workspace               workspaceState  `json:"workspace"`
	Styling                 json.RawMessage `json:"styling"`
	PublicDomain            string          `json:"publicDomain"`
	ResponseCount           *int            `json:"responseCount"`
	LanguageCode            string          `json:"languageCode"`
	Locale                  string          `json:"locale"`
	IsEmbed                 bool            `json:"isEmbed"`
	IsPreview               bool            `json:"isPreview"`
	SingleUseID             string          `json:"singleUseId,omitempty"`
	SingleUseResponseID     *uuid.UUID      `json:"singleUseResponseId,omitempty"`
	ContactID               string          `json:"contactId,omitempty"`
	CanReadUserIDFromURL    bool            `json:"canReadUserIdFromUrl"`
	RecaptchaSiteKey        string          `json:"recaptchaSiteKey,omitempty"`
	IsSpamProtectionEnabled bool            `json:"isSpamProtectionEnabled"`
	VerifiedEmail           string          `json:"verifiedEmail,omitempty"`
	ImprintURL              string          `json:"imprintUrl,omitempty"`
	PrivacyURL              string          `json:"privacyUrl,omitempty"`
	TermsURL                string          `json:"termsUrl,omitempty"`
	IsFormaCloud            bool            `json:"isFormaCloud"`
}

type workspaceState struct {
	Styling            json.RawMessage `json:"styling"`
	Logo               json.RawMessage `json:"logo"`
	LinkSurveyBranding bool            `json:"linkSurveyBranding"`
	CustomHeadScripts  *string         `json:"customHeadScripts"`
}

// publicDomain is forma2's getPublicDomain as a base URL: PUBLIC_URL when set, else WEBAPP_URL.
func (s *Service) publicDomain() *url.URL {
	if s.cfg.PublicURL != nil {
		return s.cfg.PublicURL
	}
	return s.cfg.WebAppURL
}

func (s *Service) publicDomainString() string {
	return strings.TrimRight(s.publicDomain().String(), "/")
}

// query is the request's query string as forma2's page read it: a parameter given more than once is an array there, which every check below treats as absent.
type query struct {
	values url.Values
	raw    string
}

func (q query) get(key string) string {
	if v := q.values[key]; len(v) == 1 {
		return v[0]
	}
	return ""
}

// userIDParam is forma2's getUserIdFromSearchParams: the first parameter named userId in any case, unless its value is empty.
func (q query) userIDParam() string {
	for pair := range strings.SplitSeq(q.raw, "&") {
		k, v, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(k)
		if err != nil || !strings.EqualFold(key, "userid") {
			continue
		}
		value, _ := url.QueryUnescape(v)
		return value
	}
	return ""
}

// renderInput is what forma2's renderSurvey received from the page that called it.
type renderInput struct {
	survey            *survey.Survey
	fields            []embeddeddata.LinkedField
	q                 query
	singleUseID       string
	singleUseResponse *existingResponse
	contactID         string
	allowURLUserID    bool
	workspace         workspaceContext
	locale            string
	// acceptLanguage is the browser's Accept-Language header, which picks the survey language when the link names none.
	acceptLanguage string
	responseCount  *int
	// limitPerBrowser applies the survey's one-response-per-browser setting: the /s page does; a personal link already takes one response per contact.
	limitPerBrowser bool
}

// surveyPage is forma2's LinkSurveyPage with its generateMetadata.
func (s *Service) surveyPage(ctx context.Context, rawID string, q query, acceptLanguage string) (page, error) {
	locale := mailer.MatchLocale(acceptLanguage)
	notFound := page{Status: http.StatusNotFound, Head: notFoundHead(linkViewport), State: pageState{View: viewNotFound, Locale: locale}}
	id, _ := rowid.Parse(rawID)
	sv, fields, err := s.repo.surveyByID(ctx, id)
	if err != nil {
		// forma2 answered any failure to load the survey with notFound().
		s.log.ErrorContext(ctx, "load link survey", "err", err)
		return notFound, nil
	}
	if sv == nil || sv.Type != survey.TypeLink || sv.Status == survey.StatusDraft {
		return notFound, nil
	}
	wc, err := s.repo.workspaceContext(ctx, sv.WorkspaceID)
	if err != nil {
		return page{}, err
	}
	// The title follows the language the survey opens in, negotiated from the browser when the link names none.
	md := basicSurveyMetadata(sv, metadataLanguage(q.get("lang"), acceptLanguage, sv.Languages), s.cfg.IsFormaCloud)
	h := pageHead(s.publicDomain(), sv, md, brandColor(wc.Workspace.Styling, sv.Styling), wc.whitelabel().FaviconURL, linkViewport, true)

	singleUseID := ""
	if sv.SingleUseSettings().Enabled {
		if singleUseID = s.surveys.CheckSingleUseLink(sv, q.get("suId"), q.get("suToken")); singleUseID == "" {
			return page{Status: http.StatusOK, Head: h, State: pageState{View: viewInactive, Locale: locale, Inactive: &inactiveState{
				Status: inactiveLinkInvalid, SurveyClosedMessage: json.RawMessage("null"), Workspace: &branding{LinkSurveyBranding: wc.Workspace.LinkSurveyBranding},
			}}}, nil
		}
	}
	var singleUseResponse *existingResponse
	if singleUseID != "" {
		if singleUseResponse, err = s.repo.responseBySingleUseID(ctx, sv.ID, singleUseID); err != nil {
			return page{}, err
		}
	}
	count, err := s.welcomeCardCount(ctx, sv)
	if err != nil {
		return page{}, err
	}
	p, err := s.render(ctx, renderInput{
		survey: sv, fields: fields, q: q, singleUseID: singleUseID, singleUseResponse: singleUseResponse,
		allowURLUserID: true, workspace: wc, locale: locale, acceptLanguage: acceptLanguage, responseCount: count, limitPerBrowser: true,
	})
	if p.Status == http.StatusNotFound {
		return notFound, err
	}
	p.Head = h
	return p, err
}

// welcomeCardCount is the response count, fetched only when the welcome card shows it.
func (s *Service) welcomeCardCount(ctx context.Context, sv *survey.Survey) (*int, error) {
	var wc struct {
		Enabled           bool `json:"enabled"`
		ShowResponseCount bool `json:"showResponseCount"`
	}
	_ = json.Unmarshal(sv.WelcomeCard, &wc)
	if !wc.Enabled || !wc.ShowResponseCount {
		return nil, nil
	}
	n, err := s.repo.responseCount(ctx, sv.ID)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// render is forma2's renderSurvey: the gates in front of a survey, in forma2's order, then the survey. A draft, non-link or archived survey is a 404. The duplicate-submission limits (D-FG03) come after the closed and completed screens and never apply to a preview.
func (s *Service) render(ctx context.Context, in renderInput) (page, error) {
	sv, q := in.survey, in.q
	langParam := q.get("lang")
	languageCode := surveyLanguageCode(langParam, in.acceptLanguage, sv.Languages)
	gate := gateLocale(langParam, languageCode, sv.Languages, in.locale)
	if sv.Status == survey.StatusDraft || sv.Type != survey.TypeLink || sv.ArchivedAt != nil {
		return page{Status: http.StatusNotFound}, nil
	}
	ws := &branding{LinkSurveyBranding: in.workspace.Workspace.LinkSurveyBranding}
	state := pageState{Locale: in.locale}

	if sv.Status != survey.StatusInProgress {
		state.View = viewInactive
		state.Inactive = &inactiveState{Status: sv.Status, IsScheduled: sv.Status == survey.StatusPaused && sv.PublishOn != nil, SurveyClosedMessage: orNull(sv.SurveyClosedMessage), Workspace: ws}
		return page{Status: http.StatusOK, State: state}, nil
	}
	if in.singleUseResponse != nil && in.singleUseResponse.Finished {
		state.View = viewCompleted
		state.Completed = &completedState{SingleUse: orNull(sv.SingleUse), Workspace: ws}
		return page{Status: http.StatusOK, State: state}, nil
	}

	isPreview := q.get("preview") == "true"
	if in.limitPerBrowser && sv.IsSingleResponsePerBrowserEnabled && !isPreview {
		state.BrowserLimit = &browserLimitState{SurveyID: sv.ID, Workspace: ws}
	}
	public, err := publicSurvey(sv, in.fields)
	if err != nil {
		return page{}, err
	}
	verified, fishy := "", false
	if sv.IsVerifyEmailEnabled {
		if token := q.get("verify"); token != "" {
			if verified = VerifyEmailToken(s.cfg, token, sv.ID, s.now()); verified == "" {
				fishy = true
			}
		}
		// Only the holder of the verification link learns that its address has answered: the endpoint that mails the link answers alike for every address, so it tells nobody which addresses responded.
		if verified != "" && sv.IsSingleResponsePerEmailEnabled && !isPreview {
			responded, err := s.repo.verifiedEmailResponded(ctx, sv.ID, verified)
			if err != nil {
				return page{}, err
			}
			if responded {
				state.View = viewInactive
				state.BrowserLimit = nil
				state.Inactive = &inactiveState{Status: inactiveEmailResponded, SurveyClosedMessage: json.RawMessage("null"), Workspace: ws}
				return page{Status: http.StatusOK, State: state}, nil
			}
		}
		if verified == "" && !isPreview {
			gated := public
			if sv.Pin != nil && *sv.Pin != "" {
				// The PIN gate stands behind this one; the questions it guards stay hidden until the PIN is entered.
				if gated, err = withoutQuestions(public); err != nil {
					return page{}, err
				}
			}
			state.View = viewVerifyEmail
			state.VerifyEmail = &verifyEmailState{Fishy: fishy, Survey: gated, LanguageCode: languageCode, Styling: orNull(in.workspace.Workspace.Styling), Locale: gate}
			if !fishy {
				state.VerifyEmail.SingleUseID = q.get("suId")
				if tok, ok := q.values["suToken"]; ok && len(tok) == 1 {
					state.VerifyEmail.SingleUseToken = &tok[0]
				}
			}
			return page{Status: http.StatusOK, State: state}, nil
		}
	}

	var recaptcha struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(sv.Recaptcha, &recaptcha)
	props := &surveyState{
		Survey:                  public,
		SurveyID:                sv.ID,
		Workspace:               workspaceState{Styling: orNull(in.workspace.Workspace.Styling), Logo: orNull(in.workspace.Workspace.Logo), LinkSurveyBranding: in.workspace.Workspace.LinkSurveyBranding, CustomHeadScripts: in.workspace.Workspace.CustomHeadScripts},
		Styling:                 computeStyling(in.workspace.Workspace.Styling, sv.Styling),
		PublicDomain:            s.publicDomainString(),
		ResponseCount:           in.responseCount,
		LanguageCode:            languageCode,
		Locale:                  in.locale,
		IsEmbed:                 q.get("embed") == "true",
		IsPreview:               isPreview,
		SingleUseID:             in.singleUseID,
		ContactID:               in.contactID,
		CanReadUserIDFromURL:    in.allowURLUserID && in.contactID == "" && q.userIDParam() != "",
		RecaptchaSiteKey:        s.cfg.RecaptchaSiteKey,
		IsSpamProtectionEnabled: s.cfg.RecaptchaConfigured() && recaptcha.Enabled,
		VerifiedEmail:           verified,
		ImprintURL:              s.cfg.ImprintURL,
		PrivacyURL:              s.cfg.PrivacyURL,
		TermsURL:                s.cfg.TermsURL,
		IsFormaCloud:            s.cfg.IsFormaCloud,
	}
	if in.singleUseResponse != nil {
		props.SingleUseResponseID = &in.singleUseResponse.ID
	}
	state.Survey = props
	state.View = viewSurvey
	if sv.Pin != nil && *sv.Pin != "" {
		// The survey stays on the server until the PIN is entered.
		props.Survey = json.RawMessage("null")
		props.Locale = gate
		state.View = viewPin
	}
	return page{Status: http.StatusOK, State: state, RuntimeLanguage: runtimeLanguage(languageCode, sv.Languages)}, nil
}

// runtimeLanguage is the respondent runtime's getI18nLanguage (packages/surveys/src/lib/i18n-utils.ts) for the codes a page passes it: a stored survey language code as it is, and "default" as the default language's code, or "en" when the survey has none.
func runtimeLanguage(languageCode string, languages []survey.SurveyLanguage) string {
	if languageCode != defaultLanguage {
		return languageCode
	}
	for _, l := range languages {
		if l.Default && l.Language.Code != "" {
			return l.Language.Code
		}
	}
	return "en"
}

// computeStyling is forma2's computeStyling: the survey's own styling when the workspace allows overrides and the survey overrides the theme, the workspace's otherwise.
func computeStyling(workspaceStyling, surveyStyling json.RawMessage) json.RawMessage {
	var ws struct {
		AllowStyleOverwrite bool `json:"allowStyleOverwrite"`
	}
	var ss struct {
		OverwriteThemeStyling bool `json:"overwriteThemeStyling"`
	}
	_ = json.Unmarshal(workspaceStyling, &ws)
	_ = json.Unmarshal(surveyStyling, &ss)
	if ws.AllowStyleOverwrite && ss.OverwriteThemeStyling {
		return orNull(surveyStyling)
	}
	return orNull(workspaceStyling)
}

func orNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

// publicSurvey is the survey as it reaches the respondent's browser: forma2's TSurvey with what the public page must never carry blanked (the PIN, follow-up configuration, the segment's targeting rules), in the shape the runtime reads (toJsWorkspaceStateSurvey: the segment reduced to its id and whether it filters), with the Embedded Data fields inlined.
func publicSurvey(sv *survey.Survey, fields []embeddeddata.LinkedField) (json.RawMessage, error) {
	b, err := json.Marshal(sv)
	if err != nil {
		return nil, fmt.Errorf("encode public survey: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("encode public survey: %w", err)
	}
	doc["pin"] = nil
	doc["followUps"] = []any{}
	doc["questions"] = []any{}
	if sv.Segment != nil {
		// forma2 blanked the filters before the page saw the survey, so the runtime always read hasFilters false.
		doc["segment"] = map[string]any{"id": sv.Segment.ID, "hasFilters": false}
	}
	if fields == nil {
		fields = []embeddeddata.LinkedField{}
	}
	doc["embeddedFields"] = fields
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode public survey: %w", err)
	}
	return out, nil
}

// withoutQuestions empties a public survey's blocks.
func withoutQuestions(public json.RawMessage) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(public, &doc); err != nil {
		return nil, err
	}
	doc["blocks"] = []any{}
	return json.Marshal(doc)
}

// contactPage is forma2's ContactSurveyPage with its generateMetadata: a personal link, signed and encrypted under ENCRYPTION_KEY, to one contact's survey.
func (s *Service) contactPage(ctx context.Context, token string, q query, acceptLanguage string) (page, error) {
	locale := mailer.MatchLocale(acceptLanguage)
	contactRaw, surveyRaw, err := verifyContactToken(s.cfg, token, s.now())
	if err != nil {
		// forma2 showed an expired link under its own heading, c.link_expired, which the page translates; neither screen has a workspace to brand it.
		inactive := &inactiveState{Status: inactiveLinkInvalid, SurveyClosedMessage: json.RawMessage("null")}
		if errors.Is(err, errContactTokenExpired) {
			inactive.Status = inactiveLinkExpired
		}
		return page{Status: http.StatusOK, Head: genericHead(), State: pageState{View: viewInactive, Locale: locale, Inactive: inactive}}, nil
	}
	contactID, _ := rowid.Parse(contactRaw)
	surveyID, _ := rowid.Parse(surveyRaw)
	sv, fields, err := s.repo.surveyByID(ctx, surveyID)
	if err != nil {
		return page{}, err
	}
	h := genericHead()
	var wc workspaceContext
	if sv != nil {
		if wc, err = s.repo.workspaceContext(ctx, sv.WorkspaceID); err != nil {
			return page{}, err
		}
		h = pageHead(s.publicDomain(), sv, basicSurveyMetadata(sv, "", s.cfg.IsFormaCloud), brandColor(wc.Workspace.Styling, sv.Styling), wc.whitelabel().FaviconURL, rootViewport, false)
	}
	notFound := page{Status: http.StatusNotFound, Head: notFoundHead(rootViewport), State: pageState{View: viewNotFound, Locale: locale}}

	existing, err := s.repo.contactResponse(ctx, surveyID, contactID)
	if err != nil {
		return page{}, err
	}
	if existing != nil {
		inactive := &inactiveState{Status: inactiveResponseSubmitted, SurveyClosedMessage: json.RawMessage("null")}
		if sv != nil {
			inactive.Workspace = &branding{LinkSurveyBranding: wc.Workspace.LinkSurveyBranding}
		}
		return page{Status: http.StatusOK, Head: h, State: pageState{View: viewInactive, Locale: locale, Inactive: inactive}}, nil
	}
	if sv == nil {
		return notFound, nil
	}

	singleUseID := ""
	if sv.SingleUseSettings().Enabled {
		if singleUseID = s.surveys.CheckSingleUseLink(sv, q.get("suId"), q.get("suToken")); singleUseID == "" {
			return page{Status: http.StatusOK, Head: h, State: pageState{View: viewInactive, Locale: locale, Inactive: &inactiveState{
				Status: inactiveLinkInvalid, SurveyClosedMessage: json.RawMessage("null"), Workspace: &branding{LinkSurveyBranding: wc.Workspace.LinkSurveyBranding},
			}}}, nil
		}
	}
	count, err := s.welcomeCardCount(ctx, sv)
	if err != nil {
		return page{}, err
	}
	// The runtime names the contact to the client API by a token rather than by its id: the client API takes contact ids only from workspaces that do not require verified identities, and this token, which the signed link proves, attaches the contact to this survey either way.
	contactToken, err := sdkidentity.LinkContactToken(s.cfg, sv.WorkspaceID, contactID, sv.ID)
	if err != nil {
		return page{}, err
	}
	// forma2 passed the contact's existing response here, which the check above has already ruled out.
	p, err := s.render(ctx, renderInput{
		survey: sv, fields: fields, q: q, singleUseID: singleUseID, contactID: contactToken,
		workspace: wc, locale: locale, acceptLanguage: acceptLanguage, responseCount: count,
	})
	if p.Status == http.StatusNotFound {
		return notFound, err
	}
	p.Head = h
	return p, err
}

// prettyURL is forma2's /p/{slug}: the survey's /s link with every query parameter kept, or false when no survey has the slug. Pretty URLs are a self-hosted feature.
func (s *Service) prettyURL(ctx context.Context, slug, rawQuery string) (string, bool, error) {
	if s.cfg.IsFormaCloud {
		return "", false, nil
	}
	id, ok, err := s.repo.surveyIDBySlug(ctx, slug)
	if err != nil || !ok {
		return "", false, err
	}
	target := "/s/" + id.String()
	if qs := nextQueryString(rawQuery); qs != "" {
		target += "?" + qs
	}
	return target, true, nil
}

// nextQueryString re-serialises a query the way forma2's redirect did: Next handed the page each parameter once, a repeated one as an array, and URLSearchParams wrote an array back as its values joined by commas.
func nextQueryString(raw string) string {
	var keys []string
	values := map[string][]string{}
	for pair := range strings.SplitSeq(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		key, err1 := url.QueryUnescape(k)
		value, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil {
			continue
		}
		if _, seen := values[key]; !seen {
			keys = append(keys, key)
		}
		values[key] = append(values[key], value)
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, formEncode(k)+"="+formEncode(strings.Join(values[k], ",")))
	}
	return strings.Join(parts, "&")
}

// formEncode is URLSearchParams' serialisation: letters, digits and *-._ are kept, a space is "+", everything else is percent-encoded.
func formEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteString("%" + strings.ToUpper(hexByte(c)))
		}
	}
	return b.String()
}

// pinResult is what a correct PIN unlocks: the survey and the token the response endpoints require of it.
type pinResult struct {
	Survey       json.RawMessage `json:"survey"`
	PinAuthToken string          `json:"pinAuthToken"`
}

// errInvalidPin is forma2's InvalidInputError("INVALID_PIN").
var errInvalidPin = apperr.InvalidInput("INVALID_PIN", "INVALID_PIN")

// validatePin is forma2's validateSurveyPinAction. Unlike forma2, which returned any survey to anyone for an id without a PIN, it only answers for a survey the page would put behind its PIN gate: a live, PIN-protected link survey.
func (s *Service) validatePin(ctx context.Context, rawID, pin string) (pinResult, error) {
	sv, fields, err := s.pinGatedSurvey(ctx, rawID)
	if err != nil {
		return pinResult{}, err
	}
	if *sv.Pin != pin {
		return pinResult{}, errInvalidPin
	}
	public, err := publicSurvey(sv, fields)
	if err != nil {
		return pinResult{}, err
	}
	token, err := NewPinToken(s.cfg, sv.ID, s.now())
	if err != nil {
		return pinResult{}, fmt.Errorf("sign pin token: %w", err)
	}
	return pinResult{Survey: public, PinAuthToken: token}, nil
}

// errPinTokenRejected answers a resume whose token no longer opens the survey: expired, issued for another survey, or the survey stopped saving progress. The page asks for the PIN again.
var errPinTokenRejected = apperr.NotAuthenticated("PIN token is invalid or expired")

// pinResumeMargin is how long a kept PIN token must still be valid to open the gate again. The responses refuse an expired token (403), which the page cannot recover from without losing the answer, so a token close to its end asks for the PIN again instead, and the fresh token it gets lasts another hour.
const pinResumeMargin = 15 * time.Minute

// resumePin opens the PIN gate again with the token a correct PIN returned, which the page keeps for the tab's session while the survey saves respondents' progress (isProgressSavingEnabled), so a reload goes on where the respondent left off instead of asking for the PIN again. The token comes back unchanged, so it still expires an hour after the PIN was entered; one with less than pinResumeMargin left, and any token of a survey that no longer saves progress, takes the PIN again, so a shared device does not keep it open.
func (s *Service) resumePin(ctx context.Context, rawID, token string) (pinResult, error) {
	sv, fields, err := s.pinGatedSurvey(ctx, rawID)
	if err != nil {
		return pinResult{}, err
	}
	if !sv.IsProgressSavingEnabled || !VerifyPinToken(s.cfg, token, sv.ID, s.now().Add(pinResumeMargin)) {
		return pinResult{}, errPinTokenRejected
	}
	public, err := publicSurvey(sv, fields)
	if err != nil {
		return pinResult{}, err
	}
	return pinResult{Survey: public, PinAuthToken: token}, nil
}

// pinGatedSurvey is the survey the page would put behind its PIN gate (a live, PIN-protected link survey) with its Embedded Data fields; any other id is 404 Survey.
func (s *Service) pinGatedSurvey(ctx context.Context, rawID string) (*survey.Survey, []embeddeddata.LinkedField, error) {
	id, _ := rowid.Parse(rawID)
	sv, fields, err := s.repo.surveyByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if sv == nil || sv.Type != survey.TypeLink || sv.Status != survey.StatusInProgress || sv.ArchivedAt != nil || sv.Pin == nil || *sv.Pin == "" {
		return nil, nil, apperr.NotFound("Survey", "")
	}
	return sv, fields, nil
}

// emailInput is forma2's ZLinkSurveyEmailData without the survey id, which the path carries, and without the logo, which the server reads.
type emailInput struct {
	Email              string  `json:"email"`
	SuID               *string `json:"suId"`
	SuToken            *string `json:"suToken"`
	Locale             string  `json:"locale"`
	SurveyLanguageCode *string `json:"surveyLanguageCode"`
}

// sendVerificationEmail is forma2's sendLinkSurveyEmailAction: mails the respondent a link to the survey carrying a token that proves their address. The survey's name comes from its row and the language is re-resolved against its own languages, never taken from the unauthenticated caller. It mails the link whether or not the address has already answered a survey that takes one response per verified email: refusing here would tell anyone which addresses responded. The link itself shows "already responded" to its holder (render), and the response is refused at creation (response.Create).
func (s *Service) sendVerificationEmail(ctx context.Context, rawID string, in emailInput) error {
	id, _ := rowid.Parse(rawID)
	sv, _, err := s.repo.surveyByID(ctx, id)
	if err != nil {
		return err
	}
	if sv == nil {
		return apperr.NotFound("Survey", "")
	}
	if !sv.IsVerifyEmailEnabled {
		return apperr.InvalidInput("EMAIL_VERIFICATION_NOT_ENABLED", "EMAIL_VERIFICATION_NOT_ENABLED")
	}
	wc, err := s.repo.workspaceContext(ctx, sv.WorkspaceID)
	if err != nil {
		return err
	}
	token, err := newEmailToken(s.cfg, sv.ID, in.Email, s.now())
	if err != nil {
		return fmt.Errorf("sign email verification token: %w", err)
	}
	languageCode := defaultLanguage
	if in.SurveyLanguageCode != nil {
		languageCode = resolveSurveyLanguageCode(*in.SurveyLanguageCode, sv.Languages)
	}
	logo := wc.whitelabel().LogoURL
	if strings.HasPrefix(logo, "/storage/") {
		logo = s.publicDomainString() + logo
	}
	link := verifiedLinkSurveyURL(s.publicDomainString(), sv.ID.String(), token, deref(in.SuID), deref(in.SuToken), languageCode)
	_, err = s.mailer.SendTemplate(ctx, in.Email, in.Locale, mailer.LinkSurvey{SurveyName: sv.Name, SurveyLink: link, LogoURL: logo})
	return err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// verifiedLinkSurveyURL is forma2's buildVerifiedLinkSurveyUrl: the survey link with verify always, suId and suToken for a single-use link, and lang unless the respondent stayed on the default language.
func verifiedLinkSurveyURL(publicDomain, surveyID, token, singleUseID, singleUseToken, languageCode string) string {
	params := []string{"verify=" + formEncode(token)}
	if singleUseID != "" {
		params = append(params, "suId="+formEncode(singleUseID))
		if singleUseToken != "" {
			params = append(params, "suToken="+formEncode(singleUseToken))
		}
	}
	if languageCode != "" && languageCode != defaultLanguage {
		params = append(params, "lang="+formEncode(languageCode))
	}
	return publicDomain + "/s/" + url.PathEscape(surveyID) + "?" + strings.Join(params, "&")
}
