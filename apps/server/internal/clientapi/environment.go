package clientapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/apps/server/internal/contact"
	"github.com/yldm-tech/forma/apps/server/internal/embeddeddata"
	"github.com/yldm-tech/forma/apps/server/internal/langcode"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/rowid"
	"github.com/yldm-tech/forma/apps/server/internal/survey"
)

const (
	// stateCache is the Cache-Control of the environment state: CDNs and browsers keep it as long as the server does.
	stateCache = "public, s-maxage=60, max-age=60, stale-while-revalidate=60, stale-if-error=60"
	// stateTTL is how long the server caches a workspace's state, forma2's one minute.
	stateTTL = time.Minute
	// stateExpiry is when the SDK is told to fetch the state again, so a paused survey, a new action or changed targeting reaches returning visitors within about five minutes. forma2 said an hour, which kept them on the old state that long although the server rebuilds it every minute.
	stateExpiry = 5 * time.Minute
	// maxStateSurveys is how many surveys the state lists, newest first. When a workspace has more live app surveys, the state says how many it left out under truncated. The survey list warns about the same surveys through survey.SDKState, which shares the constant and the query's filter and order.
	maxStateSurveys = survey.MaxSDKStateSurveys

	// surveyNamePlaceholder and segmentTitlePlaceholder are forma2's PUBLIC_API_SURVEY_NAME_PLACEHOLDER and its segment counterpart: older SDKs decode these fields as required, but their values are internal.
	surveyNamePlaceholder   = "[deprecated] survey name omitted from public API - will be removed soon"
	segmentTitlePlaceholder = "[deprecated] segment title omitted from public API - will be removed soon"
)

// placeholderFilter stands in for a segment's real filters, which the public state never carries: older SDKs only read whether there are any.
var placeholderFilter = json.RawMessage(`{"id":"placeholder","connector":null,"resource":{"id":"placeholder","root":{"type":"device","deviceType":"phone"},"value":"deprecated","qualifier":{"operator":"equals"}}}`)

// environment is forma2's GET environment: the workspace's SDK state, {"data": {"data": state, "expiresAt"}}. The id must look like an id (a cuid or a UUID) once trimmed.
func (h *Handler) environment(c *gin.Context) {
	ctx := c.Request.Context()
	raw := jsTrim(c.Param("workspaceId"))
	if !isID(raw) {
		writeError(c, badRequest("Invalid ID format", nil))
		return
	}
	ws, ok := h.workspace(c, raw, msgEnvironment)
	if !ok {
		return
	}
	state, err := h.workspaceState(ctx, ws.ID)
	if err != nil {
		h.fail(c, err, msgEnvironment)
		return
	}
	if state == nil {
		id := ws.ID.String()
		writeError(c, notFound("Workspace", &id))
		return
	}
	writeData(c, fields{{"data", state}, {"expiresAt", jsvalue.ISOString(h.now().Add(stateExpiry))}}, stateCache)
}

// workspaceState is forma2's getWorkspaceState, cached for a minute under forma2's key; nil when the workspace is gone. The first build after the SDK is installed marks the workspace's app setup complete.
func (h *Handler) workspaceState(ctx context.Context, workspaceID uuid.UUID) (json.RawMessage, error) {
	key := "fb:env:" + workspaceID.String() + ":state"
	if b, err := h.cache.Get(ctx, key).Bytes(); err == nil {
		return b, nil
	} else if !errors.Is(err, redis.Nil) {
		h.log.WarnContext(ctx, "workspace state cache read failed", "err", err)
	}
	state, err := h.buildState(ctx, workspaceID)
	if err != nil || state == nil {
		return nil, err
	}
	b, err := marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode workspace state: %w", err)
	}
	if err := h.cache.Set(ctx, key, b, stateTTL).Err(); err != nil {
		h.log.WarnContext(ctx, "workspace state cache write failed", "err", err)
	}
	return b, nil
}

type stateWorkspace struct {
	ID                  uuid.UUID
	AppSetupCompleted   bool
	RecontactDays       int
	ClickOutsideClose   bool
	Overlay             string
	Placement           string
	InAppSurveyBranding bool
	Styling             []byte
}

