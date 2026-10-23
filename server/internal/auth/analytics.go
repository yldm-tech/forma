package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/server/internal/platform/analytics"
)

// The product events of the identity module, forma2's (apps/web/modules/auth, sso, account). They carry the email domain but never the address or the name: the person is identified by their user id alone.

// captureSignedUp is forma2's user_signed_up with the person identified first, so the event lands on an identified person. organizationID is the organization the sign-up joined or created, nil when it has none.
func captureSignedUp(userID uuid.UUID, provider, email, source string, organizationID *uuid.UUID, attribution analytics.Properties) {
	domain := analytics.EmailDomain(email)
	analytics.Identify(userID.String(), analytics.Properties{"email_domain": domain})
	props := withAttribution(attribution, analytics.Properties{"auth_provider": provider, "email_domain": domain, "signup_source": source, "invite_organization_id": nil})
	groups := analytics.Groups{}
	if organizationID != nil {
		props["invite_organization_id"] = organizationID.String()
		groups.OrganizationID = organizationID.String()
	}
	analytics.Capture(userID.String(), "user_signed_up", props, groups)
}

// captureCredentialSignUp is the analytics of a password sign-up: an invitee's organization group, or the events of the organization the sign-up created, then the sign-up itself. The request's marketing attribution is consumed whether or not analytics is on, as forma2 cleared the cookie either way.
func (s *Service) captureCredentialSignUp(ctx context.Context, userID uuid.UUID, email string, orgID uuid.UUID, invited bool) {
	attribution := consumeAttribution(ctx)
	if !analytics.Enabled() {
		return
	}
	org, err := s.repo.organizationWithFirstWorkspace(ctx, orgID)
	if err != nil {
		s.log.WarnContext(ctx, "Failed to read the sign-up's organization for PostHog", "err", err, "organizationId", orgID.String())
	}
	source := "direct"
	switch {
	case invited:
		source = "invite"
		if org != nil {
			analytics.GroupIdentify(analytics.GroupOrganization, orgID.String(), analytics.Properties{"name": org.Name})
		}
	case org != nil:
		analytics.OrganizationCreated(userID.String(), orgID.String(), org.Name, org.WorkspaceID.String(), org.WorkspaceName, analytics.EmailDomain(email), true)
	}
	captureSignedUp(userID, "credentials", email, source, &orgID, attribution)
}

// captureSignedIn is forma2's user_signed_in: how the user signed in, how many organizations they belong to, and whether this is their first sign-in of the UTC day. previousLogin is the last sign-in before this one.
func captureSignedIn(userID uuid.UUID, method string, previousLogin *time.Time, organizations int64, now time.Time, attribution analytics.Properties) {
	firstToday := previousLogin == nil || previousLogin.UTC().Format(time.DateOnly) != now.UTC().Format(time.DateOnly)
	analytics.Capture(userID.String(), "user_signed_in", withAttribution(attribution, analytics.Properties{
		"auth_provider":        method,
		"organization_count":   organizations,
		"is_first_login_today": firstToday,
	}), analytics.Groups{})
}

// signInFacts are what user_signed_in reports, read before the sign-in is recorded.
func (r *Repository) signInFacts(ctx context.Context, userID uuid.UUID) (*time.Time, int64, error) {
	var row struct {
		LastLoginAt *time.Time
		Memberships int64
	}
	err := r.db.WithContext(ctx).Raw(`SELECT u.last_login_at, (SELECT count(*) FROM memberships m WHERE m.user_id = u.id) AS memberships FROM users u WHERE u.id = ?`, userID).Scan(&row).Error
	if err != nil {
		return nil, 0, fmt.Errorf("read sign-in facts: %w", err)
	}
	return row.LastLoginAt, row.Memberships, nil
}

type organizationSummary struct {
	Name          string
	WorkspaceID   uuid.UUID
	WorkspaceName string
}

// organizationWithFirstWorkspace is an organization's name and its oldest workspace; nil when it does not exist.
func (r *Repository) organizationWithFirstWorkspace(ctx context.Context, orgID uuid.UUID) (*organizationSummary, error) {
	var rows []organizationSummary
	err := r.db.WithContext(ctx).Raw(`SELECT o.name, w.id AS workspace_id, w.name AS workspace_name FROM organizations o
		JOIN workspaces w ON w.organization_id = o.id WHERE o.id = ? ORDER BY w.created_at, w.id LIMIT 1`, orgID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read organization: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
