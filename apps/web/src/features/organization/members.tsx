import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import {
  CheckIcon,
  ChevronDownIcon,
  CopyIcon,
  SendHorizonalIcon,
  ShareIcon,
  TrashIcon,
  XIcon,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Dialog, DialogBody, DialogDescription, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "../../components/ui/dropdown-menu";
import { Input } from "../../components/ui/input";
import { LoadingBlock, SettingsCard } from "../../components/ui/page";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../components/ui/tooltip";
import { copyText } from "../../lib/clipboard";
import { errorMessage } from "../../lib/errors";
import { isInviteExpired } from "../../lib/invites";
import { authOptionsQuery, meQuery } from "../../lib/queries/auth";
import { combinedQueryState } from "../../lib/query-state";
import {
  type Invite,
  invitesQuery,
  useChangeInviteRole,
  useDeleteInvite,
  useInviteLink,
  useResendInvite,
  userManagementQuery,
} from "../../lib/queries/invites";
import {
  type Member,
  type OrganizationDetail,
  membersQuery,
  useChangeMemberRole,
  useRemoveMember,
} from "../../lib/queries/organizations";
import {
  type OrganizationRole,
  accessFlags,
  assignableRoles,
  canDeleteRow,
  hasMoreThanOneActiveOwner,
  isRoleEditDisabled,
  roleLabel,
} from "../../lib/roles";
import { toast } from "../../lib/toast";
import { InviteMemberModal } from "./invite-member-modal";
import { LeaveOrganizationDialog } from "./leave-organization-dialog";
import { TwoFactorStatusBadge } from "./two-factor-requirement";

/** A row of the members table: an accepted membership or a pending invite. */
type MemberRow = { kind: "member"; member: Member } | { kind: "invite"; invite: Invite };

/** forma2's members card: leave and invite actions, then every member and pending invite with role and status. */
export const MembersCard = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const members = useQuery(membersQuery(organization.id));
  const invites = useQuery(invitesQuery(organization.id));
  const access = useQuery(userManagementQuery(organization.id));
  const { data: me } = useSuspenseQuery(meQuery);
  const options = useQuery(authOptionsQuery);
  // Without SMTP an invite is created but never mailed, so sending it again is not offered; the link is shared by hand instead.
  const emailConfigured = options.data?.smtpConfigured ?? true;
  const role = organization.membership.role;

  // forma2 offered user management to anyone USER_MANAGEMENT_MINIMUM_ROLE admits, and to team admins for their own teams.
  const userManagementEnabled =
    !!access.data && (access.data.canManageUsers || access.data.adminTeamIds.length > 0);
  const rows: MemberRow[] = [
    ...(members.data ?? []).map((member) => ({ kind: "member" as const, member })),
    ...(invites.data ?? []).map((invite) => ({ kind: "invite" as const, invite })),
  ];
  const doesOrgHaveMoreThanOneOwner = hasMoreThanOneActiveOwner(members.data ?? []);
  // The server sends each member's two-factor status to owners and managers only.
  const showTwoFactor = accessFlags(role).isOwnerOrManager;
  // A table missing its members, its invites or the caller's permissions would look complete and silently lose rows or actions, so any failure replaces it.
  const state = combinedQueryState([members, invites, access]);
  const retry = () => {
    for (const query of [members, invites, access]) if (query.isError) void query.refetch();
  };

  return (
    <SettingsCard
      title={t("workspace.settings.general.manage_members")}
      description={t("workspace.settings.general.manage_members_description")}
      bodyVariant="flush"
      width="full">
      <div className="px-4 pt-4">
        <OrganizationActions
          organization={organization}
          canInvite={!!access.data && !access.data.inviteDisabled && userManagementEnabled}
          adminTeamIds={access.data?.adminTeamIds ?? []}
          billingRoleAllowed={access.data?.billingRoleAllowed ?? false}
          emailConfigured={emailConfigured}
        />
      </div>
      {state.status === "error" ? (
        <div role="alert" className="flex flex-col items-start gap-3 px-4 py-6 text-sm text-red-600">
          <p>{errorMessage(t, state.error)}</p>
          <Button variant="secondary" size="sm" onClick={retry}>
            {t("common.try_again")}
          </Button>
        </div>
      ) : state.status === "pending" ? (
        <div className="space-y-4 px-4 py-4">
          <LoadingBlock />
          <LoadingBlock />
        </div>
      ) : (
        <Table aria-label={t("workspace.settings.general.manage_members")}>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[17%]">{t("common.full_name")}</TableHead>
              <TableHead className="w-[25%]">{t("common.email")}</TableHead>
              <TableHead className="w-[17%]">{t("common.role")}</TableHead>
              <TableHead className="w-[17%]">{t("common.status")}</TableHead>
              {showTwoFactor && (
                <TableHead className="w-[10%]">
                  <abbr title={t("organizations.two_factor.column_description")} className="no-underline">
                    {t("organizations.two_factor.column")}
                  </abbr>
                </TableHead>
              )}
              {userManagementEnabled && (
                <TableHead className="w-[24%] text-right">{t("common.actions")}</TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <MemberTableRow
                key={row.kind === "member" ? row.member.userId : row.invite.id}
                row={row}
                organizationId={organization.id}
                currentUserRole={role}
                currentUserId={me.id}
                userManagementEnabled={userManagementEnabled}
                billingRoleAllowed={access.data?.billingRoleAllowed ?? false}
                doesOrgHaveMoreThanOneOwner={doesOrgHaveMoreThanOneOwner}
                emailConfigured={emailConfigured}
                showTwoFactor={showTwoFactor}
              />
            ))}
          </TableBody>
        </Table>
      )}
    </SettingsCard>
  );
};

