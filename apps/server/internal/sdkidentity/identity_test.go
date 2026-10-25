package sdkidentity

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

// The survey keys equal js-core's hashString of "<userId>:<surveyId>" (packages/js-core/src/lib/common/utils.test.ts asserts the same values), so the SDK finds the surveys the state names. The last case has characters outside ASCII and outside the BMP, which JavaScript hashes as UTF-16 code units.
func TestSurveyKeyMatchesTheSDK(t *testing.T) {
	for _, tc := range []struct{ userID, surveyID, want string }{
		{"user-123", "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b", "22d4f1ac"},
		{"", "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b", "1ef50fe6"},
		{"ユーザー😀", "clsurveyaaaaaaaaaaaaaaaa1", "7cb9b764"},
	} {
		if got := SurveyKey(tc.userID, tc.surveyID); got != tc.want {
			t.Errorf("SurveyKey(%q, %q) = %s, want %s", tc.userID, tc.surveyID, got, tc.want)
		}
	}
}

// A userHash is the HMAC-SHA256 of the userId keyed with the secret's text, in hex of either case; anything else does not verify, and nothing verifies without a secret.
func TestVerified(t *testing.T) {
	s := &Settings{Secret: "s3cret"}
	// echo -n user-1 | openssl dgst -sha256 -hmac s3cret
	const want = "8e6d6d439f86ee1bfe2a4d9ba3081c530a9dda2161c0ac814a747cb06c11f100"
	if got := UserHash("s3cret", "user-1"); got != want {
		t.Fatalf("UserHash = %s, want %s", got, want)
	}
	for _, tc := range []struct {
		name, userID, hash string
		want               bool
	}{
		{"lowercase", "user-1", want, true},
		{"uppercase", "user-1", strings.ToUpper(want), true},
		{"another user", "user-2", want, false},
		{"missing", "user-1", "", false},
		{"not hex", "user-1", "not-a-hash", false},
		{"truncated", "user-1", want[:32], false},
	} {
		if got := s.Verified(tc.userID, tc.hash); got != tc.want {
			t.Errorf("%s: Verified = %v, want %v", tc.name, got, tc.want)
		}
	}
	if (&Settings{}).Verified("user-1", want) {
		t.Fatal("a workspace without a secret verified a userHash")
	}
}

func TestContactToken(t *testing.T) {
	cfg := config.Config{EncryptionKey: bytes.Repeat([]byte{7}, 32)}
	ws, other, contact := uuid.New(), uuid.New(), uuid.New()

	token, err := ContactToken(cfg, ws, contact)
	if err != nil {
		t.Fatal(err)
	}
	if !IsContactToken(token) || strings.Contains(token, contact.String()) || strings.Contains(token, strings.ReplaceAll(contact.String(), "-", "")) {
		t.Fatalf("token %q", token)
	}
	again, _ := ContactToken(cfg, ws, contact)
	if again != token {
		t.Fatal("a contact's token changed between two identifies")
	}
	if got, ok := ParseContactToken(cfg, ws, token); !ok || got != contact {
		t.Fatalf("ParseContactToken = %s %v, want %s", got, ok, contact)
	}
	if _, ok := ParseContactToken(cfg, other, token); ok {
		t.Fatal("a token opened in another workspace")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "ct1."))
	raw[len(raw)/2] ^= 1
	if _, ok := ParseContactToken(cfg, ws, "ct1."+base64.RawURLEncoding.EncodeToString(raw)); ok {
		t.Fatal("a tampered token opened")
	}
	for _, bad := range []string{"", "ct1.", "ct1.!!!", contact.String()} {
		if _, ok := ParseContactToken(cfg, ws, bad); ok {
			t.Fatalf("%q opened", bad)
		}
	}

	// After a key rotation, a token issued under the retired key still opens while the key is kept.
	rotated := config.Config{EncryptionKey: bytes.Repeat([]byte{9}, 32), EncryptionPreviousKeys: config.EncryptionKeyring{1: bytes.Repeat([]byte{7}, 32)}}
	if got, ok := ParseContactToken(rotated, ws, token); !ok || got != contact {
		t.Fatal("a token under a retired key did not open")
	}
	if _, ok := ParseContactToken(config.Config{EncryptionKey: bytes.Repeat([]byte{9}, 32)}, ws, token); ok {
		t.Fatal("a token opened under a key it was not sealed with")
	}
}
