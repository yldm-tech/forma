// Package audit emits forma2's audit events (apps/web/modules/audit-logs) as structured log records at their own level, so a log pipeline can route them apart from application logs. Like forma2 there is no audit table: the log stream is the sink.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"time"

	"github.com/yldm-tech/forma/server/internal/platform/config"
	"github.com/yldm-tech/forma/server/internal/platform/httpapi"
	"github.com/yldm-tech/forma/server/internal/platform/logger"
)

// Unknown stands in for a value the event could not be attributed with, as forma2's UNKNOWN_DATA did.
const Unknown = "unknown"

type Action string

// Actions, with forma2's names (ZAuditAction).
const (
	ActionCreated                 Action = "created"
	ActionUpdated                 Action = "updated"
	ActionDeleted                 Action = "deleted"
	ActionArchived                Action = "archived"
	ActionRestored                Action = "restored"
	ActionSignedIn                Action = "signedIn"
	ActionMerged                  Action = "merged"
	ActionVerificationEmailSent   Action = "verificationEmailSent"
	ActionCreatedFromCSV          Action = "createdFromCSV"
	ActionCopiedToOtherWorkspace  Action = "copiedToOtherWorkspace"
	ActionAddedToResponse         Action = "addedToResponse"
	ActionRemovedFromResponse     Action = "removedFromResponse"
	ActionCreatedUpdated          Action = "createdUpdated"
	ActionSubscriptionAccessed    Action = "subscriptionAccessed"
	ActionSubscriptionUpdated     Action = "subscriptionUpdated"
	ActionTwoFactorVerified       Action = "twoFactorVerified"
	ActionEmailVerified           Action = "emailVerified"
	ActionJWTTokenCreated         Action = "jwtTokenCreated"
	ActionAuthenticationAttempted Action = "authenticationAttempted"
	ActionAuthenticationSucceeded Action = "authenticationSucceeded"
	ActionPasswordVerified        Action = "passwordVerified"
	ActionTwoFactorAttempted      Action = "twoFactorAttempted"
	ActionTwoFactorRequired       Action = "twoFactorRequired"
	ActionEmailVerificationTried  Action = "emailVerificationAttempted"
	ActionUserSignedOut           Action = "userSignedOut"
	ActionPasswordReset           Action = "passwordReset"
	ActionBulkCreated             Action = "bulkCreated"
	// ActionPurged destroys every record a resource holds while keeping the resource itself.
	ActionPurged  Action = "purged"
	ActionQueried Action = "queried"
	// ActionExported is a bulk export of stored data out of the product.
	ActionExported             Action = "exported"
	ActionSSORecoveryStarted   Action = "sso_recovery_started"
	ActionSSORecoveryCompleted Action = "sso_recovery_completed"
	ActionSSORecoveryFailed    Action = "sso_recovery_failed"
)

var actions = []Action{
	ActionCreated, ActionUpdated, ActionDeleted, ActionArchived, ActionRestored, ActionSignedIn, ActionMerged,
	ActionVerificationEmailSent, ActionCreatedFromCSV, ActionCopiedToOtherWorkspace, ActionAddedToResponse,
	ActionRemovedFromResponse, ActionCreatedUpdated, ActionSubscriptionAccessed, ActionSubscriptionUpdated,
	ActionTwoFactorVerified, ActionEmailVerified, ActionJWTTokenCreated, ActionAuthenticationAttempted,
	ActionAuthenticationSucceeded, ActionPasswordVerified, ActionTwoFactorAttempted, ActionTwoFactorRequired,
	ActionEmailVerificationTried, ActionUserSignedOut, ActionPasswordReset, ActionBulkCreated, ActionPurged,
	ActionQueried, ActionExported, ActionSSORecoveryStarted, ActionSSORecoveryCompleted, ActionSSORecoveryFailed,
}

type Target string

// Targets, with forma2's names (ZAuditTarget).
const (
	TargetSegment             Target = "segment"
	TargetSurvey              Target = "survey"
	TargetWorkflow            Target = "workflow"
	TargetWebhook             Target = "webhook"
	TargetUser                Target = "user"
	TargetContactAttributeKey Target = "contactAttributeKey"
	TargetWorkspaceTeam       Target = "workspaceTeam"
	TargetTeam                Target = "team"
	TargetActionClass         Target = "actionClass"
	TargetResponse            Target = "response"
	TargetContact             Target = "contact"
	TargetOrganization        Target = "organization"
	TargetTag                 Target = "tag"
	TargetWorkspace           Target = "workspace"
	TargetLanguage            Target = "language"
	TargetInvite              Target = "invite"
	TargetMembership          Target = "membership"
	TargetTwoFactorAuth       Target = "twoFactorAuth"
	TargetAPIKey              Target = "apiKey"
	TargetIntegration         Target = "integration"
	TargetFile                Target = "file"
	TargetQuota               Target = "quota"
	TargetCubeQuery           Target = "cubeQuery"
)

var targets = []Target{
	TargetSegment, TargetSurvey, TargetWorkflow, TargetWebhook, TargetUser, TargetContactAttributeKey,
	TargetWorkspaceTeam, TargetTeam, TargetActionClass, TargetResponse, TargetContact, TargetOrganization,
	TargetTag, TargetWorkspace, TargetLanguage, TargetInvite, TargetMembership, TargetTwoFactorAuth,
	TargetAPIKey, TargetIntegration, TargetFile, TargetQuota, TargetCubeQuery,
}

type ActorType string