type stateSurvey struct {
	ID                       uuid.UUID
	WelcomeCard              []byte
	Blocks                   []byte
	Variables                []byte
	Type                     string
	ShowLanguageSwitch       *bool
	Endings                  []byte
	AutoClose                *int
	Styling                  []byte
	Status                   string
	Recaptcha                []byte
	RecontactDays            *int
	DisplayLimit             *int
	DisplayOption            string
	HiddenFields             []byte
	IsBackButtonHidden       bool
	IsAutoProgressingEnabled bool
	DisplayPercentage        *float64
	Delay                    int
	WorkspaceOverwrites      []byte
	SegmentID                *uuid.UUID
	SegmentFilters           []byte
}

type stateLanguage struct {
	SurveyID    uuid.UUID
	IsDefault   bool
	Enabled     bool
	ID          uuid.UUID
	Code        string
	Alias       *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	WorkspaceID uuid.UUID
}

// buildState is forma2's getWorkspaceStateData and the composition around it: the workspace's in-app settings, its action classes and its live app surveys in the shape the SDKs decode, with forma2's back-compat fields (project beside workspace, projectOverwrites beside workspaceOverwrites, placeholder names and segments, legacy language codes) and relative storage URLs made absolute. When the workspace has more live app surveys than maxStateSurveys, truncated counts the ones left out; it is absent otherwise, so the state stays forma2's.
func (h *Handler) buildState(ctx context.Context, workspaceID uuid.UUID) (fields, error) {
	db := h.db.WithContext(ctx)
	var workspaces []stateWorkspace
	if err := db.Raw(`SELECT id, app_setup_completed, recontact_days, click_outside_close, overlay, placement, in_app_survey_branding, styling
		FROM workspaces WHERE id = ?`, workspaceID).Scan(&workspaces).Error; err != nil {
		return nil, fmt.Errorf("load workspace state: %w", err)
	}
	if len(workspaces) == 0 {
		return nil, nil
	}
	ws := workspaces[0]

	var actionClasses []struct {
		ID           uuid.UUID
		Type         string
		Name         string
		Key          *string
		NoCodeConfig []byte
	}
	if err := db.Raw(`SELECT id, type, name, key, no_code_config FROM action_classes WHERE workspace_id = ? ORDER BY created_at, id`, workspaceID).Scan(&actionClasses).Error; err != nil {
		return nil, fmt.Errorf("load action classes: %w", err)
	}

	var surveys []stateSurvey
	if err := db.Raw(`SELECT s.id, s.welcome_card, s.blocks, s.variables, s.type, s.show_language_switch, s.endings, s.auto_close, s.styling, s.status, s.recaptcha,
			s.recontact_days, s.display_limit, s.display_option, s.hidden_fields, s.is_back_button_hidden, s.is_auto_progressing_enabled,
			s.display_percentage::float8 AS display_percentage, s.delay, s.workspace_overwrites, s.segment_id, seg.filters AS segment_filters
		FROM surveys s LEFT JOIN segments seg ON seg.id = s.segment_id
		WHERE s.workspace_id = ? AND s.type = 'app' AND s.status = 'inProgress' AND s.archived_at IS NULL
		ORDER BY s.created_at DESC, s.id DESC LIMIT ?`, workspaceID, maxStateSurveys).Scan(&surveys).Error; err != nil {
		return nil, fmt.Errorf("load state surveys: %w", err)
	}
	// truncated is how many live app surveys the cap left out, the oldest ones; the SDK never shows them.
	truncated := int64(0)
	if len(surveys) == maxStateSurveys {
		var total int64
		if err := db.Raw(`SELECT count(*) FROM surveys WHERE workspace_id = ? AND type = 'app' AND status = 'inProgress' AND archived_at IS NULL`, workspaceID).Scan(&total).Error; err != nil {
			return nil, fmt.Errorf("count state surveys: %w", err)
		}
		truncated = total - maxStateSurveys
	}
	if truncated > 0 {
		h.log.WarnContext(ctx, "workspace state truncated: older live app surveys are left out and never shown", "workspaceId", workspaceID, "limit", maxStateSurveys, "truncated", truncated)
	}
	ids := make([]uuid.UUID, len(surveys))
	for i, s := range surveys {
		ids[i] = s.ID
	}

	languages := map[uuid.UUID][]stateLanguage{}
	triggers := map[uuid.UUID][]string{}
	if len(ids) > 0 {
		var langs []stateLanguage
		if err := db.Raw(`SELECT sl.survey_id, sl.is_default, sl.enabled, l.id, l.code, l.alias, l.created_at, l.updated_at, l.workspace_id
			FROM survey_languages sl JOIN languages l ON l.id = sl.language_id WHERE sl.survey_id IN ? ORDER BY l.created_at, l.code`, ids).Scan(&langs).Error; err != nil {
			return nil, fmt.Errorf("load state survey languages: %w", err)
		}
		for _, l := range langs {
			languages[l.SurveyID] = append(languages[l.SurveyID], l)
		}
		var trs []struct {
			SurveyID uuid.UUID
			Name     string
		}
		if err := db.Raw(`SELECT st.survey_id, ac.name FROM survey_triggers st JOIN action_classes ac ON ac.id = st.action_class_id
			WHERE st.survey_id IN ? ORDER BY st.created_at, ac.name`, ids).Scan(&trs).Error; err != nil {
			return nil, fmt.Errorf("load state survey triggers: %w", err)
		}
		for _, t := range trs {
			triggers[t.SurveyID] = append(triggers[t.SurveyID], t.Name)
		}
	}

	refresh, hasRefresh, err := h.interactionRefresh(ctx, workspaceID, surveys)
	if err != nil {
		return nil, err
	}

	epoch := jsvalue.ISOString(time.Unix(0, 0))
	stateSurveys := make([]any, 0, len(surveys))
	for _, s := range surveys {
		var segment any
		if s.SegmentID != nil {
			filters := []any{}
			var real []json.RawMessage
			hasFilters := json.Unmarshal(s.SegmentFilters, &real) == nil && len(real) > 0
			if hasFilters {
				filters = []any{placeholderFilter}
			}
			segment = fields{
				{"id", s.SegmentID}, {"title", segmentTitlePlaceholder}, {"description", nil}, {"isPrivate", true}, {"filters", filters},
				{"environmentId", ws.ID.String()}, {"workspaceId", ws.ID.String()}, {"createdAt", epoch}, {"updatedAt", epoch},
				{"surveys", []any{}}, {"hasFilters", hasFilters},
			}
		}
		embedded, err := embeddeddata.SurveyFields(ctx, h.db, s.ID)
		if err != nil {
			return nil, err
		}
		if embedded == nil {
			embedded = []embeddeddata.LinkedField{}
		}
		trigs := make([]any, len(triggers[s.ID]))
		for i, name := range triggers[s.ID] {
			trigs[i] = fields{{"actionClass", fields{{"name", name}}}}
		}
		var displayPercentage any
		if s.DisplayPercentage != nil && *s.DisplayPercentage != 0 {
			displayPercentage = *s.DisplayPercentage
		}
		overwrites := jsonOrNull(s.WorkspaceOverwrites)
		survey := fields{
			{"id", s.ID}, {"welcomeCard", jsonOrNull(s.WelcomeCard)}, {"questions", []any{}}, {"blocks", jsonOrNull(s.Blocks)},
			{"variables", jsonOrNull(s.Variables)}, {"type", s.Type}, {"showLanguageSwitch", s.ShowLanguageSwitch},
			{"languages", stateLanguages(languages[s.ID])}, {"endings", jsonOrNull(s.Endings)}, {"autoClose", s.AutoClose},
			{"styling", jsonOrNull(s.Styling)}, {"status", s.Status}, {"recaptcha", jsonOrNull(s.Recaptcha)}, {"recontactDays", s.RecontactDays},
			{"displayLimit", s.DisplayLimit}, {"displayOption", s.DisplayOption}, {"hiddenFields", jsonOrNull(s.HiddenFields)},
			{"isBackButtonHidden", s.IsBackButtonHidden}, {"isAutoProgressingEnabled", s.IsAutoProgressingEnabled}, {"triggers", trigs},
			{"displayPercentage", displayPercentage}, {"delay", s.Delay}, {"workspaceOverwrites", overwrites}, {"segment", segment},
			{"embeddedFields", embedded}, {"name", surveyNamePlaceholder},
		}
		if hasRefresh {
			survey = append(survey, field{"interactionRefresh", refresh[s.ID]})
		}
		survey = append(survey, field{"projectOverwrites", overwrites})
		stateSurveys = append(stateSurveys, survey)
	}

	classes := make([]any, len(actionClasses))
	for i, a := range actionClasses {
		classes[i] = fields{{"id", a.ID}, {"type", a.Type}, {"name", a.Name}, {"key", a.Key}, {"noCodeConfig", jsonOrNull(a.NoCodeConfig)}}
	}
	settings := fields{
		{"id", ws.ID}, {"recontactDays", ws.RecontactDays}, {"clickOutsideClose", ws.ClickOutsideClose}, {"overlay", ws.Overlay},
		{"placement", ws.Placement}, {"inAppSurveyBranding", ws.InAppSurveyBranding}, {"styling", h.resolveStorageURLs(jsonOrNull(ws.Styling))},
	}

	if !ws.AppSetupCompleted {
		// Conditional, so concurrent first builds flip the flag once, and only the build that flipped it reports the activation.
		res := db.Exec(`UPDATE workspaces SET app_setup_completed = true WHERE id = ? AND app_setup_completed = false`, workspaceID)
		if res.Error != nil {
			return nil, fmt.Errorf("mark app setup completed: %w", res.Error)
		}
		if res.RowsAffected > 0 {
			code := 0
			for _, a := range actionClasses {
				if a.Type == "code" {
					code++
				}
			}
			h.captureAppConnected(ctx, workspaceID, len(surveys), code, len(actionClasses)-code)
		}
	}

	state := fields{{"surveys", h.resolveStorageURLs(stateSurveys)}, {"actionClasses", classes}, {"workspace", settings}}
	// The site key of the one spam protection service the instance uses (config.SpamProtectionProvider), which tells the SDK which token a survey with spam protection on must send.
	switch h.cfg.SpamProtectionProvider() {
	case config.SpamProtectionTurnstile:
		state = append(state, field{"turnstileSiteKey", h.cfg.TurnstileSiteKey})
	case config.SpamProtectionRecaptcha:
		state = append(state, field{"recaptchaSiteKey", h.cfg.RecaptchaSiteKey})
	}
	state = append(state, field{"project", settings})
	if truncated > 0 {
		state = append(state, field{"truncated", truncated})
	}
	return state, nil
}

