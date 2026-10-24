package oauthserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// keyCacheTTL is how long the keys are cached before the table is read again. A key another instance added becomes known sooner: a token signed with an unknown key id reloads them, at most once per keyRefreshInterval, so forged key ids cannot turn every request into a query.
const (
	keyCacheTTL        = 5 * time.Minute
	keyRefreshInterval = 30 * time.Second
)

// sealContext binds a sealed private key to its purpose.
var sealContext = []byte("oauth_signing_keys.private_key")

// signingKey is an Ed25519 key tokens are signed with, as op signs ID tokens with it (op.SigningKey).
type signingKey struct {
	id      uuid.UUID
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

func (k *signingKey) ID() string                                  { return k.id.String() }
func (k *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return jose.EdDSA }
func (k *signingKey) Key() any                                    { return k.private }

// publicKey is a signing key as <issuer>/jwks publishes it (op.Key).
type publicKey struct{ key *signingKey }

func (k publicKey) ID() string                         { return k.key.id.String() }
func (k publicKey) Algorithm() jose.SignatureAlgorithm { return jose.EdDSA }
func (k publicKey) Use() string                        { return "sig" }
func (k publicKey) Key() any                           { return k.key.public }

// Keys holds the signing keys: the newest signs, every one in the table verifies. The first instance that needs a key creates it; instances that race each create one, and both stay valid.
type Keys struct {
	db     *gorm.DB
	cipher *secret.Cipher
	now    func() time.Time

	mu       sync.Mutex
	all      []*signingKey
	loadedAt time.Time
}

func NewKeys(db *gorm.DB, cipher *secret.Cipher) *Keys {
	return &Keys{db: db, cipher: cipher, now: time.Now}
}

type keyRow struct {
	ID         uuid.UUID
	PublicKey  []byte
	PrivateKey []byte
}

// load reads every key, newest first, creating the first one when there is none.
func (k *Keys) load(ctx context.Context) ([]*signingKey, error) {
	var rows []keyRow
	if err := k.db.WithContext(ctx).Raw(`SELECT id, public_key, private_key FROM oauth_signing_keys ORDER BY created_at DESC, id DESC`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load signing keys: %w", err)
	}
	if len(rows) == 0 {
		if err := k.create(ctx); err != nil {
			return nil, err
		}
		return k.load(ctx)
	}
	keys := make([]*signingKey, len(rows))
	for i, r := range rows {
		seed, err := k.cipher.Open(r.PrivateKey, sealContext)
		if err != nil {
			return nil, fmt.Errorf("open signing key %s: %w", r.ID, err)
		}
		if len(seed) != ed25519.SeedSize || len(r.PublicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("signing key %s is malformed", r.ID)
		}
		keys[i] = &signingKey{id: r.ID, private: ed25519.NewKeyFromSeed(seed), public: ed25519.PublicKey(r.PublicKey)}
	}
	return keys, nil
}

func (k *Keys) create(ctx context.Context) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate signing key: %w", err)
	}
	sealed, err := k.cipher.Seal(private.Seed(), sealContext)
	if err != nil {
		return fmt.Errorf("seal signing key: %w", err)
	}
	if err := k.db.WithContext(ctx).Exec(`INSERT INTO oauth_signing_keys (algorithm, public_key, private_key) VALUES ('EdDSA', ?, ?)`, []byte(public), sealed).Error; err != nil {
		return fmt.Errorf("insert signing key: %w", err)
	}
	return nil
}

// cached returns the keys, reading them again when the cache is older than keyCacheTTL or refresh is set.
func (k *Keys) cached(ctx context.Context, refresh bool) ([]*signingKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !refresh && k.all != nil && k.now().Sub(k.loadedAt) < keyCacheTTL {
		return k.all, nil
	}
	keys, err := k.load(ctx)
	if err != nil {
		return nil, err
	}
	k.all, k.loadedAt = keys, k.now()
	return keys, nil
}

// signing is the key new tokens are signed with.
func (k *Keys) signing(ctx context.Context) (*signingKey, error) {
	keys, err := k.cached(ctx, false)
	if err != nil {
		return nil, err
	}
	return keys[0], nil
}

// published are the keys at <issuer>/jwks.
func (k *Keys) published(ctx context.Context) ([]*signingKey, error) {
	return k.cached(ctx, false)
}

var errUnknownKey = errors.New("token is signed with an unknown key")

// VerifySignature checks a compact JWS against the keys and returns its payload. It is go-oidc's KeySet, so /api/mcp verifies against the table directly when MCP_OAUTH_JWKS_URL is unset.
func (k *Keys) VerifySignature(ctx context.Context, token string) ([]byte, error) {
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	if len(jws.Signatures) != 1 {
		return nil, errors.New("token must carry exactly one signature")
	}
	kid := jws.Signatures[0].Header.KeyID
	keys, err := k.cached(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
	}
	key := find(keys, kid)
	if key == nil && k.stale() {
		if keys, err = k.cached(ctx, true); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
		}
		key = find(keys, kid)
	}
	if key == nil {
		return nil, errUnknownKey
	}
	return jws.Verify(key.public)
}

// stale reports whether the keys were read long enough ago that an unknown key id may be a new key.
func (k *Keys) stale() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.now().Sub(k.loadedAt) >= keyRefreshInterval
}

func find(keys []*signingKey, kid string) *signingKey {
	for _, key := range keys {
		if key.id.String() == kid {
			return key
		}
	}
	return nil
}
