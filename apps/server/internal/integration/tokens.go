package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/yldm-tech/forma/apps/server/internal/platform/jsvalue"
	"github.com/yldm-tech/forma/apps/server/internal/platform/valkey"
)

// tokenExpiryBuffer is how long before its expiry a stored access token is refreshed, forma2's TOKEN_EXPIRY_BUFFER_MS.
const tokenExpiryBuffer = 5 * time.Minute

// airtableRefreshTiming is how concurrent Airtable refreshes of one workspace are serialized: the lock's lifetime, and how a refresh that lost the lock waits for the winner's rotation to be stored.
type airtableRefreshTiming struct {
	lockTTL      time.Duration
	waitGap      time.Duration
	waitAttempts int
}

var defaultAirtableRefreshTiming = airtableRefreshTiming{lockTTL: 30 * time.Second, waitGap: 500 * time.Millisecond, waitAttempts: 6}

// tokens keeps the Google Sheets and Airtable access tokens fresh, persisting every refresh before the token is used. Notion and Slack tokens do not expire.
type tokens struct {
	repo    *Repository
	rdb     *redis.Client
	p       *providers
	log     *slog.Logger
	now     func() time.Time
	refresh airtableRefreshTiming
}

// google is forma2's googleSheet authorize: the stored access token while it is more than five minutes from expiry and Google's tokeninfo accepts it, otherwise a refreshed one, stored with the integration's email. Without a refresh token, or when Google refuses it, the error is errInvalidGrant: only reconnecting helps.
func (t *tokens) google(ctx context.Context, in *Integration) (string, error) {
	k, ok := key[GoogleKey](in)
	if !ok {
		return "", errInvalidGrant
	}
	if k.AccessToken != "" && k.ExpiryDate > t.now().Add(tokenExpiryBuffer).UnixMilli() && t.p.googleTokenValid(ctx, k.AccessToken) {
		return k.AccessToken, nil
	}
	if k.RefreshToken == "" {
		return "", errInvalidGrant
	}
	fresh, err := t.p.googleRefresh(ctx, k.RefreshToken)
	if err != nil {
		return "", err
	}
	merged := googleKey(fresh, *k)
	if err := t.store(ctx, in, merged); err != nil {
		return "", err
	}
	return merged.AccessToken, nil
}

// store replaces the integration's token set, keeping its email.
func (t *tokens) store(ctx context.Context, in *Integration, k any) error {
	raw, err := json.Marshal(k)
	if err != nil {
		return fmt.Errorf("encode integration token: %w", err)
	}
	creds := Credentials{Key: raw}
	if in.Credentials != nil {
		creds.Email = in.Credentials.Email
	}
	if err := t.repo.SaveCredentials(ctx, in.ID, creds); err != nil {
		return err
	}
	in.Credentials = &creds
	return nil
}

// fresh reports whether an Airtable token is more than five minutes from expiry; an expiry that does not parse counts as expired.
func (t *tokens) fresh(k *AirtableKey) bool {
	at, ok := jsvalue.ParseDate(k.ExpiryDate)
	return ok && at.After(t.now().Add(tokenExpiryBuffer))
}

// airtableRefreshLockKey elects the one caller that refreshes a workspace's Airtable token.
func airtableRefreshLockKey(workspaceID uuid.UUID) string {
	return valkey.KeyPrefix + "oauth:airtable-token-refresh:" + workspaceID.String()
}

// airtable is forma2's resolveAirtableCredential: the stored token while it is fresh, otherwise a refreshed one. Airtable rotates the refresh token on every use, so the refresh of one workspace is elected through a Valkey lock: a caller that does not win it waits for the winner's rotation to be stored and uses that, and refreshes on its own only when none appears. The new token set is stored before it is returned, and the lock is left to expire, since a refresh that overran it could otherwise release another caller's.
func (t *tokens) airtable(ctx context.Context, in *Integration) (*AirtableKey, error) {
	k, ok := key[AirtableKey](in)
	if !ok {
		return nil, errors.New("Failed to get Airtable token: the integration has no credential") //nolint:staticcheck // forma2's message
	}
	if t.fresh(k) {
		return k, nil
	}
	won, err := t.rdb.SetNX(ctx, airtableRefreshLockKey(in.WorkspaceID), "1", t.refresh.lockTTL).Result()
	if err == nil && !won {
		rotated, err := t.awaitRotation(ctx, in.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if rotated != nil {
			return rotated, nil
		}
		t.log.WarnContext(ctx, "Airtable refresh lock is held but no rotated token was persisted; refreshing unserialised", "workspace_id", in.WorkspaceID)
	}
	fresh, err := t.p.airtableToken(ctx, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {k.RefreshToken}, "client_id": {t.p.cfg.AirtableClientID},
	}, t.now())
	if err != nil {
		return nil, fmt.Errorf("Failed to get Airtable token: %w", err) //nolint:staticcheck // forma2's message
	}
	if err := t.store(ctx, in, fresh); err != nil {
		return nil, fmt.Errorf("Failed to get Airtable token: %w", err) //nolint:staticcheck // forma2's message
	}
	return fresh, nil
}

// awaitRotation reads the workspace's Airtable credential back until it is fresh; nil when it never becomes so or the integration is gone.
func (t *tokens) awaitRotation(ctx context.Context, workspaceID uuid.UUID) (*AirtableKey, error) {
	for range t.refresh.waitAttempts {
		in, err := t.repo.ByType(ctx, workspaceID, TypeAirtable)
		if err != nil {
			return nil, err
		}
		if in == nil {
			return nil, nil
		}
		if k, ok := key[AirtableKey](in); ok && t.fresh(k) {
			return k, nil
		}
		if err := sleep(ctx, t.refresh.waitGap); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
