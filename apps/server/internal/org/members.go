package org

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/yldm-tech/forma/apps/server/internal/apikey/disable"
	"github.com/yldm-tech/forma/apps/server/internal/authz"
	"github.com/yldm-tech/forma/apps/server/internal/platform/apperr"
	"github.com/yldm-tech/forma/apps/server/internal/platform/audit"
	"github.com/yldm-tech/forma/apps/server/internal/platform/ratelimit"
)

// Members lists the organization's members for its settings pages, which need organization.read_access: billing members are kept out of them. Each member's two-factor status is shown to owners and managers (organization.manage) only: to anyone else, a list of who is not protected would only name the accounts worth attacking.
func (s *Service) Members(ctx context.Context, userID, organizationID uuid.UUID, page int) ([]Member, error) {
	actor, resource := authz.User(userID), authz.Organization(organizationID)
	if err := s.authz.Require(ctx, actor, authz.OrganizationReadAccess, resource); err != nil {
		return nil, err
	}
	members, err := s.repo.members(ctx, organizationID, page)
	if err != nil {
		return nil, err
	}
	manages, err := s.authz.Can(ctx, actor, authz.OrganizationManage, resource)
	if err != nil {
		return nil, err
	}
	if !manages {
		for i := range members {
			members[i].TwoFactor = ""
		}
	}
	return members, nil
}

// Messages of refused membership changes, forma2's.
const (
	msgNotAMember          = "Not a member of this organization"
	msgUserManagementOff   = "User management is not allowed for your role"
	msgBillingNotAllowed   = "Billing role is not allowed"
	msgManagerAssignMember = "Managers can only assign users to the member role"
	msgOnlyOwnersEditOwner = "Only owners can change the role of an owner"
	msgLastOwnerDemote     = "You cannot demote the last owner of the organization"
	msgDeleteSelf          = "You cannot delete yourself from the organization"
	msgManagerDeleteOwner  = "You cannot delete the owner of the organization"
	msgLastOwnerDelete     = "You cannot delete the last owner of the organization"
	msgLeaveOwned          = "You cannot leave an organization you own"
	msgLeaveOnly           = "You cannot leave the only organization you are a member of"
)

// manageUsers is the gate of the user-management surface, in forma2's order: organization.manage_access, where USER_MANAGEMENT_MINIMUM_ROLE applies, then organization.manage, then the state-mutation rate limit. The policy check is a second check beside the capability, so a manager on an owner-restricted instance cannot manage members by calling the endpoint.
func (s *Service) manageUsers(ctx context.Context, userID, organizationID uuid.UUID) error {
	actor, resource := authz.User(userID), authz.Organization(organizationID)
	allowed, err := s.authz.Can(ctx, actor, authz.OrganizationManageAccess, resource)
	if err != nil {
		return err
	}
	if !allowed {
		return apperr.OperationNotAllowed(msgUserManagementOff)
	}
	if err := s.authz.Require(ctx, actor, authz.OrganizationManage, resource); err != nil {
		return err
	}
	return s.limiter.Apply(ctx, ratelimit.ActionStateMutation, organizationID.String())
}

// ChangeRole is forma2's updateMembershipAction. Billing is a Forma Cloud role; a manager may only assign the member role and may not touch an owner; and demoting an owner is refused when no other active owner would remain, checked in the same serializable transaction as the write. Promotion to owner or manager makes the user admin of their teams in the organization. Demotion from owner or manager to a role below manager disables the API keys the user created in the organization, in the same transaction; a role that was already below manager, sent again, disables nothing.
func (s *Service) ChangeRole(ctx context.Context, userID, organizationID, targetUserID uuid.UUID, role string) (*Membership, error) {
	old, m, disabled, err := s.changeRole(ctx, userID, organizationID, targetUserID, role)
	s.record(ctx, audit.ActionUpdated, audit.TargetMembership, Membership{UserID: targetUserID, OrganizationID: organizationID}.auditID(), organizationID.String(), err, old, m)
	if err == nil {
		s.recordDisabledKeys(ctx, disabled, disable.ReasonCreatorDemoted)
	}
	return m, err
}

