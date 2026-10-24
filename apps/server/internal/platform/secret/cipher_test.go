package secret

import (
	"bytes"
	"errors"
	"testing"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
)

func testKey(fill byte) config.EncryptionKey {
	return bytes.Repeat([]byte{fill}, 32)
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(config.Config{EncryptionKey: testKey(1), EncryptionKeyVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Seal([]byte("totp key"), []byte("user_totp.secret:a"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed[0] != 7 || bytes.Contains(sealed, []byte("totp key")) {
		t.Fatalf("sealed %x: want version 7 in front and no plaintext", sealed)
	}
	again, _ := c.Seal([]byte("totp key"), []byte("user_totp.secret:a"))
	if bytes.Equal(sealed, again) {
		t.Fatal("two seals of one value are identical: the nonce is not random")
	}
	got, err := c.Open(sealed, []byte("user_totp.secret:a"))
	if err != nil || string(got) != "totp key" {
		t.Fatalf("Open = %q, %v", got, err)
	}
}

func TestCipherRefuses(t *testing.T) {
	c, _ := NewCipher(config.Config{EncryptionKey: testKey(1), EncryptionKeyVersion: 1})
	sealed, _ := c.Seal([]byte("totp key"), []byte("ctx"))
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1
	unknownVersion := bytes.Clone(sealed)
	unknownVersion[0] = 9
	otherKey, _ := NewCipher(config.Config{EncryptionKey: testKey(2), EncryptionKeyVersion: 1})
	cases := []struct {
		name    string
		c       *Cipher
		sealed  []byte
		context string
	}{
		{"other context", c, sealed, "ctx2"},
		{"tampered", c, tampered, "ctx"},
		{"unknown key version", c, unknownVersion, "ctx"},
		{"other key, same version", otherKey, sealed, "ctx"},
		{"empty", c, nil, "ctx"},
		{"truncated", c, sealed[:10], "ctx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := tc.c.Open(tc.sealed, []byte(tc.context)); !errors.Is(err, ErrUndecryptable) {
				t.Fatalf("Open = %q, %v; want ErrUndecryptable", got, err)
			}
		})
	}
}

// After a rotation new values are sealed under the new version, and values sealed under the retired key still open.
func TestCipherRotation(t *testing.T) {
	before, _ := NewCipher(config.Config{EncryptionKey: testKey(1), EncryptionKeyVersion: 1})
	old, _ := before.Seal([]byte("old"), []byte("ctx"))

	after, err := NewCipher(config.Config{EncryptionKey: testKey(2), EncryptionKeyVersion: 2, EncryptionPreviousKeys: config.EncryptionKeyring{1: testKey(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := after.Open(old, []byte("ctx")); err != nil || string(got) != "old" {
		t.Fatalf("retired-key value: %q, %v", got, err)
	}
	fresh, _ := after.Seal([]byte("new"), []byte("ctx"))
	if fresh[0] != 2 {
		t.Fatalf("new value sealed under version %d, want 2", fresh[0])
	}
	if _, err := before.Open(fresh, []byte("ctx")); err == nil {
		t.Fatal("a process without the new key opened a value sealed under it")
	}
}

func TestNewCipherRejectsShortKeys(t *testing.T) {
	if _, err := NewCipher(config.Config{EncryptionKey: []byte("short"), EncryptionKeyVersion: 1}); err == nil {
		t.Fatal("accepted a 5-byte key")
	}
}
