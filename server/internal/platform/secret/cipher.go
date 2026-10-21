package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/yldm-tech/forma/server/internal/platform/config"
)

// ErrUndecryptable is a sealed value that does not open: corrupted, sealed for another context, or sealed under a key this process no longer has.
var ErrUndecryptable = errors.New("sealed value cannot be opened")

// Cipher seals values stored at rest with AES-256-GCM under ENCRYPTION_KEY. A sealed value is the key version (one byte), a random 12-byte nonce and the ciphertext with its tag, so values sealed before a key rotation still open with the retired key.
type Cipher struct {
	version byte
	keys    map[byte]cipher.AEAD
}

// NewCipher builds the cipher from the configured current and retired keys.
func NewCipher(cfg config.Config) (*Cipher, error) {
	c := &Cipher{version: byte(cfg.EncryptionKeyVersion), keys: map[byte]cipher.AEAD{}}
	add := func(version int, key []byte) error {
		block, err := aes.NewCipher(key)
		if err != nil {
			return fmt.Errorf("encryption key version %d: %w", version, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return fmt.Errorf("encryption key version %d: %w", version, err)
		}
		c.keys[byte(version)] = aead
		return nil
	}
	if err := add(cfg.EncryptionKeyVersion, cfg.EncryptionKey); err != nil {
		return nil, err
	}
	for version, key := range cfg.EncryptionPreviousKeys {
		if err := add(version, key); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// Seal encrypts plaintext under the current key. context is authenticated but not stored: Open must be given the same context, so a value copied to another row or column does not open there.
func (c *Cipher) Seal(plaintext, context []byte) ([]byte, error) {
	aead := c.keys[c.version]
	out := make([]byte, 1+aead.NonceSize(), 1+aead.NonceSize()+len(plaintext)+aead.Overhead())
	out[0] = c.version
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return aead.Seal(out, out[1:], plaintext, context), nil
}

// Open decrypts a value Seal produced for the same context, under whichever configured key version it names.
func (c *Cipher) Open(sealed, context []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, ErrUndecryptable
	}
	aead, ok := c.keys[sealed[0]]
	if !ok {
		return nil, fmt.Errorf("%w: key version %d is not configured", ErrUndecryptable, sealed[0])
	}
	body := sealed[1:]
	if len(body) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrUndecryptable
	}
	plaintext, err := aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], context)
	if err != nil {
		return nil, ErrUndecryptable
	}
	return plaintext, nil
}
