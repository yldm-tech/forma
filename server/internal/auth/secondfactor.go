package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// SecondFactor is the hand-off to the two-factor module. A sign-in of a user with two-factor authentication stops after the password check: instead of a session, the browser gets the forma.two_factor cookie holding a single-use token valid for ten minutes (Better Auth's two-factor cookie). The two-factor module reads whose sign-in is waiting with PendingUser, checks the code (and owns attempt counting and lockout), then calls Complete, which turns the pending sign-in into a session.
type SecondFactor struct {
	svc     *Service
	cookies cookieSettings
}

func NewSecondFactor(cfg config.Config, svc *Service) *SecondFactor {
	return &SecondFactor{svc: svc, cookies: newCookieSettings(cfg)}
}

// PendingUser returns the user whose sign-in waits for a second factor in this browser. Without a live pending sign-in it returns 401 invalid_two_factor_cookie.
func (f *SecondFactor) PendingUser(c *gin.Context) (uuid.UUID, error) {
	token, err := c.Cookie(f.cookies.twoFactor)
	if err != nil || token == "" {
		return uuid.Nil, errInvalidTwoFactorCookie()
	}
	ctx := c.Request.Context()
	v, err := f.svc.repo.findVerification(ctx, purposeTwoFactor, secret.HashToken(token))
	if err != nil {
		return uuid.Nil, err
	}
	if v == nil || v.ConsumedAt.Valid || !v.ExpiresAt.After(f.svc.now()) || !v.UserID.Valid {
		return uuid.Nil, errInvalidTwoFactorCookie()
	}
	return v.UserID.UUID, nil
}

// Complete consumes the pending sign-in and signs the user in: it sets the session cookie, clears the two-factor cookie and records the sign-in. Call it only after the second factor was verified for the user PendingUser returned. Of two concurrent completions only one succeeds; a user deactivated in the meantime gets 403 account_inactive and no session.
func (f *SecondFactor) Complete(c *gin.Context) error {
	token, err := c.Cookie(f.cookies.twoFactor)
	if err != nil || token == "" {
		return errInvalidTwoFactorCookie()
	}
	ctx := c.Request.Context()
	var userID uuid.UUID
	var sess issuedToken
	err = f.svc.repo.transaction(ctx, func(tx *Repository) error {
		v, err := tx.consumeVerification(ctx, purposeTwoFactor, secret.HashToken(token), f.svc.now())
		if err != nil {
			return err
		}
		if v == nil || !v.UserID.Valid {
			return errInvalidTwoFactorCookie()
		}
		acc, err := tx.lockAccount(ctx, v.UserID.UUID)
		if err != nil {
			return err
		}
		if acc == nil {
			return errInvalidTwoFactorCookie()
		}
		if !acc.IsActive {
			return errAccountInactive()
		}
		userID = acc.ID
		sess, err = f.svc.sessions.create(ctx, tx.db, acc.ID, clientMetaFrom(c))
		return err
	})
	if err != nil {
		return err
	}
	f.cookies.setSession(c, sess)
	f.cookies.clear(c, f.cookies.twoFactor)
	f.svc.finalizeSignIn(ctx, userID, signInMethodPassword)
	return nil
}
