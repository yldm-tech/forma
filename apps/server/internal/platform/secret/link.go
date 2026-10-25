package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// linkIVSize is the 16-byte IV of the link value format (the AES block size, not GCM's usual 12).
const linkIVSize = 16

// EncryptLinkValue seals a value that travels in a link or token this server issues (a contact survey token's ids, a single-use id, a verified email): AES-256-GCM under ENCRYPTION_KEY with a random 16-byte IV, written as hex "iv:ciphertext:tag". Links already sent carry this format, so it does not change.
func EncryptLinkValue(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("link value key: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, linkIVSize)
	if err != nil {
		return "", err
	}
	iv := make([]byte, linkIVSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)
	ct, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]
	return hex.EncodeToString(iv) + ":" + hex.EncodeToString(ct) + ":" + hex.EncodeToString(tag), nil
}

// DecryptLinkValue opens what EncryptLinkValue sealed under key. A value that fails its tag is ErrUndecryptable.
func DecryptLinkValue(payload string, key []byte) (string, error) {
	parts := strings.Split(payload, ":")
	if len(parts) != 3 {
		return "", errors.New("malformed link value ciphertext")
	}
	iv, err1 := hex.DecodeString(parts[0])
	ct, err2 := hex.DecodeString(parts[1])
	tag, err3 := hex.DecodeString(parts[2])
	if err := errors.Join(err1, err2, err3); err != nil {
		return "", fmt.Errorf("malformed link value ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("link value key: %w", err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, linkIVSize)
	if err != nil {
		return "", err
	}
	if len(iv) != linkIVSize || len(tag) != gcm.Overhead() {
		return "", errors.New("malformed link value ciphertext")
	}
	plaintext, err := gcm.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plaintext), nil
}
