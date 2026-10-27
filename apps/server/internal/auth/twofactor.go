package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// TwoFactor is TOTP two-factor authentication with backup codes: enrolment and removal from the account settings (forma2's modules/two-factor-auth), and the second step of a password sign-in (Better Auth's twoFactor plugin as forma2 configured it).
//
// forma2 kept two copies of a user's second factor, its own columns on User and Better Auth's TwoFactor row, which could disagree (a backup code spent at sign-in still disabled 2FA). Here there is one: the user_totp row, whose secret and backup codes are sealed with ENCRYPTION_KEY. A verified row is what turns two-factor authentication on.
type TwoFactor struct {
	svc     *Service
	cipher  *secret.Cipher
	cookies cookieSettings
}

func NewTwoFactor(cfg config.Config, svc *Service, cipher *secret.Cipher) *TwoFactor {
	return &TwoFactor{svc: svc, cipher: cipher, cookies: newCookieSettings(cfg)}
}

// Rate-limit buckets, one per endpoint as Better Auth kept one per /two-factor/* path. forma2's settings actions had no limit; their endpoints here take the same per-address limit, since each checks a password or a code.
const (
	limitTwoFactorSetup   = "setup"
	limitTwoFactorEnable  = "enable"
	limitTwoFactorDisable = "disable"
	limitVerifyTOTP       = "verify-totp"
	limitVerifyBackupCode = "verify-backup-code"
)

func (t *TwoFactor) limit(ctx context.Context, endpoint string) error {
	return t.svc.limiter.Apply(ctx, ratelimit.BATwoFactor, endpoint+":"+ratelimit.IPIdentifier(ctx))
}

// logChange records a settings change the way forma2's withAuditLogging("updated", "twoFactorAuth") did: a success with what changed, or a failure for any error.
func (t *TwoFactor) logChange(ctx context.Context, userID uuid.UUID, change string, err error) {
	e := audit.Event{Action: audit.ActionUpdated, TargetType: audit.TargetTwoFactorAuth, TargetID: userID.String(), Status: audit.StatusSuccess}
	if err != nil {
		e.Status = audit.StatusFailure
	} else {
		e.NewObject = map[string]string{"twoFactorAuth": change}
	}
	t.svc.logEvent(ctx, e)
}

// setup starts an enrolment: after the password check it stores a new secret and backup codes, unverified, and returns them for the authenticator app. Two-factor authentication stays off until enable confirms a code. An enabled second factor cannot be replaced this way; it must be disabled first, which needs a code.
func (t *TwoFactor) setup(ctx context.Context, actor httpapi.Actor, password string) (totpEnrolment, error) {
	res, err := t.startEnrolment(ctx, actor, password)
	t.logChange(ctx, actor.UserID, "setup", err)
	return res, err
}

func (t *TwoFactor) startEnrolment(ctx context.Context, actor httpapi.Actor, password string) (totpEnrolment, error) {
	if err := t.limit(ctx, limitTwoFactorSetup); err != nil {
		return totpEnrolment{}, err
	}
	acc, err := t.svc.currentAccount(ctx, actor)
	if err != nil {
		return totpEnrolment{}, err
	}
	if acc.ssoOnly() {
		return totpEnrolment{}, errThirdPartyLogin()
	}
	if acc.TwoFactorEnabled {
		return totpEnrolment{}, errTwoFactorAlreadyEnabled()
	}
	if err := t.checkPassword(acc, password); err != nil {
		return totpEnrolment{}, err
	}

	key := make([]byte, totpKeySize)
	if _, err := rand.Read(key); err != nil {
		return totpEnrolment{}, fmt.Errorf("generate totp key: %w", err)
	}
	codes, err := newBackupCodes()
	if err != nil {
		return totpEnrolment{}, err
	}
	keyURI, dataURI, err := enrolmentLinks(key, acc.Email)
	if err != nil {
		return totpEnrolment{}, err
	}
	sealedKey, sealedCodes, err := SealTOTP(t.cipher, acc.ID, TOTPMaterial{Key: key, BackupCodes: codes})
	if err != nil {
		return totpEnrolment{}, err
	}
	err = t.svc.repo.transaction(ctx, func(tx *Repository) error {
		// The row lock orders this against a concurrent enable: a code confirmed for the old secret must not turn on the new one.
		row, err := tx.lockTOTP(ctx, acc.ID)
		if err != nil {
			return err
		}
		if row != nil && row.Verified {
			return errTwoFactorAlreadyEnabled()
		}
		return tx.putPendingTOTP(ctx, acc.ID, sealedKey, sealedCodes, t.svc.now())
	})
	if err != nil {
		return totpEnrolment{}, err
	}
	return totpEnrolment{Secret: base32NoPadding.EncodeToString(key), KeyURI: keyURI, DataURI: dataURI, BackupCodes: codes}, nil
}

// checkPassword is forma2's verifyUserPassword: a user without a password cannot confirm with one.
func (t *TwoFactor) checkPassword(acc *account, password string) error {
	if !acc.hasPassword() {
		return errPasswordNotSet()
	}
	ok, err := secret.CheckPassword(acc.PasswordHash.String, password)
	if err != nil {
		return err
	}
	if !ok {
		return errIncorrectPassword()
	}
	return nil
}

