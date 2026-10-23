package forma2import

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/yldm-tech/forma/apps/server/internal/auth"
	"github.com/yldm-tech/forma/apps/server/internal/integration"
)

// identityProviders maps forma2's Account.provider (and User.identityProvider) onto user_identities.provider.
var identityProviders = map[string]string{
	"credential": "credential", "google": "google", "github": "github", "openid": "openid", "saml": "saml",
	"azuread": "azuread", "azure-ad": "azuread",
}

// identityHook is one way a user signs in. A password identity's subject is the user's own id, as this server writes it; an external identity keeps the provider's account id. Every identity has the empty issuer this server looks identities up by, and only a password identity keeps a hash.
func identityHook(r *row) (bool, error) {
	provider, ok := identityProviders[r.str("provider")]
	if !ok {
		return false, fmt.Errorf("sign-in identity %s has provider %q, which this server does not support", r.str("id"), r.str("provider"))
	}
	r.set("provider", provider)
	if provider == "credential" {
		r.set("subject", r.newID("user_id").String())
		return true, nil
	}
	if r.str("subject") == "" {
		return false, fmt.Errorf("sign-in identity %s of provider %s has no account id", r.str("id"), provider)
	}
	r.set("password_hash", nil)
	return true, nil
}

// newestInviteHook keeps the first invite per organization and address; the source query orders the newest first.
func newestInviteHook() func(r *row) (bool, error) {
	last := ""
	return func(r *row) (bool, error) {
		address := r.str("x_address")
		if address == last {
			return false, nil
		}
		last = address
		return true, nil
	}
}

func segmentHook(r *row) (bool, error) {
	if r.flag("x_private_survey") {
		r.set("title", resolveString(r.str("title")))
	}
	filters, err := rewriteSegmentFilters(r.str("filters"))
	if err != nil {
		return false, fmt.Errorf("segment %s: %w", r.str("id"), err)
	}
	r.set("filters", filters)
	return true, nil
}

// surveyHook fits the display percentage into numeric(5,2) and gives a survey that has only forma2's legacy questions the blocks they convert to.
func surveyHook(r *row) (bool, error) {
	if p, ok := r.get("display_percentage").(pgtype.Numeric); ok && p.Valid {
		f, err := p.Float64Value()
		if err != nil {
			return false, err
		}
		if f.Float64 < 0 || f.Float64 > 100 {
			return false, fmt.Errorf("survey %s has a display percentage of %v, outside 0 to 100", r.str("id"), f.Float64)
		}
	}
	if strings.TrimSpace(r.str("blocks")) == "[]" {
		var questions []json.RawMessage
		if err := json.Unmarshal([]byte(r.str("x_questions")), &questions); err == nil && len(questions) > 0 {
			blocks, err := questionsToBlocks(r.str("id"), r.str("x_questions"), r.str("endings"))
			if err != nil {
				return false, fmt.Errorf("survey %s: %w", r.str("id"), err)
			}
			r.set("blocks", blocks)
		}
	}
	return true, nil
}

func webhookHook(r *row) (bool, error) {
	raw, _ := r.get("triggers").([]any)
	triggers := make([]string, 0, len(raw))
	for _, t := range raw {
		s, ok := t.(string)
		if !ok {
			return false, fmt.Errorf("webhook %s has a trigger that is not text", r.str("id"))
		}
		triggers = append(triggers, s)
	}
	r.set("triggers", triggers)
	return true, nil
}

func integrationConfigHook(r *row) (bool, error) {
	links, err := integrationLinks(r.str("config"))
	if err != nil {
		return false, fmt.Errorf("integration %s: %w", r.str("id"), err)
	}
	r.set("config", links)
	return true, nil
}

// sealed are the ciphertexts a target row already holds, by the row's id: a re-run keeps them when they open to what it would write, since sealing again would change them every time.
type sealed map[uuid.UUID][2][]byte

