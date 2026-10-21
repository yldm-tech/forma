import { useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { Dialog, DialogBody, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import { FormError } from "../../components/ui/form-error";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { MultiSelect } from "../../components/ui/multi-select";
import { Select } from "../../components/ui/select";
import { Small } from "../../components/ui/typography";
import { ApiError } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { type InviteForm, type InviteFormErrors, validateInviteForm } from "../../lib/invite-form";
import { inviteFailureMessage, inviteFailureReason } from "../../lib/invites";
import { useCreateInvite } from "../../lib/queries/invites";
import { teamsQuery } from "../../lib/queries/teams";
import { type OrganizationRole, accessFlags, assignableRoles } from "../../lib/roles";
import { toast } from "../../lib/toast";

const EMPTY: InviteForm = { name: "", email: "", role: "member", teamIds: [] };

/**
 * forma2's invite member modal, individual tab. A team admin who is not an owner or manager invites members only, into the teams they administer. Bulk CSV invites are not offered here (see the module notes).
 */
export const InviteMemberModal = ({
  open,
  setOpen,
  organizationId,
  membershipRole,
  adminTeamIds,
  billingRoleAllowed,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  organizationId: string;
  membershipRole: OrganizationRole;
  adminTeamIds: string[];
  billingRoleAllowed: boolean;
}) => {
  const { t } = useTranslation();
  const [form, setForm] = useState<InviteForm>(EMPTY);
  const [errors, setErrors] = useState<InviteFormErrors>({});
  const teams = useQuery({ ...teamsQuery(organizationId), enabled: open });
  const create = useCreateInvite(organizationId);
  const { isOwnerOrManager } = accessFlags(membershipRole);
  const teamAdminOnly = !isOwnerOrManager && adminTeamIds.length > 0;

  const allTeams = [...(teams.data?.userTeams ?? []), ...(teams.data?.otherTeams ?? [])];
  const teamOptions = allTeams
    .filter((team) => !teamAdminOnly || adminTeamIds.includes(team.id))
    .map((team) => ({ label: team.name, value: team.id }));

  const close = () => {
    setOpen(false);
    setForm(EMPTY);
    setErrors({});
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const invitee = { ...form, role: teamAdminOnly ? ("member" as const) : form.role };
    const found = validateInviteForm(invitee, teamAdminOnly);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    const toastId = toast.loading(t("workspace.settings.general.inviting_member", { count: 1 }));
    try {
      await create.mutateAsync({ ...invitee, name: invitee.name.trim(), email: invitee.email.trim().toLowerCase() });
      toast.success(t("workspace.settings.general.member_invited_successfully"), { id: toastId });
      close();
    } catch (error) {
      const reason = error instanceof ApiError ? inviteFailureReason(error.code) : "unknown";
      toast.error(
        reason === "unknown" ? errorMessage(t, error) : inviteFailureMessage(t, invitee.email, reason),
        { id: toastId }
      );
    }
  };

  const roleDescriptions: Record<OrganizationRole, string> = {
    owner: t("workspace.settings.teams.owner_role_description"),
    manager: t("workspace.settings.teams.manager_role_description"),
    member: t("workspace.settings.teams.member_role_description"),
    billing: t("workspace.settings.teams.billing_role_description"),
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())} disableCloseOnOutsideClick>
      <DialogHeader>
        <DialogTitle>{t("workspace.settings.teams.invite_member")}</DialogTitle>
        <DialogDescription>{t("workspace.settings.teams.invite_member_description")}</DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="flex flex-col gap-6" noValidate>
        <DialogBody className="flex flex-col gap-6">
          <div className="flex flex-col gap-y-2">
            <Label htmlFor="memberNameInput">{t("common.full_name")}</Label>
            <Input
              id="memberNameInput"
              placeholder="e.g. Bob"
              value={form.name}
              isInvalid={!!errors.name}
              aria-describedby={errors.name ? "memberNameError" : undefined}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
            <FormError id="memberNameError">{errors.name}</FormError>
          </div>
          <div className="flex flex-col gap-y-2">
            <Label htmlFor="memberEmailInput">{t("common.email")}</Label>
            <Input
              id="memberEmailInput"
              type="email"
              placeholder="e.g. bob@work.com"
              value={form.email}
              isInvalid={!!errors.email}
              aria-describedby={errors.email ? "memberEmailError" : undefined}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
            <FormError id="memberEmailError">{errors.email}</FormError>
          </div>
          <div className="flex flex-col gap-y-2">
            <Label htmlFor="memberRoleSelect">{t("workspace.settings.teams.organization_role")}</Label>
            {teamAdminOnly ? (
              <Input id="memberRoleSelect" value={t("workspace.settings.teams.member")} disabled />
            ) : (
              <>
                <Select
                  id="memberRoleSelect"
                  className="capitalize"
                  value={form.role}
                  aria-describedby="memberRoleDescription"
                  onChange={(e) => setForm({ ...form, role: e.target.value as OrganizationRole })}>
                  {assignableRoles(membershipRole, billingRoleAllowed).map((role) => (
                    <option key={role} value={role}>
                      {role}
                    </option>
                  ))}
                </Select>
                <p id="memberRoleDescription" className="text-sm text-slate-500">
                  {roleDescriptions[form.role]}
                </p>
                {form.role === "member" && (
                  <Alert variant="info" role="status">
                    <AlertDescription>{t("workspace.settings.teams.member_role_info_message")}</AlertDescription>
                  </Alert>
                )}
              </>
            )}
          </div>
          <div className="flex flex-col gap-y-2">
            <Label htmlFor="memberTeamsSelect">{t("common.add_to_team")}</Label>
            <MultiSelect
              id="memberTeamsSelect"
              value={form.teamIds}
              options={teamOptions}
              placeholder={t("workspace.settings.teams.team_select_placeholder")}
              disabled={teamOptions.length === 0}
              onChange={(teamIds) => setForm({ ...form, teamIds })}
            />
            {teams.isSuccess && teamOptions.length === 0 && (
              <Small className="font-normal text-amber-600">{t("workspace.settings.teams.create_first_team_message")}</Small>
            )}
            <FormError>{errors.teamIds}</FormError>
          </div>
          <div className="flex flex-col gap-y-2">
            <Label htmlFor="teamRoleInput">{t("common.team_role")}</Label>
            <Input id="teamRoleInput" value={t("workspace.settings.teams.contributor")} disabled />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={create.isPending}>
            {t("common.invite")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