func (s *Service) changeRole(ctx context.Context, userID, organizationID, targetUserID uuid.UUID, role string) (old, updated *Membership, disabled []disable.Key, err error) {
	current, err := s.repo.membership(ctx, userID, organizationID)
	if err != nil {
		return nil, nil, nil, err
	}
	if current == nil {
		return nil, nil, nil, apperr.Forbidden("User not a member of this organization")
	}
	if err := s.manageUsers(ctx, userID, organizationID); err != nil {
		return nil, nil, nil, err
	}
	if role == RoleBilling && !s.cfg.IsFormaCloud {
		return nil, nil, nil, apperr.InvalidInput(apperr.CodeValidation, msgBillingNotAllowed)
	}
	if current.Role == RoleManager && role != RoleMember {
		return nil, nil, nil, apperr.OperationNotAllowed(msgManagerAssignMember)
	}
	if old, err = s.repo.membership(ctx, targetUserID, organizationID); err != nil {
		return nil, nil, nil, err
	}
	if old == nil {
		return nil, nil, nil, apperr.NotFound("Membership", "")
	}
	if current.Role != RoleOwner && old.Role == RoleOwner {
		return old, nil, nil, apperr.OperationNotAllowed(msgOnlyOwnersEditOwner)
	}
	write := func(tx *Repository) error {
		// Re-read under the transaction: the guards below must see the role the write replaces.
		target, err := tx.membership(ctx, targetUserID, organizationID)
		if err != nil {
			return err
		}
		if target == nil {
			return apperr.NotFound("Membership", "")
		}
		if current.Role != RoleOwner && target.Role == RoleOwner {
			return apperr.OperationNotAllowed(msgOnlyOwnersEditOwner)
		}
		if target.Role == RoleOwner && role != RoleOwner {
			owners, err := tx.activeOwnerCount(ctx, organizationID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return apperr.InvalidInput(apperr.CodeValidation, msgLastOwnerDemote)
			}
		}
		found, err := tx.updateRole(ctx, targetUserID, organizationID, role)
		if err != nil {
			return err
		}
		if !found {
			return apperr.NotFound("Membership", "")
		}
		// Assigned, not appended: a retried transaction runs this again.
		disabled = nil
		if disable.ManagesKeys(target.Role) && !disable.ManagesKeys(role) {
			if disabled, err = disable.CreatedBy(ctx, tx.db, targetUserID, &organizationID, disable.ReasonCreatorDemoted); err != nil {
				return err
			}
		}
		updated, err = tx.membership(ctx, targetUserID, organizationID)
		return err
	}
	if err := s.repo.serializable(ctx, write); err != nil {
		return old, nil, nil, err
	}
	return old, updated, disabled, nil
}

// RemoveMember is forma2's deleteMembershipAction: nobody removes themselves (they leave instead), a manager may not remove an owner, and the last active owner is never removed. The member's team memberships in the organization go with it, and the API keys they created there are disabled.
func (s *Service) RemoveMember(ctx context.Context, userID, organizationID, targetUserID uuid.UUID) error {
	old, disabled, err := s.removeMember(ctx, userID, organizationID, targetUserID)
	s.record(ctx, audit.ActionDeleted, audit.TargetMembership, Membership{UserID: targetUserID, OrganizationID: organizationID}.auditID(), organizationID.String(), err, old, nil)
	if err == nil {
		s.recordDisabledKeys(ctx, disabled, disable.ReasonCreatorRemoved)
	}
	return err
}

