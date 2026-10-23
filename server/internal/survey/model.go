// Package survey is the survey definition: creating, reading, listing, editing, copying, archiving and deleting surveys, with their languages, triggers, private segment, follow-ups and quotas; the publish and close schedule and the archive purge that run as background jobs; the template catalog; and the /api/v3/surveys management API and the /api/app endpoints the editor needs.
//
// The survey document itself (blocks, endings, ...) is stored as JSON and validated by the surveydoc package.
package survey

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Statuses and types, forma2's ZSurveyStatus and ZSurveyType.
const (
	StatusDraft      = "draft"
	StatusInProgress = "inProgress"
	StatusPaused     = "paused"
	StatusCompleted  = "completed"

	TypeLink = "link"
	TypeApp  = "app"
)

// Statuses lists the survey statuses.
var Statuses = []string{StatusDraft, StatusInProgress, StatusPaused, StatusCompleted}

// DisplayOptions lists forma2's ZSurveyDisplayOption.
var DisplayOptions = []string{"displayOnce", "displayMultiple", "respondMultiple", "displaySome"}

// Language is a workspace language.
type Language struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Code        string    `json:"code"`
	Alias       *string   `json:"alias"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
}

// SurveyLanguage is one of a survey's languages, forma2's TSurveyLanguage.
type SurveyLanguage struct {
	Language Language `json:"language"`
	Default  bool     `json:"default"`
	Enabled  bool     `json:"enabled"`
}

// ActionClass is a workspace action class as a trigger names it.
type ActionClass struct {
	ID           uuid.UUID       `json:"id"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
	Name         string          `json:"name"`
	Description  *string         `json:"description"`
	Type         string          `json:"type"`
	Key          *string         `json:"key"`
	NoCodeConfig json.RawMessage `json:"noCodeConfig"`
	WorkspaceID  uuid.UUID       `json:"workspaceId"`
}

// Trigger is forma2's survey trigger shape: the action class that shows an app survey.
type Trigger struct {
	ActionClass ActionClass `json:"actionClass"`
}

// Segment is the contact segment an app survey targets, with the ids of the surveys that use it.
type Segment struct {
	ID          uuid.UUID       `json:"id"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	Title       string          `json:"title"`
	Description *string         `json:"description"`
	IsPrivate   bool            `json:"isPrivate"`
	Filters     json.RawMessage `json:"filters"`
	WorkspaceID uuid.UUID       `json:"workspaceId"`
	Surveys     []uuid.UUID     `json:"surveys"`
}

// FollowUp is a stored follow-up definition.
type FollowUp struct {
	ID        uuid.UUID       `json:"id"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Name      string          `json:"name"`
	Trigger   json.RawMessage `json:"trigger"`
	Action    json.RawMessage `json:"action"`
	SurveyID  uuid.UUID       `json:"surveyId"`
}

// Survey is a survey with everything the editor shows, forma2's TSurvey.
type Survey struct {
	ID                          uuid.UUID        `json:"id"`
	CreatedAt                   time.Time        `json:"createdAt"`
	UpdatedAt                   time.Time        `json:"updatedAt"`
	Name                        string           `json:"name"`
	Type                        string           `json:"type"`
	WorkspaceID                 uuid.UUID        `json:"workspaceId"`
	CreatedBy                   *uuid.UUID       `json:"createdBy"`
	Status                      string           `json:"status"`
	RedirectURL                 *string          `json:"redirectUrl"`
	DisplayOption               string           `json:"displayOption"`
	AutoClose                   *int             `json:"autoClose"`
	Triggers                    []Trigger        `json:"triggers"`
	RecontactDays               *int             `json:"recontactDays"`
	DisplayLimit                *int             `json:"displayLimit"`
	WelcomeCard                 json.RawMessage  `json:"welcomeCard"`
	Blocks                      json.RawMessage  `json:"blocks"`
	Endings                     json.RawMessage  `json:"endings"`
	HiddenFields                json.RawMessage  `json:"hiddenFields"`
	Variables                   json.RawMessage  `json:"variables"`
	FollowUps                   []FollowUp       `json:"followUps"`
	Delay                       int              `json:"delay"`
	PublishOn                   *time.Time       `json:"publishOn"`
	CloseOn                     *time.Time       `json:"closeOn"`
	ArchivedAt                  *time.Time       `json:"archivedAt"`
	AutoComplete                *int             `json:"autoComplete"`
	WorkspaceOverwrites         json.RawMessage  `json:"workspaceOverwrites"`
	Styling                     json.RawMessage  `json:"styling"`
	ShowLanguageSwitch          *bool            `json:"showLanguageSwitch"`
	SurveyClosedMessage         json.RawMessage  `json:"surveyClosedMessage"`
	Segment                     *Segment         `json:"segment"`
	SingleUse                   json.RawMessage  `json:"singleUse"`
	IsVerifyEmailEnabled        bool             `json:"isVerifyEmailEnabled"`
	Recaptcha                   json.RawMessage  `json:"recaptcha"`
	IsBackButtonHidden          bool             `json:"isBackButtonHidden"`
	IsAutoProgressingEnabled    bool             `json:"isAutoProgressingEnabled"`
	IsCaptureIPEnabled          bool             `json:"isCaptureIpEnabled"`
	IsAnonymizeResponsesEnabled bool             `json:"isAnonymizeResponsesEnabled"`
	Pin                         *string          `json:"pin"`
	DisplayPercentage           *float64         `json:"displayPercentage"`
	Languages                   []SurveyLanguage `json:"languages"`
	Metadata                    json.RawMessage  `json:"metadata"`
	Slug                        *string          `json:"slug"`
	CustomHeadScripts           *string          `json:"customHeadScripts"`
	CustomHeadScriptsMode       *string          `json:"customHeadScriptsMode"`
	InlineTriggers              json.RawMessage  `json:"inlineTriggers"`
}

// DefaultLanguageCode is the code of the survey's default language, or "" when it has none.
func (s *Survey) DefaultLanguageCode() string {
	for _, l := range s.Languages {
		if l.Default {
			return l.Language.Code
		}
	}
	return ""
}

// Quota is a survey quota definition.
type Quota struct {
	ID                      uuid.UUID       `json:"id"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
	SurveyID                uuid.UUID       `json:"surveyId"`
	Name                    string          `json:"name"`
	Limit                   int             `json:"limit"`
	Logic                   json.RawMessage `json:"logic"`
	Action                  string          `json:"action"`
	EndingCardID            *string         `json:"endingCardId"`
	CountPartialSubmissions bool            `json:"countPartialSubmissions"`
}

// ListItem is a survey as the survey list shows it.
type ListItem struct {
	ID                     uuid.UUID  `json:"id"`
	Name                   string     `json:"name"`
	WorkspaceID            uuid.UUID  `json:"workspaceId"`
	Type                   string     `json:"type"`
	Status                 string     `json:"status"`
	PublishOn              *time.Time `json:"publishOn"`
	ArchivedAt             *time.Time `json:"archivedAt"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
	ResponseCount          int        `json:"responseCount"`
	CompletedResponseCount int        `json:"completedResponseCount"`
	Creator                *Creator   `json:"creator"`
}

// Creator is who created a listed survey.
type Creator struct {
	Name string `json:"name"`
}

// Lifecycle is the minimal acknowledgement of an archive or restore.
type Lifecycle struct {
	ID         uuid.UUID  `json:"id"`
	Status     string     `json:"status"`
	ArchivedAt *time.Time `json:"archivedAt"`
}
