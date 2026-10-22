package linksurvey

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// Tokens shaped exactly like jsonwebtoken's ({"alg":"HS256","typ":"JWT"}, base64url, HMAC-SHA256), made on Node with forma2's claims, secrets and its AES-GCM cipher under the hex key of 32 bytes of 7.
const (
	forma2Survey     = "clsurveyaaaaaaaaaaaaaaa1"
	forma2Contact    = "clcontactaaaaaaaaaaaaaa1"
	forma2PinToken   = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdXJ2ZXlJZCI6ImNsc3VydmV5YWFhYWFhYWFhYWFhYWFhMSIsInB1cnBvc2UiOiJsaW5rX3N1cnZleV9waW4iLCJpYXQiOjE3NjAwMDAwMDAsImV4cCI6NDEwMjQ0NDgwMH0.lVV9DUJRwGm4R2-4Kp89uTPKL5pCl17UEHWywEJp1hk"
	forma2EmailToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6IjcwNjBlMjI0OWNmNzljZjkyZGRjMDNhZWJjYzMyNjExOjY5ZmU2YjA3OGZiMGRhMDlhZWE5MGU1NTU0MzY3OTBiMjc6NmEzZDM1OTYzMDYxN2VjOTQ1ZDAxYzI4YjYxNjdmMTIiLCJzdXJ2ZXlJZCI6ImNsc3VydmV5YWFhYWFhYWFhYWFhYWFhMSIsInB1cnBvc2UiOiJsaW5rX3N1cnZleV9lbWFpbF92ZXJpZmljYXRpb24iLCJpYXQiOjE3NjAwMDAwMDAsImV4cCI6NDEwMjQ0NDgwMH0.aTVAgx8hfbVgcFiNfLM4vm0oJ9SzO9YGqsWF7grJeXs"
	// Signed with NEXTAUTH_SECRET followed by the survey's forma2 id, without a survey claim, and with the address in plain text: forma2's oldest form.
	forma2LegacyEmailToken    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6InVzZXIxQGV4YW1wbGUuY29tIiwiaWF0IjoxNzYwMDAwMDAwLCJleHAiOjQxMDI0NDQ4MDB9.sLk_3Bo-AWtBDMgO2BK1cHPA0hWJ3N6eFx3uTcVygeY"
	forma2ContactToken        = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb250YWN0SWQiOiIwZTlkZGI2Y2VhOTUzYmM3MGUyNmY1NDNjMDQzMDc0MzpjYWM3NWU0NjQ1YzdlOGE0YzAzMTUzNjUzZjhiOWQ2NGJjZmI0N2U1MWFhZjVhNGQ6MjRjM2RhMGZhYTQwYmFhN2YxY2Q1MjVmYmY3YjIxYmUiLCJzdXJ2ZXlJZCI6IjE2YzI1MWNiZmJmN2ZhZTg5MjZhYmIwZmVhNDE5N2I3OjQ5MDFiNmVlNzM1NzI4YWZhM2RlMjg3NWEyMzBiZDBmZjY2NzlhZDFiZmE4ZGNlODo4NjVkNGYwNDA1NzY3ODU3NDE1MzhiNzI5MDE3MmRhZCIsImlhdCI6MTc2MDAwMDAwMH0.j5A5V4Q996uQL7c9pN7zTqc5dCNhFNnBiDr7X3mye5o"
	forma2ExpiredContactToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb250YWN0SWQiOiJjMDJiYWNjOTRjNGFlOWM5Y2QyMmI2MzIzNDZhYjllYjowZGZmZWJhMjE1N2U5MWEyZDdmZDM1ODNjMDE3NDQ2ZTAxMDQzYzZjYjVmMDYzNGQ6MjQ5ZGE1OGJmNjg1NWQ2OTlmYTFhODBlYmNlYzY1NWQiLCJzdXJ2ZXlJZCI6IjM5ZGZmZmU2ODI3YmYwZmQzYTE5NzRhZjAyZjBkNWE3OjJmZWNiYTNlZWYxOGFlMmRiMmVkNGU2YzE3MTY5NDgwYmEyOTE3NWRkMzk0MWQwNToyMDY0YjQyNmMwNzIzNGZiYjQ3NDQ0MDdiMTQ2ZjNmZiIsImlhdCI6MTAwMCwiZXhwIjoyMDAwfQ.zzAqAH6CCjg70w5g6oDP28X7yR_8NFemAjA_WGUk_LU"

	testNextAuthSecret   = "next-secret-0123456789abcdefghijklmnop"
	testBetterAuthSecret = "better-secret-0123456789abcdefghijklmnop"
)