// stateLanguages is a survey's languages as the SDK decodes them: each language with projectId mirroring workspaceId for SDKs from before the rename, and, for a survey with more than one language, an extra entry under every legacy alias of each code (de beside de-DE) no language of the survey already uses, since older SDKs match a user's language by exact code.
func stateLanguages(langs []stateLanguage) []any {
	out := make([]any, 0, len(langs))
	entry := func(l stateLanguage, code string) fields {
		return fields{{"default", l.IsDefault}, {"enabled", l.Enabled}, {"language", fields{
			{"id", l.ID}, {"code", code}, {"alias", l.Alias}, {"createdAt", jsvalue.ISOString(l.CreatedAt)}, {"updatedAt", jsvalue.ISOString(l.UpdatedAt)},
			{"workspaceId", l.WorkspaceID}, {"projectId", l.WorkspaceID},
		}}}
	}
	seen := map[string]bool{}
	for _, l := range langs {
		out = append(out, entry(l, l.Code))
		seen[strings.ToLower(l.Code)] = true
	}
	if len(langs) <= 1 {
		return out
	}
	for _, l := range langs {
		for _, legacy := range langcode.LegacyCodes(l.Code) {
			if seen[strings.ToLower(legacy)] {
				continue
			}
			seen[strings.ToLower(legacy)] = true
			out = append(out, entry(l, legacy))
		}
	}
	return out
}