const MemberTableRow = ({
  row,
  organizationId,
  currentUserRole,
  currentUserId,
  userManagementEnabled,
  billingRoleAllowed,
  doesOrgHaveMoreThanOneOwner,
  emailConfigured,
  showTwoFactor,
}: {
  row: MemberRow;
  organizationId: string;
  currentUserRole: OrganizationRole;
  currentUserId: string;
  userManagementEnabled: boolean;
  billingRoleAllowed: boolean;
  doesOrgHaveMoreThanOneOwner: boolean;
  emailConfigured: boolean;
  showTwoFactor: boolean;
}) => {
  const name = row.kind === "member" ? row.member.name : (row.invite.name ?? "");
  const email = row.kind === "member" ? row.member.email : row.invite.email;
  const memberRole = row.kind === "member" ? row.member.role : row.invite.role;
  return (
    <TableRow>
      <TableCell>{name}</TableCell>
      <TableCell>{email}</TableCell>
      <TableCell>
        <RoleCell
          row={row}
          organizationId={organizationId}
          memberRole={memberRole}
          currentUserRole={currentUserRole}
          disabled={isRoleEditDisabled({
            isUserManagementDisabled: !userManagementEnabled,
            currentUserRole,
            currentUserId,
            memberRole,
            memberUserId: row.kind === "member" ? row.member.userId : undefined,
            doesOrgHaveMoreThanOneOwner,
          })}
          billingRoleAllowed={billingRoleAllowed}
        />
      </TableCell>
      <TableCell>
        <StatusBadge row={row} organizationId={organizationId} />
      </TableCell>
      {showTwoFactor && (
        <TableCell>
          {row.kind === "member" && row.member.twoFactor ? (
            <TwoFactorStatusBadge status={row.member.twoFactor} />
          ) : (
            <span className="text-slate-400">—</span>
          )}
        </TableCell>
      )}
      {userManagementEnabled && (
        <TableCell>
          <MemberActions
            row={row}
            organizationId={organizationId}
            name={name}
            emailConfigured={emailConfigured}
            canManageInvites={accessFlags(currentUserRole).isOwnerOrManager}
            canDelete={canDeleteRow(
              row.kind === "invite"
                ? { kind: "invite" }
                : { kind: "member", userId: row.member.userId, role: row.member.role },
              { currentUserRole, currentUserId, doesOrgHaveMoreThanOneOwner }
            )}
          />
        </TableCell>
      )}
    </TableRow>
  );
};

const StatusBadge = ({ row, organizationId }: { row: MemberRow; organizationId: string }) => {
  const { t, i18n } = useTranslation();
  if (row.kind === "invite") {
    if (isInviteExpired(row.invite)) return <Badge type="gray" text={t("common.expired")} size="tiny" />;
    const date = new Intl.DateTimeFormat(i18n.language, { dateStyle: "long" }).format(
      new Date(row.invite.expiresAt)
    );
    const expires = t("workspace.settings.general.invite_expires_on", { date });
    return (
      <span title={expires}>
        <Badge type="warning" text={t("common.pending")} size="tiny" />
        <span className="sr-only">{expires}</span>
      </span>
    );
  }
  if (!row.member.isActive) return <InactiveBadge organizationId={organizationId} email={row.member.email} />;
  return <Badge type="success" text={t("common.active")} size="tiny" />;
};

