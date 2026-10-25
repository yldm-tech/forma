package survey

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// gcmSealedCuid is a cuid sealed as an "iv:ciphertext:tag" link value under the hex key of 32 bytes of 7, a format this server never issues single-use ids in.
const gcmSealedCuid = "09c05f293700f84d7187b8eace9d3785:3a8755771cba4d64bae5b49ec578e8629c7f0d23ca96d6571c:a1cddcba7e2b21700e1b49907954f933"

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
	key := string(s.cfg.EncryptionKeyText)
	token := SingleUseSignature(key, native.ID.String(), "su-1")
	cases := []struct {
		name        string
		sv          *Survey
		suID, suTok string
		want        string
	}{
		{"signed for the survey", native, "su-1", token, "su-1"},
		{"the id is trimmed before the signature is checked", native, "  su-1 ", token, "su-1"},
		{"missing token", native, "su-1", "", ""},
		{"tampered token", native, "su-1", strings.Repeat("0", 64), ""},
		{"token of another id", native, "su-2", token, ""},
		{"blank id", native, "   ", token, ""},
		{"signed for another survey", singleUseSurvey(uuid.New(), false), "su-1", token, ""},
		{"signed over a cuid2", native, "su-1", SingleUseSignature(key, "clsurveyaaaaaaaaaaaaaaa1", "su-1"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CheckSingleUseLink(tc.sv, tc.suID, tc.suTok); got != tc.want {
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
	cases := []struct {
		name string
		suID string
		want string
	}{
		{"an iv:ciphertext:tag link value", gcmSealedCuid, ""},
		{"sealed here for this survey", seal(sv.ID, "abcdefghijklmnopqrstuvwx"), "abcdefghijklmnopqrstuvwx"},
		{"sealed here for another survey", seal(uuid.New(), "abcdefghijklmnopqrstuvwx"), ""},
		{"sealed here, not a cuid", seal(sv.ID, "Not-A-Cuid"), ""},
		{"plain id on an encrypted survey", "abcdefghijklmnopqrstuvwx", ""},
		{"garbage", "%%%", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.CheckSingleUseLink(sv, tc.suID, ""); got != tc.want {
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
