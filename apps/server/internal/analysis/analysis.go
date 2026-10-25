// Package analysis is what the admin app shows about a survey's responses: the summary with forma2's per-element statistics, drop-offs and quotas, the trend over time (responses, completion and the score elements by day, week or month in the organization's time zone), the values the response filter offers, the contacts who saw the survey, the CSV and XLSX export, and the survey reset. Filtering itself is the response module's: every operation here takes forma2's TResponseFilterCriteria and compiles it with response.Where.
package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/response"
	"github.com/yldm-tech/forma/apps/server/internal/survey/surveydoc"
)

// Service answers the analysis views. Every operation authorizes the caller on the survey first; the survey's workspace is never taken from the client.
type Service struct {
	db    *gorm.DB
	cfg   config.Config
	authz *authz.Authorizer
	jobs  *jobs.Enqueuer
	audit *audit.Emitter
	log   *slog.Logger
}

func NewService(cfg config.Config, db *gorm.DB, authorizer *authz.Authorizer, enqueuer *jobs.Enqueuer, emitter *audit.Emitter, log *slog.Logger) *Service {
	return &Service{db: db, cfg: cfg, authz: authorizer, jobs: enqueuer, audit: emitter, log: log}
}

// element is one element of the survey: the typed view the statistics read, the stored JSON the summary hands back, and the block it sits in.
type element struct {
	surveydoc.Element
	Raw     json.RawMessage
	BlockID string
}

// surveyLanguage is one of the survey's languages.
type surveyLanguage struct {
	Code    string
	Default bool
}

// surveyDoc is what analysis reads of a survey: the response module's view plus the name, the welcome card switch, the languages, the elements with their blocks, and the organization with its display time zone.
type surveyDoc struct {
	*response.Survey
	Name               string
	ArchivedAt         *time.Time
	WelcomeCardEnabled bool
	Languages          []surveyLanguage
	BlockIDs           []string
	Elements           []element
	OrganizationID     uuid.UUID
	DisplayTimeZone    *string
}

// loadSurveyDoc reads a survey for analysis; nil when it does not exist.
func loadSurveyDoc(ctx context.Context, db *gorm.DB, id uuid.UUID) (*surveyDoc, error) {
	sv, err := response.LoadSurvey(ctx, db, id)
	if err != nil || sv == nil {
		return nil, err
	}
	var rows []struct {
		Name               string
		ArchivedAt         *time.Time
		WelcomeCardEnabled bool
		OrganizationID     uuid.UUID
		DisplayTimeZone    *string
	}
	if err := db.WithContext(ctx).Raw(`SELECT s.name, s.archived_at, COALESCE(s.welcome_card -> 'enabled' = 'true'::jsonb, false) AS welcome_card_enabled,
		w.organization_id, o.display_time_zone
		FROM surveys s JOIN workspaces w ON w.id = s.workspace_id JOIN organizations o ON o.id = w.organization_id WHERE s.id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load survey: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	doc := &surveyDoc{Survey: sv, Name: rows[0].Name, ArchivedAt: rows[0].ArchivedAt, WelcomeCardEnabled: rows[0].WelcomeCardEnabled,
		OrganizationID: rows[0].OrganizationID, DisplayTimeZone: rows[0].DisplayTimeZone}
	var languages []struct {
		Code      string
		IsDefault bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT l.code, sl.is_default FROM survey_languages sl JOIN languages l ON l.id = sl.language_id
		WHERE sl.survey_id = ? ORDER BY sl.is_default DESC, l.code`, id).Scan(&languages).Error; err != nil {
		return nil, fmt.Errorf("load survey languages: %w", err)
	}
	for _, l := range languages {
		doc.Languages = append(doc.Languages, surveyLanguage{Code: l.Code, Default: l.IsDefault})
	}
	if err := doc.readBlocks(sv.Blocks); err != nil {
		return nil, fmt.Errorf("decode survey %s blocks: %w", id, err)
	}
	return doc, nil
}

// readBlocks sets the block ids and the elements, in document order, from the stored blocks.
func (sv *surveyDoc) readBlocks(raw json.RawMessage) error {
	var blocks []struct {
		ID       string            `json:"id"`
		Elements []json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return err
	}
	for _, b := range blocks {
		sv.BlockIDs = append(sv.BlockIDs, b.ID)
		for _, raw := range b.Elements {
			var el surveydoc.Element
			if err := json.Unmarshal(raw, &el); err != nil {
				return err
			}
			sv.Elements = append(sv.Elements, element{Element: el, Raw: raw, BlockID: b.ID})
		}
	}
	return nil
}

// elementIDs are the ids of the survey's elements in document order.
func (sv *surveyDoc) elementIDs() []string {
	ids := make([]string, len(sv.Elements))
	for i, el := range sv.Elements {
		ids[i] = el.ID
	}
	return ids
}

// readableSurvey authorizes reading the survey's responses (forma2's workspace.read on the survey's workspace) and loads it. An unknown survey is the same 403 as a foreign one, so ids cannot be probed.
func (s *Service) readableSurvey(ctx context.Context, userID, surveyID uuid.UUID, action authz.Action) (*surveyDoc, error) {
	if err := s.authz.Require(ctx, authz.User(userID), action, authz.Survey(surveyID)); err != nil {
		return nil, err
	}
	sv, err := loadSurveyDoc(ctx, s.db, surveyID)
	if err != nil {
		return nil, err
	}
	if sv == nil {
		return nil, apperr.Forbidden("Not authorized")
	}
	return sv, nil
}

// languageCode is forma2's getLanguageCode: the response's language as the survey's labels are keyed, "default" for the survey's default language, a language the survey does not have, or none.
func (sv *surveyDoc) languageCode(language *string) string {
	if len(sv.Languages) == 0 || language == nil || *language == "" {
		return "default"
	}
	for _, l := range sv.Languages {
		if l.Code == *language {
			if l.Default {
				return "default"
			}
			return l.Code
		}
	}
	return "default"
}
