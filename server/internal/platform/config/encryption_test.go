package config

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const testHexKey = "a0ff678f48df0da911d2287b6d928688e948dccb2554cdd16f61050fe3d90a86"

func TestEncryptionKeyFormats(t *testing.T) {
	hexBytes := []byte{0xa0, 0xff, 0x67, 0x8f}
	cases := []struct {
		name  string
		value string
		// prefix is the start of the decoded key; empty means the value is refused.
		prefix []byte
	}{
		{"64 hex characters", testHexKey, hexBytes},
		{"upper-case hex", strings.ToUpper(testHexKey), hexBytes},
		{"32 characters are Latin-1 bytes", "0123456789abcdefghijklmnopqrstuv", []byte("0123")},
		// Node's Buffer.from(s, "latin1") keeps the low byte of each UTF-16 code unit.
		{"32 characters beyond Latin-1", "é€" + strings.Repeat("x", 30), []byte{0xe9, 0xac, 'x'}},
		{"31 characters", strings.Repeat("x", 31), nil},
		{"33 characters", strings.Repeat("x", 33), nil},
		{"64 characters that are not hex", strings.Repeat("g", 64), nil},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var k EncryptionKey
			err := k.UnmarshalText([]byte(tc.value))
			if tc.prefix == nil {
				if err == nil {
					t.Fatalf("accepted %q", tc.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(k) != 32 || !bytes.HasPrefix(k, tc.prefix) {
				t.Fatalf("key %x, want 32 bytes starting %x", []byte(k), tc.prefix)
			}
		})
	}
}

func TestLoadEncryptionKeys(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.EncryptionKey) != 32 || cfg.EncryptionKeyVersion != 1 || len(cfg.EncryptionPreviousKeys) != 0 {
		t.Fatalf("key %d bytes, version %d, %d previous; want 32, 1, 0", len(cfg.EncryptionKey), cfg.EncryptionKeyVersion, len(cfg.EncryptionPreviousKeys))
	}

	t.Setenv("ENCRYPTION_KEY_VERSION", "3")
	t.Setenv("ENCRYPTION_PREVIOUS_KEYS", "1:0123456789abcdefghijklmnopqrstuv, 2:"+testHexKey)
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EncryptionKeyVersion != 3 || string(cfg.EncryptionPreviousKeys[1]) != "0123456789abcdefghijklmnopqrstuv" || len(cfg.EncryptionPreviousKeys[2]) != 32 {
		t.Fatalf("version %d, previous %d keys", cfg.EncryptionKeyVersion, len(cfg.EncryptionPreviousKeys))
	}
	// The keys never reach a log line or an error message through formatting.
	if s := fmt.Sprintf("%v %+v %s", cfg.EncryptionKey, cfg.EncryptionPreviousKeys, cfg.EncryptionKey); strings.Contains(s, "0123456789") || strings.Contains(s, "a0ff") {
		t.Fatalf("formatted keys leak: %s", s)
	}
}

func TestLoadRejectsInvalidEncryptionKeys(t *testing.T) {
	cases := map[string]map[string]string{
		"malformed key":            {"ENCRYPTION_KEY": "too-short"},
		"version zero":             {"ENCRYPTION_KEY_VERSION": "0"},
		"version too high":         {"ENCRYPTION_KEY_VERSION": "256"},
		"previous without version": {"ENCRYPTION_PREVIOUS_KEYS": testHexKey},
		"previous version zero":    {"ENCRYPTION_PREVIOUS_KEYS": "0:" + testHexKey},
		"previous malformed key":   {"ENCRYPTION_PREVIOUS_KEYS": "2:short"},
		"previous duplicate":       {"ENCRYPTION_PREVIOUS_KEYS": "2:" + testHexKey + ",2:" + testHexKey},
		"previous is current":      {"ENCRYPTION_PREVIOUS_KEYS": "1:" + testHexKey},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			for k, v := range vars {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %v", vars)
			}
		})
	}
}

// forma2 keyed its signatures with ENCRYPTION_KEY's text, so the text is kept next to the decoded key, and it never prints either.
func TestLoadKeepsTheEncryptionKeyText(t *testing.T) {
	setRequired(t)
	t.Setenv("ENCRYPTION_KEY", strings.ToUpper(testHexKey))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.EncryptionKeyText) != strings.ToUpper(testHexKey) || len(cfg.EncryptionKey) != 32 {
		t.Fatalf("text %d chars, key %d bytes", len(cfg.EncryptionKeyText), len(cfg.EncryptionKey))
	}
	if s := fmt.Sprintf("%v %s %+v", cfg.EncryptionKeyText, cfg.EncryptionKeyText, cfg); strings.Contains(strings.ToLower(s), "a0ff678f") {
		t.Fatalf("formatted key text leaks: %s", s)
	}
}

func TestLinkSurveyTokenSecrets(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	derived := cfg.LinkSurveyEmailSecret()
	if len(derived) != 64 || derived != cfg.LinkSurveyPinSecret() || strings.Contains(derived, testHexKey) {
		t.Fatalf("derived secrets %q / %q", derived, cfg.LinkSurveyPinSecret())
	}

	// forma2: email tokens use NEXTAUTH_SECRET; PIN tokens prefer BETTER_AUTH_SECRET and fall back to NEXTAUTH_SECRET.
	t.Setenv("NEXTAUTH_SECRET", "next-secret")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.LinkSurveyEmailSecret() != "next-secret" || cfg.LinkSurveyPinSecret() != "next-secret" {
		t.Fatalf("with NEXTAUTH_SECRET: %q / %q", cfg.LinkSurveyEmailSecret(), cfg.LinkSurveyPinSecret())
	}
	t.Setenv("BETTER_AUTH_SECRET", "better-secret")
	if cfg, err = Load(); err != nil {
		t.Fatal(err)
	}
	if cfg.LinkSurveyEmailSecret() != "next-secret" || cfg.LinkSurveyPinSecret() != "better-secret" {
		t.Fatalf("with both: %q / %q", cfg.LinkSurveyEmailSecret(), cfg.LinkSurveyPinSecret())
	}
	if s := fmt.Sprintf("%+v", cfg); strings.Contains(s, "next-secret") || strings.Contains(s, "better-secret") {
		t.Fatalf("formatted config leaks a secret: %s", s)
	}
}