/**
 * An account switched off with isActive: false, which only the v2 management API writes; the pages have no control that undoes it, so the badge says how to. Its explanation is a tooltip for the eye, reached by hover or focus, and hidden text beside the badge for a screen reader reading the table. The hidden text stays out of the badge's button: focusing the button opens the tooltip, which Radix announces as the button's description, and the explanation inside the button would be read a second time as its name.
 */
const InactiveBadge = ({ organizationId, email }: { organizationId: string; email: string }) => {
  const { t } = useTranslation();
  const restore = t("organizations.members.inactive_tooltip", { organizationId, email });
  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          {/* A button only so the keyboard can reach the tooltip; it does nothing when pressed. */}
          <button
            type="button"
            className="focus-visible:ring-ring inline-flex cursor-default rounded-full focus-visible:ring-2 focus-visible:outline-hidden">
            <Badge type="gray" text={t("common.inactive")} size="tiny" />
          </button>
        </TooltipTrigger>
        <TooltipContent className="max-w-sm break-words">{restore}</TooltipContent>
      </Tooltip>
      <span className="sr-only">{restore}</span>
    </TooltipProvider>
  );
};

/** forma2's EditMembershipRole: owners and managers get a menu of the roles they may assign, everyone else sees the role. */
const RoleCell = ({
  row,
  organizationId,
  memberRole,
  currentUserRole,
  disabled,
  billingRoleAllowed,
}: {
  row: MemberRow;
  organizationId: string;
  memberRole: OrganizationRole;
  currentUserRole: OrganizationRole;
  disabled: boolean;
  billingRoleAllowed: boolean;
}) => {
  const { t } = useTranslation();
  const changeMemberRole = useChangeMemberRole(organizationId);
  const changeInviteRole = useChangeInviteRole(organizationId);
  const pending = changeMemberRole.isPending || changeInviteRole.isPending;

  if (!accessFlags(currentUserRole).isOwnerOrManager) {
    return <Badge size="tiny" type="gray" text={roleLabel(t, memberRole)} />;
  }

  const change = async (role: OrganizationRole) => {
    try {
      if (row.kind === "member") await changeMemberRole.mutateAsync({ userId: row.member.userId, role });
      else await changeInviteRole.mutateAsync({ inviteId: row.invite.id, role });
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={disabled || pending}>
        <Button
          className="flex items-center gap-1 p-2 text-xs"
          loading={pending}
          size="sm"
          variant="secondary">
          <span className="ml-1">{roleLabel(t, memberRole)}</span>
          <ChevronDownIcon className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuRadioGroup
          value={memberRole}
          onValueChange={(value) => void change(value as OrganizationRole)}>
          {assignableRoles(currentUserRole, billingRoleAllowed).map((role) => (
            <DropdownMenuRadioItem key={role} value={role}>
              {roleLabel(t, role)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};

/** forma2's MemberActions: remove, and for an invite share its link or, where the instance sends email, send it again. A membership row has nothing to share or resend, so it shows neither button; nor does an invite row for a team admin, since the server shares and resends invites for owners and managers only. */
const MemberActions = ({
  row,
  organizationId,
  name,
  canDelete,
  canManageInvites,
  emailConfigured,
}: {
  row: MemberRow;
  organizationId: string;
  name: string;
  canDelete: boolean;
  canManageInvites: boolean;
  emailConfigured: boolean;
}) => {
  const { t } = useTranslation();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [shareUrl, setShareUrl] = useState<string | null>(null);
  const removeMember = useRemoveMember(organizationId);
  const deleteInvite = useDeleteInvite(organizationId);
  const resend = useResendInvite(organizationId);
  const link = useInviteLink(organizationId);
  const invite = row.kind === "invite" && canManageInvites ? row.invite : null;

  const remove = async () => {
    try {
      if (row.kind === "invite") {
        await deleteInvite.mutateAsync(row.invite.id);
        toast.success(t("workspace.settings.general.invite_deleted_successfully"));
      } else {
        await removeMember.mutateAsync(row.member.userId);
        toast.success(t("workspace.settings.general.member_deleted_successfully"));
      }
      setDeleteOpen(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const share = async () => {
    if (!invite) return;
    try {
      const result = await link.mutateAsync(invite.id);
      setShareUrl(result.inviteUrl);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const sendAgain = async () => {
    if (!invite) return;
    try {
      await resend.mutateAsync(invite.id);
      toast.success(t("workspace.settings.general.invitation_sent_once_more"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="flex justify-end gap-2">
      <Button
        variant="destructive"
        size="icon"
        disabled={!canDelete}
        title={canDelete ? t("common.delete") : undefined}
        aria-label={t("common.delete")}
        onClick={() => setDeleteOpen(true)}>
        <TrashIcon />
      </Button>
      {invite && (
        <Button
          variant="secondary"
          size="icon"
          loading={link.isPending}
          title={t("workspace.settings.general.share_invite_link")}
          aria-label={t("workspace.settings.general.share_invite_link")}
          onClick={() => void share()}>
          <ShareIcon />
        </Button>
      )}
      {invite && emailConfigured && (
        <Button
          variant="secondary"
          size="icon"
          loading={resend.isPending}
          title={t("workspace.settings.general.resend_invitation_email")}
          aria-label={t("workspace.settings.general.resend_invitation_email")}
          onClick={() => void sendAgain()}>
          <SendHorizonalIcon />
        </Button>
      )}
      <DeleteDialog
        open={deleteOpen}
        setOpen={setDeleteOpen}
        deleteWhat={t("workspace.settings.general.from_your_organization", { memberName: name })}
        onDelete={() => void remove()}
        isDeleting={removeMember.isPending || deleteInvite.isPending}
        text={t("workspace.settings.general.delete_member_confirmation")}
      />
      <ShareInviteModal url={shareUrl} onClose={() => setShareUrl(null)} />
    </div>
  );
};

const ShareInviteModal = ({ url, onClose }: { url: string | null; onClose: () => void }) => {
  const { t } = useTranslation();
  return (
    <Dialog open={url !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <CheckIcon />
        <DialogTitle>{t("workspace.settings.general.organization_invite_link_ready")}</DialogTitle>
        <DialogDescription>
          {t(
            "workspace.settings.general.share_this_link_to_let_your_organization_member_join_your_organization"
          )}
        </DialogDescription>
      </DialogHeader>
      <DialogBody>
        <div className="flex items-center gap-2">
          <Input
            readOnly
            id="inviteLinkText"
            value={url ?? ""}
            aria-label={t("workspace.settings.general.share_invite_link")}
          />
          <Button
            variant="secondary"
            onClick={() => url && void copyText(url, t)}
            title={t("workspace.settings.general.copy_invite_link_to_clipboard")}>
            {t("common.copy_link")}
            <CopyIcon />
          </Button>
        </div>
      </DialogBody>
    </Dialog>
  );
};

/** forma2's OrganizationActions: leave (not for owners, and not your only organization) and invite. Where the instance sends no email, a new invite's link opens at once for sharing by hand. */
const OrganizationActions = ({
  organization,
  canInvite,
  adminTeamIds,
  billingRoleAllowed,
  emailConfigured,
}: {
  organization: OrganizationDetail;
  canInvite: boolean;
  adminTeamIds: string[];
  billingRoleAllowed: boolean;
  emailConfigured: boolean;
}) => {
  const { t } = useTranslation();
  const [leaveOpen, setLeaveOpen] = useState(false);
  const [inviteOpen, setInviteOpen] = useState(false);
  const link = useInviteLink(organization.id);
  const [shareUrl, setShareUrl] = useState<string | null>(null);
  const role = organization.membership.role;

  const shareNewInvite = async (inviteId: string) => {
    try {
      const result = await link.mutateAsync(inviteId);
      setShareUrl(result.inviteUrl);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <>
      <div className="mb-4 flex justify-end gap-x-2 text-right">
        {role !== "owner" && (
          <Button variant="destructive" size="sm" onClick={() => setLeaveOpen(true)}>
            {t("workspace.settings.general.leave_organization")}
            <XIcon />
          </Button>
        )}
        {canInvite && (
          <Button size="sm" onClick={() => setInviteOpen(true)}>
            {t("workspace.settings.teams.invite_member")}
          </Button>
        )}
      </div>
      <InviteMemberModal
        open={inviteOpen}
        setOpen={setInviteOpen}
        organizationId={organization.id}
        membershipRole={role}
        adminTeamIds={adminTeamIds}
        billingRoleAllowed={billingRoleAllowed}
        emailConfigured={emailConfigured}
        onCreatedWithoutEmail={(inviteId) => void shareNewInvite(inviteId)}
      />
      <ShareInviteModal url={shareUrl} onClose={() => setShareUrl(null)} />
      <LeaveOrganizationDialog
        organizationId={organization.id}
        open={leaveOpen}
        onOpenChange={setLeaveOpen}
      />
    </>
  );
};
