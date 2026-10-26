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

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Personal link tokens shaped exactly like jsonwebtoken's ({"alg":"HS256","typ":"JWT"}, base64url, HMAC-SHA256), made on Node with jsonwebtoken and the AES-GCM link value cipher under the hex key of 32 bytes of 7, so verification is checked against an independent implementation.
const (
	nodeSurvey              = "clsurveyaaaaaaaaaaaaaaa1"
	nodeContact             = "clcontactaaaaaaaaaaaaaa1"
	nodeContactToken        = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb250YWN0SWQiOiIwZTlkZGI2Y2VhOTUzYmM3MGUyNmY1NDNjMDQzMDc0MzpjYWM3NWU0NjQ1YzdlOGE0YzAzMTUzNjUzZjhiOWQ2NGJjZmI0N2U1MWFhZjVhNGQ6MjRjM2RhMGZhYTQwYmFhN2YxY2Q1MjVmYmY3YjIxYmUiLCJzdXJ2ZXlJZCI6IjE2YzI1MWNiZmJmN2ZhZTg5MjZhYmIwZmVhNDE5N2I3OjQ5MDFiNmVlNzM1NzI4YWZhM2RlMjg3NWEyMzBiZDBmZjY2NzlhZDFiZmE4ZGNlODo4NjVkNGYwNDA1NzY3ODU3NDE1MzhiNzI5MDE3MmRhZCIsImlhdCI6MTc2MDAwMDAwMH0.j5A5V4Q996uQL7c9pN7zTqc5dCNhFNnBiDr7X3mye5o"
	nodeExpiredContactToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJjb250YWN0SWQiOiJjMDJiYWNjOTRjNGFlOWM5Y2QyMmI2MzIzNDZhYjllYjowZGZmZWJhMjE1N2U5MWEyZDdmZDM1ODNjMDE3NDQ2ZTAxMDQzYzZjYjVmMDYzNGQ6MjQ5ZGE1OGJmNjg1NWQ2OTlmYTFhODBlYmNlYzY1NWQiLCJzdXJ2ZXlJZCI6IjM5ZGZmZmU2ODI3YmYwZmQzYTE5NzRhZjAyZjBkNWE3OjJmZWNiYTNlZWYxOGFlMmRiMmVkNGU2YzE3MTY5NDgwYmEyOTE3NWRkMzk0MWQwNToyMDY0YjQyNmMwNzIzNGZiYjQ3NDQ0MDdiMTQ2ZjNmZiIsImlhdCI6MTAwMCwiZXhwIjoyMDAwfQ.zzAqAH6CCjg70w5g6oDP28X7yR_8NFemAjA_WGUk_LU"

	testTokenSecret    = "next-secret-0123456789abcdefghijklmnop"
	testPinTokenSecret = "better-secret-0123456789abcdefghijklmnop"
)

func tokenConfig() config.Config {
	return config.Config{
		EncryptionKey:            bytes.Repeat([]byte{7}, 32),
		EncryptionKeyText:        config.Secret(strings.Repeat("07", 32)),
		LinkSurveyTokenSecret:    testTokenSecret,
		LinkSurveyPinTokenSecret: testPinTokenSecret,
	}
}

var testNow = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

// sign2 signs claims with secret, failing the test when it cannot.
func sign2(t *testing.T, secret string, claims any) string {
	t.Helper()
	s, err := sign(secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tamper(token string) string {
	parts := strings.Split(token, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	payload = bytes.Replace(payload, []byte(`"iat":`), []byte(`"iat":1`), 1)
	return parts[0] + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + parts[2]
}

// A PIN token proves the PIN for its survey only, for an hour.
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyPinToken(cfg, tc.token, tc.survey, tc.now); got != tc.want {
				t.Fatalf("VerifyPinToken = %v, want %v", got, tc.want)
			}
		})
	}
}

// LINK_SURVEY_PIN_TOKEN_SECRET wins over LINK_SURVEY_TOKEN_SECRET, which is the fallback.
func TestPinTokenSecretPrecedence(t *testing.T) {
	cfg := tokenConfig()
	surveyID := uuid.New()
	token, _ := NewPinToken(cfg, surveyID, testNow)
	nextOnly := cfg
	nextOnly.LinkSurveyPinTokenSecret = ""
	if VerifyPinToken(nextOnly, token, surveyID, testNow) {
		t.Fatal("a LINK_SURVEY_PIN_TOKEN_SECRET token verified under LINK_SURVEY_TOKEN_SECRET")
	}
	fallback, _ := NewPinToken(nextOnly, surveyID, testNow)
	viaNext, _ := sign(testTokenSecret, pinClaims{SurveyID: surveyID.String(), Purpose: pinTokenPurpose, Claims: jwt.Claims{Expiry: jwt.NewNumericDate(testNow.Add(time.Hour))}})
	if !VerifyPinToken(nextOnly, fallback, surveyID, testNow) || !VerifyPinToken(nextOnly, viaNext, surveyID, testNow) {
		t.Fatal("the LINK_SURVEY_TOKEN_SECRET fallback does not sign PIN tokens")
	}
}

