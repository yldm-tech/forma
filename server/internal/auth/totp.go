package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"

	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// TOTP and backup codes as forma2 issued them (apps/web/modules/two-factor-auth/lib/two-factor-auth.ts, and the Better Auth twoFactor plugin options in modules/auth/lib/auth.ts): RFC 6238 with HMAC-SHA1, six digits and a 30-second step under the issuer "Forma"; a 20-byte key, shown as 32 base32 characters; ten backup codes of ten lowercase hex characters each.
const (
	totpIssuer      = "Forma"
	totpKeySize     = 20
	totpPeriod      = 30
	backupCodeCount = 10
	backupCodeBytes = 5
	qrCodeSize      = 200
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpWindow is how many steps before and after the current one a code may come from.
type totpWindow struct {
	past, future int
}

var (
	// signInWindow is Better Auth's verifyTOTP default, which forma2's sign-in ran on: one step either side.
	signInWindow = totpWindow{past: 1, future: 1}
	// settingsWindow is otplib's default [1, 0], which forma2's enable and disable checks ran on: the current step and the one before.
	settingsWindow = totpWindow{past: 1, future: 0}
)

// checkTOTP reports whether code is the key's code for a step inside the window around now. Every step is computed and compared in constant time, so the response time does not tell which step matched.
func checkTOTP(key []byte, code string, now time.Time, w totpWindow) (bool, error) {
	encoded := base32NoPadding.EncodeToString(key)
	current := now.Unix() / totpPeriod
	matched := 0
	for step := current - int64(w.past); step <= current+int64(w.future); step++ {
		want, err := hotp.GenerateCodeCustom(encoded, uint64(step), hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}) //nolint:gosec // steps count from the Unix epoch and the clock is never before it
		if err != nil {
			return false, fmt.Errorf("compute totp: %w", err)
		}
		matched |= subtle.ConstantTimeCompare([]byte(want), []byte(code))
	}
	return matched == 1, nil
}

// newBackupCodes is forma2's crypto.randomBytes(5).toString("hex"), ten times.
func newBackupCodes() ([]string, error) {
	codes := make([]string, backupCodeCount)
	for i := range codes {
		var b [backupCodeBytes]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, fmt.Errorf("generate backup code: %w", err)
		}
		codes[i] = hex.EncodeToString(b[:])
	}
	return codes, nil
}

// normalizeBackupCode drops the hyphens a code is displayed with (xxxxx-xxxxx); codes are stored without them, so a code is accepted with or without its hyphen, as forma2's disable check did. Case matters: codes migrated from Better Auth mix upper and lower case.
func normalizeBackupCode(code string) string {
	return strings.ReplaceAll(code, "-", "")
}

// matchBackupCode returns the index of input among codes, or -1. Every stored code is compared in constant time.
func matchBackupCode(codes []string, input string) int {
	in := []byte(normalizeBackupCode(input))
	found := -1
	for i, c := range codes {
		if subtle.ConstantTimeCompare([]byte(c), in) == 1 && found < 0 {
			found = i
		}
	}
	return found
}

// TOTPMaterial is the plaintext of a user_totp row: the HMAC key authenticator apps share with the server, and the backup codes not yet used, without hyphens.
type TOTPMaterial struct {
	Key         []byte
	BackupCodes []string
}

// totpSealContext binds a sealed column to its row, so a ciphertext copied to another user or column does not open.
func totpSealContext(column string, userID uuid.UUID) []byte {
	return []byte("user_totp." + column + ":" + userID.String())
}

// SealTOTP returns what a user_totp row stores for m: secret_ciphertext and backup_codes_ciphertext. The data migration writes the rows it carries over from forma2 with it.
func SealTOTP(c *secret.Cipher, userID uuid.UUID, m TOTPMaterial) (secretCiphertext, backupCodesCiphertext []byte, err error) {
	if secretCiphertext, err = c.Seal(m.Key, totpSealContext("secret", userID)); err != nil {
		return nil, nil, err
	}
	if backupCodesCiphertext, err = sealBackupCodes(c, userID, m.BackupCodes); err != nil {
		return nil, nil, err
	}
	return secretCiphertext, backupCodesCiphertext, nil
}

func sealBackupCodes(c *secret.Cipher, userID uuid.UUID, codes []string) ([]byte, error) {
	if codes == nil {
		codes = []string{}
	}
	raw, err := json.Marshal(codes)
	if err != nil {
		return nil, err
	}
	return c.Seal(raw, totpSealContext("backup_codes", userID))
}