const (
	ActorUser   ActorType = "user"
	ActorAPI    ActorType = "api"
	ActorSystem ActorType = "system"
)

type Status string

const (
	StatusSuccess Status = "success"
	StatusFailure Status = "failure"
)

// Event is what a module reports. Empty ActorID, TargetID and OrganizationID are recorded as "unknown". OldObject and NewObject are any JSON-serialisable values: with both, the event records what changed; with one, that object; either way with personal data masked.
type Event struct {
	Action         Action
	TargetType     Target
	TargetID       string
	ActorType      ActorType
	ActorID        string
	OrganizationID string
	Status         Status
	OldObject      any
	NewObject      any
	// EventID correlates a failure with the error report; it is recorded on failures only.
	EventID string
	// APIURL is the absolute URL of the API call that caused the event.
	APIURL string
	// IPAddress is the client address. Leave it empty to take it from the request context (see FromRequest).
	IPAddress string
}

type Emitter struct {
	log      *slog.Logger
	enabled  bool
	recordIP bool
	now      func() time.Time
}

func New(cfg config.Config, log *slog.Logger) *Emitter {
	return &Emitter{log: log, enabled: cfg.AuditLogEnabled, recordIP: cfg.AuditLogGetUserIP, now: time.Now}
}

// FromRequest fills the actor and client address from the request context: the session's user (as a "user" actor) and the address httpapi resolved. Fields already set are kept.
func FromRequest(ctx context.Context, e Event) Event {
	if e.ActorID == "" {
		if a, ok := httpapi.ActorFrom(ctx); ok {
			e.ActorID = a.UserID.String()
			if e.ActorType == "" {
				e.ActorType = ActorUser
			}
		}
	}
	if e.IPAddress == "" {
		e.IPAddress = httpapi.ClientIP(ctx)
	}
	return e
}

// Log records the event. It never fails the caller: an event that does not validate, or whose objects cannot be serialised, is reported as an application error and dropped, as in forma2.
func (em *Emitter) Log(ctx context.Context, e Event) {
	if !em.enabled {
		return
	}
	attrs, err := em.build(e)
	if err != nil {
		em.log.ErrorContext(ctx, "Failed to log audit event", "err", err, "action", string(e.Action), "target", string(e.TargetType))
		return
	}
	em.log.LogAttrs(ctx, logger.LevelAudit, "audit", attrs...)
}

func (em *Emitter) build(e Event) ([]slog.Attr, error) {
	if !slices.Contains(actions, e.Action) {
		return nil, fmt.Errorf("invalid audit action %q", e.Action)
	}
	if !slices.Contains(targets, e.TargetType) {
		return nil, fmt.Errorf("invalid audit target %q", e.TargetType)
	}
	if e.ActorType != ActorUser && e.ActorType != ActorAPI && e.ActorType != ActorSystem {
		return nil, fmt.Errorf("invalid audit actor type %q", e.ActorType)
	}
	if e.Status != StatusSuccess && e.Status != StatusFailure {
		return nil, fmt.Errorf("invalid audit status %q", e.Status)
	}
	if e.APIURL != "" {
		if u, err := url.Parse(e.APIURL); err != nil || !u.IsAbs() || u.Host == "" {
			return nil, fmt.Errorf("audit apiUrl must be an absolute URL")
		}
	}

	changes, err := changesOf(e.OldObject, e.NewObject)
	if err != nil {
		return nil, err
	}
	ip := Unknown
	if em.recordIP && e.IPAddress != "" {
		ip = e.IPAddress
	}

	attrs := []slog.Attr{
		slog.Group("actor", slog.String("id", orUnknown(e.ActorID)), slog.String("type", string(e.ActorType))),
		slog.String("action", string(e.Action)),
		slog.Group("target", slog.String("id", orUnknown(e.TargetID)), slog.String("type", string(e.TargetType))),
		slog.String("status", string(e.Status)),
		slog.String("timestamp", em.now().UTC().Format("2006-01-02T15:04:05.000Z")),
		slog.String("organizationId", orUnknown(e.OrganizationID)),
		slog.String("ipAddress", ip),
	}
	if e.APIURL != "" {
		attrs = append(attrs, slog.String("apiUrl", e.APIURL))
	}
	if changes != nil {
		attrs = append(attrs, slog.Any("changes", changes))
	}
	if e.Status == StatusFailure && e.EventID != "" {
		attrs = append(attrs, slog.String("eventId", e.EventID))
	}
	return attrs, nil
}

func orUnknown(s string) string {
	if s == "" {
		return Unknown
	}
	return s
}

// changesOf is forma2's rule: the redacted diff when both objects are given, otherwise the redacted object that is.
func changesOf(oldObject, newObject any) (map[string]any, error) {
	oldV, err := toJSONValue(oldObject)
	if err != nil {
		return nil, err
	}
	newV, err := toJSONValue(newObject)
	if err != nil {
		return nil, err
	}
	var changes any
	switch {
	case oldV != nil && newV != nil:
		diff, changed := deepDiff(oldV, newV)
		if !changed {
			return nil, nil
		}
		changes = diff
	case newV != nil:
		changes = newV
	case oldV != nil:
		changes = oldV
	default:
		return nil, nil
	}
	m, ok := redact(changes).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("audit changes must be an object, got %T", changes)
	}
	return m, nil
}

// toJSONValue normalises any value to the generic shape encoding/json produces (maps, slices, strings, float64, bool, nil), which is the shape the diff and redaction walk.
func toJSONValue(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("serialise audit object: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("serialise audit object: %w", err)
	}
	return out, nil
}