func (im *importer) loadTOTP(ctx context.Context) error {
	im.existing = sealed{}
	rows, err := im.tx.Query(ctx, `SELECT user_id, secret_ciphertext, backup_codes_ciphertext FROM user_totp`)
	if err != nil {
		return fmt.Errorf("read user_totp: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var a, b []byte
		if err := rows.Scan(&id, &a, &b); err != nil {
			return err
		}
		im.existing[id] = [2][]byte{a, b}
	}
	return rows.Err()
}

func (im *importer) loadCredentials(ctx context.Context) error {
	im.existing = sealed{}
	rows, err := im.tx.Query(ctx, `SELECT integration_id, ciphertext FROM integration_credentials`)
	if err != nil {
		return fmt.Errorf("read integration_credentials: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var a []byte
		if err := rows.Scan(&id, &a); err != nil {
			return err
		}
		im.existing[id] = [2][]byte{a}
	}
	return rows.Err()
}

// betterAuthTOTPHook re-encrypts a TwoFactor row: Better Auth's XChaCha20-Poly1305 under its secret, opened, then sealed with ENCRYPTION_KEY for the row it belongs to.
func (im *importer) betterAuthTOTPHook(r *row) (bool, error) {
	m, err := auth.ImportBetterAuthTwoFactor(im.opts.BetterAuth, r.str("x_secret"), r.str("x_backup_codes"))
	if err != nil {
		return false, fmt.Errorf("two-factor secret of user %s (is NEXTAUTH_SECRET, or BETTER_AUTH_SECRET, forma2's value?): %w", r.str("user_id"), err)
	}
	return true, im.sealTOTP(r, m)
}

// legacyTOTPHook re-encrypts forma2's legacy User.twoFactorSecret and User.backupCodes, encrypted with forma2's ENCRYPTION_KEY.
func (im *importer) legacyTOTPHook(r *row) (bool, error) {
	m, err := auth.ImportForma2TwoFactor(im.opts.Forma2Key, r.str("x_secret"), r.str("x_backup_codes"))
	if err != nil {
		return false, fmt.Errorf("legacy two-factor secret of user %s (is ENCRYPTION_KEY forma2's value?): %w", r.str("user_id"), err)
	}
	return true, im.sealTOTP(r, m)
}

func (im *importer) sealTOTP(r *row, m auth.TOTPMaterial) error {
	userID := r.newID("user_id")
	if prev, ok := im.existing[userID]; ok {
		if have, err := auth.OpenTOTP(im.opts.Cipher, userID, prev[0], prev[1]); err == nil && bytes.Equal(have.Key, m.Key) && slices.Equal(have.BackupCodes, m.BackupCodes) {
			r.set("secret_ciphertext", prev[0])
			r.set("backup_codes_ciphertext", prev[1])
			return nil
		}
	}
	secretCT, codesCT, err := auth.SealTOTP(im.opts.Cipher, userID, m)
	if err != nil {
		return err
	}
	r.set("secret_ciphertext", secretCT)
	r.set("backup_codes_ciphertext", codesCT)
	return nil
}

// credentialsHook moves config.key and config.email into sealed credentials, decrypting the secret fields forma2 encrypted at rest.
func (im *importer) credentialsHook(r *row) (bool, error) {
	key, undecryptable, err := decryptCredentialKey(r.str("x_key"), im.opts.Forma2Key)
	if err != nil {
		return false, fmt.Errorf("integration %s: %w", r.str("integration_id"), err)
	}
	if len(undecryptable) > 0 {
		im.warn("integration %s: %s did not decrypt with ENCRYPTION_KEY and are kept as stored; the integration fails at its provider until it is reconnected", r.str("integration_id"), strings.Join(undecryptable, ", "))
	}
	creds := integration.Credentials{Key: key, Email: r.str("x_email")}
	integrationID := r.newID("integration_id")
	if prev, ok := im.existing[integrationID]; ok {
		if have, err := integration.OpenCredentials(im.opts.Cipher, integrationID, prev[0]); err == nil && have.Email == creds.Email && sameJSON(have.Key, creds.Key) {
			r.set("ciphertext", prev[0])
			return true, nil
		}
	}
	ciphertext, err := integration.SealCredentials(im.opts.Cipher, integrationID, creds)
	if err != nil {
		return false, err
	}
	r.set("ciphertext", ciphertext)
	return true, nil
}