// An email token proves its address for its survey only, for seven days, and only with the address encrypted.
func TestEmailToken(t *testing.T) {
	cfg := tokenConfig()
	surveyID := uuid.New()
	token, err := newEmailToken(cfg, surveyID, "user1@example.com", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, "dXNlcjFAZXhhbXBsZS5jb2") {
		t.Fatal("the address travels unencrypted")
	}
	claims := func(c emailClaims) string {
		s, err := sign(testTokenSecret, c)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	future := jwt.Claims{Expiry: jwt.NewNumericDate(testNow.Add(time.Hour))}
	id, inviteish, purpose := surveyID.String(), "email_verification", emailTokenPurpose
	encrypted, _ := secret.EncryptLinkValue("other@example.com", cfg.EncryptionKey)
	cases := []struct {
		name   string
		token  string
		survey uuid.UUID
		now    time.Time
		want   string
	}{
		{"valid", token, surveyID, testNow, "user1@example.com"},
		{"another survey", token, uuid.New(), testNow, ""},
		{"expired", token, surveyID, testNow.Add(emailTokenTTL + time.Second), ""},
		{"tampered", tamper(token), surveyID, testNow, ""},
		{"garbage", "not-a-token", surveyID, testNow, ""},
		{"no survey claim", claims(emailClaims{Email: encrypted, Claims: future}), surveyID, testNow, ""},
		{"signed with the secret followed by the survey id", sign2(t, testTokenSecret+surveyID.String(), emailClaims{Email: encrypted, Claims: future}), surveyID, testNow, ""},
		{"another flow's purpose", claims(emailClaims{Email: encrypted, SurveyID: &id, Purpose: &inviteish, Claims: future}), surveyID, testNow, ""},
		{"no email", claims(emailClaims{SurveyID: &id, Purpose: &purpose, Claims: future}), surveyID, testNow, ""},
		{"plain-text address", claims(emailClaims{Email: "plain@example.com", SurveyID: &id, Claims: future}), surveyID, testNow, ""},
		{"encrypted address of another token", claims(emailClaims{Email: encrypted, SurveyID: &id, Claims: future}), surveyID, testNow, "other@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyEmailToken(cfg, tc.token, tc.survey, tc.now); got != tc.want {
				t.Fatalf("VerifyEmailToken = %q, want %q", got, tc.want)
			}
		})
	}
}

// Without LINK_SURVEY_TOKEN_SECRET the token is signed with the derived secret.
func TestEmailTokenWithoutTokenSecret(t *testing.T) {
	cfg := tokenConfig()
	cfg.LinkSurveyTokenSecret = ""
	surveyID := uuid.New()
	token, err := newEmailToken(cfg, surveyID, "user1@example.com", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := VerifyEmailToken(cfg, token, surveyID, testNow); got != "user1@example.com" {
		t.Fatalf("VerifyEmailToken = %q", got)
	}
}

// verifyContactSurveyToken: a personal link token made by jsonwebtoken verifies, and returns its ids as they were issued.
func TestContactToken(t *testing.T) {
	cfg := tokenConfig()
	contactID, surveyID, err := verifyContactToken(cfg, nodeContactToken, testNow)
	if err != nil || contactID != nodeContact || surveyID != nodeSurvey {
		t.Fatalf("verifyContactToken = %q, %q, %v", contactID, surveyID, err)
	}
	if _, _, err := verifyContactToken(cfg, nodeExpiredContactToken, testNow); !errors.Is(err, errContactTokenExpired) {
		t.Fatalf("expired token: %v", err)
	}
	plainIDs, _ := sign(string(cfg.EncryptionKeyText), contactClaims{ContactID: nodeContact, SurveyID: nodeSurvey})
	encContact, _ := secret.EncryptLinkValue(nodeContact, cfg.EncryptionKey)
	missingSurvey, _ := sign(string(cfg.EncryptionKeyText), contactClaims{ContactID: encContact})
	otherKey := cfg
	otherKey.EncryptionKeyText = config.Secret(strings.Repeat("08", 32))
	for name, token := range map[string]string{
		"tampered":              tamper(nodeContactToken),
		"garbage":               "abc",
		"ids not encrypted":     plainIDs,
		"survey id missing":     missingSurvey,
		"signed by another key": nodeContactToken,
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

// A personal link the management API issues opens the survey page, and expires after the days it was given.
func TestNewContactToken(t *testing.T) {
	cfg := tokenConfig()
	contact, survey := uuid.New(), uuid.New()
	token, err := NewContactToken(cfg, contact, survey, testNow, 0)
	if err != nil {
		t.Fatal(err)
	}
	gotContact, gotSurvey, err := verifyContactToken(cfg, token, testNow.Add(1000*24*time.Hour))
	if err != nil || gotContact != contact.String() || gotSurvey != survey.String() {
		t.Fatalf("verifyContactToken = %q, %q, %v", gotContact, gotSurvey, err)
	}
	expiring, err := NewContactToken(cfg, contact, survey, testNow, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyContactToken(cfg, expiring, testNow.Add(7*24*time.Hour-time.Second)); err != nil {
		t.Fatalf("a link within its days: %v", err)
	}
	if _, _, err := verifyContactToken(cfg, expiring, testNow.Add(7*24*time.Hour+time.Second)); !errors.Is(err, errContactTokenExpired) {
		t.Fatalf("a link after its days: %v", err)
	}
	parsed, err := jwt.ParseSigned(expiring, hs256)
	if err != nil {
		t.Fatal(err)
	}
	if h := parsed.Headers[0]; h.Algorithm != "HS256" || h.ExtraHeaders["typ"] != "JWT" {
		t.Errorf("header = %+v", h)
	}
}
