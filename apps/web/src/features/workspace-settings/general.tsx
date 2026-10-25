import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertTriangleIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { FormError } from "../../components/ui/form-error";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { cn } from "../../lib/cn";
import { errorMessage } from "../../lib/errors";
import { organizationKeys } from "../../lib/queries/organizations";
import {
  type WorkspaceDetail,
  useDeleteWorkspace,
  useUpdateWorkspace,
  workspacesQuery,
} from "../../lib/queries/workspaces";
import { accessFlags } from "../../lib/roles";
import { forgetWorkspace, rememberWorkspace } from "../../lib/storage";
import { toast } from "../../lib/toast";
import {
  cooldownError,
  hasMatchingWorkspaceDeleteConfirmation,
  truncate,
  workspaceNameError,
} from "../../lib/workspace-settings";

const ReadOnlyNotice = () => {
  const { t } = useTranslation();
  return (
    <Alert variant="warning" className="mt-4" role="status">
      <AlertDescription>
        {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
      </AlertDescription>
    </Alert>
  );
};

/** forma2's workspace general settings page: name, cooldown period, custom scripts, deletion and the workspace id. */
export const WorkspaceGeneralPage = ({ workspace }: { workspace: WorkspaceDetail }) => {
  const { t } = useTranslation();
  const { isReadOnly, role } = workspace.access;
  const { isOwnerOrManager } = accessFlags(role);
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.workspace_settings")} />
      <SettingsCard
        width="full"
        title={t("common.workspace_name")}
        description={t("workspace.general.workspace_name_settings_description")}>
        <EditWorkspaceNameForm key={workspace.name} workspace={workspace} isReadOnly={isReadOnly} />
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.general.recontact_cooldown_period")}
        description={t("workspace.general.recontact_cooldown_period_settings_description")}>
        <EditCooldownPeriodForm key={workspace.recontactDays} workspace={workspace} isReadOnly={isReadOnly} />
      </SettingsCard>
      {/* forma2 offered custom scripts on self-hosted instances only; this server refuses them on Forma Cloud. */}
      <SettingsCard
        width="full"
        title={t("workspace.general.custom_scripts")}
        description={t("workspace.general.custom_scripts_card_description")}>
        <CustomScriptsForm
          key={workspace.customHeadScripts ?? ""}
          workspace={workspace}
          isReadOnly={!isOwnerOrManager}
        />
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.general.delete_workspace")}
        description={t("workspace.general.delete_workspace_settings_description")}>
        <DeleteWorkspace workspace={workspace} isOwnerOrManager={isOwnerOrManager} />
      </SettingsCard>
      <div className="space-y-2">
        <IdBadge id={workspace.id} label={t("common.workspace_id")} />
      </div>
    </PageContentWrapper>
  );
};

