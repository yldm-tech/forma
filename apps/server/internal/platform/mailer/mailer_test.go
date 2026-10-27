package mailer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer/mailertest"
	"github.com/yldm-tech/forma/apps/server/internal/platform/repotest"
)

var update = flag.Bool("update", false, "rewrite locales/*.json from apps/web/locales")

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func baseConfig() config.Config {
	return config.Config{MailFrom: "noreply@forma.yldm.ai", MailFromName: "Forma"}
}

func newMailer(t *testing.T, cfg config.Config, tr mailer.Transport) *mailer.Mailer {
	t.Helper()
	m, err := mailer.NewWithTransport(cfg, tr, discard)
	if err != nil {
		t.Fatalf("NewWithTransport: %v", err)
	}
	return m
}

var everyTemplate = []mailer.Content{
	mailer.Verification{VerifyLink: "https://app.example.com/verify?token=v1", VerificationRequestLink: "https://app.example.com/auth/verification-requested"},
	mailer.NewEmailVerification{VerifyLink: "https://app.example.com/verify-email-change?token=c1"},
	mailer.PasswordReset{VerifyLink: "https://app.example.com/auth/forgot-password/reset?token=r1", LinkValidityInMinutes: 30},
	mailer.PasswordChanged{},
	mailer.Invite{InviteeName: "Grace", InviterName: "Ada", VerifyLink: "https://app.example.com/invite?token=i1"},
	mailer.InviteAccepted{InviterName: "Ada", InviteeName: "Grace"},
	mailer.DeleteAccountConfirmation{DeleteLink: "https://app.example.com/auth/delete-account?token=d1", LinkValidityInMinutes: 60},
	mailer.SSORecoveryFactorsRemoved{PasswordRemoved: true, TwoFactorRemoved: true, SingleSignOnRemoved: true, SecuritySettingsLink: "https://app.example.com/account/settings/profile"},
	mailer.LinkSurvey{SurveyName: "Customer Feedback", SurveyLink: "https://surveys.example.com/s/s1?verify=t1"},
	responseFinished,
	followUp,
	mailer.EmbedSurveyPreview{HTML: `<table><tr><td>How likely are you to recommend us?</td></tr></table>`, WorkspaceID: "ws1"},
	mailer.EmailCustomizationPreview{UserName: "Ada"},
	weeklySummary,
}

func ptr[T any](v T) *T { return &v }

var weeklySummary = mailer.WeeklySummary{
	OrganizationName: "Acme <Labs>", PeriodStart: "2026-10-19", PeriodEnd: "2026-10-25", TimeZone: "Asia/Tokyo",
	Responses: 26, PreviousResponses: 18,
	Surveys: []mailer.WeeklySummarySurvey{
		{
			Name: "Customer Feedback", WorkspaceName: "Production", SummaryURL: "https://app.example.com/app/workspaces/ws1/surveys/s1/summary?filter=x&utm_source=email_weekly_summary",
			Responses: 24, PreviousResponses: 18, Completed: 21,
			NPS:  &mailer.WeeklySummaryScore{Current: ptr(35.0), Previous: ptr(-12.5)},
			CSAT: &mailer.WeeklySummaryScore{Current: ptr(80.0)},
			Tags: []mailer.WeeklySummaryTag{{Name: "billing", Count: 3}, {Name: "ui", Count: 2}},
		},
		{Name: "Exit poll", WorkspaceName: "Staging", SummaryURL: "https://app.example.com/app/workspaces/ws2/surveys/s2/summary", Responses: 0, PreviousResponses: 2},
	},
	MoreSurveys: 3,
	SettingsURL: "https://app.example.com/app/account/settings/notifications?elementId=o1&type=unsubscribedWeeklySummaryOrganizationIds",
}