func (s *Service) removeMember(ctx context.Context, userID, organizationID, targetUserID uuid.UUID) (old *Membership, disabled []disable.Key, err error) {
	if err := s.manageUsers(ctx, userID, organizationID); err != nil {
		return nil, nil, err
	}
	if targetUserID == userID {
		return nil, nil, apperr.OperationNotAllowed(msgDeleteSelf)
	}
	current, err := s.repo.membership(ctx, userID, organizationID)
	if err != nil {
		return nil, nil, err
	}
	if current == nil {
		return nil, nil, apperr.Forbidden(msgNotAMember)
	}
	target, err := s.repo.membership(ctx, targetUserID, organizationID)
	if err != nil {
		return nil, nil, err
	}
	if target == nil {
		return nil, nil, &apperr.Error{Status: http.StatusNotFound, Code: apperr.CodeNotFound, Message: msgNotAMember}
	}
	if current.Role == RoleManager && target.Role == RoleOwner {
		return target, nil, apperr.OperationNotAllowed(msgManagerDeleteOwner)
	}
	err = s.repo.serializable(ctx, func(tx *Repository) error {
		// Re-read under the transaction: the owner guards must see the role being removed.
		target, err := tx.membership(ctx, targetUserID, organizationID)
		if err != nil {
			return err
		}
		if target == nil {
			return &apperr.Error{Status: http.StatusNotFound, Code: apperr.CodeNotFound, Message: msgNotAMember}
		}
		if current.Role == RoleManager && target.Role == RoleOwner {
			return apperr.OperationNotAllowed(msgManagerDeleteOwner)
		}
		if target.Role == RoleOwner {
			owners, err := tx.activeOwnerCount(ctx, organizationID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return apperr.InvalidInput(apperr.CodeValidation, msgLastOwnerDelete)
			}
		}
		if err := tx.deleteMembership(ctx, targetUserID, organizationID); err != nil {
			return err
		}
		disabled, err = disable.CreatedBy(ctx, tx.db, targetUserID, &organizationID, disable.ReasonCreatorRemoved)
		return err
	})
	if err != nil {
		return target, nil, err
	}
	return target, disabled, nil
}

// Leave is forma2's leaveOrganizationAction: any member may leave, except an owner (even when other owners exist; they must be demoted first) and a user for whom this is their only organization. The API keys the member created in the organization are disabled.
func (s *Service) Leave(ctx context.Context, userID, organizationID uuid.UUID) error {
	old, disabled, err := s.leave(ctx, userID, organizationID)
	s.record(ctx, audit.ActionDeleted, audit.TargetMembership, Membership{UserID: userID, OrganizationID: organizationID}.auditID(), organizationID.String(), err, old, nil)
	if err == nil {
		s.recordDisabledKeys(ctx, disabled, disable.ReasonCreatorLeft)
	}
	return err
}

func (s *Service) leave(ctx context.Context, userID, organizationID uuid.UUID) (old *Membership, disabled []disable.Key, err error) {
	// A member the organization's two-factor requirement refuses may still leave it rather than enrol.
	allowed, err := s.authz.CanReadOrganizationDespiteSecondFactor(ctx, authz.User(userID), organizationID)
	if err != nil {
		return nil, nil, err
	}
	if !allowed {
		return nil, nil, apperr.Forbidden("Not authorized")
	}
	if err := s.limiter.Apply(ctx, ratelimit.ActionStateMutation, organizationID.String()); err != nil {
		return nil, nil, err
	}
	m, err := s.repo.membership(ctx, userID, organizationID)
	if err != nil {
		return nil, nil, err
	}
	if m == nil {
		return nil, nil, apperr.Forbidden(msgNotAMember)
	}
	if m.Role == RoleOwner {
		return m, nil, apperr.OperationNotAllowed(msgLeaveOwned)
	}
	// Counted inside the transaction, so leaving two organizations at once cannot leave the user with none.
	err = s.repo.serializable(ctx, func(tx *Repository) error {
		n, err := tx.membershipCount(ctx, userID)
		if err != nil {
			return err
		}
		if n <= 1 {
			return apperr.InvalidInput(apperr.CodeValidation, msgLeaveOnly)
		}
		if err := tx.deleteMembership(ctx, userID, organizationID); err != nil {
			return err
		}
		disabled, err = disable.CreatedBy(ctx, tx.db, userID, &organizationID, disable.ReasonCreatorLeft)
		return err
	})
	if err != nil {
		return m, nil, err
	}
	return m, disabled, nil
}

// recordDisabledKeys records the audit events of the API keys a membership change disabled, attributed to the user who made the change.
func (s *Service) recordDisabledKeys(ctx context.Context, keys []disable.Key, reason string) {
	for _, e := range disable.Events(keys, reason) {
		e.ActorType = audit.ActorUser
		s.audit.Log(ctx, audit.FromRequest(ctx, e))
	}
}
