package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"

	"github.com/yldm-tech/forma/server/internal/platform/valkey"
)

// stateTTL is how long an OAuth flow may take from authorize to callback, forma2's INTEGRATION_OAUTH_STATE_TTL_MS.
const stateTTL = 10 * time.Minute

// stateToken is the shape of a state or PKCE verifier forma2 issued: base64url without padding, 43 characters for its 32 random bytes. A callback state of any other shape is refused before Valkey is read.
var stateToken = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)

// errInvalidState is a callback whose state was never issued, expired, was already used, or was issued to another provider, user or session: forma2's IntegrationOAuthStateError, 400 "Invalid OAuth state".
var errInvalidState = errors.New("invalid OAuth state")

// safeCallbackErrors are the provider error codes a callback passes on to the integrations page; any other becomes oauth_error, forma2's SAFE_OAUTH_CALLBACK_ERRORS.
var safeCallbackErrors = map[string]bool{
	"access_denied": true, "invalid_request": true, "invalid_scope": true, "server_error": true, "temporarily_unavailable": true,
}

// safeCallbackError is forma2's getSafeOAuthCallbackError: "" when the callback carries no error.
func safeCallbackError(e string) string {
	switch {
	case e == "":
		return ""
	case safeCallbackErrors[e]:
		return e
	}
	return "oauth_error"
}

// oauthState is what an authorize request leaves for its callback: who started the flow, in which session, for which provider and workspace, and the PKCE verifier of a flow that has one. The workspace never appears in the URL.
type oauthState struct {
	Provider     string    `json:"provider"`
	UserID       uuid.UUID `json:"userId"`
	SessionID    uuid.UUID `json:"sessionId"`
	WorkspaceID  uuid.UUID `json:"workspaceId"`
	PKCEVerifier string    `json:"pkceCodeVerifier,omitempty"`
	CreatedAt    int64     `json:"createdAt"`
}

// stateStore keeps OAuth states in Valkey under the SHA-256 of the state, so a dump of Valkey does not hold usable states, each consumed once. It fails closed: without Valkey no flow starts and no callback is accepted.
type stateStore struct {
	rdb *redis.Client
	now func() time.Time
}

func stateKey(state string) string {
	sum := sha256.Sum256([]byte(state))
	return valkey.KeyPrefix + "oauth:state:" + hex.EncodeToString(sum[:])
}

// create stores s and returns the state to send to the provider: 32 random bytes, base64url.
func (st *stateStore) create(ctx context.Context, s oauthState) (string, error) {
	state := oauth2.GenerateVerifier()
	s.CreatedAt = st.now().UnixMilli()
	raw, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("encode OAuth state: %w", err)
	}
	if err := st.rdb.Set(ctx, stateKey(state), raw, stateTTL).Err(); err != nil {
		return "", fmt.Errorf("store OAuth state: %w", err)
	}
	return state, nil
}

// consume takes the state of a callback, forma2's consumeIntegrationOAuthState: it must have the issued shape, still be stored (it is deleted as it is read, so it works once), and belong to the provider, the user and the session of the callback. Valkey failing is an error, never an accepted state.
func (st *stateStore) consume(ctx context.Context, provider string, userID, sessionID uuid.UUID, state string) (*oauthState, error) {
	if !stateToken.MatchString(state) {
		return nil, errInvalidState
	}
	raw, err := st.rdb.GetDel(ctx, stateKey(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, errInvalidState
	}
	if err != nil {
		return nil, fmt.Errorf("consume OAuth state: %w", err)
	}
	var s oauthState
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, errInvalidState
	}
	if s.Provider != provider || s.UserID != userID || s.SessionID != sessionID || s.WorkspaceID == uuid.Nil {
		return nil, errInvalidState
	}
	return &s, nil
}