var responseFinished = mailer.ResponseFinished{
	SurveyName: "Customer Feedback",
	Elements: []mailer.ResponseElement{
		{Label: "What did you like?", Type: "openText", Text: "Speed\nand <b>price</b>"},
		{Label: "Skipped", Type: "openText"},
		{Label: "Attach a file", Type: "fileUpload", Items: []string{"https://app.example.com/storage/ws/private/My%20Report.pdf"}},
		{Label: "Pick one", Type: "pictureSelection", Items: []string{"https://cdn.example.com/a/cat.png"}},
		{Label: "Rank these", Type: "ranking", Items: []string{"Price", "", "Speed"}},
		{Label: "No files", Type: "fileUpload", Items: []string{}},
	},
	EmbeddedFields:             []mailer.EmbeddedValue{{Name: "Score", Number: true, Value: "0"}},
	HiddenFields:               []mailer.HiddenValue{{ID: "utm_source", Value: "newsletter"}},
	ResponsesURL:               "https://app.example.com/workspaces/ws1/surveys/s1/responses?utm_source=email_notification&utm_medium=email&utm_content=view_responses_CTA",
	ResponseCount:              15,
	UnsubscribeSurveyURL:       "https://app.example.com/workspaces/ws1/settings/notifications?type=alert&elementId=s1",
	UnsubscribeOrganizationURL: "https://app.example.com/workspaces/ws1/settings/notifications?type=unsubscribedOrganizationIds&elementId=o1",
}

var followUp = mailer.FollowUp{
	Subject:      "Thanks for {your} feedback",
	Body:         `<p>Thanks <b>Ada</b>!</p>`,
	ResponseData: []mailer.ResponseElement{{Label: "What did you like?", Type: "openText", Text: "Speed"}, {Label: "Attach a file", Type: "fileUpload", Items: []string{"https://app.example.com/storage/ws/private/a.pdf"}}},
	Variables:    []mailer.EmbeddedValue{{Name: "Score", Number: true, Value: "7"}, {Name: "Tier", Value: "gold"}},
	HiddenFields: []mailer.HiddenValue{{ID: "utm_source", Value: "newsletter"}},
}

var (
	placeholder  = regexp.MustCompile(`\{[A-Za-z]+\}`)
	untranslated = regexp.MustCompile(`emails\.[a-z0-9_]+`)
)

// Every template renders a complete document in every locale with no key or placeholder left untranslated (forma2's render.test.ts).
func TestRenderEveryTemplateInEveryLocale(t *testing.T) {
	m := newMailer(t, baseConfig(), nil)
	for _, locale := range mailer.Locales {
		for _, content := range everyTemplate {
			t.Run(fmt.Sprintf("%s/%T", locale, content), func(t *testing.T) {
				subject, html, err := m.Render(locale, content)
				if err != nil {
					t.Fatal(err)
				}
				if subject == "" || strings.HasPrefix(subject, "emails.") {
					t.Fatalf("subject = %q", subject)
				}
				if !strings.HasPrefix(html, "<!DOCTYPE html>") || !strings.Contains(html, `<html lang="`+locale+`">`) || !strings.HasSuffix(strings.TrimSpace(html), "</html>") {
					t.Fatalf("not a complete document:\n%s", html)
				}
				_, literalSubject := content.(mailer.FollowUp)
				if untranslated.MatchString(html) || placeholder.MatchString(html) || (!literalSubject && placeholder.MatchString(subject)) {
					t.Fatalf("untranslated key or placeholder left:\n%s", html)
				}
			})
		}
	}
}