func tokenConfig() config.Config {
	return config.Config{
		EncryptionKey:     bytes.Repeat([]byte{7}, 32),
		EncryptionKeyText: config.Secret(strings.Repeat("07", 32)),
		NextAuthSecret:    testNextAuthSecret,
		BetterAuthSecret:  testBetterAuthSecret,
	}
}

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func tamper(token string) string {
	parts := strings.Split(token, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	payload = bytes.Replace(payload, []byte(`"iat":`), []byte(`"iat":1`), 1)
	return parts[0] + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + parts[2]
}

// forma2's pin-token.test.ts.
func TestPinToken(t *testing.T) {
	cfg := tokenConfig()
	surveyID := uuid.New()
	token, err := NewPinToken(cfg, surveyID, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(token, ".")) != 3 {
		t.Fatalf("token %q is not a JWS", token)
	}
	other, _ := sign(cfg.LinkSurveyPinSecret(), pinClaims{SurveyID: surveyID.String(), Purpose: "something_else", Claims: jwt.Claims{Expiry: jwt.NewNumericDate(testNow.Add(time.Hour))}})
	cases := []struct {
		name   string
		token  string
		survey uuid.UUID
		now    time.Time
		want   bool
	}{
		{"valid token for the survey", token, surveyID, testNow, true},
		{"wrong survey", token, uuid.New(), testNow, false},
		{"tampered", tamper(token), surveyID, testNow, false},
		{"empty", "", surveyID, testNow, false},
		{"another purpose", other, surveyID, testNow, false},
		{"expired", token, surveyID, testNow.Add(pinTokenTTL + time.Second), false},
		{"forma2 token for the migrated survey", forma2PinToken, legacyid.ID(forma2Survey), testNow, true},
		{"forma2 token for another survey", forma2PinToken, surveyID, testNow, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyPinToken(cfg, tc.token, tc.survey, tc.now); got != tc.want {
				t.Fatalf("VerifyPinToken = %v, want %v", got, tc.want)
			}
		})
	}
}

// BETTER_AUTH_SECRET wins over NEXTAUTH_SECRET, which is the fallback.
func TestPinTokenSecretPrecedence(t *testing.T) {
	cfg := tokenConfig()
	surveyID := uuid.New()
	token, _ := NewPinToken(cfg, surveyID, testNow)
	nextOnly := cfg
	nextOnly.BetterAuthSecret = ""
	if VerifyPinToken(nextOnly, token, surveyID, testNow) {
		t.Fatal("a BETTER_AUTH_SECRET token verified under NEXTAUTH_SECRET")
	}
	fallback, _ := NewPinToken(nextOnly, surveyID, testNow)
	viaNext, _ := sign(testNextAuthSecret, pinClaims{SurveyID: surveyID.String(), Purpose: pinTokenPurpose, Claims: jwt.Claims{Expiry: jwt.NewNumericDate(testNow.Add(time.Hour))}})
	if !VerifyPinToken(nextOnly, fallback, surveyID, testNow) || !VerifyPinToken(nextOnly, viaNext, surveyID, testNow) {
		t.Fatal("the NEXTAUTH_SECRET fallback does not sign PIN tokens")
	}
}

