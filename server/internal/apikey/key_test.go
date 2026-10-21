package apikey

import (
	"net/http"
	"regexp"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

func TestNewKeyHasForma2sFormatAndHashes(t *testing.T) {
	k, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^fbk_[A-Za-z0-9_-]{43}$`).MatchString(k.Key) {
		t.Fatalf("key %q is not fbk_ followed by 32 bytes of unpadded base64url", k.Key)
	}
	s := k.Key[len(Prefix):]
	if k.LookupHash != sha256Hex(s) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(k.LookupHash) {
		t.Fatalf("lookup hash %q is not the lowercase hex SHA-256 of the secret without its prefix", k.LookupHash)
	}
	if cost, err := bcrypt.Cost([]byte(k.HashedKey)); err != nil || cost != 12 {
		t.Fatalf("hashed key cost = %d, %v; want bcrypt cost 12", cost, err)
	}
	if ok, err := secret.CheckPassword(k.HashedKey, s); err != nil || !ok {
		t.Fatalf("hashed key does not verify the secret: %v %v", ok, err)
	}
	if ok, _ := secret.CheckPassword(k.HashedKey, k.Key); ok {
		t.Fatal("hashed key verifies the prefixed key; it must hash the secret alone")
	}
	other, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	if other.Key == k.Key || other.LookupHash == k.LookupHash {
		t.Fatal("two keys are equal")
	}
}

func TestParseV2(t *testing.T) {
	cases := []struct {
		key    string
		secret string
		ok     bool
	}{
		{"fbk_abc", "abc", true},
		{"fbk_a-b_c-D9", "a-b_c-D9", true},
		{"fbk_fbk_x", "fbk_x", true},
		{"", "", false},
		{"fbk_", "", false},
		{"not_fbk_secret", "", false},
		{"FBK_abc", "", false},
		{"fbk_abc+def", "", false},
		{"fbk_abc/def", "", false},
		{"fbk_abc=", "", false},
		{"fbk_ab cd", "", false},
		{"fbk_abc!", "", false},
		{"fbk_abc@", "", false},
		{"fbk_abc#", "", false},
		{"fbk_abc$", "", false},
		{"fbk_é", "", false},
	}
	for _, tc := range cases {
		s, ok := parseV2(tc.key)
		if s != tc.secret || ok != tc.ok {
			t.Errorf("parseV2(%q) = %q, %v; want %q, %v", tc.key, s, ok, tc.secret, tc.ok)
		}
	}
}

func TestSHA256HexIsLowercaseHex(t *testing.T) {
	if got := sha256Hex("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sha256Hex(abc) = %s", got)
	}
}

func TestLegacyMatch(t *testing.T) {
	digest := sha256Hex("legacy")
	if !legacyMatch(digest, digest) {
		t.Fatal("a legacy row with the digest does not match")
	}
	hashed, err := secret.HashPassword("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacyMatch(hashed, digest) || legacyMatch("", digest) || legacyMatch(digest[:63], digest) {
		t.Fatal("a non-legacy row matched")
	}
}

func TestKeyFromHeaders(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    string
		bearer  string
	}{
		{"x-api-key takes precedence over Authorization", map[string]string{"X-Api-Key": "fbk_from_header", "Authorization": "Bearer fbk_from_bearer"}, "fbk_from_header", "fbk_from_bearer"},
		{"a Bearer API key", map[string]string{"Authorization": "Bearer fbk_from_bearer"}, "fbk_from_bearer", "fbk_from_bearer"},
		{"the scheme in any case", map[string]string{"Authorization": "bEaReR fbk_secret"}, "fbk_secret", "fbk_secret"},
		{"whitespace around the header and the token", map[string]string{"Authorization": "  Bearer   fbk_secret  "}, "fbk_secret", "fbk_secret"},
		{"a JWT-shaped Bearer token is not an API key", map[string]string{"Authorization": "Bearer header.payload.signature"}, "", "header.payload.signature"},
		{"an opaque Bearer token is not an API key", map[string]string{"Authorization": "Bearer opaque_service_token"}, "", "opaque_service_token"},
		{"a legacy key works only through x-api-key", map[string]string{"Authorization": "Bearer 0123456789abcdef"}, "", "0123456789abcdef"},
		{"x-api-key in any format, trimmed", map[string]string{"X-Api-Key": "  0123456789abcdef "}, "0123456789abcdef", ""},
		{"a blank x-api-key falls through to Authorization", map[string]string{"X-Api-Key": "   ", "Authorization": "Bearer fbk_secret"}, "fbk_secret", "fbk_secret"},
		{"an empty Bearer token", map[string]string{"Authorization": "Bearer   "}, "", ""},
		{"another scheme", map[string]string{"Authorization": "Basic fbk_secret"}, "", ""},
		{"a scheme without a space", map[string]string{"Authorization": "Bearerfbk_secret"}, "", ""},
		{"nothing", map[string]string{}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			if got := KeyFromHeaders(h); got != tc.want {
				t.Errorf("KeyFromHeaders = %q, want %q", got, tc.want)
			}
			if got := BearerToken(h); got != tc.bearer {
				t.Errorf("BearerToken = %q, want %q", got, tc.bearer)
			}
		})
	}
}

func TestStoredAccessGrantsOnlyExactTrue(t *testing.T) {
	cases := []struct {
		raw  string
		want AccessFlags
	}{
		{`{"accessControl":{"read":true,"write":false}}`, AccessFlags{Read: true}},
		{`{"accessControl":{"read":false,"write":true}}`, AccessFlags{Write: true}},
		{`{"accessControl":{"read":true,"write":true}}`, AccessFlags{Read: true, Write: true}},
		{`{}`, AccessFlags{}},
		{`{"accessControl":{"read":"true","write":1}}`, AccessFlags{}},
		{`{"accessControl":true}`, AccessFlags{}},
		{`[]`, AccessFlags{}},
		{`not json`, AccessFlags{}},
	}
	for _, tc := range cases {
		if got := storedAccess([]byte(tc.raw)).AccessControl; got != tc.want {
			t.Errorf("storedAccess(%s) = %+v, want %+v", tc.raw, got, tc.want)
		}
	}
}
