import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PlusIcon, Trash2Icon, UsersIcon, XIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { FormError } from "../../components/ui/form-error";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { LoadingBlock, SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../components/ui/table";
import { Muted } from "../../components/ui/typography";
import { untilAuthorized } from "../../lib/consistency";
import { errorMessage } from "../../lib/errors";
import { type Member, type OrganizationDetail, membersQuery } from "../../lib/queries/organizations";
import {
  type TeamDetails,
  type TeamPermission,
  type TeamRole,
  teamQuery,
  teamRoleQuery,
  teamsQuery,
  useCreateTeam,
  useDeleteTeam,
  useUpdateTeam,
} from "../../lib/queries/teams";
import { workspacesQuery } from "../../lib/queries/workspaces";
import { accessFlags, canManageTeam, defaultTeamRole } from "../../lib/roles";
import {
  type TeamForm,
  type TeamFormErrors,
  remainingOptions,
  toTeamUpdate,
  validateTeamForm,
} from "../../lib/team-form";
import { toast } from "../../lib/toast";
import { teamNameError } from "../../lib/validation";

/** forma2's teams card: the caller's teams first, then the organization's other teams, each with who may manage it. */
export const TeamsCard = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const teams = useQuery(teamsQuery(organization.id));
  const [createOpen, setCreateOpen] = useState(false);
  const [managedTeamId, setManagedTeamId] = useState<string | null>(null);
  const { isOwnerOrManager } = accessFlags(organization.membership.role);
  const rows = [
    ...(teams.data?.userTeams ?? []).map((team) => ({ team, userRole: team.userRole, isMember: true })),
    ...(teams.data?.otherTeams ?? []).map((team) => ({ team, userRole: undefined, isMember: false })),
  ];

  return (
    <SettingsCard
      title={t("workspace.settings.teams.teams")}
      description={t("workspace.settings.teams.teams_description")}
      bodyVariant="flush"
      width="full">
      {isOwnerOrManager && (
        <div className="mb-4 flex justify-end px-4 pt-4">
          <Button size="sm" variant="secondary" onClick={() => setCreateOpen(true)}>
            {t("workspace.settings.teams.create_new_team")}
          </Button>
        </div>
      )}
      {teams.isPending ? (
        <div className="px-4 py-4">
          <LoadingBlock />
        </div>
      ) : rows.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-slate-500">
          {t("workspace.settings.teams.empty_teams_state")}
        </p>
      ) : (
        <Table aria-label={t("common.teams")}>
          <TableHeader>
            <TableRow>
              <TableHead className="w-[40%]">{t("workspace.settings.teams.team_name")}</TableHead>
              <TableHead className="w-[20%]">{t("common.size")}</TableHead>
              <TableHead className="w-[20%]">
                <span className="sr-only">{t("common.membership")}</span>
              </TableHead>
              <TableHead className="w-[20%]">
                <span className="sr-only">{t("common.actions")}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(({ team, userRole, isMember }) => {
              const manageable = canManageTeam({ userRole }, isOwnerOrManager);
              return (
                <TableRow key={team.id}>
                  <TableCell>{team.name}</TableCell>
                  <TableCell>{t("common.count_members", { count: team.memberCount })}</TableCell>
                  <TableCell>
                    {isMember && (
                      <Badge
                        type="success"
                        size="tiny"
                        text={t("workspace.settings.teams.you_are_a_member")}
                      />
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end">
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={!manageable}
                        title={manageable ? undefined : t("workspace.settings.teams.manage_team_disabled")}
                        onClick={() => setManagedTeamId(team.id)}>
                        {t("workspace.settings.teams.manage_team")}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
      <CreateTeamModal open={createOpen} setOpen={setCreateOpen} organizationId={organization.id} />
      {managedTeamId && (
        <TeamSettingsModal
          teamId={managedTeamId}
          organization={organization}
          onClose={() => setManagedTeamId(null)}
        />
      )}
    </SettingsCard>
  );
};

const CreateTeamModal = ({
  open,
  setOpen,
  organizationId,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
  organizationId: string;
}) => {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const create = useCreateTeam(organizationId);
  const queryClient = useQueryClient();

  const close = () => {
    setOpen(false);
    setName("");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const { id } = await create.mutateAsync(name.trim());
      // The team is managed right after this; wait until its permissions have arrived.
      await untilAuthorized(() => queryClient.fetchQuery(teamQuery(id)));
      toast.success(t("workspace.settings.teams.team_created_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())}>
      <DialogHeader>
        <UsersIcon />
        <DialogTitle>{t("workspace.settings.teams.create_new_team")}</DialogTitle>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        <DialogBody>
          <div className="grid w-full gap-y-2 pb-4">
            <Label htmlFor="team-name">{t("workspace.settings.teams.team_name")}</Label>
            <Input
              id="team-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("workspace.settings.teams.enter_team_name")}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" type="button" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={teamNameError(name) !== null} loading={create.isPending}>
            {t("workspace.settings.teams.create")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};

/** Loads what the team settings form needs: the team, the caller's role in it, and the organization's members and workspaces to choose from. */
const TeamSettingsModal = ({
  teamId,
  organization,
  onClose,
}: {
  teamId: string;
  organization: OrganizationDetail;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const team = useQuery(teamQuery(teamId));
  const teamRole = useQuery(teamRoleQuery(teamId));
  const members = useQuery(membersQuery(organization.id));
  const workspaces = useQuery(workspacesQuery(organization.id));
  const error = team.error ?? teamRole.error ?? members.error ?? workspaces.error;
  const ready = team.data && teamRole.isSuccess && members.data && workspaces.data;

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      {error ? (
        <>
          <DialogHeader>
            <DialogTitle>{t("common.something_went_wrong")}</DialogTitle>
          </DialogHeader>
          <p role="alert" className="text-sm text-slate-600">
            {errorMessage(t, error)}
          </p>
        </>
      ) : ready ? (
        <TeamSettingsForm
          team={team.data}
          userTeamRole={teamRole.data ?? null}
          orgMembers={members.data}
          orgWorkspaces={workspaces.data}
          organization={organization}
          onClose={onClose}
        />
      ) : (
        <div className="space-y-4 py-4" role="status" aria-label={t("common.loading")}>
          <DialogTitle className="sr-only">{t("common.loading")}</DialogTitle>
          <LoadingBlock />
          <LoadingBlock />
        </div>
      )}
    </Dialog>
  );
};

/**
 * forma2's TeamSettingsModal. Owners and managers change everything; a team admin who is a plain member changes the name and the members (other than themselves) but not which workspaces the team reaches; anyone else only looks. Rows that existed when the form opened keep their member or workspace.
 */
const TeamSettingsForm = ({
  team,
  userTeamRole,
  orgMembers,
  orgWorkspaces,
  organization,
  onClose,
}: {
  team: TeamDetails;
  userTeamRole: TeamRole | null;
  orgMembers: Member[];
  orgWorkspaces: { id: string; name: string }[];
  organization: OrganizationDetail;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const { isOwnerOrManager, isMember } = accessFlags(organization.membership.role);
  const isTeamAdminMember = isMember && userTeamRole === "admin";
  const isTeamContributorMember = isMember && userTeamRole === "contributor";
  const canEditMembers = isOwnerOrManager || isTeamAdminMember;
  const currentUserId = organization.membership.userId;
  const initialMemberIds = new Set(team.members.map((member) => member.userId));
  const initialWorkspaceIds = new Set(team.workspaces.map((workspace) => workspace.workspaceId));
  const [form, setForm] = useState<TeamForm>(() => ({
    name: team.name,
    members: team.members.length
      ? team.members.map(({ userId, role }) => ({ userId, role }))
      : [{ userId: "", role: "contributor" }],
    workspaces: team.workspaces.length
      ? team.workspaces.map(({ workspaceId, permission }) => ({ workspaceId, permission }))
      : [{ workspaceId: "", permission: "read" }],
  }));
  const [errors, setErrors] = useState<TeamFormErrors>({});
  const update = useUpdateTeam(organization.id, team.id);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const remove = useDeleteTeam(organization.id, team.id);

  const people = orgMembers.map((member) => ({ id: member.userId, name: member.name, role: member.role }));
  const chosenMembers = form.members.map((member) => member.userId);
  const chosenWorkspaces = form.workspaces.map((workspace) => workspace.workspaceId);
  const hasEmptyMember = form.members.some((member) => !member.userId);
  const hasEmptyWorkspace = form.workspaces.some((workspace) => !workspace.workspaceId);

  const setMember = (index: number, userId: string) => {
    const person = people.find((p) => p.id === userId);
    setForm({
      ...form,
      members: form.members.map((member, i) =>
        i === index ? { userId, role: person ? defaultTeamRole(person.role) : member.role } : member
      ),
    });
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const found = validateTeamForm(form);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    try {
      await update.mutateAsync(toTeamUpdate(form));
      toast.success(t("workspace.settings.teams.team_updated_successfully"));
      onClose();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const deleteTeam = async () => {
    try {
      await remove.mutateAsync();
      toast.success(t("workspace.settings.teams.team_deleted_successfully"));
      setDeleteOpen(false);
      onClose();
    } catch (error) {
      toast.error(errorMessage(t, error));
      setDeleteOpen(false);
    }
  };

  return (
    <>
      <DialogHeader className="pb-4">
        <DialogTitle>
          {t("workspace.settings.teams.team_name_settings_title", { teamName: team.name })}
        </DialogTitle>
        <DialogDescription>{t("workspace.settings.teams.team_settings_description")}</DialogDescription>
      </DialogHeader>
      <form className="contents" onSubmit={submit} noValidate>
        <DialogBody className="space-y-6">
          <div className="space-y-2">
            <Label htmlFor="team-settings-name">{t("common.team_name")}</Label>
            <Input
              id="team-settings-name"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              disabled={!canEditMembers}
              isInvalid={!!errors.name}
            />
            <FormError>{errors.name}</FormError>
          </div>
          <IdBadge id={team.id} label={t("common.team_id")} />

          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-slate-800">{t("common.members")}</legend>
            <Muted className="block">{t("workspace.settings.teams.add_members_description")}</Muted>
            {form.members.map((member, index) => {
              const chosen = people.find((p) => p.id === member.userId);
              const isExisting = !!member.userId && initialMemberIds.has(member.userId);
              const roleLocked =
                chosen === undefined
                  ? !canEditMembers
                  : chosen.role === "owner" ||
                    chosen.role === "manager" ||
                    isTeamContributorMember ||
                    chosen.id === currentUserId;
              return (
                // oxlint-disable-next-line react/no-array-index-key -- rows are positional until a member is chosen
                <div key={index} className="flex gap-2.5">
                  <Select
                    aria-label={t("workspace.settings.teams.member")}
                    value={member.userId}
                    disabled={isExisting || !canEditMembers}
                    onChange={(e) => setMember(index, e.target.value)}>
                    <option value="" disabled>
                      {t("common.select")}
                    </option>
                    {remainingOptions(people, chosenMembers, member.userId).map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </Select>
                  <Select
                    aria-label={t("common.team_role")}
                    value={member.role}
                    disabled={roleLocked}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        members: form.members.map((m, i) =>
                          i === index ? { ...m, role: e.target.value as TeamRole } : m
                        ),
                      })
                    }>
                    <option value="admin">{t("workspace.settings.teams.team_admin")}</option>
                    <option value="contributor">{t("workspace.settings.teams.contributor")}</option>
                  </Select>
                  {form.members.length > 1 && (
                    <Button
                      size="icon"
                      type="button"
                      variant="destructive"
                      className="shrink-0"
                      aria-label={t("common.remove_from_team")}
                      title={t("common.remove_from_team")}
                      disabled={!isOwnerOrManager && (!isTeamAdminMember || member.userId === currentUserId)}
                      onClick={() =>
                        setForm({ ...form, members: form.members.filter((_, i) => i !== index) })
                      }>
                      <XIcon className="size-4" />
                    </Button>
                  )}
                </div>
              );
            })}
            <FormError>{errors.members}</FormError>
            <Button
              size="sm"
              type="button"
              variant="secondary"
              title={
                hasEmptyMember
                  ? t("workspace.settings.teams.please_fill_all_member_fields")
                  : chosenMembers.length === people.length
                    ? t("workspace.settings.teams.all_members_added")
                    : undefined
              }
              disabled={!canEditMembers || chosenMembers.length === people.length || hasEmptyMember}
              onClick={() =>
                setForm({ ...form, members: [...form.members, { userId: "", role: "contributor" }] })
              }>
              <PlusIcon className="size-4" />
              <span>{t("common.add_member")}</span>
            </Button>
          </fieldset>

          <fieldset className="space-y-2">
            <legend className="text-sm font-medium text-slate-800">{t("common.workspaces")}</legend>
            <Muted className="block">{t("workspace.settings.teams.add_workspaces_description")}</Muted>
            {form.workspaces.map((workspace, index) => {
              const isExisting = !!workspace.workspaceId && initialWorkspaceIds.has(workspace.workspaceId);
              const known = orgWorkspaces.some((w) => w.id === workspace.workspaceId);
              const fallbackName = team.workspaces.find(
                (w) => w.workspaceId === workspace.workspaceId
              )?.workspaceName;
              return (
                // oxlint-disable-next-line react/no-array-index-key -- rows are positional until a workspace is chosen
                <div key={index} className="flex gap-2.5">
                  <Select
                    aria-label={t("common.workspace")}
                    value={workspace.workspaceId}
                    disabled={isExisting || !isOwnerOrManager}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        workspaces: form.workspaces.map((w, i) =>
                          i === index ? { ...w, workspaceId: e.target.value } : w
                        ),
                      })
                    }>
                    <option value="" disabled>
                      {t("common.select")}
                    </option>
                    {!known && workspace.workspaceId && (
                      <option value={workspace.workspaceId}>{fallbackName ?? workspace.workspaceId}</option>
                    )}
                    {remainingOptions(orgWorkspaces, chosenWorkspaces, workspace.workspaceId).map(
                      (option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      )
                    )}
                  </Select>
                  <Select
                    aria-label={t("workspace.api_keys.permissions")}
                    value={workspace.permission}
                    disabled={!isOwnerOrManager}
                    onChange={(e) =>
                      setForm({
                        ...form,
                        workspaces: form.workspaces.map((w, i) =>
                          i === index ? { ...w, permission: e.target.value as TeamPermission } : w
                        ),
                      })
                    }>
                    <option value="read">{t("workspace.settings.teams.read")}</option>
                    <option value="readWrite">{t("workspace.settings.teams.read_write")}</option>
                    <option value="manage">{t("workspace.settings.teams.manage")}</option>
                  </Select>
                  {form.workspaces.length > 1 && (
                    <Button
                      size="icon"
                      type="button"
                      variant="secondary"
                      className="shrink-0"
                      aria-label={t("common.remove")}
                      disabled={!isOwnerOrManager}
                      onClick={() =>
                        setForm({ ...form, workspaces: form.workspaces.filter((_, i) => i !== index) })
                      }>
                      <Trash2Icon className="size-4" />
                    </Button>
                  )}
                </div>
              );
            })}
            <FormError>{errors.workspaces}</FormError>
            <Button
              size="sm"
              type="button"
              variant="secondary"
              title={
                hasEmptyWorkspace
                  ? t("workspace.settings.teams.please_fill_all_workspace_fields")
                  : chosenWorkspaces.length === orgWorkspaces.length
                    ? t("workspace.settings.teams.all_workspaces_added")
                    : undefined
              }
              disabled={
                !isOwnerOrManager || chosenWorkspaces.length === orgWorkspaces.length || hasEmptyWorkspace
              }
              onClick={() =>
                setForm({
                  ...form,
                  workspaces: [...form.workspaces, { workspaceId: "", permission: "read" }],
                })
              }>
              <PlusIcon className="size-4" />
              {t("common.add_workspace")}
            </Button>
          </fieldset>
        </DialogBody>
        <DialogFooter>
          <div className="w-full">
            <Button
              variant="destructive"
              type="button"
              disabled={!isOwnerOrManager}
              title={isOwnerOrManager ? undefined : t("workspace.settings.teams.team_deletion_not_allowed")}
              onClick={() => setDeleteOpen(true)}>
              {t("workspace.settings.teams.delete_team")}
            </Button>
          </div>
          <Button type="button" variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={update.isPending} disabled={!canEditMembers}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </form>
      <DeleteDialog
        open={deleteOpen}
        setOpen={setDeleteOpen}
        deleteWhat={t("common.team")}
        text={t("workspace.settings.teams.are_you_sure_you_want_to_delete_this_team")}
        onDelete={() => void deleteTeam()}
        isDeleting={remove.isPending}
      />
    </>
  );
};