// forma2's jwt.test.ts (verifyTokenForLinkSurvey).
func TestEmailToken(t *testing.T) {
	cfg := tokenConfig()
	surveyID := uuid.New()
	migrated := legacyid.ID(forma2Survey)
	token, err := newEmailToken(cfg, surveyID, "user1@example.com", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, "dXNlcjFAZXhhbXBsZS5jb2") {
		t.Fatal("the address travels unencrypted")
	}
	claims := func(c emailClaims) string {
		s, err := sign(testNextAuthSecret, c)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	future := jwt.Claims{Expiry: jwt.NewNumericDate(testNow.Add(time.Hour))}
	id, inviteish, purpose := surveyID.String(), "email_verification", emailTokenPurpose
	encrypted, _ := secret.EncryptForma2("other@example.com", cfg.EncryptionKey)
	cases := []struct {
		name         string
		token        string
		survey       uuid.UUID
		linkSurveyID string
		now          time.Time
		want         string
	}{
		{"valid", token, surveyID, surveyID.String(), testNow, "user1@example.com"},
		{"another survey", token, uuid.New(), "", testNow, ""},
		{"expired", token, surveyID, surveyID.String(), testNow.Add(emailTokenTTL + time.Second), ""},
		{"tampered", tamper(token), surveyID, surveyID.String(), testNow, ""},
		{"garbage", "not-a-token", surveyID, surveyID.String(), testNow, ""},
		{"no survey claim under the plain secret", claims(emailClaims{Email: "user1@example.com", Claims: future}), surveyID, surveyID.String(), testNow, ""},
		{"another flow's purpose", claims(emailClaims{Email: encrypted, SurveyID: &id, Purpose: &inviteish, Claims: future}), surveyID, surveyID.String(), testNow, ""},
		{"no email", claims(emailClaims{SurveyID: &id, Purpose: &purpose, Claims: future}), surveyID, surveyID.String(), testNow, ""},
		{"plain-text address", claims(emailClaims{Email: "plain@example.com", SurveyID: &id, Claims: future}), surveyID, surveyID.String(), testNow, "plain@example.com"},
		{"forma2 token", forma2EmailToken, migrated, forma2Survey, testNow, "user1@example.com"},
		{"forma2 token reached by the new id", forma2EmailToken, migrated, migrated.String(), testNow, "user1@example.com"},
		{"forma2 legacy token by the forma2 id", forma2LegacyEmailToken, migrated, forma2Survey, testNow, "user1@example.com"},
		// The legacy key is the secret followed by the id the link was sent with; the new id cannot reproduce it.
		{"forma2 legacy token by the new id", forma2LegacyEmailToken, migrated, migrated.String(), testNow, ""},
		{"forma2 legacy token for another survey", forma2LegacyEmailToken, surveyID, forma2Survey, testNow, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyEmailToken(cfg, tc.token, tc.survey, tc.linkSurveyID, tc.now); got != tc.want {
				t.Fatalf("VerifyEmailToken = %q, want %q", got, tc.want)
			}
		})
	}
}

// Without NEXTAUTH_SECRET the token is signed with the derived secret, and forma2's secret-plus-id fallback does not exist.
func TestEmailTokenWithoutNextAuthSecret(t *testing.T) {
	cfg := tokenConfig()
	cfg.NextAuthSecret = ""
	surveyID := uuid.New()
	token, err := newEmailToken(cfg, surveyID, "user1@example.com", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := VerifyEmailToken(cfg, token, surveyID, surveyID.String(), testNow); got != "user1@example.com" {
		t.Fatalf("VerifyEmailToken = %q", got)
	}
	if got := VerifyEmailToken(cfg, forma2LegacyEmailToken, legacyid.ID(forma2Survey), forma2Survey, testNow); got != "" {
		t.Fatalf("a legacy token verified without NEXTAUTH_SECRET: %q", got)
	}
}

// forma2's contact-survey-link.test.ts (verifyContactSurveyToken).
func TestContactToken(t *testing.T) {
	cfg := tokenConfig()
	contactID, surveyID, err := verifyContactToken(cfg, forma2ContactToken, testNow)
	if err != nil || contactID != forma2Contact || surveyID != forma2Survey {
		t.Fatalf("verifyContactToken = %q, %q, %v", contactID, surveyID, err)
	}
	if _, _, err := verifyContactToken(cfg, forma2ExpiredContactToken, testNow); !errors.Is(err, errContactTokenExpired) {
		t.Fatalf("expired token: %v", err)
	}
	plainIDs, _ := sign(string(cfg.EncryptionKeyText), contactClaims{ContactID: forma2Contact, SurveyID: forma2Survey})
	encContact, _ := secret.EncryptForma2(forma2Contact, cfg.EncryptionKey)
	missingSurvey, _ := sign(string(cfg.EncryptionKeyText), contactClaims{ContactID: encContact})
	otherKey := cfg
	otherKey.EncryptionKeyText = config.Secret(strings.Repeat("08", 32))
	for name, token := range map[string]string{
		"tampered":              tamper(forma2ContactToken),
		"garbage":               "abc",
		"ids not encrypted":     plainIDs,
		"survey id missing":     missingSurvey,
		"signed by another key": forma2ContactToken,
	} {
		t.Run(name, func(t *testing.T) {
			c := cfg
			if name == "signed by another key" {
				c = otherKey
			}
			if _, _, err := verifyContactToken(c, token, testNow); !errors.Is(err, errContactTokenInvalid) {
				t.Fatalf("err = %v, want invalid", err)
			}
		})
	}
}
