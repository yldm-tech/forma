package mailer

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// DefaultLocale is the fallback for an unknown locale and for a key a locale lacks.
const DefaultLocale = "en-US"

// Locales are the catalogues forma2 shipped (AVAILABLE_LOCALES).
var Locales = []string{"en-US", "ja-JP", "zh-Hans-CN", "zh-Hant-TW"}

//go:embed templates/*.html
var templateFS embed.FS

// locales/<locale>.json is the "emails" namespace of web/locales/<locale>.json; TestCatalogueMatchesWebLocales keeps the two identical.
//
//go:embed locales/*.json
var localeFS embed.FS

// Content is one kind of email. Each implementation names its template and subject key; modules fill the fields and call Mailer.SendTemplate.
type Content interface {
	template() string
	subjectKey() string
}

// Verification asks a new user to confirm their address. VerificationRequestLink is where an expired link can be replaced.
type Verification struct {
	VerifyLink              string
	VerificationRequestLink string
}

func (Verification) template() string   { return "verification" }
func (Verification) subjectKey() string { return "emails.verification_email_subject" }

// NewEmailVerification confirms a changed address; it is sent to the new address only.
type NewEmailVerification struct {
	VerifyLink string
}

func (NewEmailVerification) template() string   { return "new_email_verification" }
func (NewEmailVerification) subjectKey() string { return "emails.verification_new_email_subject" }

// PasswordReset carries the reset link and how long it stays valid.
type PasswordReset struct {
	VerifyLink            string
	LinkValidityInMinutes int
}

func (PasswordReset) template() string   { return "password_reset" }
func (PasswordReset) subjectKey() string { return "emails.forgot_password_email_subject" }

// PasswordChanged tells the user their password was just changed.
type PasswordChanged struct{}

func (PasswordChanged) template() string   { return "password_changed" }
func (PasswordChanged) subjectKey() string { return "emails.password_reset_notify_email_subject" }

// DeleteAccountConfirmation carries the link that confirms deleting an account that has no password to confirm with.
type DeleteAccountConfirmation struct {
	DeleteLink            string
	LinkValidityInMinutes int
}

func (DeleteAccountConfirmation) template() string   { return "delete_account" }
func (DeleteAccountConfirmation) subjectKey() string { return "emails.delete_account_email_subject" }

// Invite asks InviteeName to join InviterName's organization.
type Invite struct {
	InviteeName string
	InviterName string
	VerifyLink  string
}

func (Invite) template() string   { return "invite" }
func (Invite) subjectKey() string { return "emails.invite_member_email_subject" }

// InviteAccepted tells the inviter that the invitee joined.
type InviteAccepted struct {
	InviterName string
	InviteeName string
}

func (InviteAccepted) template() string   { return "invite_accepted" }
func (InviteAccepted) subjectKey() string { return "emails.invite_accepted_email_subject" }

// SSORecoveryFactorsRemoved tells a user that linking single sign-on to their account removed the password, two-factor authentication and other single sign-on connections set on it while its address was unproven, and where to set them up again.
type SSORecoveryFactorsRemoved struct {
	PasswordRemoved      bool
	TwoFactorRemoved     bool
	SingleSignOnRemoved  bool
	SecuritySettingsLink string
}

func (SSORecoveryFactorsRemoved) template() string { return "sso_recovery_factors_removed" }
func (SSORecoveryFactorsRemoved) subjectKey() string {
	return "emails.sso_recovery_factors_removed_email_subject"
}

// LinkSurvey sends a respondent who verified their address the link to a survey that requires email verification. SurveyName comes from the survey row, never from the request. LogoURL is the organization's email logo, absolute, or empty for Forma's.
type LinkSurvey struct {
	SurveyName string
	SurveyLink string
	LogoURL    string
}

func (LinkSurvey) template() string   { return "link_survey" }
func (LinkSurvey) subjectKey() string { return "emails.verified_link_survey_email_subject" }
func (c LinkSurvey) logo() string     { return c.LogoURL }

// brandedContent is content that replaces Forma's logo at the top of the email with the organization's.
type brandedContent interface {
	logo() string
}

// unsignedContent is content without forma2's EmailFooter sign-off ("Have a great day! The Forma Team"), which only the account and invite emails and the link survey email carried.
type unsignedContent interface {
	unsigned()
}

// lightOnlyContent is content rendered with forma2's forceLightMode: email clients are asked not to darken it, so embedded survey markup keeps its colors.
type lightOnlyContent interface {
	lightOnly()
}

// subjectContent is content whose subject takes {name} values, or is not a catalogue entry at all.
type subjectContent interface {
	subject(t func(key string, pairs ...any) string) string
}

