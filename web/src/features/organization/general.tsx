import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { PlusCircleIcon, ShieldCheckIcon } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../components/ui/alert";
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
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { Switch } from "../../components/ui/switch";
import { untilAuthorized } from "../../lib/consistency";
import { errorMessage } from "../../lib/errors";
import { billingSummaryQuery } from "../../lib/queries/billing";
import { instanceQuery } from "../../lib/queries/instance";
import {
  type OrganizationDetail,
  organizationKeys,
  organizationQuery,
  useCreateOrganization,
  useDeleteOrganization,
  useUpdateOrganization,
} from "../../lib/queries/organizations";
import { accessFlags } from "../../lib/roles";
import { forgetWorkspace } from "../../lib/storage";
import { UTC_DEFAULT, ianaTimeZones, organizationSettingsChanges } from "../../lib/time-zones";
import { toast } from "../../lib/toast";
import { aiKeys, organizationAIQuery } from "../ai/availability";
import { organizationNameError } from "../../lib/validation";
import { EmailCustomizationCard } from "./whitelabel/email-customization-card";

/** forma2's SecurityListTip, shown self-hosted: an invitation to the security announcements list. */
const SecurityListTip = () => {
  const { t } = useTranslation();
  return (
    <div className="max-w-4xl">
      <div className="flex items-center gap-x-3 rounded-lg border border-blue-100 bg-blue-50 p-4 text-sm text-blue-900 shadow-xs">
        <ShieldCheckIcon className="size-5 shrink-0 text-blue-400" />
        <p className="text-sm">
          {t("workspace.settings.general.security_list_tip")}{" "}
          <a
            href="https://forma.yldm.ai/security?utm_source=forma-app&utm_medium=webapp&utm_campaign=security_tip#stay-informed-with-forma-security-updates"
            target="_blank"
            rel="noopener noreferrer"
            className="underline hover:text-blue-700">
            {t("workspace.settings.general.security_list_tip_link")}
          </a>
        </p>
      </div>
    </div>
  );
};

/** forma2's organization general settings page: on top a warning without file storage and, self-hosted, the security list tip; at the foot the organization's id and the Forma version. */
export const OrganizationGeneralPage = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const role = organization.membership.role;
  const { data: instance } = useQuery(instanceQuery);
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("workspace.settings.general.organization_settings")} />
      {instance?.storageConfigured === false && (
        <div className="max-w-4xl">
          <Alert variant="warning" role="status">
            <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
          </Alert>
        </div>
      )}
      {billing?.isFormaCloud === false && <SecurityListTip />}
      <SettingsCard
        width="full"
        title={t("workspace.settings.general.organization_settings")}
        description={t("workspace.settings.general.organization_settings_description")}>
        <EditOrganizationSettingsForm key={organization.updatedAt} organization={organization} />
      </SettingsCard>
      <SettingsCard
        width="full"
        title={t("workspace.settings.general.ai_enabled")}
        description={t("workspace.settings.general.ai_enabled_description")}>
        <AISettingsToggle organization={organization} />
      </SettingsCard>
      <EmailCustomizationCard key={organization.updatedAt} organization={organization} />
      <SettingsCard
        width="full"
        title={t("workspace.settings.general.delete_organization")}
        description={t("workspace.settings.general.delete_organization_description")}>
        <DeleteOrganization organization={organization} isOwner={role === "owner"} />
      </SettingsCard>
      <CreateOrganizationCard />
      <div className="space-y-2">
        <IdBadge id={organization.id} label={t("common.organization_id")} />
        {instance && <IdBadge id={instance.version} label={t("common.forma_version")} />}
      </div>
    </PageContentWrapper>
  );
};

