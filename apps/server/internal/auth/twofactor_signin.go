package auth

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// pendingSignInMaxAttempts is Better Auth's per-challenge budget: after five refused codes the pending sign-in is spent and the password must be entered again.
const pendingSignInMaxAttempts = 5

// secondFactor is the kind of code a sign-in's second step was given.
type secondFactor int

const (
	factorTOTP secondFactor = iota
	factorBackupCode
)

func (f secondFactor) limitBucket() string {
	if f == factorBackupCode {
		return limitVerifyBackupCode
	}
	return limitVerifyTOTP
}

func (f secondFactor) notEnabled() error {
	if f == factorBackupCode {
		return errBackupCodesNotEnabled()
	}
	return errTOTPNotEnabled()
}

func (f secondFactor) refused() error {
	if f == factorBackupCode {
		return &apperr.Error{Status: http.StatusUnauthorized, Code: CodeInvalidBackupCode, Message: "Invalid backup code"}
	}
	return &apperr.Error{Status: http.StatusUnauthorized, Code: CodeInvalidCode, Message: "Invalid code"}
}

// verifySignIn is the second step of a password sign-in: token is the forma.two_factor cookie the password step set. A code from the authenticator app, or a backup code (spent by its use), completes the sign-in with a session.
//
// The order and limits are Better Auth's verify-totp and verify-backup-code as forma2 ran them: the per-address limit, then a live pending sign-in, an enabled second factor, the account lockout, and the pending sign-in's budget of five refused codes, before the code is checked. A refused code counts against both the budget and the lockout; a correct one clears the lockout count. A value that cannot be decrypted fails the request without charging an attempt.
func (t *TwoFactor) verifySignIn(ctx context.Context, token string, factor secondFactor, code string, meta clientMeta) (issuedToken, error) {
	if err := t.limit(ctx, factor.limitBucket()); err != nil {
		return issuedToken{}, err
	}
	if token == "" {
		return issuedToken{}, errInvalidTwoFactorCookie()
	}
	hash := secret.HashToken(token)
	now := t.svc.now()
	pending, err := t.svc.repo.findVerification(ctx, purposeTwoFactor, hash)
	if err != nil {
		return issuedToken{}, err
	}
	if pending == nil || pending.ConsumedAt.Valid || !pending.ExpiresAt.After(now) || !pending.UserID.Valid {
		return issuedToken{}, errInvalidTwoFactorCookie()
	}

	var sess issuedToken
	// refusal is an outcome whose bookkeeping (a counted failure, a spent sign-in) must be committed before it is returned.
	var refusal error
	err = t.svc.repo.transaction(ctx, func(tx *Repository) error {
		// The account is locked first, the order a password reset takes, which revokes pending sign-ins while holding it.
		acc, err := tx.lockAccount(ctx, pending.UserID.UUID)
		if err != nil {
			return err
		}
		if acc == nil {
			return errInvalidTwoFactorCookie()
		}
		p, err := tx.lockPendingSignIn(ctx, hash, now)
		if err != nil {
			return err
		}
		if p == nil || p.UserID != acc.ID {
			return errInvalidTwoFactorCookie()
		}
		row, err := tx.lockTOTP(ctx, acc.ID)
		if err != nil {
			return err
		}
		if row == nil || !row.Verified {
			return factor.notEnabled()
		}
		if row.LockedUntil.Valid {
			if left := row.LockedUntil.Time.Sub(now); left > 0 {
				return errSecondFactorLocked(int(math.Ceil(left.Seconds())))
			}
			if err := tx.resetTOTPFailures(ctx, acc.ID, now); err != nil {
				return err
			}
		}
		if p.Attempts >= pendingSignInMaxAttempts {
			refusal = errTooManyTwoFactorAttempts()
			return tx.consumePendingSignIn(ctx, p.ID, now)
		}
		ok, err := t.checkSecondFactor(ctx, tx, factor, row, code, now)
		if err != nil {
			return err
		}
		if !ok {
			refusal = factor.refused()
			if err := tx.countPendingSignInFailure(ctx, p.ID); err != nil {
				return err
			}
			return tx.recordTOTPFailure(ctx, acc.ID, now)
		}
		if err := tx.resetTOTPFailures(ctx, acc.ID, now); err != nil {
			return err
		}
		if err := tx.consumePendingSignIn(ctx, p.ID, now); err != nil {
			return err
		}
		if !acc.IsActive {
			return errAccountInactive()
		}
		sess, err = t.svc.sessions.create(ctx, tx.db, acc.ID, meta)
		return err
	})
	if err != nil {
		return issuedToken{}, err
	}
	if refusal != nil {
		return issuedToken{}, refusal
	}
	t.svc.finalizeSignIn(ctx, pending.UserID.UUID, signInMethodPassword)
	return sess, nil
}

// checkSecondFactor checks the code against the locked row. A matching backup code is removed in the same transaction, so it works once.
func (t *TwoFactor) checkSecondFactor(ctx context.Context, tx *Repository, factor secondFactor, row *totpRow, code string, now time.Time) (bool, error) {
	if factor == factorTOTP {
		key, err := openTOTPKey(t.cipher, row.UserID, row.SecretCiphertext)
		if err != nil {
			return false, err
		}
		return checkTOTP(key, code, now, signInWindow)
	}
	codes, err := openBackupCodes(t.cipher, row.UserID, row.BackupCodesCiphertext)
	if err != nil {
		return false, err
	}
	i := matchBackupCode(codes, code)
	if i < 0 {
		return false, nil
	}
	sealed, err := sealBackupCodes(t.cipher, row.UserID, append(codes[:i:i], codes[i+1:]...))
	if err != nil {
		return false, err
	}
	return true, tx.replaceBackupCodes(ctx, row.UserID, sealed, now)
}
