package secret

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The ciphertexts below were produced by an independent Node 24 implementation of the format (crypto.createCipheriv("aes-256-gcm") with a 16-byte IV), so the decryptor is checked against the real algorithm rather than against itself.
const (
	linkHexKey    = "a0ff678f48df0da911d2287b6d928688e948dccb2554cdd16f61050fe3d90a86"
	linkLatinKey  = "0123456789abcdefghijklmnopqrstuv"
	linkSecretGCM = "332ddfed67aac001421296c59d2f293f:a3f470b555e6235e57943f4cbd384d3150a337d7f4037b3d14f9f27981381f16:c78b14354fe49ff5f445e74b792d4f9d"
	linkCodesGCM  = "0373bf459c0635cb1d81728883bce013:64b198ebdc6a24194acb93c9f372f8cab63b94195cd8c455956ded1055b6f862:acaefcae0c74b8d02d7df3f78c30ecd4"
	// An AES-256-CBC "iv:ciphertext" value, a format this server never accepted from a link.
	linkCBC    = "23296984ee1459c93925a9e98f74e9a8:30f3848534bbca5642d3e9191cea439d3da6cf726c6fa0e8ddc9332ca709cf37d0614372431fb386cfc3b58960db663b"
	linkSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
)

func TestDecryptLinkValue(t *testing.T) {
	hexKey, _ := hex.DecodeString(linkHexKey)
	latinKey := []byte(linkLatinKey)
	cases := []struct {
		name    string
		payload string
		key     []byte
		want    string
	}{
		{"under a hex key", linkSecretGCM, hexKey, linkSecret},
		{"under a 32-character key", linkCodesGCM, latinKey, `["0123456789",null,"abcdef0123"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecryptLinkValue(tc.payload, tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecryptLinkValueRefuses(t *testing.T) {
	hexKey, _ := hex.DecodeString(linkHexKey)
	parts := strings.Split(linkSecretGCM, ":")
	flipped := parts[0] + ":" + parts[1] + ":" + strings.Repeat("0", len(parts[2]))
	cases := []struct {
		name    string
		payload string
		key     []byte
	}{
		{"CBC", linkCBC, hexKey},
		{"wrong key", linkSecretGCM, []byte(linkLatinKey)},
		{"bad tag", flipped, hexKey},
		{"one part", "abcdef", hexKey},
		{"not hex", "zz:zz:zz", hexKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DecryptLinkValue(tc.payload, tc.key); err == nil {
				t.Fatalf("decrypted to %q", got)
			}
		})
	}
	if _, err := DecryptLinkValue(flipped, hexKey); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("bad tag: %v, want ErrUndecryptable", err)
	}
}

func TestEncryptLinkValueRoundTrips(t *testing.T) {
	hexKey, _ := hex.DecodeString(linkHexKey)
	sealed, err := EncryptLinkValue("user1@example.com", hexKey)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sealed, ":")
	if len(parts) != 3 || len(parts[0]) != 32 || len(parts[2]) != 32 || strings.Contains(sealed, "user1") {
		t.Fatalf("sealed %q: want hex iv(16):ciphertext:tag(16)", sealed)
	}
	if got, err := DecryptLinkValue(sealed, hexKey); err != nil || got != "user1@example.com" {
		t.Fatalf("DecryptLinkValue = %q, %v", got, err)
	}
	again, _ := EncryptLinkValue("user1@example.com", hexKey)
	if again == sealed {
		t.Fatal("two encryptions are identical: the IV is not random")
	}
	if _, err := DecryptLinkValue(sealed, []byte(linkLatinKey)); err == nil {
		t.Fatal("opened under another key")
	}
}