type legal struct {
	PrivacyURL     string
	ImprintURL     string
	ImprintAddress string
}

type renderer struct {
	templates map[string]*template.Template
	catalogue map[string]map[string]string
	legal     legal
}

// button is the argument of the shared "button" template.
type button struct {
	Href  string
	Label string
}

func newRenderer(cfg config.Config) (*renderer, error) {
	catalogue, err := loadCatalogue()
	if err != nil {
		return nil, err
	}
	base, err := template.New("layout.html").
		Funcs(template.FuncMap{
			"button":      func(href, label string) button { return button{Href: href, Label: label} },
			"fileName":    FileName,
			"lastSegment": LastSegment,
			"inc":         func(i int) int { return i + 1 },
		}).
		ParseFS(templateFS, "templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("parse email layout: %w", err)
	}
	entries, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	templates := map[string]*template.Template{}
	for _, file := range entries {
		name := strings.TrimSuffix(path.Base(file), ".html")
		if name == "layout" {
			continue
		}
		t, err := base.Clone()
		if err == nil {
			t, err = t.ParseFS(templateFS, file)
		}
		if err != nil {
			return nil, fmt.Errorf("parse email template %s: %w", name, err)
		}
		templates[name] = t
	}
	return &renderer{
		templates: templates,
		catalogue: catalogue,
		legal:     legal{PrivacyURL: cfg.PrivacyURL, ImprintURL: cfg.ImprintURL, ImprintAddress: cfg.ImprintAddress},
	}, nil
}

func loadCatalogue() (map[string]map[string]string, error) {
	catalogue := map[string]map[string]string{}
	for _, locale := range Locales {
		b, err := localeFS.ReadFile("locales/" + locale + ".json")
		if err != nil {
			return nil, fmt.Errorf("read email catalogue %s: %w", locale, err)
		}
		var messages map[string]string
		if err := json.Unmarshal(b, &messages); err != nil {
			return nil, fmt.Errorf("parse email catalogue %s: %w", locale, err)
		}
		catalogue[locale] = messages
	}
	return catalogue, nil
}

// view is what the templates see: the content, the footer's legal links, and T for translated copy.
type view struct {
	Locale  string
	Content Content
	Legal   legal
	r       *renderer
}

// Logo is the organization logo that replaces Forma's, or "" for Forma's (forma2's EmailTemplate treats its own logo URL as no custom logo).
func (v view) Logo() string {
	if b, ok := v.Content.(brandedContent); ok && b.logo() != formaLogoURL {
		return b.logo()
	}
	return ""
}

// SignOff reports whether the email ends with forma2's EmailFooter sign-off.
func (v view) SignOff() bool {
	_, unsigned := v.Content.(unsignedContent)
	return !unsigned
}

// LightOnly reports whether the email is rendered with forma2's forceLightMode.
func (v view) LightOnly() bool {
	_, light := v.Content.(lightOnlyContent)
	return light
}

// formaLogoURL is the default logo of every email.
const formaLogoURL = "https://forma.yldm.ai/logo-transparent.png"

// T translates an "emails." key in the view's locale and interpolates {name} placeholders from name/value pairs. A key missing from the locale falls back to en-US, and one missing there renders as the key, as i18next does.
func (v view) T(key string, pairs ...any) string {
	return v.r.translate(v.Locale, key, pairs...)
}

func (r *renderer) translate(locale, key string, pairs ...any) string {
	name := strings.TrimPrefix(key, "emails.")
	msg, ok := r.catalogue[locale][name]
	if !ok {
		if msg, ok = r.catalogue[DefaultLocale][name]; !ok {
			return key
		}
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		msg = strings.ReplaceAll(msg, "{"+fmt.Sprint(pairs[i])+"}", fmt.Sprint(pairs[i+1]))
	}
	return msg
}

func (r *renderer) render(locale string, content Content) (subject, html string, err error) {
	if _, ok := r.catalogue[locale]; !ok {
		locale = DefaultLocale
	}
	t, ok := r.templates[content.template()]
	if !ok {
		return "", "", fmt.Errorf("no email template %q", content.template())
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", view{Locale: locale, Content: content, Legal: r.legal, r: r}); err != nil {
		return "", "", fmt.Errorf("render email %s: %w", content.template(), err)
	}
	if sc, ok := content.(subjectContent); ok {
		return sc.subject(func(key string, pairs ...any) string { return r.translate(locale, key, pairs...) }), buf.String(), nil
	}
	return r.translate(locale, content.subjectKey()), buf.String(), nil
}
