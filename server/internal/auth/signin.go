package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// signInResult is either a session or, for a user with two-factor authentication, a pending sign-in the second factor completes.
type signInResult struct {
	session *issuedToken
	pending *issuedToken
}

// signIn checks an email and password the way Better Auth's /sign-in/email did for forma2. Rejections are audited, throttled per address.
func (s *Service) signIn(ctx context.Context, email, password string, meta clientMeta) (signInResult, error) {
	res, err := s.checkCredentials(ctx, email, password, meta)
	if err != nil {
		s.auditSignInFailure(ctx, email, err)
	}
	return res, err
}

func (s *Service) checkCredentials(ctx context.Context, email, password string, meta clientMeta) (signInResult, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BASignIn); err != nil {
		return signInResult{}, err
	}
	if s.cfg.EmailAuthDisabled {
		return signInResult{}, errEmailPasswordDisabled()
	}
	acc, err := s.repo.accountByEmail(ctx, email)
	if err != nil {
		return signInResult{}, err
	}
	// An unknown address and an account without a password still pay for one bcrypt comparison, so the response time does not tell them apart from a wrong password.
	hash := ""
	if acc != nil && acc.hasPassword() {
		hash = acc.PasswordHash.String
	}
	ok, err := secret.CheckPassword(hash, password)
	if err != nil {
		return signInResult{}, err
	}
	if acc == nil || !ok {
		return signInResult{}, errInvalidCredentials()
	}
	// Only a caller who knows the password learns that the address is unverified; the fresh link replaces one that may have expired.
	if s.requireEmailVerification() && !acc.EmailVerified {
		s.sendVerificationEmail(ctx, acc)
		return signInResult{}, errEmailNotVerified()
	}
	if !acc.IsActive {
		return signInResult{}, errAccountInactive()
	}
	if acc.TwoFactorEnabled {
		pending, err := s.issueVerification(ctx, s.repo, purposeTwoFactor, acc.ID, normalizeEmail(acc.Email), twoFactorPendingTTL)
		if err != nil {
			return signInResult{}, err
		}
		return signInResult{pending: &pending}, nil
	}
	sess, err := s.sessions.create(ctx, s.repo.db, acc.ID, meta)
	if err != nil {
		return signInResult{}, err
	}
	s.finalizeSignIn(ctx, acc.ID, signInMethodPassword)
	return signInResult{session: &sess}, nil
}

// currentAccount is the signed-in user.
func (s *Service) currentAccount(ctx context.Context, actor httpapi.Actor) (*account, error) {
	acc, err := s.repo.accountByID(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, apperr.NotAuthenticated("Not authenticated")
	}
	return acc, nil
}

// signInRejections are the outcomes forma2 audited as failed authentication attempts; a rate-limited request never reached its audit hook.
var signInRejections = map[string]bool{
	CodeInvalidEmailOrPassword: true,
	CodeEmailNotVerified:       true,
	CodeAccountInactive:        true,
	CodeEmailPasswordDisabled:  true,
}

// auditSignInFailure records a rejected sign-in against a hashed identity derived from the address, never the address itself.
func (s *Service) auditSignInFailure(ctx context.Context, email string, err error) {
	e, ok := apperr.As(err)
	if !ok || !signInRejections[e.Code] || !s.failures.shouldRecord(ctx, email) {
		return
	}
	actor := auditIdentifier(email, "email")
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionAuthenticationAttempted, TargetType: audit.TargetUser, TargetID: actor, ActorID: actor, Status: audit.StatusFailure,
		NewObject: map[string]string{"failureReason": e.Code, "provider": "credentials", "authMethod": "password"},
	})
}

// signOutReasons are the reasons forma2's logSignOutAction accepted.
var signOutReasons = []string{"user_initiated", "account_deletion", "email_change", "session_timeout", "forced_logout", "password_reset"}

// signOut ends the request's session and records why. The organization is kept in the record only when the user belongs to it.
func (s *Service) signOut(ctx context.Context, actor httpapi.Actor, reason string, organizationID *uuid.UUID) error {
	if reason == "" {
		reason = "user_initiated"
	}
	details := map[string]any{"provider": "session", "authMethod": "sign_out", "reason": reason}
	if organizationID != nil {
		member, err := s.repo.isMember(ctx, actor.UserID, *organizationID)
		if err != nil {
			return err
		}
		if member {
			details["organizationId"] = organizationID.String()
		}
	}
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionUserSignedOut, TargetType: audit.TargetUser, TargetID: actor.UserID.String(), ActorID: actor.UserID.String(),
		Status: audit.StatusSuccess, NewObject: details,
	})
	return s.sessions.delete(ctx, actor.SessionID)
}