const EditWorkspaceNameForm = ({
  workspace,
  isReadOnly,
}: {
  workspace: WorkspaceDetail;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [name, setName] = useState(workspace.name);
  const update = useUpdateWorkspace(workspace.id);
  const nameError = workspaceNameError(name);
  const isDirty = name !== workspace.name;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (nameError) {
      toast.error(nameError);
      return;
    }
    try {
      await update.mutateAsync({ name: name.trim() });
      toast.success(t("workspace.general.workspace_name_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="pb-4">
      <form className="w-full max-w-sm items-center space-y-2" onSubmit={submit} noValidate>
        <Label htmlFor="workspace-name">{t("workspace.general.whats_your_workspace_called")}</Label>
        <Input
          id="workspace-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder={t("common.workspace_name")}
          autoComplete="off"
          required
          isInvalid={!!nameError}
          aria-describedby={nameError ? "workspace-name-error" : undefined}
          disabled={isReadOnly}
        />
        <FormError id="workspace-name-error">{nameError}</FormError>
        <Button type="submit" size="sm" loading={update.isPending} disabled={!isDirty || isReadOnly}>
          {t("common.update")}
        </Button>
      </form>
      {isReadOnly && <ReadOnlyNotice />}
    </div>
  );
};

const EditCooldownPeriodForm = ({
  workspace,
  isReadOnly,
}: {
  workspace: WorkspaceDetail;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [value, setValue] = useState(String(workspace.recontactDays));
  const update = useUpdateWorkspace(workspace.id);
  const error = cooldownError(value);
  const isDirty = value !== String(workspace.recontactDays);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (error) return;
    try {
      await update.mutateAsync({ recontactDays: Number.parseInt(value, 10) });
      toast.success(t("workspace.general.cooldown_period_updated_successfully"));
    } catch (err) {
      toast.error(errorMessage(t, err));
    }
  };

  return (
    <div className="pb-4">
      <form className="flex w-full max-w-sm flex-col gap-y-4" onSubmit={submit} noValidate>
        <div className="space-y-2">
          <Label htmlFor="recontactDays">
            {t("workspace.general.wait_x_days_before_showing_next_survey")}
          </Label>
          <Input
            type="number"
            id="recontactDays"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            isInvalid={!!error}
            aria-describedby={error ? "recontactDays-error" : undefined}
            disabled={isReadOnly}
          />
          <FormError id="recontactDays-error">{error}</FormError>
        </div>
        <Button
          type="submit"
          size="sm"
          className="w-fit"
          loading={update.isPending}
          disabled={!isDirty || !!error || isReadOnly}>
          {t("common.update")}
        </Button>
      </form>
      {isReadOnly && <ReadOnlyNotice />}
    </div>
  );
};

const CustomScriptsForm = ({
  workspace,
  isReadOnly,
}: {
  workspace: WorkspaceDetail;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const initial = workspace.customHeadScripts ?? "";
  const [scripts, setScripts] = useState(initial);
  const update = useUpdateWorkspace(workspace.id);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await update.mutateAsync({ customHeadScripts: scripts || null });
      toast.success(t("workspace.general.custom_scripts_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div className="pb-4">
      <form className="flex w-full flex-col gap-y-4" onSubmit={submit}>
        <Alert variant="warning" className="flex items-start gap-2" role="status">
          <AlertTriangleIcon className="mt-0.5 size-4 shrink-0" />
          <AlertDescription>{t("workspace.general.custom_scripts_warning")}</AlertDescription>
        </Alert>
        <div className="space-y-1">
          <Label htmlFor="customHeadScripts">{t("workspace.general.custom_scripts_label")}</Label>
          <p id="customHeadScripts-description" className="text-sm text-slate-500">
            {t("workspace.general.custom_scripts_description")}
          </p>
          <textarea
            id="customHeadScripts"
            rows={8}
            aria-describedby="customHeadScripts-description"
            placeholder={t("workspace.general.custom_scripts_placeholder")}
            className={cn(
              "focus:border-ink flex w-full rounded-md border border-slate-300 bg-transparent px-3 py-2 font-mono text-xs text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50",
              isReadOnly && "bg-slate-50"
            )}
            value={scripts}
            onChange={(e) => setScripts(e.target.value)}
            disabled={isReadOnly}
          />
        </div>
        <Button
          type="submit"
          size="sm"
          className="w-fit"
          loading={update.isPending}
          disabled={scripts === initial || isReadOnly}>
          {t("common.save")}
        </Button>
      </form>
      {isReadOnly && <ReadOnlyNotice />}
    </div>
  );
};

/** forma2's DeleteWorkspace: owners and managers, never the last workspace they can open, confirmed by typing the name. */
const DeleteWorkspace = ({
  workspace,
  isOwnerOrManager,
}: {
  workspace: WorkspaceDetail;
  isOwnerOrManager: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const workspaces = useQuery(workspacesQuery(workspace.organizationId));
  const [open, setOpen] = useState(false);
  const [confirmationName, setConfirmationName] = useState("");
  const remove = useDeleteWorkspace(workspace.id);
  const isDeleteDisabled = (workspaces.data?.length ?? 0) <= 1 || !isOwnerOrManager;
  const hasValidConfirmation = hasMatchingWorkspaceDeleteConfirmation(confirmationName, workspace.name);

  const setDialogOpen = (next: boolean) => {
    if (!next) setConfirmationName("");
    setOpen(next);
  };

  const deleteWorkspace = async () => {
    if (!hasValidConfirmation) return;
    try {
      const { destination } = await remove.mutateAsync(confirmationName);
      if (destination.workspaceId) rememberWorkspace(destination.workspaceId);
      else forgetWorkspace();
      toast.success(t("workspace.general.workspace_deleted_successfully"));
      await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(workspace.organizationId) });
      await navigate({ href: destination.path });
    } catch (error) {
      toast.error(errorMessage(t, error));
      setDialogOpen(false);
    }
  };

  if (workspaces.isPending) return null;

  return (
    <div className="pb-4">
      {isDeleteDisabled ? (
        <Alert variant="warning" role="status">
          <AlertDescription>
            {isOwnerOrManager
              ? t("workspace.general.cannot_delete_only_workspace")
              : t("workspace.general.only_owners_or_managers_can_delete_workspaces")}
          </AlertDescription>
        </Alert>
      ) : (
        <div className="space-y-2">
          <p className="text-sm text-slate-900">
            {t("workspace.general.delete_workspace_name_includes_surveys_responses_people_and_more", {
              workspaceName: truncate(workspace.name, 30),
            })}{" "}
            <strong>{t("workspace.general.this_action_cannot_be_undone")}</strong>
          </p>
          <Button variant="destructive" onClick={() => setDialogOpen(true)}>
            {t("common.delete")}
          </Button>
        </div>
      )}
      <DeleteDialog
        deleteWhat={t("workspace.settings.domain.workspace")}
        open={open}
        setOpen={setDialogOpen}
        onDelete={() => void deleteWorkspace()}
        text={t("workspace.general.delete_workspace_confirmation", {
          workspaceName: truncate(workspace.name, 30),
        })}
        isDeleting={remove.isPending}
        disabled={!hasValidConfirmation}>
        <div className="py-5">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void deleteWorkspace();
            }}>
            <label htmlFor="deleteWorkspaceConfirmation">
              {t("workspace.general.delete_workspace_confirmation_name", { workspaceName: workspace.name })}
            </label>
            <Input
              id="deleteWorkspaceConfirmation"
              value={confirmationName}
              onChange={(e) => setConfirmationName(e.target.value)}
              placeholder={workspace.name}
              className="mt-2"
              autoComplete="off"
            />
          </form>
        </div>
      </DeleteDialog>
    </div>
  );
};