// interactionRefresh computes the per-survey refresh gates; the workspace's segments are read only when some survey has filters, to resolve the segments a filter refers to.
func (h *Handler) interactionRefresh(ctx context.Context, workspaceID uuid.UUID, surveys []stateSurvey) (map[uuid.UUID]*contact.InteractionRefresh, bool, error) {
	delivered := make([]contact.SurveyFilters, len(surveys))
	anyFilters := false
	for i, s := range surveys {
		delivered[i].SurveyID = s.ID
		if len(s.SegmentFilters) > 0 {
			if err := json.Unmarshal(s.SegmentFilters, &delivered[i].Filters); err != nil {
				h.log.WarnContext(ctx, "skipping unreadable segment filters", "survey_id", s.ID, "err", err)
				continue
			}
			anyFilters = anyFilters || len(delivered[i].Filters) > 0
		}
	}
	segments := map[uuid.UUID]contact.Filters{}
	if anyFilters {
		var rows []struct {
			ID      uuid.UUID
			Filters []byte
		}
		if err := h.db.WithContext(ctx).Raw(`SELECT id, filters FROM segments WHERE workspace_id = ?`, workspaceID).Scan(&rows).Error; err != nil {
			return nil, false, fmt.Errorf("load workspace segments: %w", err)
		}
		for _, r := range rows {
			var f contact.Filters
			if json.Unmarshal(r.Filters, &f) == nil {
				segments[r.ID] = f
			}
		}
	}
	refresh, hasAny := contact.InteractionRefreshMap(delivered, func(segmentID string) (contact.Filters, bool) {
		id, _ := rowid.Parse(segmentID)
		f, ok := segments[id]
		return f, ok
	})
	return refresh, hasAny, nil
}

func jsonOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

// storageURL is forma2's STORAGE_URL_PATTERN, a relative URL of a stored file.
var storageURL = regexp.MustCompile(`^/storage/[^/]+/(public|private)/.+`)

// resolveStorageURLs is forma2's resolveStorageUrlsInObject: every relative storage URL in v, at any depth, made absolute against the public domain (PUBLIC_URL, else WEBAPP_URL), or WEBAPP_URL for a private file. JSON documents are decoded to be walked.
func (h *Handler) resolveStorageURLs(v any) any {
	switch x := v.(type) {
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(x, &decoded) != nil {
			return x
		}
		return h.resolveStorageURLs(decoded)
	case string:
		if !storageURL.MatchString(x) {
			return x
		}
		base := h.cfg.WebAppURL
		if !strings.Contains(x, "/private/") && h.cfg.PublicURL != nil {
			base = h.cfg.PublicURL
		}
		return strings.TrimSuffix(base.String(), "/") + x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = h.resolveStorageURLs(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = h.resolveStorageURLs(e)
		}
		return out
	case fields:
		out := make(fields, len(x))
		for i, f := range x {
			out[i] = field{f.Key, h.resolveStorageURLs(f.Value)}
		}
		return out
	case []embeddeddata.LinkedField:
		b, err := json.Marshal(x)
		if err != nil {
			return x
		}
		return h.resolveStorageURLs(json.RawMessage(b))
	}
	return v
}

// captureAppConnected is forma2's app_connected, the first time the SDK loads a workspace's state: what the workspace had to show it. The workspace is the distinct id; nobody is signed in.
func (h *Handler) captureAppConnected(ctx context.Context, workspaceID uuid.UUID, surveys, codeActions, noCodeActions int) {
	if !analytics.Enabled() {
		return
	}
	var orgs []string
	if err := h.db.WithContext(ctx).Raw(`SELECT organization_id::text FROM workspaces WHERE id = ?`, workspaceID).Scan(&orgs).Error; err != nil {
		h.log.WarnContext(ctx, "Failed to capture PostHog app_connected event", "err", err)
		return
	}
	org, groups := "", analytics.Groups{}
	if len(orgs) == 1 {
		org, groups = orgs[0], analytics.Groups{OrganizationID: orgs[0], WorkspaceID: workspaceID.String()}
	}
	analytics.Capture(workspaceID.String(), "app_connected", analytics.Properties{
		"num_surveys":         surveys,
		"num_code_actions":    codeActions,
		"num_no_code_actions": noCodeActions,
		"organization_id":     org,
		"workspace_id":        workspaceID.String(),
	}, groups)
}
