package mailer

import (
	"html/template"
	"net/url"
	"strings"
)

// Element types whose answers render differently, forma2's renderEmailResponseValue cases.
const (
	elementFileUpload       = "fileUpload"
	elementPictureSelection = "pictureSelection"
	elementRanking          = "ranking"
)

// ResponseElement is one answered element of a response, forma2's ProcessedResponseElement: the element's label (its headline as text), its type, and the answer as text or, for file uploads, picture selections and rankings, as a list. An element without an answer is not rendered.
type ResponseElement struct {
	Label string
	Type  string
	Text  string
	Items []string
}

// Answered is forma2's `if (!e.response) return null`: an empty text is no answer, while an empty list still renders its label.
func (e ResponseElement) Answered() bool {
	if e.Items != nil {
		return true
	}
	return e.Text != ""
}

// FileUpload, PictureSelection and Ranking select how the answer renders.
func (e ResponseElement) FileUpload() bool       { return e.Type == elementFileUpload }
func (e ResponseElement) PictureSelection() bool { return e.Type == elementPictureSelection }
func (e ResponseElement) Ranking() bool          { return e.Type == elementRanking }

// RankedItems are the ranking's non-empty items, numbered from one as forma2 rendered them.
func (e ResponseElement) RankedItems() []string {
	var out []string
	for _, item := range e.Items {
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// FileName is forma2's getOriginalFileNameFromUrl in the email package: the last path segment of the URL, percent-decoded, or "file".
func FileName(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		name := u.EscapedPath()
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			return "file"
		}
		if decoded, err := url.PathUnescape(name); err == nil {
			return decoded
		}
		return name
	}
	parts := strings.Split(raw, "/")
	if last := parts[len(parts)-1]; last != "" {
		return last
	}
	return "file"
}

// LastSegment is the alt text forma2 gave a picture: the URL's last "/" segment.
func LastSegment(raw string) string {
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}

// EmbeddedValue is a computed Embedded Data field's value on a response, forma2's variables block: its name, whether it is a number, and the value as text.
type EmbeddedValue struct {
	Name   string
	Number bool
	Value  string
}

// HiddenValue is an ingested (hidden) field's value on a response.
type HiddenValue struct {
	ID    string
	Value string
}

// ResponseFinished tells a member who subscribed to a survey's alerts that a response was completed, forma2's ResponseFinishedEmail. The URLs are absolute; ResponseCount counts every response of the survey, finished or not.
type ResponseFinished struct {
	SurveyName                 string
	Elements                   []ResponseElement
	EmbeddedFields             []EmbeddedValue
	HiddenFields               []HiddenValue
	ResponsesURL               string
	ResponseCount              int
	UnsubscribeSurveyURL       string
	UnsubscribeOrganizationURL string
	LogoURL                    string
}

func (ResponseFinished) template() string   { return "response_finished" }
func (ResponseFinished) subjectKey() string { return "emails.response_finished_email_subject" }
func (c ResponseFinished) logo() string     { return c.LogoURL }
func (ResponseFinished) unsigned()          {}

// The subject names the survey and never the respondent.
func (c ResponseFinished) subject(t func(string, ...any) string) string {
	return t(c.subjectKey(), "surveyName", c.SurveyName)
}

// MoreResponses is the number in "View {n} more responses", or 0 for "View survey summary".
func (c ResponseFinished) MoreResponses() int {
	if c.ResponseCount > 1 {
		return c.ResponseCount - 1
	}
	return 0
}

// FollowUp is a survey follow-up email and the workflow send_email step's body, forma2's FollowUpEmail: the author's body, already recall-expanded and sanitized, then the optional response data, variables and hidden fields. Subject is the author's subject as written.
type FollowUp struct {
	Subject      string
	Body         template.HTML
	ResponseData []ResponseElement
	Variables    []EmbeddedValue
	HiddenFields []HiddenValue
	LogoURL      string
}

func (FollowUp) template() string   { return "follow_up" }
func (FollowUp) subjectKey() string { return "" }
func (c FollowUp) logo() string     { return c.LogoURL }
func (FollowUp) unsigned()          {}

func (c FollowUp) subject(func(string, ...any) string) string { return c.Subject }

// EmbedSurveyPreview sends a member the email-embed snippet of a survey, forma2's EmbedSurveyPreviewEmail. HTML is the snippet as the share modal renders it, trusted markup the server generated.
type EmbedSurveyPreview struct {
	HTML        template.HTML
	WorkspaceID string
	LogoURL     string
}

func (EmbedSurveyPreview) template() string   { return "embed_survey_preview" }
func (EmbedSurveyPreview) subjectKey() string { return "emails.embed_survey_preview_email_subject" }
func (c EmbedSurveyPreview) logo() string     { return c.LogoURL }
func (EmbedSurveyPreview) unsigned()          {}
func (EmbedSurveyPreview) lightOnly()         {}

// EmailCustomizationPreview shows an organization owner the logo their emails will carry, forma2's EmailCustomizationPreviewEmail.
type EmailCustomizationPreview struct {
	UserName string
	LogoURL  string
}

func (EmailCustomizationPreview) template() string { return "email_customization_preview" }
func (EmailCustomizationPreview) subjectKey() string {
	return "emails.email_customization_preview_email_subject"
}
func (c EmailCustomizationPreview) logo() string { return c.LogoURL }
func (EmailCustomizationPreview) unsigned()      {}
