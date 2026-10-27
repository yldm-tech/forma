package auth

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// rfcKey and the codes below are RFC 6238's SHA-1 test vectors, truncated to six digits.
var rfcKey = []byte("12345678901234567890")

func TestCheckTOTPMatchesRFC6238(t *testing.T) {
	cases := []struct {
		unix int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, tc := range cases {
		step, ok, err := checkTOTP(rfcKey, tc.code, time.Unix(tc.unix, 0), totpWindow{})
		if err != nil || !ok || step != tc.unix/totpPeriod {
			t.Errorf("code %s at %d: step %d, %v, %v", tc.code, tc.unix, step, ok, err)
		}
	}
}

// forma2's totp.test.ts pins the settings window ([1, 0]: the previous step but no future one); Better Auth's sign-in accepted one step either side.
func TestCheckTOTPWindows(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	at := func(steps int) string {
		return totpCodeAt(t, rfcKey, now.Add(time.Duration(steps)*totpPeriod*time.Second))
	}
	cases := []struct {
		name   string
		window totpWindow
		steps  int
		want   bool
	}{
		{"settings: current step", settingsWindow, 0, true},
		{"settings: previous step", settingsWindow, -1, true},
		{"settings: two steps back", settingsWindow, -2, false},
		{"settings: next step", settingsWindow, 1, false},
		{"sign-in: current step", signInWindow, 0, true},
		{"sign-in: previous step", signInWindow, -1, true},
		{"sign-in: next step", signInWindow, 1, true},
		{"sign-in: two steps ahead", signInWindow, 2, false},
		{"sign-in: two steps back", signInWindow, -2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step, got, err := checkTOTP(rfcKey, at(tc.steps), now, tc.window)
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
			// The matched step is reported, so a replay of it can be refused.
			if want := now.Unix()/totpPeriod + int64(tc.steps); got && step != want {
				t.Fatalf("step %d, want %d", step, want)
			}
		})
	}
	for _, malformed := range []string{"", "12345", "1234567", "abcdef", at(0) + " "} {
		if _, ok, _ := checkTOTP(rfcKey, malformed, now, signInWindow); ok {
			t.Errorf("malformed code %q accepted", malformed)
		}
	}
}

// The codes Better Auth 1.7's createOTP produced for a secret it generated, keyed with the secret's UTF-8 bytes (vectors from @better-auth/utils 0.4.2 hotp()).
func TestCheckTOTPMatchesBetterAuth(t *testing.T) {
	key := []byte("OfBqWQvwnfdc8grnNxXfX4rYQ76-cBb-")
	for counter, code := range map[int64]string{0: "697308", 1: "967714", 59_000_000: "730991"} {
		_, ok, err := checkTOTP(key, code, time.Unix(counter*totpPeriod, 0), totpWindow{})
		if err != nil || !ok {
			t.Errorf("Better Auth code %s at step %d rejected: %v", code, counter, err)
		}
	}
}

func TestNewBackupCodes(t *testing.T) {
	codes, err := newBackupCodes()
	if err != nil {
		t.Fatal(err)
	}
	hexCode := regexp.MustCompile(`^[0-9a-f]{10}$`)
	if len(codes) != 10 {
		t.Fatalf("%d codes, want 10", len(codes))
	}
	for _, c := range codes {
		if !hexCode.MatchString(c) {
			t.Fatalf("code %q is not ten lowercase hex characters", c)
		}
	}
	sorted := slices.Clone(codes)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != 10 {
		t.Fatalf("duplicate codes in %v", codes)
	}
}

func TestMatchBackupCode(t *testing.T) {
	codes := []string{"0123456789", "AbCdE12345"}
	cases := []struct {
		in   string
		want int
	}{
		{"0123456789", 0},
		{"01234-56789", 0},
		{"AbCdE-12345", 1},
		{"abcde-12345", -1},
		{"0123456788", -1},
		{"", -1},
		{"0123456789 ", -1},
	}
	for _, tc := range cases {
		if got := matchBackupCode(codes, tc.in); got != tc.want {
			t.Errorf("matchBackupCode(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestEnrolmentLinks(t *testing.T) {
	keyURI, dataURI, err := enrolmentLinks(rfcKey, "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(keyURI)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/Forma:ada@example.com" || q.Get("issuer") != "Forma" ||
		q.Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" || q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("key uri %s", keyURI)
	}
	raw, ok := strings.CutPrefix(dataURI, "data:image/png;base64,")
	if !ok {
		t.Fatalf("data uri %.40s", dataURI)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != qrCodeSize || img.Bounds().Dy() != qrCodeSize {
		t.Fatalf("qr code %v, want %dx%d", img.Bounds(), qrCodeSize, qrCodeSize)
	}
}

func testCipher(t *testing.T) *secret.Cipher {
	t.Helper()
	c, err := secret.NewCipher(config.Config{EncryptionKey: bytes.Repeat([]byte{7}, 32), EncryptionKeyVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSealTOTPIsBoundToTheRow(t *testing.T) {
	c := testCipher(t)
	ada, bob := uuid.New(), uuid.New()
	sealedKey, sealedCodes, err := SealTOTP(c, ada, TOTPMaterial{Key: rfcKey, BackupCodes: []string{"0123456789"}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedKey, rfcKey) || bytes.Contains(sealedCodes, []byte("0123456789")) {
		t.Fatal("plaintext visible in the sealed columns")
	}
	key, err := openTOTPKey(c, ada, sealedKey)
	if err != nil || !bytes.Equal(key, rfcKey) {
		t.Fatalf("key %q, %v", key, err)
	}
	codes, err := openBackupCodes(c, ada, sealedCodes)
	if err != nil || !slices.Equal(codes, []string{"0123456789"}) {
		t.Fatalf("codes %v, %v", codes, err)
	}
	if _, err := openTOTPKey(c, bob, sealedKey); err == nil {
		t.Fatal("another user's row opened the secret")
	}
	if _, err := openBackupCodes(c, ada, sealedKey); err == nil {
		t.Fatal("the secret column opened as backup codes")
	}
	// An empty list stays a list, so a user who spent every code still has a valid row.
	_, none, _ := SealTOTP(c, ada, TOTPMaterial{Key: rfcKey})
	if codes, err := openBackupCodes(c, ada, none); err != nil || codes == nil || len(codes) != 0 {
		t.Fatalf("empty codes: %v, %v", codes, err)
	}
}