const EditOrganizationSettingsForm = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const role = organization.membership.role;
  const { isOwner, isOwnerOrManager: canEditTimeZone } = accessFlags(role);
  const initial = { name: organization.name, displayTimeZone: organization.displayTimeZone };
  const [name, setName] = useState(initial.name);
  const [displayTimeZone, setDisplayTimeZone] = useState(initial.displayTimeZone);
  const [nameError, setNameError] = useState<string | null>(null);
  const update = useUpdateOrganization(organization.id);
  const queryClient = useQueryClient();
  const zones = useMemo(() => ianaTimeZones(), []);
  const changes = organizationSettingsChanges(initial, { name, displayTimeZone }, role);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!changes) return;
    if (changes.name !== undefined) {
      const invalid = organizationNameError(changes.name);
      if (invalid) {
        setNameError(invalid);
        return;
      }
    }
    // forma2 saved the two fields through separate actions, each with its own confirmation.
    try {
      if (changes.name !== undefined) {
        await update.mutateAsync({ name: changes.name });
        toast.success(t("workspace.settings.general.organization_name_updated_successfully"));
      }
      if (changes.displayTimeZone !== undefined) {
        await update.mutateAsync({ displayTimeZone: changes.displayTimeZone });
        toast.success(t("workspace.settings.general.display_time_zone_updated_successfully"));
      }
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
    await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organization.id) });
  };

  return (
    <>
      <form className="w-full max-w-md space-y-4 pb-4" onSubmit={submit} noValidate>
        <div className="space-y-2">
          <Label htmlFor="organization-name">{t("workspace.settings.general.organization_name")}</Label>
          <Input
            id="organization-name"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setNameError(null);
            }}
            disabled={!isOwner}
            isInvalid={!!nameError}
            aria-describedby={nameError ? "organization-name-error" : undefined}
            placeholder={t("workspace.settings.general.organization_name_placeholder")}
            required
          />
          <FormError id="organization-name-error">{nameError}</FormError>
        </div>
        <div className="space-y-2">
          <Label htmlFor="display-time-zone">{t("workspace.settings.general.display_time_zone")}</Label>
          <p id="display-time-zone-description" className="text-sm text-slate-500">
            {t("workspace.settings.general.display_time_zone_description")}
          </p>
          <Select
            id="display-time-zone"
            aria-describedby="display-time-zone-description"
            value={displayTimeZone ?? UTC_DEFAULT}
            onChange={(e) => setDisplayTimeZone(e.target.value === UTC_DEFAULT ? null : e.target.value)}
            disabled={!canEditTimeZone}>
            <option value={UTC_DEFAULT}>
              {t("workspace.settings.general.display_time_zone_utc_default")}
            </option>
            {zones.map((zone) => (
              <option key={zone} value={zone}>
                {zone}
              </option>
            ))}
          </Select>
        </div>
        <Button type="submit" size="sm" loading={update.isPending} disabled={!changes}>
          {t("common.update")}
        </Button>
      </form>
      {!canEditTimeZone && (
        <Alert variant="warning" role="status" className="mb-4">
          <AlertDescription>
            {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
          </AlertDescription>
        </Alert>
      )}
    </>
  );
};

/**
 * forma2's AISettingsToggle: owners and managers switch AI smart tools on and off. AI needs a provider configured on the instance; without one the toggle reads off, cannot be used, and says why.
 */
