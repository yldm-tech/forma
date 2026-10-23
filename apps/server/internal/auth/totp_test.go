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
		ok, err := checkTOTP(rfcKey, tc.code, time.Unix(tc.unix, 0), totpWindow{})
		if err != nil || !ok {
			t.Errorf("code %s at %d rejected: %v", tc.code, tc.unix, err)
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
			got, err := checkTOTP(rfcKey, at(tc.steps), now, tc.window)
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	for _, malformed := range []string{"", "12345", "1234567", "abcdef", at(0) + " "} {
		if ok, _ := checkTOTP(rfcKey, malformed, now, signInWindow); ok {
			t.Errorf("malformed code %q accepted", malformed)
		}
	}
}

// The codes Better Auth 1.7's createOTP produced for a secret it generated, keyed with the secret's UTF-8 bytes (vectors from @better-auth/utils 0.4.2 hotp()).
func TestCheckTOTPMatchesBetterAuth(t *testing.T) {
	key := []byte("OfBqWQvwnfdc8grnNxXfX4rYQ76-cBb-")
	for counter, code := range map[int64]string{0: "697308", 1: "967714", 59_000_000: "730991"} {
		ok, err := checkTOTP(key, code, time.Unix(counter*totpPeriod, 0), totpWindow{})
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

// The ciphertexts are a forma2 TwoFactor row as Better Auth 1.7.0 wrote it (see internal/platform/secret/legacy_test.go): the secret from before a rotation, under the legacy secret, and the backup codes under rotated version 2.
func TestImportBetterAuthTwoFactor(t *testing.T) {
	secrets := secret.BetterAuthSecrets{
		Secret:   "forma2-better-auth-secret-for-tests-0123456789",
		Versions: map[int]string{1: "rotated-secret-version-one-abcdefghijklmnop", 2: "rotated-secret-version-two-abcdefghijklmnop"},
	}
	m, err := ImportBetterAuthTwoFactor(secrets,
		"35f9fee0a496c7e61d9743f66d878bf7d470eef781f566dfb1cb0f4fae03ccb7fce6282c3e4c1efb238c24682997956db22d5975ebe4f858fdea1a9512c88aa6d204595a7672f1f3",
		"$ba$2$0cc577907db558d7adf731c265b5b3373425d4928f9afa916d5c5615fcb6e2e0094ba1fe9dc4b9274dceee212641ebecf7c9a897aa2d23caba3f99278322b06cbff1c0ca4c",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(m.Key) != "OfBqWQvwnfdc8grnNxXfX4rYQ76-cBb-" || !slices.Equal(m.BackupCodes, []string{"AbCdE12345", "zz9YxQwErT"}) {
		t.Fatalf("imported %q, %v", m.Key, m.BackupCodes)
	}
	// The user's authenticator keeps working: Better Auth's own code for this secret verifies against the imported key.
	if ok, _ := checkTOTP(m.Key, "697308", time.Unix(0, 0), signInWindow); !ok {
		t.Fatal("Better Auth's code does not verify with the imported key")
	}
	// The codes were saved hyphenated; they still match as entered.
	if matchBackupCode(m.BackupCodes, "zz9Yx-QwErT") != 1 {
		t.Fatal("hyphenated Better Auth code does not match")
	}
	if _, err := ImportBetterAuthTwoFactor(secret.BetterAuthSecrets{Secret: "wrong"}, "00", "00"); err == nil {
		t.Fatal("imported under the wrong secret")
	}
}

// The ciphertexts are forma2's legacy User.twoFactorSecret and User.backupCodes as lib/crypto.ts wrote them under a 32-character ENCRYPTION_KEY; the secret is RFC 6238's key in otplib's base32.
func TestImportForma2TwoFactor(t *testing.T) {
	key := []byte("0123456789abcdefghijklmnopqrstuv")
	secretCT := "3585b5280990daff2823b852df5e8756:5a92c9ef5d095bc285470346f7a5a28eb4533b4bd314f8bc64c3baf96f442b59:b5831f915a49742752c2ae0df2bd1cb4"
	codesCT := "7ebae65205b8d0090a96fc7f37a2e238:536ccb249682420c0864783824b8816ac50632196297b34e25f8e63c8514c7b6:26a0c62d95cf5b70cf5d4fd84de65e51"
	m, err := ImportForma2TwoFactor(key, secretCT, codesCT)
	if err != nil {
		t.Fatal(err)
	}
	// The null slot was a code spent at a legacy sign-in.
	if !bytes.Equal(m.Key, rfcKey) || !slices.Equal(m.BackupCodes, []string{"0123456789", "abcdef0123"}) {
		t.Fatalf("imported %q, %v", m.Key, m.BackupCodes)
	}
	if ok, _ := checkTOTP(m.Key, "287082", time.Unix(59, 0), settingsWindow); !ok {
		t.Fatal("the legacy secret does not verify after import")
	}
	noCodes, err := ImportForma2TwoFactor(key, secretCT, "")
	if err != nil || noCodes.BackupCodes == nil || len(noCodes.BackupCodes) != 0 {
		t.Fatalf("without backup codes: %v, %v", noCodes.BackupCodes, err)
	}
	if _, err := ImportForma2TwoFactor([]byte("ffffffffffffffffffffffffffffffff"), secretCT, codesCT); err == nil {
		t.Fatal("imported under the wrong key")
	}
}