func TestRenderEnglishCopy(t *testing.T) {
	m := newMailer(t, baseConfig(), nil)
	cases := []struct {
		content mailer.Content
		subject string
		want    []string
	}{
		{everyTemplate[0], "Please verify your email to use Forma", []string{
			"Almost there!", "To start using Forma please verify your email below:", "Verify email",
			"You can also click on this link:", `href="https://app.example.com/verify?token=v1"`, "The link is valid for 24 hours.",
			"If it has expired please request a new token here:", "Request new verification",
		}},
		{everyTemplate[1], "Email change verification", []string{
			"To verify your new email address, please click the button below:",
			"If you did not request this email change, please ignore this email or contact support immediately.",
		}},
		{everyTemplate[2], "Reset your Forma password", []string{
			"Change password", "You have requested a link to change your password.", "The link is valid for 30 minutes.",
			"If you did not request this, please ignore this email.",
		}},
		{everyTemplate[3], "Your Forma password has been changed", []string{"Password changed", "Your password has been changed successfully."}},
		{everyTemplate[4], "You are invited to collaborate on Forma!", []string{
			"Hey Grace", "Your colleague Ada invited you to join them at Forma.", "Join organization", `href="https://app.example.com/invite?token=i1"`,
		}},
		{everyTemplate[5], "You have got a new organization member!", []string{"Hey Ada", "Just letting you know that Grace accepted your invitation."}},
		{everyTemplate[6], "Confirm your Forma account deletion", []string{
			"Confirm your account deletion", "You have requested to delete your Forma account. To confirm, please click the button below:",
			"This will permanently delete your account and all associated data. This action cannot be undone.",
			"Confirm account deletion", `href="https://app.example.com/auth/delete-account?token=d1"`, "The link is valid for 60 minutes.",
			"If you did not request this, please ignore this email and your account will remain active.",
		}},
		{everyTemplate[7], "Sign-in factors were removed from your Forma account", []string{
			"Sign-in factors removed from your account",
			"You just signed in with single sign-on for the first time. Because your email address had never been verified, we removed the sign-in factors that were set on the account before:",
			"Your password was removed.", "Two-factor authentication was removed.", "Single sign-on connections added to the account before were removed.",
			"You can keep signing in with single sign-on. To use a password or two-factor authentication again, set them up in your security settings.",
			"Review security settings", `href="https://app.example.com/account/settings/profile"`,
			"If you did not just sign in with single sign-on, contact your administrator immediately.",
		}},
		{everyTemplate[8], "Your survey is ready to be filled out.", []string{
			"Hey 👋", "Thanks for validating your email!", "To fill out the survey please click on the button below:", "Take survey",
			`href="https://surveys.example.com/s/s1?verify=t1"`, "Survey name: Customer Feedback",
		}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%T", tc.content), func(t *testing.T) {
			t.Parallel()
			subject, html, err := m.Render("en-US", tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if subject != tc.subject {
				t.Errorf("subject = %q, want %q", subject, tc.subject)
			}
			for _, want := range append(tc.want, "Have a great day!", "The Forma Team", "This email was sent via Forma.", "logo-transparent.png") {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q", want)
				}
			}
		})
	}
}

