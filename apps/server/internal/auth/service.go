// Package auth is the identity module: sign-up, email verification, sign-in and sign-out, browser sessions, TOTP two-factor authentication, password reset and change, email change and account deletion, and single sign-on through Google, GitHub, Azure AD and OpenID Connect, with forma2's rules (apps/web/modules/auth, modules/account, modules/two-factor-auth, modules/sso, and the Better Auth configuration they ran on).
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/billing"
	"github.com/yldm-tech/forma/apps/server/internal/brevo"
	"github.com/yldm-tech/forma/apps/server/internal/platform/analytics"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/config"
	"github.com/yldm-tech/forma/apps/server/internal/platform/database"
	"github.com/yldm-tech/forma/apps/server/internal/platform/jobs"
	"github.com/yldm-tech/forma/apps/server/internal/platform/mailer"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/secret"
)

// Token lifetimes. Email verification links live an hour (forma2's EMAIL_VERIFICATION_TTL_SECONDS) and the sign-up intent as long, so the browser that signed up can still be recognized when the link is used; email change links a day; a pending second factor ten minutes (Better Auth's two-factor cookie); an emailed account deletion an hour.
const (
	emailVerificationTTL = time.Hour
	signupIntentTTL      = time.Hour
	emailChangeTTL       = 24 * time.Hour
	twoFactorPendingTTL  = 10 * time.Minute
	accountDeletionTTL   = time.Hour
)

// Pages of the SPA that emailed links open. Links already mailed carry these paths, so they do not change.
const (
	verifyEmailPath           = "/auth/verify-email"
	verificationRequestedPath = "/auth/verification-requested"
	resetPasswordPath         = "/auth/forgot-password/reset" //nolint:gosec // a page path, not a credential
	verifyEmailChangePath     = "/verify-email-change"
	deleteAccountPath         = "/auth/delete-account"
	loginPath                 = "/auth/login"
)

// cloudAccountDeletionSurveyURL is where Forma Cloud sends a user after deleting their account.
const cloudAccountDeletionSurveyURL = "https://forma.yldm.ai/s/clri52y3z8f221225wjdhsoo2"

// InviteRedeemer is the invite module's part in sign-up: whether a token is a live invite for the address signing up, and granting it to the account the sign-up creates, inside the sign-up's transaction. The invite module implements it; this package does not import that module.
type InviteRedeemer interface {
	MatchForSignup(ctx context.Context, token, email string) (bool, error)
	// RedeemForSignup returns ok false, having written nothing, when the invite is no longer usable. notify runs after the commit.
	RedeemForSignup(ctx context.Context, tx *gorm.DB, token string, userID uuid.UUID, name, email string) (organizationID uuid.UUID, notify func(context.Context), ok bool, err error)
}

// Service holds the credential rules. Handlers turn its results into cookies and responses.
type Service struct {
	cfg       config.Config
	repo      *Repository
	sessions  *Sessions
	invites   InviteRedeemer
	limiter   *ratelimit.Limiter
	mailer    *mailer.Mailer
	audit     *audit.Emitter
	breaches  *breachChecker
	turnstile *turnstileVerifier
	failures  *failureThrottle
	billing   *billing.Lifecycle
	// delivery projects the access an account gains or loses into SpiceDB before the request answers; nil with AuthZed disabled.
	delivery *authz.Delivery
	jobs     *jobs.Enqueuer
	contacts *brevo.Contacts
	log      *slog.Logger
	now      func() time.Time
}

func NewService(cfg config.Config, repo *Repository, sessions *Sessions, invites InviteRedeemer, limiter *ratelimit.Limiter, mail *mailer.Mailer, emitter *audit.Emitter, rdb *redis.Client, lifecycle *billing.Lifecycle, delivery *authz.Delivery, enqueuer *jobs.Enqueuer, contacts *brevo.Contacts, log *slog.Logger) *Service {
	client := &http.Client{}
	return &Service{
		cfg:       cfg,
		repo:      repo,
		sessions:  sessions,
		invites:   invites,
		limiter:   limiter,
		mailer:    mail,
		audit:     emitter,
		breaches:  &breachChecker{client: client, rangeURL: hibpRangeURL, disabled: cfg.PasswordHIBPCheckDisabled, log: log},
		turnstile: &turnstileVerifier{client: client, verifyURL: turnstileURL, secret: cfg.TurnstileSecretKey},
		failures:  &failureThrottle{rdb: rdb, log: log, now: time.Now},
		billing:   lifecycle,
		delivery:  delivery,
		jobs:      enqueuer,
		contacts:  contacts,
		log:       log,
		now:       time.Now,
	}
}

// jobTransaction runs fn with a repository bound to a transaction that background jobs are enqueued in, so they commit or roll back with the change that causes them.
func (s *Service) jobTransaction(ctx context.Context, fn func(tx *Repository, jtx *jobs.Tx) error) error {
	return s.jobs.Transaction(ctx, s.repo.db, func(jtx *jobs.Tx) error {
		return fn(&Repository{db: jtx.DB}, jtx)
	})
}