const AISettingsToggle = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const { isOwnerOrManager: canEdit } = accessFlags(organization.membership.role);
  const update = useUpdateOrganization(organization.id);
  const queryClient = useQueryClient();
  const ai = useQuery(organizationAIQuery(organization.id));
  const isInstanceConfigured = ai.data?.isInstanceConfigured ?? false;
  const enabled = isInstanceConfigured && organization.isAISmartToolsEnabled;
  const blockedMessage = t("workspace.settings.general.ai_instance_not_configured");

  const toggle = async (checked: boolean) => {
    if (checked && !isInstanceConfigured) {
      toast.error(blockedMessage);
      return;
    }
    try {
      await update.mutateAsync({ isAISmartToolsEnabled: checked });
      toast.success(t("workspace.settings.general.ai_settings_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
    await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organization.id) });
    await queryClient.invalidateQueries({ queryKey: aiKeys.organization(organization.id) });
  };

  return (
    <div className="space-y-4 pb-4">
      {ai.isSuccess && !isInstanceConfigured && (
        <Alert variant="warning" role="status">
          <AlertDescription>{blockedMessage}</AlertDescription>
        </Alert>
      )}
      <div className="flex items-center gap-x-4">
        <Switch
          id="ai-smart-tools-toggle"
          aria-labelledby="ai-smart-tools-title"
          checked={enabled}
          disabled={!canEdit || !isInstanceConfigured || update.isPending}
          onCheckedChange={(checked) => void toggle(checked)}
        />
        <div>
          <p id="ai-smart-tools-title" className="text-sm font-semibold text-slate-800">
            {t("workspace.settings.general.ai_smart_tools_enabled")}
          </p>
          <p className="text-xs text-slate-600">
            {t("workspace.settings.general.ai_smart_tools_enabled_description")}
          </p>
        </div>
      </div>
      {!canEdit && (
        <Alert variant="warning" role="status">
          <AlertDescription>
            {t("common.only_owners_managers_and_manage_access_members_can_perform_this_action")}
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
};

/** forma2's DeleteOrganization: owners only, confirmed by typing the organization's exact name. */
const DeleteOrganization = ({
  organization,
  isOwner,
}: {
  organization: OrganizationDetail;
  isOwner: boolean;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const remove = useDeleteOrganization(organization.id);

  if (!isOwner) {
    return (
      <Alert variant="warning" role="status" className="mb-4">
        <AlertDescription>
          {t("workspace.settings.general.only_org_owner_can_perform_action")}
        </AlertDescription>
      </Alert>
    );
  }

  const deleteOrganization = async () => {
    try {
      await remove.mutateAsync();
    } catch (error) {
      toast.error(errorMessage(t, error));
      return;
    }
    toast.success(t("workspace.settings.general.organization_deleted_successfully"));
    forgetWorkspace();
    setOpen(false);
    queryClient.removeQueries({ queryKey: organizationKeys.detail(organization.id) });
    await queryClient.invalidateQueries({ queryKey: organizationKeys.all });
    await navigate({ to: "/" });
  };

  return (
    <div className="pb-4">
      <p className="text-sm text-slate-900">{t("workspace.settings.general.once_its_gone_its_gone")}</p>
      <Button size="sm" variant="destructive" className="mt-4" onClick={() => setOpen(true)}>
        {t("common.delete")}
      </Button>
      <DeleteDialog
        open={open}
        setOpen={(next) => {
          setOpen(next);
          if (!next) setConfirmation("");
        }}
        deleteWhat={t("common.organization")}
        onDelete={() => void deleteOrganization()}
        text={t("workspace.settings.general.delete_organization_warning")}
        disabled={confirmation !== organization.name}
        isDeleting={remove.isPending}>
        <div className="py-5">
          <ul className="list-disc pb-6 pl-6">
            <li>{t("workspace.settings.general.delete_organization_warning_1")}</li>
            <li>{t("workspace.settings.general.delete_organization_warning_2")}</li>
          </ul>
          <form onSubmit={(e) => e.preventDefault()}>
            <label htmlFor="deleteOrganizationConfirmation">
              {t("workspace.settings.general.delete_organization_warning_3", {
                organizationName: organization.name,
              })}
            </label>
            <Input
              id="deleteOrganizationConfirmation"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
              placeholder={organization.name}
              className="mt-5"
              autoComplete="off"
            />
          </form>
        </div>
      </DeleteDialog>
    </div>
  );
};

/** forma2's CreateOrganizationCard: any signed-in user may create another organization. */
const CreateOrganizationCard = () => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <SettingsCard
      width="full"
      title={t("workspace.settings.general.create_new_organization")}
      description={t("workspace.settings.general.create_new_organization_description")}>
      <div className="pb-4">
        <Button size="sm" onClick={() => setOpen(true)}>
          {t("workspace.settings.general.create_new_organization")}
        </Button>
      </div>
      <CreateOrganizationModal open={open} setOpen={setOpen} />
    </SettingsCard>
  );
};

export const CreateOrganizationModal = ({
  open,
  setOpen,
}: {
  open: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useCreateOrganization();

  const close = () => {
    setOpen(false);
    setName("");
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (name.trim() === "") return;
    try {
      const organization = await create.mutateAsync(name.trim());
      await untilAuthorized(() => queryClient.fetchQuery(organizationQuery(organization.id)));
      await queryClient.invalidateQueries({ queryKey: organizationKeys.all });
      toast.success(t("workspace.settings.general.organization_created_successfully"));
      close();
      await navigate({ href: `/organizations/${organization.id}/` });
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? setOpen(true) : close())} disableCloseOnOutsideClick>
      <DialogHeader>
        <PlusCircleIcon />
        <DialogTitle>{t("workspace.settings.general.create_new_organization")}</DialogTitle>
        <DialogDescription>
          {t("workspace.settings.general.create_new_organization_description")}
        </DialogDescription>
      </DialogHeader>
      <form onSubmit={submit} className="space-y-4">
        <DialogBody>
          <div className="grid w-full gap-y-2">
            <Label htmlFor="new-organization-name">{t("workspace.settings.general.organization_name")}</Label>
            <Input
              id="new-organization-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("workspace.settings.general.organization_name_placeholder")}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={close}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" loading={create.isPending} disabled={name.trim() === ""}>
            {t("workspace.settings.general.create_new_organization")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
