package survey

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/legacyid"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// Vectors made with forma2's algorithms on Node (lib/crypto.ts symmetricEncrypt, lib/utils/single-use-surveys.ts generateSurveySingleUseSignature) under the hex key of 32 bytes of 7.
const (
	forma2SealedCuid    = "09c05f293700f84d7187b8eace9d3785:3a8755771cba4d64bae5b49ec578e8629c7f0d23ca96d6571c:a1cddcba7e2b21700e1b49907954f933"
	forma2SealedNotCuid = "09c05f293700f84d7187b8eace9d3785:17a4723333f0625486d2:bfc1a526d9098c829e43c3252a3b04a2"
	forma2LegacySurvey  = "clsurveyaaaaaaaaaaaaaaa1"
	forma2LegacyToken   = "9e055152961ae65b7f985557e6ba3197da282c32c89ecdefef7f304ea8bc78d2"
)

func singleUseService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Config{EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyText: config.Secret(strings.Repeat("07", 32)), EncryptionKeyVersion: 1}
	cipher, err := secret.NewCipher(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{cfg: cfg, cipher: cipher}
}

func singleUseSurvey(id uuid.UUID, encrypted bool) *Survey {
	doc, _ := json.Marshal(SingleUse{Enabled: true, IsEncrypted: encrypted})
	return &Survey{ID: id, SingleUse: doc}
}

func TestCheckSingleUseLinkPlain(t *testing.T) {
	s := singleUseService(t)
	native := singleUseSurvey(uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527"), false)
	migrated := singleUseSurvey(legacyid.ID(forma2LegacySurvey), false)
	key := string(s.cfg.EncryptionKeyText)
	token := SingleUseSignature(key, native.ID.String(), "su-1")
	cases := []struct {
		name                string
		sv                  *Survey
		linkID, suID, suTok string
		want                string
	}{
		{"signed for the survey", native, native.ID.String(), "su-1", token, "su-1"},
		{"the id is trimmed before the signature is checked", native, native.ID.String(), "  su-1 ", token, "su-1"},
		{"missing token", native, native.ID.String(), "su-1", "", ""},
		{"tampered token", native, native.ID.String(), "su-1", strings.Repeat("0", 64), ""},
		{"token of another id", native, native.ID.String(), "su-2", token, ""},
		{"blank id", native, native.ID.String(), "   ", token, ""},
		{"signed for another survey", singleUseSurvey(uuid.New(), false), native.ID.String(), "su-1", token, ""},
		// A link forma2 issued was signed over the survey's forma2 id, which its path still carries.
		{"forma2 link by its forma2 id", migrated, forma2LegacySurvey, "su-legacy", forma2LegacyToken, "su-legacy"},
		{"forma2 link reached by the new id", migrated, migrated.ID.String(), "su-legacy", forma2LegacyToken, ""},
		{"a forma2 id that is not this survey's", native, forma2LegacySurvey, "su-legacy", forma2LegacyToken, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CheckSingleUseLink(tc.sv, tc.linkID, tc.suID, tc.suTok); got != tc.want {
				t.Fatalf("CheckSingleUseLink = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckSingleUseLinkEncrypted(t *testing.T) {
	s := singleUseService(t)
	sv := singleUseSurvey(uuid.MustParse("01a16211-77de-7d59-ada1-7e2ea6ddb527"), true)
	seal := func(surveyID uuid.UUID, id string) string {
		sealed, err := s.cipher.Seal([]byte(id), singleUseContext(surveyID))
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(sealed)
	}
	parts := strings.Split(forma2SealedCuid, ":")
	cases := []struct {
		name string
		suID string
		want string
	}{
		{"sealed by forma2", forma2SealedCuid, "clsingleuseaaaaaaaaaaaaa1"},
		{"sealed by forma2, not a cuid", forma2SealedNotCuid, ""},
		{"forma2 tag tampered", parts[0] + ":" + parts[1] + ":" + strings.Repeat("0", 32), ""},
		// The unauthenticated CBC form is never accepted from a URL.
		{"forma2 CBC form", parts[0] + ":" + parts[1], ""},
		{"sealed here for this survey", seal(sv.ID, "abcdefghijklmnopqrstuvwx"), "abcdefghijklmnopqrstuvwx"},
		{"sealed here for another survey", seal(uuid.New(), "abcdefghijklmnopqrstuvwx"), ""},
		{"sealed here, not a cuid", seal(sv.ID, "Not-A-Cuid"), ""},
		{"plain id on an encrypted survey", "abcdefghijklmnopqrstuvwx", ""},
		{"garbage", "%%%", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CheckSingleUseLink(sv, sv.ID.String(), tc.suID, ""); got != tc.want {
				t.Fatalf("CheckSingleUseLink = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsCuid(t *testing.T) {
	for id, want := range map[string]bool{"ab": true, "a": false, strings.Repeat("a", 32): true, strings.Repeat("a", 33): false, "ABC": false, "a-b": false, "": false} {
		if isCuid(id) != want {
			t.Errorf("isCuid(%q) = %v", id, !want)
		}
	}
}