// serializableJobTransaction is jobTransaction at Serializable isolation, retried as database.Serializable retries.
func (s *Service) serializableJobTransaction(ctx context.Context, fn func(tx *Repository, jtx *jobs.Tx) error) error {
	return database.RetrySerializable(func() error {
		return s.jobTransaction(ctx, func(tx *Repository, jtx *jobs.Tx) error {
			if err := tx.db.Exec("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE").Error; err != nil {
				return err
			}
			return fn(tx, jtx)
		})
	})
}

// requireEmailVerification is Better Auth's requireEmailVerification as forma2 set it.
func (s *Service) requireEmailVerification() bool {
	return !s.cfg.EmailVerificationDisabled
}

// issueVerification stores a new single-use token and returns it; only its hash is kept.
func (s *Service) issueVerification(ctx context.Context, repo *Repository, purpose string, userID uuid.UUID, email string, ttl time.Duration) (issuedToken, error) {
	token, err := secret.NewToken()
	if err != nil {
		return issuedToken{}, err
	}
	expires := s.now().Add(ttl)
	if err := repo.insertVerification(ctx, purpose, userID, email, secret.HashToken(token), expires); err != nil {
		return issuedToken{}, err
	}
	return issuedToken{Value: token, ExpiresAt: expires, MaxAge: ttl}, nil
}

// link is an absolute URL to an SPA page carrying a token.
func (s *Service) link(path, token string) string {
	u := s.cfg.AppPageURL(path)
	u.RawQuery = url.Values{"token": {token}}.Encode()
	return u.String()
}

// pageURL is an absolute URL to an SPA page.
func (s *Service) pageURL(path string) string {
	return s.cfg.AppPageURL(path).String()
}

// sendVerificationEmail mails a fresh verification link and reports whether it went out. A failure, including a mailer with no SMTP server, is logged with the user id and the address's domain only, never the address or the link.
func (s *Service) sendVerificationEmail(ctx context.Context, acc *account) bool {
	token, err := s.issueVerification(ctx, s.repo, purposeEmailVerification, acc.ID, normalizeEmail(acc.Email), emailVerificationTTL)
	if err == nil {
		var sent bool
		sent, err = s.mailer.SendTemplate(ctx, acc.Email, acc.Locale, mailer.Verification{
			VerifyLink:              s.link(verifyEmailPath, token.Value),
			VerificationRequestLink: s.pageURL(verificationRequestedPath),
		})
		if err == nil && !sent {
			err = errMailNotSent
		}
	}
	if err != nil {
		s.log.ErrorContext(ctx, "Failed to send verification email", "err", err, "userId", acc.ID.String(), "emailDomain", emailDomain(acc.Email))
		return false
	}
	return true
}

// errMailNotSent is a send the mailer skipped because no SMTP server is configured; forma2 treated it as a failed send so it would not pass for a delivered one.
var errMailNotSent = errors.New("email was not sent: SMTP is not configured")

// logEvent records an audit event, filling the actor and client address from the request. Unauthenticated flows have no actor and are recorded as an unknown user, as forma2's were.
func (s *Service) logEvent(ctx context.Context, e audit.Event) {
	if e.ActorType == "" {
		e.ActorType = audit.ActorUser
	}
	if e.OrganizationID == "" {
		e.OrganizationID = audit.Unknown
	}
	s.audit.Log(ctx, audit.FromRequest(ctx, e))
}

// logFailure records that an audited operation failed, as forma2's withAuditLogging did for every error its handler threw, a rate-limit refusal included. The error itself is reported where the request is answered.
func (s *Service) logFailure(ctx context.Context, action audit.Action, targetID string) {
	s.logEvent(ctx, audit.Event{Action: action, TargetType: audit.TargetUser, TargetID: targetID, Status: audit.StatusFailure})
}

// The authMethod forma2 recorded for a sign-in completion: password for the credential paths (the password check, email verification and the second factor), sso for an identity provider's callback and SSO recovery.
const (
	signInMethodPassword = "password"
	signInMethodSSO      = "sso"
)

// finalizeSignIn is what forma2 ran after every sign-in completion: the signedIn audit event, user_signed_in with the request's marketing attribution (consumed whether or not analytics is on), and the user's last login time. None of it may fail a sign-in that already succeeded.
func (s *Service) finalizeSignIn(ctx context.Context, userID uuid.UUID, method string) {
	attribution := consumeAttribution(ctx)
	s.logEvent(ctx, audit.Event{
		Action: audit.ActionSignedIn, TargetType: audit.TargetUser, TargetID: userID.String(), ActorID: userID.String(),
		Status: audit.StatusSuccess, NewObject: map[string]string{"authMethod": method, "sessionStrategy": "database"},
	})
	if analytics.Enabled() {
		if previous, organizations, err := s.repo.signInFacts(ctx, userID); err != nil {
			s.log.WarnContext(ctx, "Failed to capture PostHog sign-in event", "err", err)
		} else {
			captureSignedIn(userID, method, previous, organizations, s.now(), attribution)
		}
	}
	if err := s.repo.touchLastLogin(ctx, userID, s.now()); err != nil {
		s.log.ErrorContext(ctx, "Failed to record successful sign-in", "err", err, "userId", userID.String())
	}
}