const (
	messageTwoFactorEnabled  = "Two factor authentication enabled"
	messageTwoFactorDisabled = "Two factor authentication disabled"
)

// enable confirms an enrolment with a code from the authenticator app and turns two-factor authentication on. Every other session of the user, and every OAuth grant, is signed out in the same transaction: a session opened with the password alone, possibly by someone who stole it, would otherwise outlive the switch to a second factor. The current session stays, as in forma2.
func (t *TwoFactor) enable(ctx context.Context, actor httpapi.Actor, code string) error {
	err := t.confirmEnrolment(ctx, actor, code)
	t.logChange(ctx, actor.UserID, "enabled", err)
	return err
}

func (t *TwoFactor) confirmEnrolment(ctx context.Context, actor httpapi.Actor, code string) error {
	if err := t.limit(ctx, limitTwoFactorEnable); err != nil {
		return err
	}
	acc, err := t.svc.currentAccount(ctx, actor)
	if err != nil {
		return err
	}
	if acc.ssoOnly() {
		return errThirdPartyLogin()
	}
	if !acc.hasPassword() {
		return apperr.InvalidInput(CodePasswordNotSet, "User does not have a password set")
	}
	return t.svc.repo.transaction(ctx, func(tx *Repository) error {
		row, err := tx.lockTOTP(ctx, acc.ID)
		if err != nil {
			return err
		}
		if row != nil && row.Verified {
			return errTwoFactorAlreadyEnabled()
		}
		if row == nil {
			return apperr.InvalidInput(CodeTwoFactorSetupIncomplete, "Two factor setup has not been completed")
		}
		ok, err := t.acceptTOTP(ctx, tx, row, code, t.svc.now(), settingsWindow)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.InvalidInput(CodeInvalidCode, "Invalid code")
		}
		if err := tx.markTOTPVerified(ctx, acc.ID, t.svc.now()); err != nil {
			return err
		}
		_, err = t.svc.sessions.endOtherAccess(ctx, tx.db, acc.ID, actor.SessionID)
		return err
	})
}

// disable turns two-factor authentication off after the password and either a backup code or a current code; a backup code, when given, is the one checked. The secret and the remaining backup codes are deleted, and every OAuth grant is revoked with them, as enabling does: a change to how the account signs in ends the MCP clients authorized under the old one.
func (t *TwoFactor) disable(ctx context.Context, actor httpapi.Actor, password, code, backupCode string) error {
	err := t.removeSecondFactor(ctx, actor, password, code, backupCode)
	t.logChange(ctx, actor.UserID, "disabled", err)
	return err
}

func (t *TwoFactor) removeSecondFactor(ctx context.Context, actor httpapi.Actor, password, code, backupCode string) error {
	if err := t.limit(ctx, limitTwoFactorDisable); err != nil {
		return err
	}
	acc, err := t.svc.currentAccount(ctx, actor)
	if err != nil {
		return err
	}
	if !acc.TwoFactorEnabled {
		return errTwoFactorNotEnabled()
	}
	if acc.ssoOnly() {
		return errThirdPartyLogin()
	}
	if err := t.checkPassword(acc, password); err != nil {
		return err
	}
	return t.svc.repo.transaction(ctx, func(tx *Repository) error {
		row, err := tx.lockTOTP(ctx, acc.ID)
		if err != nil {
			return err
		}
		if row == nil || !row.Verified {
			return errTwoFactorNotEnabled()
		}
		if backupCode != "" {
			codes, err := openBackupCodes(t.cipher, acc.ID, row.BackupCodesCiphertext)
			if err != nil {
				return err
			}
			if matchBackupCode(codes, backupCode) < 0 {
				return apperr.InvalidInput(CodeInvalidBackupCode, "Incorrect backup code")
			}
		} else {
			if code == "" {
				return apperr.InvalidInput(CodeTwoFactorCodeRequired, "Two factor code required")
			}
			ok, err := t.acceptTOTP(ctx, tx, row, code, t.svc.now(), settingsWindow)
			if err != nil {
				return err
			}
			if !ok {
				return apperr.InvalidInput(CodeInvalidCode, "Invalid code")
			}
		}
		if err := tx.deleteTOTP(ctx, acc.ID); err != nil {
			return err
		}
		_, err = RevokeOAuthGrants(ctx, tx.db, acc.ID)
		return err
	})
}

// acceptTOTP checks code against the locked row's key and refuses a code from a step at or before the last one accepted for the row, as RFC 6238 section 5.2 asks: a code is good once, however long it stays inside the window, so one seen over a shoulder or replayed from a phished sign-in cannot complete a second. The accepted step is recorded in the caller's transaction.
func (t *TwoFactor) acceptTOTP(ctx context.Context, tx *Repository, row *totpRow, code string, now time.Time, w totpWindow) (bool, error) {
	key, err := openTOTPKey(t.cipher, row.UserID, row.SecretCiphertext)
	if err != nil {
		return false, err
	}
	step, ok, err := checkTOTP(key, code, now, w)
	if err != nil || !ok {
		return false, err
	}
	if row.LastUsedStep.Valid && step <= row.LastUsedStep.Int64 {
		return false, nil
	}
	return true, tx.setTOTPLastUsedStep(ctx, row.UserID, step, now)
}
