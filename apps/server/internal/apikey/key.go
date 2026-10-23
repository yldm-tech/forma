// Package apikey is the organization API key module, forma2's apps/web/modules/organization/settings/api-keys plus the API key half of its request authentication (apps/web/modules/api/lib/api-key-auth.ts): keys are created, listed, relabelled and deleted under /api/app by an organization's owners and managers, and the public APIs authenticate a request carrying one through Authenticator.
//
// A key is "fbk_" followed by 32 random bytes in unpadded base64url. It is stored twice: lookup_hash, the hex SHA-256 of the secret (without the prefix), finds the row through a unique index, and hashed_key, a bcrypt of the secret at cost 12, verifies it. Keys minted before forma2 added bcrypt hold the hex SHA-256 of the whole raw key in both columns and are still accepted, through the x-api-key header only.
package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Prefix marks a current-format key. forma2 named the format v2; everything else is a legacy key.
const Prefix = "fbk_"

// issuedKey is a freshly minted key: the plaintext the caller sees once, and the two hashes that are stored.
type issuedKey struct {
	Key        string
	LookupHash string
	HashedKey  string
}

// newKey mints a key as forma2's createApiKey did: randomBytes(32) as base64url, sha256 hex for the lookup, bcrypt cost 12 for the verification.
func newKey() (issuedKey, error) {
	s, err := secret.NewToken()
	if err != nil {
		return issuedKey{}, err
	}
	hashed, err := secret.HashPassword(s)
	if err != nil {
		return issuedKey{}, fmt.Errorf("hash api key: %w", err)
	}
	return issuedKey{Key: Prefix + s, LookupHash: sha256Hex(s), HashedKey: hashed}, nil
}

// parseV2 returns the secret of a current-format key: the prefix, then at least one base64url character and nothing else. There is no length check, as in forma2's parseApiKeyV2.
func parseV2(key string) (string, bool) {
	s, ok := strings.CutPrefix(key, Prefix)
	if !ok || s == "" {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return "", false
		}
	}
	return s, true
}

// sha256Hex is forma2's hashSha256: lowercase hex.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// legacyMatch reports whether a stored row is a pre-bcrypt key whose hash is digest. Such a row holds the SHA-256 in hashed_key; a current row holds a bcrypt hash there, so a current key's bare secret, which has the same lookup hash, never passes for a legacy key.
func legacyMatch(hashedKey, digest string) bool {
	return subtle.ConstantTimeCompare([]byte(hashedKey), []byte(digest)) == 1
}
