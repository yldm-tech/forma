package auth

import (
	"strings"
	"testing"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
)

func rulesOf(t *testing.T, err error) map[string]string {
	t.Helper()
	got := map[string]string{}
	if err == nil {
		return got
	}
	e, ok := apperr.As(err)
	if !ok || e.Code != apperr.CodeValidation {
		t.Fatalf("err = %v, want a validation error", err)
	}
	for _, f := range e.Details["fields"].([]apperr.FieldError) {
		got[f.Field] = f.Rule
	}
	return got
}

// ZUserName: trimmed, at least one character, letters, marks, space and ' , . & ( ) digits -.
func TestCheckName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		rule string
	}{
		{"  Ada Lovelace ", "Ada Lovelace", ""},
		{"J. Smith", "J. Smith", ""},
		{"Smith & Co (Ltd), O'Brien-2", "Smith & Co (Ltd), O'Brien-2", ""},
		{"Zoë Åström", "Zoë Åström", ""},
		{"山田 太郎", "山田 太郎", ""},
		{"   ", "", "min"},
		{"", "", "min"},
		{"Ada 🚀", "Ada 🚀", "name_format"},
		{"<script>", "<script>", "name_format"},
		{"Ada\nLovelace", "Ada\nLovelace", "name_format"},
		{"١٢٣", "١٢٣", "name_format"}, // \d is ASCII-only in a JavaScript pattern without the u flag
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var errs fieldErrors
			got := checkName(&errs, tc.in)
			if got != tc.want || rulesOf(t, errs.err())["name"] != tc.rule {
				t.Fatalf("checkName(%q) = %q, rules %v; want %q, rule %q", tc.in, got, rulesOf(t, errs.err()), tc.want, tc.rule)
			}
		})
	}
}

// ZUserEmail is zod 4's email pattern with a 255-character limit.
func TestCheckEmail(t *testing.T) {
	cases := []struct {
		in   string
		rule string
	}{
		{"ada@example.com", ""},
		{"Ada.Lovelace+forma@Sub.Example.CO", ""},
		{"o'brien@example.ie", ""},
		{"", "required"},
		{".ada@example.com", "email"},
		{"ada..lovelace@example.com", "email"},
		{"ada.@example.com", "email"},
		{"ada@example", "email"},
		{"ada@example.c", "email"},
		{"ada@-example.com", "email"},
		{" ada@example.com", "email"},
		{"ada@exa mple.com", "email"},
		{"ada@example.com.", "email"},
		{strings.Repeat("a", 244) + "@example.com", "max"},
		{strings.Repeat("a", 243) + "@example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			var errs fieldErrors
			checkEmail(&errs, tc.in)
			if got := rulesOf(t, errs.err())["email"]; got != tc.rule {
				t.Fatalf("checkEmail(%q) rule %q, want %q", tc.in, got, tc.rule)
			}
		})
	}
}

// ZUserPassword: 8 to 128 UTF-16 units, an uppercase letter and a digit, and no line terminator.
func TestCheckPassword(t *testing.T) {
	cases := []struct {
		name string
		in   string
		rule string
	}{
		{"valid", "Password1", ""},
		{"exactly 8", "Abcdefg1", ""},
		{"too short", "Abcdef1", "min"},
		{"exactly 128", "A1" + strings.Repeat("x", 126), ""},
		{"too long", "A1" + strings.Repeat("x", 127), "max"},
		{"emoji count as two units", "A1" + strings.Repeat("😀", 63), ""},
		{"emoji over the limit", "A1" + strings.Repeat("😀", 64), "max"},
		{"no uppercase", "password1", "password_composition"},
		{"no digit", "Password", "password_composition"},
		{"non-ASCII uppercase only", "Ölpassword1", "password_composition"},
		{"line feed", "Password1\nmore", "password_composition"},
		{"carriage return", "Password1\r", "password_composition"},
		{"line separator", "Password1 ", "password_composition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errs fieldErrors
			checkPassword(&errs, "password", tc.in)
			if got := rulesOf(t, errs.err())["password"]; got != tc.rule {
				t.Fatalf("checkPassword(%q) rule %q, want %q", tc.in, got, tc.rule)
			}
		})
	}
}

func TestCheckLocale(t *testing.T) {
	for in, rule := range map[string]string{"": "", "ja-JP": "", "zh-Hant-TW": "", "en": "oneof", "xx-XX": "oneof"} {
		var errs fieldErrors
		checkLocale(&errs, "userLocale", in)
		if got := rulesOf(t, errs.err())["userLocale"]; got != rule {
			t.Errorf("checkLocale(%q) rule %q, want %q", in, got, rule)
		}
	}
}

// forma2's signup-email-domain.test.ts.
func TestIsBlockedEmailDomain(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"ada@gmail.com", true},
		{"Ada@GMAIL.com", true},
		{"ada@gmail.com.", true},
		{"ada@gmail.com...", true},
		{"  ada@outlook.com  ", true},
		{"ada@proton.me", true},
		{"ada@privaterelay.appleid.com", true},
		{"test@mailinator.com", true},
		{"test@guerrillamail.com", true},
		{"test@yopmail.com", true},
		{"test@10minutemail.com", true},
		{"test@sharklasers.com", true},
		{"test@33mail.com", true},
		{"Test@Mailinator.COM.", true},
		{"alice@acme-corp.com", false},
		{"bob@forma.yldm.ai", false},
		{"carol@stripe.com", false},
		{"dave@my-company.co.uk", false},
		{"a@b@gmail.com", true},
		{"ada@sub.gmail.com", false},
		{"ada@example.com", false},
		{"ada@gmail.company", false},
		{"@gmail.com", false},
		{"ada@", false},
		{"gmail.com", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isBlockedEmailDomain(tc.in); got != tc.want {
			t.Errorf("isBlockedEmailDomain(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The vendored list is disposable-email-domains 1.0.62's index.json, whole: one lowercase domain per line, sorted, no duplicates.
func TestDisposableEmailDomainList(t *testing.T) {
	var domains []string
	for _, line := range strings.Split(strings.TrimSpace(disposableEmailDomainList), "\n") {
		if !strings.HasPrefix(line, "#") {
			domains = append(domains, line)
		}
	}
	if len(domains) != 121570 || len(disposableEmailDomains()) != len(domains) {
		t.Fatalf("%d lines, %d distinct domains, want 121570", len(domains), len(disposableEmailDomains()))
	}
	for i, d := range domains {
		if d != strings.ToLower(strings.TrimSpace(d)) || d == "" || (i > 0 && domains[i-1] >= d) {
			t.Fatalf("line %d %q is not a lowercase domain in sorted order", i, d)
		}
	}
}

// forma2's createAuditIdentifier: prefix, underscore, the first 32 hex characters of SHA-256 of the lowercased value.
func TestAuditIdentifier(t *testing.T) {
	a := auditIdentifier("Ada@Example.com", "email")
	if a != auditIdentifier("ada@example.com", "email") {
		t.Fatal("identifier depends on case")
	}
	if !strings.HasPrefix(a, "email_") || len(a) != len("email_")+32 || strings.Contains(a, "ada") {
		t.Fatalf("identifier %q", a)
	}
	if auditIdentifier("ada@example.com", "ratelimit") == a {
		t.Fatal("prefix ignored")
	}
}

func TestEmailDomain(t *testing.T) {
	if got := emailDomain("ada@example.com"); got != "example.com" {
		t.Fatalf("emailDomain = %q", got)
	}
	if got := emailDomain("nonsense"); got != "" {
		t.Fatalf("emailDomain = %q", got)
	}
}
