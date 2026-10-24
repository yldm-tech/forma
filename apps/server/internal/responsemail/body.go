package responsemail

import (
	"html/template"

	"github.com/microcosm-cc/bluemonday"

	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/userinput"
)

// bodyPolicy is forma2's send-time sanitize-html allowlist for an email body an author wrote: paragraphs, inline emphasis, links, line breaks and lists, the dir and class attributes everywhere, a link's href, rel and target, an ordered list's start and an item's value, and only http and https links. Disallowed tags are dropped with their text kept, except script and style, whose text goes too. Relative links are kept, as sanitize-html kept them.
var bodyPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "span", "b", "strong", "i", "em", "a", "br", "ul", "ol", "li")
	p.AllowAttrs("dir", "class").Globally()
	p.AllowAttrs("href", "rel", "target").OnElements("a")
	p.AllowAttrs("start").OnElements("ol")
	p.AllowAttrs("value").OnElements("li")
	p.AllowURLSchemes("http", "https")
	p.AllowRelativeURLs(true)
	// A link whose href was refused stays as a bare <a>, as sanitize-html left it.
	p.AllowNoAttrs().OnElements("a")
	return p
}()

// SanitizeBody is forma2's sanitizeBody: recall tokens expanded against the response's answers and reserved values with every value escaped and dates in locale, then the result reduced to the allowlist. The escaping is what keeps an answer from smuggling a link into a Forma-branded email, since the allowlist permits the author's own links.
func SanitizeBody(body string, values, variables map[string]any, locale string) template.HTML {
	return template.HTML(bodyPolicy.Sanitize(ParseRecall(body, values, variables, locale, true))) //nolint:gosec // sanitized by the allowlist above
}

// EmailOptions are the parts of a survey-response email its author chose, forma2's buildSurveyResponseEmailHtml arguments.
type EmailOptions struct {
	Subject             string
	Body                string
	AttachResponseData  bool
	IncludeVariables    bool
	IncludeHiddenFields bool
	// LogoURL is the organization's email logo, as stored; relative storage URLs are made absolute.
	LogoURL string
	// Locale is the respondent's; empty means en-US.
	Locale string
	// PublicBase is the origin relative storage URLs resolve against.
	PublicBase string
}

// FollowUpContent is forma2's buildSurveyResponseEmailHtml as the email it renders: the sanitized body, then, when the author attached the response data, its elements, and, when they also asked for them, the variables and the hidden fields.
func (a *Answers) FollowUpContent(sv *Survey, o EmailOptions) (mailer.FollowUp, error) {
	locale := o.Locale
	if locale == "" {
		locale = mailer.DefaultLocale
	}
	values, err := a.ServerValues(sv)
	if err != nil {
		return mailer.FollowUp{}, err
	}
	c := mailer.FollowUp{Subject: o.Subject, Body: SanitizeBody(o.Body, values, a.Variables, locale)}
	if o.LogoURL != "" {
		c.LogoURL = ResolveStorageURL(o.PublicBase, o.LogoURL)
	}
	if !o.AttachResponseData {
		return c, nil
	}
	elements, err := a.Elements(sv)
	if err != nil {
		return mailer.FollowUp{}, err
	}
	c.ResponseData = WithAbsoluteFiles(elements, o.PublicBase)
	if o.IncludeVariables {
		c.Variables = a.ComputedValues(sv)
	}
	if o.IncludeHiddenFields {
		c.HiddenFields = a.HiddenValues(sv)
	}
	return c, nil
}

// Reasons a recipient does not resolve, forma2's resolveResponseRecipient messages.
const (
	ReasonValueNotFound = "Recipient value not found in response data"
	ReasonInvalid       = "Resolved email recipient is not a valid address"
	ReasonNoEmail       = "Email address not found in response data"
)

// ResolveRecipient is forma2's resolveResponseRecipient: to itself when it is an address; otherwise the answer stored under to (an open text or hidden field, or verifiedEmail), or the third entry of a contact info answer, which must be an address. It returns the address, or "" and the reason.
func (a *Answers) ResolveRecipient(to string) (string, string) {
	if userinput.ValidEmailFormat(to) {
		return to, ""
	}
	value, ok := a.Data[to]
	if !ok || !truthy(value) {
		return "", ReasonValueNotFound
	}
	switch x := value.(type) {
	case string:
		if userinput.ValidEmailFormat(x) {
			return x, ""
		}
	case []any:
		if len(x) < 3 || !truthy(x[2]) {
			return "", ReasonNoEmail
		}
		if s, ok := x[2].(string); ok && userinput.ValidEmailFormat(s) {
			return s, ""
		}
	}
	return "", ReasonInvalid
}
