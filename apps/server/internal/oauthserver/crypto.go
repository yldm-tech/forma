package oauthserver

import (
	"errors"
	"strings"
	"sync"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// tokenFormat is op's Crypto, the one place op turns an id into what the client receives. op asks it for two things: an authorization code for a request id, and a bearer access token for "<token id>:<subject>".
//
// Codes are random and stored as their hash (saveCode). Access tokens are JWTs typed at+jwt (RFC 9068), which op's own JWT access tokens are not: the storage signs each one when it records it, and Encrypt hands it over. Decrypt never succeeds, so op verifies a presented access token as a JWT against the published keys.
type tokenFormat struct {
	mu      sync.Mutex
	pending map[string]string
}

var _ op.Crypto = (*tokenFormat)(nil)

func newTokenFormat() *tokenFormat {
	return &tokenFormat{pending: map[string]string{}}
}

var errNotEncrypted = errors.New("access tokens are signed, not encrypted")

// stage holds the signed access token for its id until op asks for it, which it does right after the storage created it.
func (f *tokenFormat) stage(tokenID, signed string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending[tokenID] = signed
}

func (f *tokenFormat) Encrypt(value string) (string, error) {
	tokenID, _, isAccessToken := strings.Cut(value, ":")
	if !isAccessToken {
		return secret.NewToken()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	signed, ok := f.pending[tokenID]
	if !ok {
		return "", errors.New("access token was not signed")
	}
	delete(f.pending, tokenID)
	return signed, nil
}

func (f *tokenFormat) Decrypt(string) (string, error) {
	return "", errNotEncrypted
}