func TestRenderEscapesContent(t *testing.T) {
	_, html, err := newMailer(t, baseConfig(), nil).Render("en-US", mailer.Invite{
		InviteeName: `<script>alert(1)</script>`, InviterName: "Ada & Co", VerifyLink: "javascript:alert(1)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, `href="javascript:`) {
		t.Fatalf("unescaped content:\n%s", html)
	}
	if !strings.Contains(html, "Ada &amp; Co") {
		t.Fatalf("inviter name not escaped as text:\n%s", html)
	}
}

func TestRenderFallsBackToEnglish(t *testing.T) {
	m := newMailer(t, baseConfig(), nil)
	subject, html, err := m.Render("fr-FR", mailer.PasswordChanged{})
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Your Forma password has been changed" || !strings.Contains(html, `lang="en-US"`) {
		t.Fatalf("subject %q, want the en-US rendering", subject)
	}
	if subject, _, _ := m.Render("ja-JP", mailer.PasswordChanged{}); subject == "Your Forma password has been changed" {
		t.Fatal("ja-JP rendered in English")
	}
}

func TestRenderLegalLinksOnlyWhenSet(t *testing.T) {
	_, bare, _ := newMailer(t, baseConfig(), nil).Render("en-US", mailer.PasswordChanged{})
	for _, absent := range []string{"Imprint", "Privacy Policy", " • "} {
		if strings.Contains(bare, absent) {
			t.Errorf("%q rendered without legal settings", absent)
		}
	}

	cfg := baseConfig()
	cfg.PrivacyURL = "https://example.com/privacy"
	cfg.ImprintURL = "https://example.com/imprint"
	cfg.ImprintAddress = "Example GmbH, Berlin"
	_, html, _ := newMailer(t, cfg, nil).Render("en-US", mailer.PasswordChanged{})
	for _, want := range []string{`href="https://example.com/privacy"`, "Privacy Policy", `href="https://example.com/imprint"`, "Imprint", " • ", "Example GmbH, Berlin"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}

	cfg.ImprintURL = ""
	_, onlyPrivacy, _ := newMailer(t, cfg, nil).Render("en-US", mailer.PasswordChanged{})
	if strings.Contains(onlyPrivacy, " • ") || !strings.Contains(onlyPrivacy, "Privacy Policy") {
		t.Error("the separator must only join two links")
	}
}

func TestSendWithoutSMTPIsSkipped(t *testing.T) {
	var logs bytes.Buffer
	m, err := mailer.New(baseConfig(), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	sent, err := m.SendTemplate(context.Background(), "user@example.com", "en-US", mailer.PasswordChanged{})
	if sent || err != nil || m.Configured() {
		t.Fatalf("SendTemplate = %v, %v; want not sent and no error", sent, err)
	}
	if !strings.Contains(logs.String(), "SMTP is not configured, skipping email sending") {
		t.Fatalf("log = %q", logs.String())
	}
}

func TestSendThroughTransport(t *testing.T) {
	tr := &mailertest.Transport{}
	m := newMailer(t, baseConfig(), tr)
	sent, err := m.SendTemplate(context.Background(), "grace@example.com", "en-US", everyTemplate[4])
	if !sent || err != nil {
		t.Fatalf("SendTemplate = %v, %v", sent, err)
	}
	if _, err := m.Send(context.Background(), mailer.Message{To: "x@example.com", From: "Other <other@example.com>", Subject: "s", HTML: "<p>h</p>"}); err != nil {
		t.Fatal(err)
	}
	msgs := tr.Messages()
	if len(msgs) != 2 {
		t.Fatalf("%d messages", len(msgs))
	}
	if msgs[0].To != "grace@example.com" || msgs[0].From != `"Forma" <noreply@forma.yldm.ai>` || msgs[0].Subject != "You are invited to collaborate on Forma!" || !strings.Contains(msgs[0].HTML, "Hey Grace") {
		t.Fatalf("message = %+v", msgs[0])
	}
	if msgs[1].From != "Other <other@example.com>" {
		t.Fatalf("a caller's From was overridden: %q", msgs[1].From)
	}

	tr.Err = errors.New("550 relay denied")
	sent, err = m.SendTemplate(context.Background(), "grace@example.com", "en-US", everyTemplate[3])
	if sent || err == nil || !strings.Contains(err.Error(), "failed to send email: 550 relay denied") {
		t.Fatalf("SendTemplate on failure = %v, %v", sent, err)
	}
}

// locales/*.json must stay the "emails" namespace of the web catalogue; run with -update after changing apps/web/locales.
func TestCatalogueMatchesWebLocales(t *testing.T) {
	for _, locale := range mailer.Locales {
		web, err := os.ReadFile(filepath.Join(repotest.Root(t), "apps/web/locales", locale+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var catalogue struct {
			Emails map[string]string `json:"emails"`
		}
		if err := json.Unmarshal(web, &catalogue); err != nil {
			t.Fatal(err)
		}
		want, err := json.MarshalIndent(catalogue.Emails, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		path := filepath.Join("locales", locale+".json")
		if *update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(normalizeJSON(t, got), normalizeJSON(t, want)) {
			t.Errorf("%s differs from the emails namespace of apps/web/locales/%s.json; run go test ./internal/platform/mailer -run TestCatalogueMatchesWebLocales -update", path, locale)
		}
	}
}

func normalizeJSON(t *testing.T, b []byte) []byte {
	t.Helper()
	var v map[string]string
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(v)
	return out
}

// Delivers through a real SMTP server (Mailpit) and reads the message back from its API.
func TestSMTPDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	ctx := context.Background()
	container, err := testcontainers.Run(ctx, "axllent/mailpit:v1.27",
		testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/api/v1/messages").WithPort("8025/tcp").WithStartupTimeout(60*time.Second)),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start mailpit: %v", err)
	}
	smtpAddr, err := container.PortEndpoint(ctx, "1025/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	apiURL, err := container.PortEndpoint(ctx, "8025/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(smtpAddr)

	cfg := baseConfig()
	cfg.SMTPHost, cfg.SMTPPort = host, port
	cfg.SMTPAuthenticated = false
	cfg.SMTPRejectUnauthorizedTLS = true
	m, err := mailer.New(cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	sent, err := m.SendTemplate(ctx, "grace@example.com", "ja-JP", everyTemplate[4])
	if !sent || err != nil {
		t.Fatalf("SendTemplate = %v, %v", sent, err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/api/v1/messages", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var list struct {
		Messages []struct {
			Subject string `json:"Subject"`
			From    struct {
				Name    string `json:"Name"`
				Address string `json:"Address"`
			} `json:"From"`
			To []struct {
				Address string `json:"Address"`
			} `json:"To"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Messages) != 1 {
		t.Fatalf("%d messages delivered", len(list.Messages))
	}
	got := list.Messages[0]
	want, _, _ := m.Render("ja-JP", everyTemplate[4])
	if got.Subject != want || got.From.Name != "Forma" || got.From.Address != "noreply@forma.yldm.ai" || got.To[0].Address != "grace@example.com" {
		t.Fatalf("delivered %+v", got)
	}
}

// An organization's logo replaces Forma's in the link survey email; Forma's own logo URL counts as no custom logo, as in forma2's EmailTemplate.
func TestRenderOrganizationLogo(t *testing.T) {
	m := newMailer(t, baseConfig(), nil)
	_, html, err := m.Render("en-US", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1", LogoURL: "https://cdn.example.com/acme.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `src="https://cdn.example.com/acme.png"`) || strings.Contains(html, "logo-transparent.png") || strings.Contains(html, "utm_campaign=email_logo") {
		t.Fatalf("organization logo not used:\n%s", html)
	}
	_, html, err = m.Render("en-US", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1", LogoURL: "https://forma.yldm.ai/logo-transparent.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "utm_campaign=email_logo") {
		t.Fatalf("Forma's logo URL was treated as a custom logo:\n%s", html)
	}
}

// Forma's logo is drawn in a box of fixed width and height, in the aspect ratio of apps/web/public/logo-transparent.png, so no client stretches it or leaves the transparent margin of a square canvas above the card: Outlook ignores max-height and object-fit, and sizes an image only by its width and height attributes.
func TestRenderFormaLogoSize(t *testing.T) {
	t.Parallel()
	f, err := os.Open(filepath.Join(repotest.Root(t), "apps/web/public/logo-transparent.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	asset, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	m := newMailer(t, baseConfig(), nil)
	img := regexp.MustCompile(`<img alt="Logo" src="https://forma\.yldm\.ai/logo-transparent\.png"[^>]*>`)
	size := regexp.MustCompile(`width="(\d+)" height="(\d+)"`)
	for _, content := range everyTemplate {
		_, html, err := m.Render("en-US", content)
		if err != nil {
			t.Fatal(err)
		}
		tag := img.FindString(html)
		match := size.FindStringSubmatch(tag)
		if match == nil {
			t.Fatalf("%T: Forma's logo has no width and height attributes: %q", content, tag)
		}
		width, _ := strconv.Atoi(match[1])
		height, _ := strconv.Atoi(match[2])
		if height > 48 || width > 240 {
			t.Errorf("%T: logo drawn at %dx%d, want at most 240x48", content, width, height)
		}
		// The drawn box keeps the file's aspect ratio to within a pixel.
		if want := width * asset.Height / asset.Width; height < want-1 || height > want+1 {
			t.Errorf("%T: logo drawn at %dx%d, but the file is %dx%d", content, width, height, asset.Width, asset.Height)
		}
		if !strings.Contains(tag, fmt.Sprintf("width:%dpx;height:%dpx", width, height)) {
			t.Errorf("%T: the logo's CSS size does not match its attributes: %q", content, tag)
		}
	}
}

// The call to action is Forma's crimson only under Forma's logo: an email that carries an organization's logo keeps the neutral black button, so white-labelled emails never show Forma's brand colour.
func TestRenderButtonColorFollowsLogo(t *testing.T) {
	t.Parallel()
	m := newMailer(t, baseConfig(), nil)
	const crimson, neutral = "background-color:#d3264a", "background-color:#000000"
	for _, tc := range []struct {
		name    string
		content mailer.Content
		want    string
		notWant string
	}{
		{"forma logo", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1"}, crimson, neutral},
		{"forma logo url", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1", LogoURL: "https://forma.yldm.ai/logo-transparent.png"}, crimson, neutral},
		{"organization logo", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1", LogoURL: "https://cdn.example.com/acme.png"}, neutral, crimson},
		{"organization logo on response finished", mailer.ResponseFinished{SurveyName: "S", ResponsesURL: "https://x.example.com/r", LogoURL: "https://cdn.example.com/acme.png"}, neutral, crimson},
		{"account email", mailer.PasswordReset{VerifyLink: "https://x.example.com/reset", LinkValidityInMinutes: 30}, crimson, neutral},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, html, err := m.Render("en-US", tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(html, tc.want+";color:#ffffff") || strings.Contains(html, tc.notWant) {
				t.Fatalf("button colour: want %s, not %s:\n%s", tc.want, tc.notWant, html)
			}
		})
	}
}

// forma2's ResponseFinishedEmail (packages/email render.test.ts and response-finished-email.tsx): the survey in the subject, every answered element, the computed and hidden fields, the CTA counting the other responses, and the two unsubscribe links, without the sign-off of the account emails.
func TestRenderResponseFinished(t *testing.T) {
	t.Parallel()
	m := newMailer(t, baseConfig(), nil)
	subject, html, err := m.Render("en-US", responseFinished)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "A response for Customer Feedback was completed ✅" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{
		"Hey 👋", "Congrats, you received a new response to your survey! Someone just completed your survey: Customer Feedback",
		"What did you like?", "Speed\nand &lt;b&gt;price&lt;/b&gt;", "white-space:pre-wrap",
		`href="https://app.example.com/storage/ws/private/My%20Report.pdf"`, ">My Report.pdf</a>",
		`alt="cat.png" src="https://cdn.example.com/a/cat.png"`, "#1</td>", ">Price</td>", "#2</td>", ">Speed</td>", "No files",
		"Score", ">0</p>", "utm_source", "newsletter", "View 14 more responses",
		`href="https://app.example.com/workspaces/ws1/surveys/s1/responses?utm_source=email_notification&amp;utm_medium=email&amp;utm_content=view_responses_CTA"`,
		"Do not want to get these notifications?",
		`href="https://app.example.com/workspaces/ws1/settings/notifications?type=alert&amp;elementId=s1"`, "Turn off notifications for this form",
		`href="https://app.example.com/workspaces/ws1/settings/notifications?type=unsubscribedOrganizationIds&amp;elementId=o1"`, "Turn off notifications for all newly created forms",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, absent := range []string{"Skipped", "Have a great day!", "#3</td>"} {
		if strings.Contains(html, absent) {
			t.Errorf("unexpected %q", absent)
		}
	}
	single := responseFinished
	single.ResponseCount = 1
	if _, html, _ := m.Render("en-US", single); !strings.Contains(html, "View survey summary") || strings.Contains(html, "more responses") {
		t.Error("a first response must link to the summary")
	}
	if subject, _, _ := m.Render("ja-JP", responseFinished); !strings.Contains(subject, "Customer Feedback") || strings.Contains(subject, "{surveyName}") {
		t.Errorf("ja-JP subject = %q", subject)
	}
}

// The weekly summary lists each survey's numbers against the week before, a dash for a number there is nothing to compute from, and the switch that turns it off; the organization's name is escaped.
func TestRenderWeeklySummary(t *testing.T) {
	t.Parallel()
	m := newMailer(t, baseConfig(), nil)
	subject, html, err := m.Render("en-US", weeklySummary)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Weekly summary for Acme <Labs>" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{
		"Your weekly summary", "Acme &lt;Labs&gt;, 2026-10-19 to 2026-10-25 (Asia/Tokyo)", "New responses: 26", "(previous week: 18)",
		"Customer Feedback", "Production", "<strong>24</strong>", "Completion rate", "<strong>87.5%</strong>",
		"NPS", "<strong>35</strong>", "(previous week: -12.5)", "CSAT (satisfied)", "<strong>80%</strong>", "(previous week: –)",
		"Top tags", "billing (3), ui (2)",
		`href="https://app.example.com/app/workspaces/ws1/surveys/s1/summary?filter=x&amp;utm_source=email_weekly_summary"`, "View the summary for this week",
		"Exit poll", "<strong>0</strong>", "<strong>–</strong>",
		"Other surveys with responses: 3. Open Forma to see them all.",
		"You get this summary every Monday for the surveys you can see in Acme &lt;Labs&gt;.",
		`href="https://app.example.com/app/account/settings/notifications?elementId=o1&amp;type=unsubscribedWeeklySummaryOrganizationIds"`, "Turn off the weekly summary for this organization",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The second survey has no NPS or CSAT element and no tags, so those rows are left out; the email is not signed.
	if n := strings.Count(html, "CSAT (satisfied)"); n != 1 {
		t.Errorf("CSAT rows = %d, want 1", n)
	}
	for _, absent := range []string{"Have a great day!", "Acme <Labs>"} {
		if strings.Contains(html, absent) {
			t.Errorf("unexpected %q", absent)
		}
	}
	if subject, _, _ := m.Render("ja-JP", weeklySummary); subject != "Acme <Labs> の週間サマリー" {
		t.Errorf("ja-JP subject = %q", subject)
	}
}

// forma2's FollowUpEmail: the body as given, then the response data with file links withheld, the variables and the hidden fields, every text node dir="auto"; the subject is the author's.
func TestRenderFollowUp(t *testing.T) {
	t.Parallel()
	m := newMailer(t, baseConfig(), nil)
	subject, html, err := m.Render("en-US", followUp)
	if err != nil {
		t.Fatal(err)
	}
	if subject != "Thanks for {your} feedback" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{
		`<div dir="auto"><p>Thanks <b>Ada</b>!</p></div>`, "Response data", "What did you like?", "Speed",
		"Link to uploaded file is not included for data privacy reasons", "Number variable: <span dir=\"auto\">Score</span>",
		"Text variable: <span dir=\"auto\">Tier</span>", "gold", "Hidden field: utm_source", "newsletter",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, "a.pdf") || strings.Contains(html, "Have a great day!") {
		t.Error("file link or sign-off rendered")
	}
	bare := mailer.FollowUp{Subject: "Hi", Body: "<p>Hi</p>"}
	if _, html, _ := m.Render("en-US", bare); strings.Contains(html, "Response data") {
		t.Error("the response data heading needs response data")
	}
}

func TestRenderPreviewEmails(t *testing.T) {
	t.Parallel()
	m := newMailer(t, baseConfig(), nil)
	subject, html, err := m.Render("en-US", everyTemplate[11])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<meta name="color-scheme" content="only light">`, `<meta name="supported-color-schemes" content="light">`, ":root{color-scheme:only light}",
		"Preview Email Embed", "This is how the code snippet looks embedded into an email:", "<b>Did not request this?</b> Help us fight spam",
		"<td>How likely are you to recommend us?</td>", "Workspace ID: ws1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("embed preview missing %q", want)
		}
	}
	if subject != "Forma Email Survey Preview" || strings.Contains(html, "Have a great day!") {
		t.Errorf("embed preview subject %q or sign-off rendered", subject)
	}
	subject, html, err = m.Render("en-US", mailer.EmailCustomizationPreview{UserName: "Ada", LogoURL: "https://cdn.example.com/logo.png"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Hey Ada", "This is an email preview to show you which logo will be rendered in the emails.", `src="https://cdn.example.com/logo.png"`} {
		if !strings.Contains(html, want) {
			t.Errorf("customization preview missing %q", want)
		}
	}
	if subject != "Forma Email Customization Preview" || strings.Contains(html, "color-scheme") {
		t.Errorf("customization preview subject %q or light mode", subject)
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"https://app.example.com/storage/ws/private/My%20Report.pdf": "My Report.pdf",
		"https://app.example.com/":                                   "file",
		"/storage/ws/public/a%20b.png":                               "a%20b.png",
		"plain":                                                      "plain",
	} {
		if got := mailer.FileName(in); got != want {
			t.Errorf("FileName(%q) = %q, want %q", in, got, want)
		}
	}
}
