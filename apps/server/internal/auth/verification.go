package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/server/internal/platform/audit"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/server/internal/platform/secret"
)

// resendVerification mails a new verification link to an unverified account. It answers the same for an unknown or already verified address, so it cannot be used to probe which addresses are registered, and a send failure is only logged for the same reason. When the browser holds a valid sign-up intent for this account, the intent is renewed alongside the link, so the browser that signed up stays recognized for as long as the new link lives; an intent is never created here.
func (s *Service) resendVerification(ctx context.Context, email, intentToken string) (*issuedToken, error) {
	renewed, err := s.resend(ctx, email, intentToken)
	if err != nil {
		s.logFailure(ctx, audit.ActionVerificationEmailSent, "")
	}
	return renewed, err
}

func (s *Service) resend(ctx context.Context, email, intentToken string) (*issuedToken, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.AuthVerifyEmail); err != nil {
		return nil, err
	}
	acc, err := s.repo.accountByEmail(ctx, email)
	if err != nil || acc == nil || acc.EmailVerified {
		return nil, err
	}
	if !s.sendVerificationEmail(ctx, acc) {
		return nil, nil
	}
	s.logEvent(ctx, audit.Event{Action: audit.ActionVerificationEmailSent, TargetType: audit.TargetUser, TargetID: acc.ID.String(), Status: audit.StatusSuccess})

	if intentToken == "" {
		return nil, nil
	}
	var renewed *issuedToken
	err = s.repo.transaction(ctx, func(tx *Repository) error {
		old, err := tx.consumeUserVerification(ctx, purposeSignupIntent, secret.HashToken(intentToken), acc.ID, s.now())
		if err != nil || old == nil {
			return err
		}
		t, err := s.issueVerification(ctx, tx, purposeSignupIntent, acc.ID, normalizeEmail(acc.Email), signupIntentTTL)
		renewed = &t
		return err
	})
	if err != nil {
		// The link already went out; failing to renew the intent costs only the automatic sign-in.
		s.log.ErrorContext(ctx, "Failed to renew the sign-up intent on resend", "err", err, "userId", acc.ID.String())
		return nil, nil
	}
	return renewed, nil
}

// verifyEmailResult says whether the request ended signed in, and carries the session to set when this request created it.
type verifyEmailResult struct {
	SignedIn bool
	session  *issuedToken
}

// Reasons a verification does not sign the browser in (forma2's TWithheldReason).
const (
	withheldAbsent      = "absent"
	withheldInvalid     = "invalid"
	withheldOtherUser   = "other_user"
	withheldGrantFailed = "grant_failed"
)

// verifyEmail confirms an address with an emailed token.
//
// Verifying proves the clicker owns the mailbox, not that they chose the account's password, so it signs in only the browser that created the account (ENG-2562): the one holding a sign-up intent for the same user. Anyone else, a mail scanner or another device included, is told to sign in; otherwise an attacker could register a victim's address with a password of their choosing and be handed the victim's session when the victim clicks the link.
func (s *Service) verifyEmail(ctx context.Context, token, intentToken string, actor *httpapi.Actor, meta clientMeta) (verifyEmailResult, error) {
	if err := s.limiter.ApplyIP(ctx, ratelimit.BAVerifyEmail); err != nil {
		return verifyEmailResult{}, err
	}
	v, err := s.repo.findVerification(ctx, purposeEmailVerification, secret.HashToken(token))
	if err != nil {
		return verifyEmailResult{}, err
	}
	if v == nil || !v.UserID.Valid {
		return verifyEmailResult{}, errInvalidToken()
	}
	if !v.ExpiresAt.After(s.now()) {
		return verifyEmailResult{}, errTokenExpired()
	}

	justVerified := false
	// The address becomes a Brevo contact with its first verification, as forma2's afterEmailVerification hook made it.
	err = s.jobTransaction(ctx, func(tx *Repository, jtx *jobs.Tx) error {
		acc, err := tx.lockAccount(ctx, v.UserID.UUID)
		if err != nil {
			return err
		}
		// A link for an address the account no longer has proves nothing about the current one.
		if acc == nil || normalizeEmail(acc.Email) != v.Email {
			return errInvalidToken()
		}
		if acc.EmailVerified {
			return nil
		}
		if _, err := tx.consumeVerification(ctx, purposeEmailVerification, secret.HashToken(token), s.now()); err != nil {
			return err
		}
		justVerified = true
		if err := tx.markEmailVerified(ctx, acc.ID, s.now()); err != nil {
			return err
		}
		return s.contacts.Verified(ctx, jtx, acc.ID, acc.Email)
	})
	if err != nil {
		return verifyEmailResult{}, err
	}
	userID := v.UserID.UUID
	if justVerified {
		analytics.Capture(userID.String(), "user_email_confirmed", nil, analytics.Groups{})
	}
	alreadySignedIn := actor != nil && actor.UserID == userID
	// An already verified address short-circuits: the link was used before, and only that first use may sign anyone in.
	if !justVerified || alreadySignedIn {
		return verifyEmailResult{SignedIn: alreadySignedIn}, nil
	}

	sess, reason := s.grantVerificationSession(ctx, userID, intentToken, meta)
	if reason != "" {
		s.log.InfoContext(ctx, "Withheld the post-verification session", "userId", userID.String(), "reason", reason)
		s.logEvent(ctx, audit.Event{
			Action: audit.ActionUpdated, TargetType: audit.TargetUser, TargetID: userID.String(), ActorID: userID.String(), Status: audit.StatusSuccess,
			NewObject: map[string]any{"verificationSessionWithheldMarker": true, "reason": reason},
		})
		return verifyEmailResult{}, nil
	}
	s.finalizeSignIn(ctx, userID, signInMethodPassword)
	return verifyEmailResult{SignedIn: true, session: sess}, nil
}

// grantVerificationSession signs the browser in when its intent token names the verified user, consuming the intent. Otherwise it returns why not; nothing here fails the verification itself.
func (s *Service) grantVerificationSession(ctx context.Context, userID uuid.UUID, intentToken string, meta clientMeta) (*issuedToken, string) {
	if intentToken == "" {
		return nil, withheldAbsent
	}
	var sess *issuedToken
	reason := ""
	err := s.repo.transaction(ctx, func(tx *Repository) error {
		intent, err := tx.findVerification(ctx, purposeSignupIntent, secret.HashToken(intentToken))
		if err != nil {
			return err
		}
		switch {
		case intent == nil || intent.ConsumedAt.Valid || !intent.ExpiresAt.After(s.now()) || !intent.UserID.Valid:
			reason = withheldInvalid
			return nil
		case intent.UserID.UUID != userID:
			reason = withheldOtherUser
			return nil
		}
		if _, err := tx.consumeUserVerification(ctx, purposeSignupIntent, secret.HashToken(intentToken), userID, s.now()); err != nil {
			return err
		}
		acc, err := tx.lockAccount(ctx, userID)
		if err != nil {
			return err
		}
		// Session creation keeps the deactivation gate every sign-in has.
		if acc == nil || !acc.IsActive {
			reason = withheldGrantFailed
			return nil
		}
		t, err := s.sessions.create(ctx, tx.db, userID, meta)
		sess = &t
		return err
	})
	if err != nil {
		s.log.ErrorContext(ctx, "Post-verification sign-in failed", "err", err, "userId", userID.String())
		return nil, withheldGrantFailed
	}
	if reason != "" {
		return nil, reason
	}
	return sess, ""
}
