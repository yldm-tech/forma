package secret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// The decryptors below read values forma2 stored, so the data migration can carry them over. Nothing in the running server writes these formats.

// forma2GCMIVSize is the 16-byte IV forma2's lib/crypto.ts used with AES-GCM (the AES block size, not GCM's usual 12).
const forma2GCMIVSize = 16

// DecryptForma2 opens a value forma2's lib/crypto.ts symmetricEncrypt wrote under ENCRYPTION_KEY: AES-256-GCM as hex "iv:ciphertext:tag". The two-part "iv:ciphertext" form is the pre-GCM AES-256-CBC format; like forma2, it is refused unless allowLegacyCBC is set, which is only safe for values read from forma2's own database, because CBC carries no authentication tag. A GCM value that fails its tag is never retried as CBC.
func DecryptForma2(payload string, key []byte, allowLegacyCBC bool) (string, error) {
	parts := strings.Split(payload, ":")
	if len(parts) == 2 {
		if !allowLegacyCBC {
			return "", errors.New("unsupported encrypted payload format")
		}
		return decryptForma2CBC(parts[0], parts[1], key)
	}
	if len(parts) != 3 {
		return "", errors.New("malformed forma2 ciphertext")
	}
	iv, err1 := hex.DecodeString(parts[0])
	ct, err2 := hex.DecodeString(parts[1])
	tag, err3 := hex.DecodeString(parts[2])
	if err := errors.Join(err1, err2, err3); err != nil {
		return "", fmt.Errorf("malformed forma2 ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("forma2 key: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, forma2GCMIVSize)
	if err != nil {
		return "", err
	}
	if len(iv) != forma2GCMIVSize || len(tag) != gcm.Overhead() {
		return "", errors.New("malformed forma2 ciphertext")
	}
	plaintext, err := gcm.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plaintext), nil
}

// decryptForma2CBC is forma2's symmetricDecryptV1: AES-256-CBC with PKCS#7 padding, as Node's createDecipheriv("aes256") reads it.
func decryptForma2CBC(ivHex, ctHex string, key []byte) (string, error) {
	iv, err1 := hex.DecodeString(ivHex)
	ct, err2 := hex.DecodeString(ctHex)
	if err := errors.Join(err1, err2); err != nil {
		return "", fmt.Errorf("malformed forma2 ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("forma2 key: %w", err)
	}
	if len(iv) != aes.BlockSize || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errors.New("malformed forma2 ciphertext")
	}
	plaintext := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ct)
	pad := int(plaintext[len(plaintext)-1])
	if pad < 1 || pad > aes.BlockSize || !bytes.Equal(plaintext[len(plaintext)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return "", ErrUndecryptable
	}
	return string(plaintext[:len(plaintext)-pad]), nil
}

// BetterAuthSecrets is the secret configuration Better Auth encrypted with (better-auth 1.7 context/create-context.mjs). Without Versions it is the plain-string form forma2 ran with, BETTER_AUTH_SECRET falling back to NEXTAUTH_SECRET, and Secret decrypts bare-hex values. With Versions it is the rotated form (BETTER_AUTH_SECRETS): "$ba$<version>$<hex>" values open with the version's secret, and Secret, when set, is the legacy secret that wrote the bare-hex values from before the rotation.
type BetterAuthSecrets struct {
	Secret   string
	Versions map[int]string
}

const betterAuthEnvelopePrefix = "$ba$"

// DecryptBetterAuth is Better Auth's symmetricDecrypt (better-auth 1.7 crypto/index.mjs): the key is SHA-256 of the secret's UTF-8 bytes, the value is hex of a 24-byte nonce followed by the XChaCha20-Poly1305 ciphertext and tag (@noble/ciphers managedNonce), and the plaintext is UTF-8.
func DecryptBetterAuth(secrets BetterAuthSecrets, data string) (string, error) {
	if secrets.Versions == nil {
		return decryptBetterAuthRaw(secrets.Secret, data)
	}
	if version, ciphertext, ok := parseBetterAuthEnvelope(data); ok {
		s, found := secrets.Versions[version]
		if !found {
			return "", fmt.Errorf("better auth secret version %d not found in keys (key may have been retired)", version)
		}
		return decryptBetterAuthRaw(s, ciphertext)
	}
	if secrets.Secret != "" {
		return decryptBetterAuthRaw(secrets.Secret, data)
	}
	return "", errors.New("cannot decrypt legacy bare-hex payload: no legacy secret available")
}

// parseBetterAuthEnvelope is Better Auth's parseEnvelope: "$ba$", a non-negative decimal version, "$", then the ciphertext. Like JavaScript's parseInt it reads the leading digits of the version field.
func parseBetterAuthEnvelope(data string) (int, string, bool) {
	rest, ok := strings.CutPrefix(data, betterAuthEnvelopePrefix)
	if !ok {
		return 0, "", false
	}
	field, ciphertext, ok := strings.Cut(rest, "$")
	if !ok {
		return 0, "", false
	}
	digits := field
	if i := strings.IndexFunc(field, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		digits = field[:i]
	}
	version, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", false
	}
	return version, ciphertext, true
}

func decryptBetterAuthRaw(secret, hexData string) (string, error) {
	if secret == "" {
		return "", errors.New("a Better Auth secret is required")
	}
	sealed, err := hex.DecodeString(hexData)
	if err != nil {
		return "", fmt.Errorf("malformed Better Auth ciphertext: %w", err)
	}
	key := sha256.Sum256([]byte(secret))
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("malformed Better Auth ciphertext")
	}
	plaintext, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plaintext), nil
}
