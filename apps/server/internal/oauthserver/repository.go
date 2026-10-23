package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type repository struct {
	db *gorm.DB
}

// textArray is a text[] literal; GORM would expand a []string argument into a list.
func textArray(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// arrayColumn is a text[] column selected as to_jsonb and where it is decoded to.
type arrayColumn struct {
	raw []byte
	out *[]string
}

func decodeArrays(columns ...arrayColumn) error {
	for _, c := range columns {
		*c.out = []string{}
		if len(c.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(c.raw, c.out); err != nil {
			return fmt.Errorf("decode text array: %w", err)
		}
	}
	return nil
}

// ---- clients ----

const selectClient = `SELECT client_id, secret_hash, name, client_uri, logo_uri, to_jsonb(redirect_uris) AS redirect_uris, application_type, token_endpoint_auth_method,
	to_jsonb(grant_types) AS grant_types, to_jsonb(response_types) AS response_types, to_jsonb(scopes) AS scopes, to_jsonb(resources) AS resources, disabled, created_at
	FROM oauth_clients`

type clientRow struct {
	ClientID                string
	SecretHash              []byte
	Name                    string
	ClientURI               *string
	LogoURI                 *string
	RedirectURIs            []byte
	ApplicationType         string
	TokenEndpointAuthMethod string
	GrantTypes              []byte
	ResponseTypes           []byte
	Scopes                  []byte
	Resources               []byte
	Disabled                bool
	CreatedAt               time.Time
}

func (r *repository) client(ctx context.Context, clientID string) (*Client, error) {
	var rows []clientRow
	if err := r.db.WithContext(ctx).Raw(selectClient+` WHERE client_id = ?`, clientID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load oauth client: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	x := rows[0]
	c := &Client{
		id: x.ClientID, secretHash: x.SecretHash, name: x.Name, clientURI: x.ClientURI, logoURI: x.LogoURI, applicationType: x.ApplicationType,
		authMethod: x.TokenEndpointAuthMethod, disabled: x.Disabled, createdAt: x.CreatedAt,
	}
	if err := decodeArrays(arrayColumn{x.RedirectURIs, &c.redirectURIs}, arrayColumn{x.GrantTypes, &c.grantTypes}, arrayColumn{x.ResponseTypes, &c.responseTypes}, arrayColumn{x.Scopes, &c.scopes}, arrayColumn{x.Resources, &c.resources}); err != nil {
		return nil, err
	}
	return c, nil
}

func (r *repository) insertClient(ctx context.Context, c *Client) error {
	return r.db.WithContext(ctx).Raw(`INSERT INTO oauth_clients (client_id, secret_hash, name, client_uri, logo_uri, redirect_uris, application_type, token_endpoint_auth_method, grant_types, response_types, scopes, resources)
		VALUES (?, ?, ?, ?, ?, ?::text[], ?, ?, ?::text[], ?::text[], ?::text[], ?::text[]) RETURNING created_at`,
		c.id, c.secretHash, c.name, c.clientURI, c.logoURI, textArray(c.redirectURIs), c.applicationType, c.authMethod,
		textArray(c.grantTypes), textArray(c.responseTypes), textArray(c.scopes), textArray(c.resources)).Row().Scan(&c.createdAt)
}

// ---- authorization requests ----

const selectAuthRequest = `SELECT id, created_at, client_id, redirect_uri, to_jsonb(scopes) AS scopes, resource, state, nonce, response_mode, to_jsonb(prompt) AS prompt,
	code_challenge, user_id, auth_time FROM oauth_authorization_requests`

type authRequestRow struct {
	ID            uuid.UUID
	CreatedAt     time.Time
	ClientID      string
	RedirectURI   string
	Scopes        []byte
	Resource      string
	State         string
	Nonce         string
	ResponseMode  string
	Prompt        []byte
	CodeChallenge string
	UserID        *uuid.UUID
	AuthTime      *time.Time
}

func (r *repository) findAuthRequest(ctx context.Context, where string, args ...any) (*AuthRequest, error) {
	var rows []authRequestRow
	if err := r.db.WithContext(ctx).Raw(selectAuthRequest+" WHERE "+where+" AND expires_at > now()", args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load authorization request: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	x := rows[0]
	a := &AuthRequest{
		id: x.ID, createdAt: x.CreatedAt, clientID: x.ClientID, redirectURI: x.RedirectURI, resource: x.Resource, state: x.State, nonce: x.Nonce,
		responseMode: x.ResponseMode, codeChallenge: x.CodeChallenge, userID: x.UserID, authTime: x.AuthTime,
	}
	if err := decodeArrays(arrayColumn{x.Scopes, &a.scopes}, arrayColumn{x.Prompt, &a.prompt}); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *repository) authRequest(ctx context.Context, id uuid.UUID) (*AuthRequest, error) {
	return r.findAuthRequest(ctx, "id = ?", id)
}

func (r *repository) authRequestByCode(ctx context.Context, codeHash []byte) (*AuthRequest, error) {
	return r.findAuthRequest(ctx, "code_hash = ?", codeHash)
}

func (r *repository) insertAuthRequest(ctx context.Context, a *AuthRequest, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Raw(`INSERT INTO oauth_authorization_requests (expires_at, client_id, redirect_uri, scopes, resource, state, nonce, response_mode, prompt, code_challenge, code_challenge_method)
		VALUES (?, ?, ?, ?::text[], ?, ?, ?, ?, ?::text[], ?, 'S256') RETURNING id, created_at`,
		expiresAt, a.clientID, a.redirectURI, textArray(a.scopes), a.resource, a.state, a.nonce, a.responseMode, textArray(a.prompt), a.codeChallenge).Row().Scan(&a.id, &a.createdAt)
}

// bindAuthRequest records that user signed in and approved the request. It is false when the request is gone, expired or already bound.
func (r *repository) bindAuthRequest(ctx context.Context, id, userID uuid.UUID, authTime time.Time) (bool, error) {
	res := r.db.WithContext(ctx).Exec(`UPDATE oauth_authorization_requests SET user_id = ?, auth_time = ? WHERE id = ? AND user_id IS NULL AND expires_at > now()`, userID, authTime, id)
	if res.Error != nil {
		return false, fmt.Errorf("bind authorization request: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// saveCode attaches the code to an approved request; a request gets one code only.
func (r *repository) saveCode(ctx context.Context, id uuid.UUID, codeHash []byte) error {
	res := r.db.WithContext(ctx).Exec(`UPDATE oauth_authorization_requests SET code_hash = ? WHERE id = ? AND user_id IS NOT NULL AND code_hash IS NULL AND expires_at > now()`, codeHash, id)
	if res.Error != nil {
		return fmt.Errorf("save authorization code: %w", res.Error)
	}
	if res.RowsAffected != 1 {
		return errors.New("authorization request cannot be issued a code")
	}
	return nil
}

func (r *repository) deleteAuthRequest(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM oauth_authorization_requests WHERE id = ?`, id).Error; err != nil {
		return fmt.Errorf("delete authorization request: %w", err)
	}
	return nil
}

// ---- consents ----

type consentRow struct {
	ID        uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
	ClientID  string
	Scopes    []byte
	Resources []byte
	Name      string
	ClientURI *string
	LogoURI   *string
}

// Consent is what a user approved for a client.
type Consent struct {
	ID        uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
	ClientID  string
	Scopes    []string
	Resources []string
	// The client's public metadata.
	ClientName string
	ClientURI  *string
	LogoURI    *string
}

func (r *repository) consents(ctx context.Context, where string, args ...any) ([]Consent, error) {
	var rows []consentRow
	if err := r.db.WithContext(ctx).Raw(`SELECT c.id, c.created_at, c.updated_at, c.client_id, to_jsonb(c.scopes) AS scopes, to_jsonb(c.resources) AS resources, k.name, k.client_uri, k.logo_uri
		FROM oauth_consents c JOIN oauth_clients k ON k.client_id = c.client_id WHERE `+where+` ORDER BY c.updated_at DESC, c.id`, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load oauth consents: %w", err)
	}
	out := make([]Consent, len(rows))
	for i, x := range rows {
		out[i] = Consent{ID: x.ID, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt, ClientID: x.ClientID, ClientName: x.Name, ClientURI: x.ClientURI, LogoURI: x.LogoURI}
		if err := decodeArrays(arrayColumn{x.Scopes, &out[i].Scopes}, arrayColumn{x.Resources, &out[i].Resources}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *repository) consent(ctx context.Context, userID uuid.UUID, clientID string) (*Consent, error) {
	list, err := r.consents(ctx, "c.user_id = ? AND c.client_id = ?", userID, clientID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// addConsent records that the user approved scopes and resource for the client, on top of what they approved before.
func (r *repository) addConsent(ctx context.Context, userID uuid.UUID, clientID string, scopes []string, resource string) error {
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO oauth_consents (user_id, client_id, scopes, resources) VALUES (?, ?, ?::text[], ?::text[])
		ON CONFLICT (user_id, client_id) DO UPDATE SET
			scopes = ARRAY(SELECT DISTINCT unnest(oauth_consents.scopes || EXCLUDED.scopes) ORDER BY 1),
			resources = ARRAY(SELECT DISTINCT unnest(oauth_consents.resources || EXCLUDED.resources) ORDER BY 1),
			updated_at = now()`,
		userID, clientID, textArray(scopes), textArray([]string{resource})).Error; err != nil {
		return fmt.Errorf("record oauth consent: %w", err)
	}
	return nil
}

// deleteConsent removes one of the user's consents and revokes every token the client holds for them. It is false when the user has no such consent.
func (r *repository) deleteConsent(ctx context.Context, userID, consentID uuid.UUID) (bool, error) {
	found := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var clientIDs []string
		if err := tx.Raw(`DELETE FROM oauth_consents WHERE id = ? AND user_id = ? RETURNING client_id`, consentID, userID).Scan(&clientIDs).Error; err != nil {
			return fmt.Errorf("delete oauth consent: %w", err)
		}
		if len(clientIDs) == 0 {
			return nil
		}
		found = true
		if err := tx.Exec(`UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL`, userID, clientIDs[0]).Error; err != nil {
			return fmt.Errorf("revoke refresh tokens: %w", err)
		}
		if err := tx.Exec(`UPDATE oauth_access_tokens SET revoked_at = now() WHERE user_id = ? AND client_id = ? AND revoked_at IS NULL`, userID, clientIDs[0]).Error; err != nil {
			return fmt.Errorf("revoke access tokens: %w", err)
		}
		return nil
	})
	return found, err
}

// ---- tokens ----

// grant is what one issued token carries.
type grant struct {
	ClientID  string
	UserID    uuid.UUID
	Scopes    []string
	Resources []string
	AuthTime  time.Time
}

func (r *repository) insertAccessToken(ctx context.Context, tx *gorm.DB, g grant, refreshFamily *uuid.UUID, expiresAt time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.WithContext(ctx).Raw(`INSERT INTO oauth_access_tokens (expires_at, client_id, user_id, scopes, resources, refresh_family_id) VALUES (?, ?, ?, ?::text[], ?::text[], ?) RETURNING id`,
		expiresAt, g.ClientID, g.UserID, textArray(g.Scopes), textArray(g.Resources), refreshFamily).Row().Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("insert access token: %w", err)
	}
	return id, nil
}

func (r *repository) insertRefreshToken(ctx context.Context, tx *gorm.DB, g grant, familyID uuid.UUID, tokenHash []byte, expiresAt time.Time) error {
	if err := tx.WithContext(ctx).Exec(`INSERT INTO oauth_refresh_tokens (expires_at, token_hash, family_id, client_id, user_id, scopes, resources, auth_time) VALUES (?, ?, ?, ?, ?, ?::text[], ?::text[], ?)`,
		expiresAt, tokenHash, familyID, g.ClientID, g.UserID, textArray(g.Scopes), textArray(g.Resources), g.AuthTime).Error; err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

type tokenRow struct {
	ID        uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
	FamilyID  *uuid.UUID
	ClientID  string
	UserID    uuid.UUID
	Scopes    []byte
	Resources []byte
	AuthTime  *time.Time
	RevokedAt *time.Time
}

// storedToken is an issued access or refresh token.
type storedToken struct {
	ID        uuid.UUID
	CreatedAt time.Time
	ExpiresAt time.Time
	FamilyID  uuid.UUID
	RevokedAt *time.Time
	grant
}

func (x tokenRow) decode() (*storedToken, error) {
	t := &storedToken{ID: x.ID, CreatedAt: x.CreatedAt, ExpiresAt: x.ExpiresAt, RevokedAt: x.RevokedAt, grant: grant{ClientID: x.ClientID, UserID: x.UserID}}
	if x.FamilyID != nil {
		t.FamilyID = *x.FamilyID
	}
	if x.AuthTime != nil {
		t.AuthTime = *x.AuthTime
	}
	if err := decodeArrays(arrayColumn{x.Scopes, &t.Scopes}, arrayColumn{x.Resources, &t.Resources}); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *repository) findToken(ctx context.Context, query string, args ...any) (*storedToken, error) {
	var rows []tokenRow
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].decode()
}

// refreshToken is a refresh token by its hash, expired or revoked ones included.
func (r *repository) refreshToken(ctx context.Context, tokenHash []byte) (*storedToken, error) {
	return r.findToken(ctx, `SELECT id, created_at, expires_at, family_id, client_id, user_id, to_jsonb(scopes) AS scopes, to_jsonb(resources) AS resources, auth_time, revoked_at
		FROM oauth_refresh_tokens WHERE token_hash = ?`, tokenHash)
}

func (r *repository) refreshTokenByID(ctx context.Context, id uuid.UUID) (*storedToken, error) {
	return r.findToken(ctx, `SELECT id, created_at, expires_at, family_id, client_id, user_id, to_jsonb(scopes) AS scopes, to_jsonb(resources) AS resources, auth_time, revoked_at
		FROM oauth_refresh_tokens WHERE id = ?`, id)
}

func (r *repository) accessToken(ctx context.Context, id uuid.UUID) (*storedToken, error) {
	return r.findToken(ctx, `SELECT id, created_at, expires_at, refresh_family_id AS family_id, client_id, user_id, to_jsonb(scopes) AS scopes, to_jsonb(resources) AS resources, revoked_at
		FROM oauth_access_tokens WHERE id = ?`, id)
}

// spendRefreshToken marks a refresh token used. It is false when it was already used or revoked: the caller is replaying it.
func (r *repository) spendRefreshToken(ctx context.Context, tx *gorm.DB, id uuid.UUID) (bool, error) {
	res := tx.WithContext(ctx).Exec(`UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE id = ? AND revoked_at IS NULL`, id)
	if res.Error != nil {
		return false, fmt.Errorf("spend refresh token: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

// revokeFamily revokes every refresh token of one authorization and the access tokens issued from them.
func (r *repository) revokeFamily(ctx context.Context, familyID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE oauth_refresh_tokens SET revoked_at = now() WHERE family_id = ? AND revoked_at IS NULL`, familyID).Error; err != nil {
			return fmt.Errorf("revoke refresh token family: %w", err)
		}
		if err := tx.Exec(`UPDATE oauth_access_tokens SET revoked_at = now() WHERE refresh_family_id = ? AND revoked_at IS NULL`, familyID).Error; err != nil {
			return fmt.Errorf("revoke access tokens of family: %w", err)
		}
		return nil
	})
}

func (r *repository) revokeAccessToken(ctx context.Context, id uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec(`UPDATE oauth_access_tokens SET revoked_at = now() WHERE id = ? AND revoked_at IS NULL`, id).Error; err != nil {
		return fmt.Errorf("revoke access token: %w", err)
	}
	return nil
}

// ---- users ----

type user struct {
	ID            uuid.UUID
	Name          string
	Email         string
	EmailVerified bool
	IsActive      bool
}

func (r *repository) user(ctx context.Context, id uuid.UUID) (*user, error) {
	var rows []user
	if err := r.db.WithContext(ctx).Raw(`SELECT id, name, email, email_verified, is_active FROM users WHERE id = ?`, id).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
