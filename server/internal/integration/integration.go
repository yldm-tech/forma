// Package integration is forma2's third-party integrations: Google Sheets, Airtable, Notion and Slack (apps/web/lib/{googleSheet,airtable,notion,slack,integration,oauth}, the integration routes under apps/web/app/api, the workspace integrations pages' actions, and the response pipeline's handle-integrations). A workspace connects each provider once through its OAuth flow, at the callback paths registered with the providers' apps, and links surveys to a spreadsheet, table, database or channel; every finished response is then written there as forma2 wrote it.
//
// The integrations table holds only the links ({"data": [...]}); the provider's token set and the connected account's email live in integration_credentials, sealed with platform/secret and never sent to the browser. Only the OAuth callbacks and the token refreshes write credentials.
package integration

import (
	"encoding/json"
	"time"
	"unicode/utf16"

	"github.com/google/uuid"
)

// The integration types, forma2's ZIntegrationType without n8n, which nothing ever handled.
const (
	TypeGoogleSheets = "googleSheets"
	TypeAirtable     = "airtable"
	TypeNotion       = "notion"
	TypeSlack        = "slack"
)

// Types are the integration types in the order forma2's handlers were tried.
var Types = []string{TypeGoogleSheets, TypeAirtable, TypeNotion, TypeSlack}

// Message size limits, forma2's SLACK_MESSAGE_LIMIT, GOOGLE_SHEET_MESSAGE_LIMIT, AIRTABLE_MESSAGE_LIMIT and NOTION_RICH_TEXT_LIMIT.
const (
	slackMessageLimit       = 2995
	googleSheetMessageLimit = 49995
	airtableMessageLimit    = 99995
	notionRichTextLimit     = 1995
)

// truncate is forma2's `text.length > limit ? truncateText(text, limit) : text`: lengths count UTF-16 code units, as JavaScript's do, and a cut text ends in "...".
func truncate(text string, limit int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	return string(utf16.Decode(units[:limit])) + "..."
}

// LinkBase is what every link carries, forma2's ZIntegrationBaseSurveyData without the element selection: the survey, when the link was made, and which extra columns a row gets. A flag that is absent counts as off.
type LinkBase struct {
	CreatedAt                time.Time `json:"createdAt" binding:"required"`
	SurveyID                 string    `json:"surveyId" binding:"required"`
	SurveyName               string    `json:"surveyName"`
	IncludeVariables         *bool     `json:"includeVariables,omitempty"`
	IncludeHiddenFields      *bool     `json:"includeHiddenFields,omitempty"`
	IncludeMetadata          *bool     `json:"includeMetadata,omitempty"`
	IncludeCreatedAt         *bool     `json:"includeCreatedAt,omitempty"`
	IncludeContactAttributes *bool     `json:"includeContactAttributes,omitempty"`
}

func (l LinkBase) base() LinkBase { return l }

// Elements is the element selection of a Google Sheets, Airtable or Slack link: the element ids whose answers become columns, in order, and the label the pages show for the selection ("All questions" or "Selected questions").
type Elements struct {
	ElementIDs []string `json:"elementIds" binding:"required"`
	Elements   string   `json:"elements"`
}

// SheetLink links a survey to a spreadsheet; rows go to its first sheet.
type SheetLink struct {
	LinkBase
	Elements
	SpreadsheetID   string `json:"spreadsheetId" binding:"required"`
	SpreadsheetName string `json:"spreadsheetName"`
}

// AirtableLink links a survey to an Airtable table.
type AirtableLink struct {
	LinkBase
	Elements
	BaseID    string `json:"baseId" binding:"required"`
	TableID   string `json:"tableId" binding:"required"`
	TableName string `json:"tableName"`
}

// SlackLink links a survey to a Slack channel.
type SlackLink struct {
	LinkBase
	Elements
	ChannelID   string `json:"channelId" binding:"required"`
	ChannelName string `json:"channelName"`
}

// NotionLink links a survey to a Notion database, each mapping entry writing one value into one property.
type NotionLink struct {
	LinkBase
	Mapping      []NotionMapping `json:"mapping" binding:"required,dive"`
	DatabaseID   string          `json:"databaseId" binding:"required"`
	DatabaseName string          `json:"databaseName"`
}

// NotionMapping maps an element (or "metadata", "createdAt", "person.<attribute key>", a hidden field's or a variable's storage key) to a database property.
type NotionMapping struct {
	Element NotionRef `json:"element"`
	Column  NotionRef `json:"column"`
}

// NotionRef names one side of a mapping entry.
type NotionRef struct {
	ID   string `json:"id" binding:"required"`
	Name string `json:"name"`
	Type string `json:"type" binding:"required"`
}

// linkConfig is the integrations.config column: the links in the order the pages keep them.
type linkConfig[T any] struct {
	Data []T `json:"data"`
}

// decodeLinks reads a stored config's links.
func decodeLinks[T any](raw json.RawMessage) ([]T, error) {
	var c linkConfig[T]
	if len(raw) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return c.Data, nil
}

// encodeLinks writes the config column for links; no links is an empty list, as forma2 stored it.
func encodeLinks[T any](links []T) (json.RawMessage, error) {
	if links == nil {
		links = []T{}
	}
	return json.Marshal(linkConfig[T]{Data: links})
}

// emptyConfig is the config of an integration that was just connected.
var emptyConfig = json.RawMessage(`{"data":[]}`)

// GoogleKey is forma2's ZGoogleCredential: Google's token response, with expires_in turned into expiry_date (epoch milliseconds) as google-auth-library stored it.
type GoogleKey struct {
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ExpiryDate   int64  `json:"expiry_date"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// AirtableKey is forma2's ZIntegrationAirtableCredential; expiry_date is an ISO timestamp.
type AirtableKey struct {
	ExpiryDate   string `json:"expiry_date"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// SlackKey is forma2's ZIntegrationSlackCredential: the bot token of oauth.v2.access, which does not expire.
type SlackKey struct {
	AppID      string `json:"app_id"`
	AuthedUser struct {
		ID string `json:"id"`
	} `json:"authed_user"`
	TokenType   string `json:"token_type"`
	AccessToken string `json:"access_token"`
	BotUserID   string `json:"bot_user_id"`
	Team        struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"team"`
}

// notionKey is what is read of Notion's token response, which is stored whole.
type notionKey struct {
	AccessToken   string  `json:"access_token"`
	BotID         string  `json:"bot_id"`
	WorkspaceName *string `json:"workspace_name"`
}

// Credentials is what integration_credentials seals for an integration: the provider's token set as forma2 kept it in config.key, and the connected account's email (Google Sheets and Airtable), which forma2 kept in config.email. The data migration splits forma2's config into this and the links.
type Credentials struct {
	Key   json.RawMessage `json:"key"`
	Email string          `json:"email,omitempty"`
}

// Integration is one connected provider of a workspace.
type Integration struct {
	ID                  uuid.UUID
	WorkspaceID         uuid.UUID
	Type                string
	Config              json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	LastErrorAt         *time.Time
	LastErrorMessage    *string
	ConsecutiveFailures int
	// Credentials are nil when the row has none or they do not open (a wrong or rotated ENCRYPTION_KEY): the integration then fails at the provider until it is reconnected, as forma2's did.
	Credentials *Credentials
}

// key decodes the integration's token set; false when it has none or it does not decode.
func key[T any](in *Integration) (*T, bool) {
	if in.Credentials == nil || len(in.Credentials.Key) == 0 {
		return nil, false
	}
	var k T
	if err := json.Unmarshal(in.Credentials.Key, &k); err != nil {
		return nil, false
	}
	return &k, true
}
