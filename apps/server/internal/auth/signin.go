package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// signInResult is either a session or, for a user with two-factor authentication, a pending sign-in the second factor completes.
type signInResult struct {
	session *issuedToken
	pending *issuedToken
}

// pendingSignInPayload is what a sign-in waiting for its second factor carries, in the payload of its two_factor verification beside the attempts count verifySignIn keeps there.
type pendingSignInPayload struct {
	// Method is the authMethod the completed sign-in is recorded with. Empty, as a pending sign-in written before it was kept reads, is a password sign-in.
	Method string `json:"method,omitempty"`
	// Link is an external identity the sign-in links to the account once the second factor is accepted: SSO recovery's, since proving the mailbox alone must not attach a new way into an account that has two-factor authentication.
	Link *identityLink `json:"link,omitempty"`
}

// method is the authMethod of the completed sign-in.
func (p pendingSignInPayload) method() string {
	if p.Method == "" {
		return signInMethodPassword
	}
	return p.Method
}

// identityLink names an external identity by provider and subject.
type identityLink struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
}

// grantSignIn is the one way a sign-in that passed its first factor (a password, an identity provider, a recovery link) becomes a session, so no path can skip the second factor. An account with two-factor authentication gets a pending sign-in instead, which verifySignIn completes with a code, and what the sign-in would change on the account (then.Link) waits in it until the code is accepted; otherwise that change and the session are written now, in tx. The caller runs finalizeSignIn for a session once tx has committed.
func (s *Service) grantSignIn(ctx context.Context, tx *Repository, acc *account, meta clientMeta, then pendingSignInPayload) (signInResult, error) {
	if acc.TwoFactorEnabled {
		token, err := secret.NewToken()
		if err != nil {
			return signInResult{}, err
		}
		expires := s.now().Add(twoFactorPendingTTL)
		if err := tx.insertPayloadVerification(ctx, purposeTwoFactor, acc.ID, normalizeEmail(acc.Email), secret.HashToken(token), expires, then); err != nil {
			return signInResult{}, err
		}
		return signInResult{pending: &issuedToken{Value: token, ExpiresAt: expires, MaxAge: twoFactorPendingTTL}}, nil
	}
	if err := tx.linkIdentity(ctx, acc.ID, then.Link); err != nil {
		return signInResult{}, err
	}
	sess, err := s.sessions.create(ctx, tx.db, acc.ID, meta)
	if err != nil {
		return signInResult{}, err
	}
	return signInResult{session: &sess}, nil
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
	// The per-account limits are checked before the password costs a bcrypt comparison, and charged below only when it is wrong. An unknown address is limited the same way, so a 429 does not tell that an account exists.
	guesser := accountIdentifier(email) + ":" + ratelimit.IPIdentifier(ctx)
	for _, rule := range signInFailureRules {
		if err := s.limiter.AssertAvailable(ctx, rule, guesser); err != nil {
			return signInResult{}, err
		}
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
		for _, rule := range signInFailureRules {
			if _, err := s.limiter.Check(ctx, rule, guesser, 1); err != nil {
				return signInResult{}, err
			}
		}
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
	res, err := s.grantSignIn(ctx, s.repo, acc, meta, pendingSignInPayload{Method: signInMethodPassword})
	if err != nil {
		return signInResult{}, err
	}
	if res.session != nil {
		s.finalizeSignIn(ctx, acc.ID, signInMethodPassword)
	}
	return res, nil
}

// signInFailureRules count rejected passwords per account and client address (ratelimit.AuthSignInAccountIP): a short window that forgives a few typos and a daily cap on one source's guesses at one account.
var signInFailureRules = []ratelimit.Rule{ratelimit.AuthSignInAccountIP, ratelimit.AuthSignInAccountIPDaily}

// accountIdentifier keys a per-account limit: the SHA-256 hex digest of the normalized address, so addresses never appear in Valkey keys or logs.
func accountIdentifier(email string) string {
	sum := sha256.Sum256([]byte(normalizeEmail(email)))
	return hex.EncodeToString(sum[:])
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
