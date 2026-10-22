package mailer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/server/internal/platform/mailer/mailertest"
)

var update = flag.Bool("update", false, "rewrite locales/*.json from web/locales")

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func baseConfig() config.Config {
	return config.Config{MailFrom: "noreply@forma.ylam.ai", MailFromName: "Forma"}
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
}

var placeholder = regexp.MustCompile(`\{[A-Za-z]+\}`)

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
				if strings.Contains(html, "emails.") || placeholder.MatchString(html) || placeholder.MatchString(subject) {
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
	if msgs[0].To != "grace@example.com" || msgs[0].From != `"Forma" <noreply@forma.ylam.ai>` || msgs[0].Subject != "You are invited to collaborate on Forma!" || !strings.Contains(msgs[0].HTML, "Hey Grace") {
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

// locales/*.json must stay the "emails" namespace of the web catalogue; run with -update after changing web/locales.
func TestCatalogueMatchesWebLocales(t *testing.T) {
	for _, locale := range mailer.Locales {
		web, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "web", "locales", locale+".json"))
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
			t.Errorf("%s differs from the emails namespace of web/locales/%s.json; run go test ./internal/platform/mailer -run TestCatalogueMatchesWebLocales -update", path, locale)
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
	if got.Subject != want || got.From.Name != "Forma" || got.From.Address != "noreply@forma.ylam.ai" || got.To[0].Address != "grace@example.com" {
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
	_, html, err = m.Render("en-US", mailer.LinkSurvey{SurveyName: "S", SurveyLink: "https://x.example.com/s/1", LogoURL: "https://app.forma.ylam.ai/logo-transparent.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "utm_campaign=email_logo") {
		t.Fatalf("Forma's logo URL was treated as a custom logo:\n%s", html)
	}
}
