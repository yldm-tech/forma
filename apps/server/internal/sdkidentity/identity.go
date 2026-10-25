// Package sdkidentity is how far the client API trusts the SDK with a workspace's contacts. The workspace id in an SDK snippet is public, so an identify (POST /api/v1|v2/client/{id}/user) proves nothing about who is calling: this package holds the rules that keep such a caller from taking a contact over, and the settings page's routes for them under /api/app.
//
//   - A workspace may hold an identity secret. The customer's own server computes the userHash, the HMAC-SHA256 of the userId under that secret in hex, and the SDK sends it with setUserId; an identify whose userHash matches is verified.
//   - Email is written only on a verified identify, and the attributes the workspace's managers mark as protected never through the client API: those come from the Management API, a CSV import or the contact page.
//   - A workspace that requires verification refuses an identify without a valid userHash, and the v1 display and response routes do not attach a userId that comes without one.
//   - The state an identify answers names the contact by an opaque token (ContactToken) instead of its id, and the surveys the contact was shown or answered by a key (SurveyKey) instead of their ids.
package sdkidentity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Settings are a workspace's identity rules as the client API applies them.
type Settings struct {
	// Secret is the identity secret in clear, "" when the workspace has none.
	Secret string
	// Required refuses an identify that is not verified.
	Required bool
	// ProtectedKeys are the attribute keys the client API never writes.
	ProtectedKeys []string
}

// secretContext binds a sealed identity secret to its workspace, so a value copied to another row does not open there.
func secretContext(workspaceID uuid.UUID) []byte {
	return []byte("workspace-sdk-identity-secret:" + workspaceID.String())
}

// errNoWorkspace is a settings read for a workspace that does not exist.
var errNoWorkspace = errors.New("workspace not found")

// row is the identity columns of one workspace.
type row struct {
	OrganizationID uuid.UUID
	Secret         []byte
	Required       bool
	ProtectedKeys  []string
}

// load reads a workspace's identity columns, errNoWorkspace when there is no such workspace. locked takes the row lock a settings change holds until its transaction ends.
func load(ctx context.Context, db *gorm.DB, workspaceID uuid.UUID, locked bool) (*row, error) {
	query := `SELECT organization_id, sdk_identity_secret, sdk_identity_required, to_jsonb(sdk_protected_attribute_keys)::text FROM workspaces WHERE id = ?`
	if locked {
		query += ` FOR UPDATE`
	}
	var r row
	var keys string
	err := db.WithContext(ctx).Raw(query, workspaceID).Row().Scan(&r.OrganizationID, &r.Secret, &r.Required, &keys)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoWorkspace
	}
	if err != nil {
		return nil, fmt.Errorf("load workspace identity settings: %w", err)
	}
	if err := json.Unmarshal([]byte(keys), &r.ProtectedKeys); err != nil {
		return nil, fmt.Errorf("decode protected attribute keys: %w", err)
	}
	return &r, nil
}

// Load is the identity settings of a workspace the client API has already resolved. A secret that no longer opens (ENCRYPTION_KEY replaced without keeping the old key) is an error rather than no secret: treating it as absent would let a workspace that requires verification accept anyone.
func Load(ctx context.Context, db *gorm.DB, c *secret.Cipher, workspaceID uuid.UUID) (*Settings, error) {
	r, err := load(ctx, db, workspaceID, false)
	if err != nil {
		return nil, err
	}
	s := &Settings{Required: r.Required, ProtectedKeys: r.ProtectedKeys}
	if r.Secret != nil {
		plain, err := c.Open(r.Secret, secretContext(workspaceID))
		if err != nil {
			return nil, fmt.Errorf("open workspace identity secret: %w", err)
		}
		s.Secret = string(plain)
	}
	return s, nil
}

// UserHash is the userHash of userID under an identity secret: HMAC-SHA256 keyed with the secret's text, over the userId's UTF-8 bytes, in lowercase hex. It is what the customer's server computes, e.g. Node's createHmac("sha256", secret).update(userId).digest("hex").
func UserHash(identitySecret, userID string) string {
	mac := hmac.New(sha256.New, []byte(identitySecret))
	mac.Write([]byte(userID))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verified reports whether userHash proves userID under the workspace's secret. Without a secret nothing is verified. Hex is accepted in either case; the comparison takes constant time.
func (s *Settings) Verified(userID, userHash string) bool {
	if s.Secret == "" || userHash == "" {
		return false
	}
	got, err := hex.DecodeString(userHash)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(userID))
	return hmac.Equal(mac.Sum(nil), got)
}

