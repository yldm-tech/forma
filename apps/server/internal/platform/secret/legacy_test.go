package secret

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The vectors below were produced by better-auth 1.7.0's own symmetricEncrypt (with @better-auth/utils 0.4.2 and @noble/ciphers 2.2.0, the versions forma2 pinned) and by a copy of forma2's lib/crypto.ts on Node 24, so the decryptors are checked against the real algorithms rather than against themselves.
const (
	baSecret        = "forma2-better-auth-secret-for-tests-0123456789"
	baTOTPSecret    = "OfBqWQvwnfdc8grnNxXfX4rYQ76-cBb-"
	baTOTPSecretHex = "35f9fee0a496c7e61d9743f66d878bf7d470eef781f566dfb1cb0f4fae03ccb7fce6282c3e4c1efb238c24682997956db22d5975ebe4f858fdea1a9512c88aa6d204595a7672f1f3"

	baVersionOne   = "rotated-secret-version-one-abcdefghijklmnop"
	baVersionTwo   = "rotated-secret-version-two-abcdefghijklmnop"
	baCodesV2      = "$ba$2$0cc577907db558d7adf731c265b5b3373425d4928f9afa916d5c5615fcb6e2e0094ba1fe9dc4b9274dceee212641ebecf7c9a897aa2d23caba3f99278322b06cbff1c0ca4c"
	baV1           = "$ba$1$7e2487c8c811af431833dcf075914c8923954a58e5a1b874828bec7f7522bcb435eb459fea646b19e5857daf317eb94516833d"
	baBareBeforeV2 = "653f4c08f6a65c114a6203d61652dfd970649ec1304564a066b82316abfc7f208ab480bfa581f344c409dfc8bda08311bf2e0b60e968b7023c80e63c093ca458"

	forma2HexKey    = "a0ff678f48df0da911d2287b6d928688e948dccb2554cdd16f61050fe3d90a86"
	forma2LatinKey  = "0123456789abcdefghijklmnopqrstuv"
	forma2SecretGCM = "332ddfed67aac001421296c59d2f293f:a3f470b555e6235e57943f4cbd384d3150a337d7f4037b3d14f9f27981381f16:c78b14354fe49ff5f445e74b792d4f9d"
	forma2CodesGCM  = "0373bf459c0635cb1d81728883bce013:64b198ebdc6a24194acb93c9f372f8cab63b94195cd8c455956ded1055b6f862:acaefcae0c74b8d02d7df3f78c30ecd4"
	forma2SecretCBC = "23296984ee1459c93925a9e98f74e9a8:30f3848534bbca5642d3e9191cea439d3da6cf726c6fa0e8ddc9332ca709cf37d0614372431fb386cfc3b58960db663b"
	forma2Secret    = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
)

func TestDecryptBetterAuth(t *testing.T) {
	rotated := BetterAuthSecrets{Secret: baSecret, Versions: map[int]string{1: baVersionOne, 2: baVersionTwo}}
	cases := []struct {
		name    string
		secrets BetterAuthSecrets
		data    string
		want    string
	}{
		{"plain secret, bare hex", BetterAuthSecrets{Secret: baSecret}, baTOTPSecretHex, baTOTPSecret},
		{"rotated, current version", rotated, baCodesV2, `["AbCdE-12345","zz9Yx-QwErT"]`},
		{"rotated, retired version", rotated, baV1, "version one"},
		{"rotated, bare hex from before the rotation", rotated, baBareBeforeV2, "bare hex before rotation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecryptBetterAuth(tc.secrets, tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecryptBetterAuthRefuses(t *testing.T) {
	tampered := []byte(baTOTPSecretHex)
	tampered[len(tampered)-1] ^= 1
	cases := []struct {
		name    string
		secrets BetterAuthSecrets
		data    string
	}{
		{"wrong secret", BetterAuthSecrets{Secret: baSecret + "x"}, baTOTPSecretHex},
		{"tampered", BetterAuthSecrets{Secret: baSecret}, string(tampered)},
		{"not hex", BetterAuthSecrets{Secret: baSecret}, "zz"},
		{"too short", BetterAuthSecrets{Secret: baSecret}, "00ff"},
		// A plain-string configuration does not understand envelopes, as in Better Auth.
		{"envelope without versions", BetterAuthSecrets{Secret: baSecret}, baCodesV2},
		{"retired version not configured", BetterAuthSecrets{Secret: baSecret, Versions: map[int]string{2: baVersionTwo}}, baV1},
		{"bare hex without a legacy secret", BetterAuthSecrets{Versions: map[int]string{2: baVersionTwo}}, baBareBeforeV2},
		{"no secret", BetterAuthSecrets{}, baTOTPSecretHex},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DecryptBetterAuth(tc.secrets, tc.data); err == nil {
				t.Fatalf("decrypted to %q", got)
			}
		})
	}
}

func TestDecryptForma2(t *testing.T) {
	hexKey, _ := hex.DecodeString(forma2HexKey)
	latinKey := []byte(forma2LatinKey)
	cases := []struct {
		name    string
		payload string
		key     []byte
		cbc     bool
		want    string
	}{
		{"GCM under a hex key", forma2SecretGCM, hexKey, false, forma2Secret},
		{"GCM under a 32-character key", forma2CodesGCM, latinKey, false, `["0123456789",null,"abcdef0123"]`},
		{"legacy CBC when allowed", forma2SecretCBC, hexKey, true, forma2Secret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecryptForma2(tc.payload, tc.key, tc.cbc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecryptForma2Refuses(t *testing.T) {
	hexKey, _ := hex.DecodeString(forma2HexKey)
	parts := strings.Split(forma2SecretGCM, ":")
	flipped := parts[0] + ":" + parts[1] + ":" + strings.Repeat("0", len(parts[2]))
	cases := []struct {
		name    string
		payload string
		key     []byte
		cbc     bool
	}{
		{"CBC unless allowed", forma2SecretCBC, hexKey, false},
		{"wrong key", forma2SecretGCM, []byte(forma2LatinKey), false},
		{"bad tag, even with CBC allowed", flipped, hexKey, true},
		{"CBC under the wrong key", forma2SecretCBC, []byte(forma2LatinKey), true},
		{"one part", "abcdef", hexKey, true},
		{"not hex", "zz:zz:zz", hexKey, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DecryptForma2(tc.payload, tc.key, tc.cbc); err == nil {
				t.Fatalf("decrypted to %q", got)
			}
		})
	}
	if _, err := DecryptForma2(flipped, hexKey, false); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("bad tag: %v, want ErrUndecryptable", err)
	}
}

func TestEncryptForma2RoundTrips(t *testing.T) {
	hexKey, _ := hex.DecodeString(forma2HexKey)
	sealed, err := EncryptForma2("user1@example.com", hexKey)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sealed, ":")
	if len(parts) != 3 || len(parts[0]) != 32 || len(parts[2]) != 32 || strings.Contains(sealed, "user1") {
		t.Fatalf("sealed %q: want hex iv(16):ciphertext:tag(16)", sealed)
	}
	if got, err := DecryptForma2(sealed, hexKey, false); err != nil || got != "user1@example.com" {
		t.Fatalf("DecryptForma2 = %q, %v", got, err)
	}
	again, _ := EncryptForma2("user1@example.com", hexKey)
	if again == sealed {
		t.Fatal("two encryptions are identical: the IV is not random")
	}
	if _, err := DecryptForma2(sealed, []byte(forma2LatinKey), false); err == nil {
		t.Fatal("opened under another key")
	}
}