func openTOTPKey(c *secret.Cipher, userID uuid.UUID, sealed []byte) ([]byte, error) {
	key, err := c.Open(sealed, totpSealContext("secret", userID))
	if err != nil {
		return nil, fmt.Errorf("open totp secret: %w", err)
	}
	return key, nil
}

func openBackupCodes(c *secret.Cipher, userID uuid.UUID, sealed []byte) ([]string, error) {
	raw, err := c.Open(sealed, totpSealContext("backup_codes", userID))
	if err != nil {
		return nil, fmt.Errorf("open backup codes: %w", err)
	}
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil {
		return nil, fmt.Errorf("decode backup codes: %w", err)
	}
	return codes, nil
}

// totpEnrolment is what a user is shown to set up an authenticator app.
type totpEnrolment struct {
	Secret      string
	KeyURI      string
	DataURI     string
	BackupCodes []string
}

// enrolmentLinks returns the otpauth:// URI for key and the same URI as a QR code PNG data URI.
func enrolmentLinks(key []byte, account string) (keyURI, dataURI string, err error) {
	k, err := totp.Generate(totp.GenerateOpts{
		Issuer: totpIssuer, AccountName: account, Period: totpPeriod, Secret: key, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", fmt.Errorf("build otpauth uri: %w", err)
	}
	img, err := k.Image(qrCodeSize, qrCodeSize)
	if err != nil {
		return "", "", fmt.Errorf("render qr code: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", fmt.Errorf("encode qr code: %w", err)
	}
	return k.URL(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ImportBetterAuthTwoFactor decrypts a forma2 TwoFactor row, Better Auth's secret and backupCodes columns, into the material of a user_totp row. Better Auth keyed the HMAC with the UTF-8 bytes of the decrypted secret string: a secret it generated is 32 random characters, and a secret forma2 re-encoded from its legacy columns is the original key bytes read as Latin-1, all below 0x80 because otplib masked them to ASCII, so its UTF-8 bytes are the original key. Either way the bytes are the key the user's authenticator app holds.
func ImportBetterAuthTwoFactor(secrets secret.BetterAuthSecrets, secretCiphertext, backupCodesCiphertext string) (TOTPMaterial, error) {
	s, err := secret.DecryptBetterAuth(secrets, secretCiphertext)
	if err != nil {
		return TOTPMaterial{}, fmt.Errorf("decrypt two-factor secret: %w", err)
	}
	raw, err := secret.DecryptBetterAuth(secrets, backupCodesCiphertext)
	if err != nil {
		return TOTPMaterial{}, fmt.Errorf("decrypt backup codes: %w", err)
	}
	var codes []string
	if err := json.Unmarshal([]byte(raw), &codes); err != nil {
		return TOTPMaterial{}, fmt.Errorf("decode backup codes: %w", err)
	}
	for i, c := range codes {
		codes[i] = normalizeBackupCode(c)
	}
	return TOTPMaterial{Key: []byte(s), BackupCodes: codes}, nil
}

// ImportForma2TwoFactor decrypts forma2's legacy User.twoFactorSecret and User.backupCodes (lib/crypto.ts under ENCRYPTION_KEY, legacy CBC included, as forma2 read them at rest) for a user who has no TwoFactor row, the case forma2 healed at sign-in. backupCodesCiphertext is empty when the column was null. The secret is otplib's base32; a used backup code was a null slot, and is dropped.
func ImportForma2TwoFactor(encryptionKey []byte, secretCiphertext, backupCodesCiphertext string) (TOTPMaterial, error) {
	s, err := secret.DecryptForma2(secretCiphertext, encryptionKey, true)
	if err != nil {
		return TOTPMaterial{}, fmt.Errorf("decrypt two-factor secret: %w", err)
	}
	key, err := base32NoPadding.DecodeString(strings.TrimRight(strings.ToUpper(s), "="))
	if err != nil {
		return TOTPMaterial{}, fmt.Errorf("decode two-factor secret: %w", err)
	}
	m := TOTPMaterial{Key: key, BackupCodes: []string{}}
	if backupCodesCiphertext == "" {
		return m, nil
	}
	raw, err := secret.DecryptForma2(backupCodesCiphertext, encryptionKey, true)
	if err != nil {
		return TOTPMaterial{}, fmt.Errorf("decrypt backup codes: %w", err)
	}
	var slots []*string
	if err := json.Unmarshal([]byte(raw), &slots); err != nil {
		return TOTPMaterial{}, fmt.Errorf("decode backup codes: %w", err)
	}
	for _, c := range slots {
		if c != nil {
			m.BackupCodes = append(m.BackupCodes, normalizeBackupCode(*c))
		}
	}
	return m, nil
}