// IsProtected reports whether the client API must not write key.
func (s *Settings) IsProtected(key string) bool {
	for _, k := range s.ProtectedKeys {
		if k == key {
			return true
		}
	}
	return false
}

// SurveyKey is how the SDK state names a survey for one user: js-core's hashString (packages/js-core/src/lib/common/utils.ts, FNV-1a over the UTF-16 code units finished with murmur3's fmix32) of "<userId>:<surveyId>", as eight lowercase hex digits. The SDK computes the same key for each survey it knows, so the state can say which surveys the user saw and answered without listing survey ids.
func SurveyKey(userID, surveyID string) string {
	h := uint32(0x811c9dc5)
	for _, unit := range utf16.Encode([]rune(userID + ":" + surveyID)) {
		h ^= uint32(unit)
		h *= 0x01000193
	}
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return fmt.Sprintf("%08x", h)
}

// contactTokenPrefix starts every contact token, so the client API tells one from a contact id.
const contactTokenPrefix = "ct1."

// IsContactToken reports whether s has the shape of a contact token, valid or not.
func IsContactToken(s string) bool {
	return strings.HasPrefix(s, contactTokenPrefix)
}

// tokenAEAD is the AES-256-GCM a contact token is sealed with under key, and the key its nonce is derived with. Both are derived from the encryption key with fixed labels, so tokens need no configuration of their own.
func tokenAEAD(key []byte) (cipher.AEAD, []byte, error) {
	derive := func(label string) []byte {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(label))
		return mac.Sum(nil)
	}
	block, err := aes.NewCipher(derive("forma.sdk-contact-token.encryption.v1"))
	if err != nil {
		return nil, nil, fmt.Errorf("contact token key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("contact token cipher: %w", err)
	}
	return aead, derive("forma.sdk-contact-token.nonce.v1"), nil
}

// ContactToken is the opaque name the client API gives a contact in place of its id: "ct1." and the contact id sealed with AES-256-GCM under a key derived from ENCRYPTION_KEY, bound to its workspace, in unpadded base64url. The nonce is an HMAC of the workspace and contact ids, so a contact always gets the same token: the SDK keeps it across identifies and the survey runtime compares it when it restores a response. The display and response routes accept it wherever they accept a contact id.
func ContactToken(cfg config.Config, workspaceID, contactID uuid.UUID) (string, error) {
	aead, nonceKey, err := tokenAEAD(cfg.EncryptionKey)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, nonceKey)
	mac.Write(workspaceID[:])
	mac.Write(contactID[:])
	nonce := make([]byte, aead.NonceSize())
	copy(nonce, mac.Sum(nil))
	sealed := aead.Seal(append([]byte(nil), nonce...), nonce, contactID[:], workspaceID[:])
	return contactTokenPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// ParseContactToken is the contact a token names in workspaceID, false for a token this server did not issue for that workspace. Tokens sealed under a retired encryption key still open while the key is kept in ENCRYPTION_PREVIOUS_KEYS.
func ParseContactToken(cfg config.Config, workspaceID uuid.UUID, token string) (uuid.UUID, bool) {
	raw, ok := strings.CutPrefix(token, contactTokenPrefix)
	if !ok {
		return uuid.Nil, false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return uuid.Nil, false
	}
	keys := [][]byte{cfg.EncryptionKey}
	for _, k := range cfg.EncryptionPreviousKeys {
		keys = append(keys, []byte(k))
	}
	for _, key := range keys {
		aead, _, err := tokenAEAD(key)
		if err != nil || len(b) < aead.NonceSize()+aead.Overhead() {
			continue
		}
		plain, err := aead.Open(nil, b[:aead.NonceSize()], b[aead.NonceSize():], workspaceID[:])
		if err != nil {
			continue
		}
		if id, err := uuid.FromBytes(plain); err == nil {
			return id, true
		}
	}
	return uuid.Nil, false
}
