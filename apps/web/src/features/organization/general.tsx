import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ShieldCheckIcon } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { CreateOrganizationModal } from "../../components/shell/create-organization-modal";
import { useUnsavedChanges } from "../../components/shell/unsaved-changes";
import { Alert, AlertDescription } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { FormError } from "../../components/ui/form-error";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { InputCombobox } from "../../components/ui/input-combo-box";
import { Label } from "../../components/ui/label";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { Switch } from "../../components/ui/switch";
import { TransLink } from "../../components/ui/trans-link";
import { errorMessage } from "../../lib/errors";
import { billingSummaryQuery } from "../../lib/queries/billing";
import { instanceQuery } from "../../lib/queries/instance";
import {
  type OrganizationDetail,
  organizationCreationQuery,
  organizationKeys,
  useDeleteOrganization,
  useUpdateOrganization,
} from "../../lib/queries/organizations";
import { type OrganizationRole, accessFlags } from "../../lib/roles";
import { forgetWorkspace } from "../../lib/storage";
import {
  INSTANCE_DEFAULT,
  UTC_DEFAULT,
  ianaTimeZones,
  organizationSettingsChanges,
  schedulingTimeZones,
} from "../../lib/time-zones";
import { toast } from "../../lib/toast";
import { aiKeys, organizationAIQuery } from "../ai/availability";
import { getSurveySchedulingTimeLabel } from "../survey-editor/lib/scheduling-config";
import { organizationNameError } from "../../lib/validation";
import { RetentionCard } from "./retention";
import { TwoFactorRequirementCard } from "./two-factor-requirement";
import { EmailCustomizationCard } from "./whitelabel/email-customization-card";

/** The GitHub page where Forma publishes its security advisories; its Watch menu subscribes to them and to the releases that ship the fixes. */
const SECURITY_ADVISORIES_URL = "https://github.com/yldm-tech/forma/security/advisories";

/** forma2's SecurityListTip, shown self-hosted to owners, who run the instance and decide when it is upgraded: how to be told about security fixes. forma2 pointed at a mailing list that Forma does not have; the fixes are announced as GitHub security advisories and releases instead. */
const SecurityListTip = () => (
  <div className="flex items-center gap-x-3 rounded-lg border border-blue-100 bg-blue-50 p-4 text-sm text-blue-900 shadow-xs">
    <ShieldCheckIcon className="size-5 shrink-0 text-blue-400" />
    <p className="text-sm">
      <Trans
        i18nKey="organizations.security_tip_github"
        components={{
          githubLink: (
            <TransLink
              href={SECURITY_ADVISORIES_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="underline hover:text-blue-700"
            />
          ),
        }}
      />
    </p>
  </div>
);

/** forma2's organization general settings page: on top a warning without file storage and, self-hosted and for owners, the security list tip; for owners, the Data retention card; at the foot the organization's id and the Forma version. A member or billing user, who can change nothing here, is told so once at the top rather than in every card. */
export const OrganizationGeneralPage = ({ organization }: { organization: OrganizationDetail }) => {
  const { t } = useTranslation();
  const role = organization.membership.role;
  const { isOwner, isOwnerOrManager } = accessFlags(role);
  const { data: instance } = useQuery(instanceQuery);
  const { data: billing } = useQuery(billingSummaryQuery(organization.id));
  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("workspace.settings.general.organization_settings")} />
      {!isOwnerOrManager && (
        <Alert variant="info" role="status">
          <AlertDescription>{t("workspace.settings.general.view_only_access")}</AlertDescription>
        </Alert>
      )}
      {instance?.storageConfigured === false && (
        <Alert variant="warning" role="status">
          <AlertDescription>{t("common.storage_not_configured")}</AlertDescription>
        </Alert>
      )}
      {isOwner && billing?.isFormaCloud === false && <SecurityListTip />}
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
      <TwoFactorRequirementCard organization={organization} />
      {isOwner && (
        <RetentionCard organizationId={organization.id} isFormaCloud={billing?.isFormaCloud === true} />
      )}
      <EmailCustomizationCard key={organization.updatedAt} organization={organization} />
      <SettingsCard
        width="full"
        title={t("workspace.settings.general.delete_organization")}
        description={t("workspace.settings.general.delete_organization_description")}>
        <DeleteOrganization organization={organization} role={role} />
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
  const initial = {
    name: organization.name,
    displayTimeZone: organization.displayTimeZone,
    surveySchedulingTimeZone: organization.surveySchedulingTimeZone,
  };
  const [name, setName] = useState(initial.name);
  const [displayTimeZone, setDisplayTimeZone] = useState(initial.displayTimeZone);
  const [surveySchedulingTimeZone, setSurveySchedulingTimeZone] = useState(initial.surveySchedulingTimeZone);
  const [nameError, setNameError] = useState<string | null>(null);
  const update = useUpdateOrganization(organization.id);
  const queryClient = useQueryClient();
  const storedDisplayTimeZone = organization.displayTimeZone;
  const zones = useMemo(() => ianaTimeZones(storedDisplayTimeZone), [storedDisplayTimeZone]);
  const { instanceTimeZone } = organization.surveyScheduling;
  const storedSchedulingTimeZone = organization.surveySchedulingTimeZone;
  const schedulingZoneOptions = useMemo(
    () => [
      {
        value: INSTANCE_DEFAULT,
        label: t("workspace.settings.general.survey_scheduling_time_zone_instance_default", {
          timeZone: instanceTimeZone,
        }),
      },
      ...schedulingTimeZones(storedSchedulingTimeZone).map((zone) => ({ value: zone, label: zone })),
    ],
    [t, instanceTimeZone, storedSchedulingTimeZone]
  );
  const changes = organizationSettingsChanges(
    initial,
    { name, displayTimeZone, surveySchedulingTimeZone },
    role
  );
  useUnsavedChanges(changes !== null);

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
      if (changes.surveySchedulingTimeZone !== undefined) {
        await update.mutateAsync({ surveySchedulingTimeZone: changes.surveySchedulingTimeZone });
        toast.success(t("workspace.settings.general.survey_scheduling_time_zone_updated_successfully"));
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
        <div className="space-y-2">
          <Label id="survey-scheduling-time-zone-label">
            {t("workspace.settings.general.survey_scheduling_time_zone")}
          </Label>
          <p id="survey-scheduling-time-zone-description" className="text-sm text-slate-500">
            {t("workspace.settings.general.survey_scheduling_time_zone_description_with_times", {
              time: getSurveySchedulingTimeLabel(organization.surveyScheduling),
            })}
          </p>
          <InputCombobox
            id="survey-scheduling-time-zone"
            aria-labelledby="survey-scheduling-time-zone-label"
            aria-describedby="survey-scheduling-time-zone-description"
            options={schedulingZoneOptions}
            value={surveySchedulingTimeZone ?? INSTANCE_DEFAULT}
            onChangeValue={(value) =>
              setSurveySchedulingTimeZone(
                typeof value === "string" && value !== INSTANCE_DEFAULT && value !== "" ? value : null
              )
            }
            showCheckIcon
            comboboxClasses="max-w-none"
            disabled={!canEditTimeZone}
          />
        </div>
        <Button type="submit" size="sm" loading={update.isPending} disabled={!changes}>
          {t("common.update")}
        </Button>
      </form>
    </>
  );
};

/**
 * forma2's AISettingsToggle: owners and managers switch AI smart tools on and off. AI needs a provider configured on the instance; without one the toggle reads off, cannot be used, and says why.
 *
 * Under it, the "Allow AI to read responses" switch, which forma2 did not have: features that send respondents' answers to the AI provider (the open-text summary) need it on as well as AI. It is independent of the AI switch and of the instance's provider, so owners and managers can decide it before turning AI on; everyone else sees both read-only.
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

  const save = async (
    change: { isAISmartToolsEnabled: boolean } | { isAIResponseReadingEnabled: boolean }
  ) => {
    try {
      await update.mutateAsync(change);
      toast.success(t("workspace.settings.general.ai_settings_updated_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
    await queryClient.invalidateQueries({ queryKey: organizationKeys.detail(organization.id) });
    await queryClient.invalidateQueries({ queryKey: aiKeys.organization(organization.id) });
  };

  const toggle = async (checked: boolean) => {
    if (checked && !isInstanceConfigured) {
      toast.error(blockedMessage);
      return;
    }
    await save({ isAISmartToolsEnabled: checked });
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
      <div className="flex items-center gap-x-4">
        <Switch
          id="ai-response-reading-toggle"
          aria-labelledby="ai-response-reading-title"
          aria-describedby="ai-response-reading-description"
          checked={organization.isAIResponseReadingEnabled}
          disabled={!canEdit || update.isPending}
          onCheckedChange={(checked) => void save({ isAIResponseReadingEnabled: checked })}
        />
        <div>
          <p id="ai-response-reading-title" className="text-sm font-semibold text-slate-800">
            {t("workspace.settings.general.ai_response_reading_enabled")}
          </p>
          <p id="ai-response-reading-description" className="text-xs text-slate-600">
            {t("workspace.settings.general.ai_response_reading_enabled_description")}
          </p>
        </div>
      </div>
    </div>
  );
};

/** forma2's DeleteOrganization: owners only, confirmed by typing the organization's exact name. A manager, who can change the rest of the page, is told only owners may delete; for a member or billing user the page's view-only notice already says it, so the button just stays disabled. */
const DeleteOrganization = ({
  organization,
  role,
}: {
  organization: OrganizationDetail;
  role: OrganizationRole;
}) => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [confirmation, setConfirmation] = useState("");
  const remove = useDeleteOrganization(organization.id);

  const { isOwner, isManager } = accessFlags(role);

  if (isManager) {
    return (
      <Alert variant="warning" role="status" className="mb-4">
        <AlertDescription>
          {t("workspace.settings.general.only_org_owner_can_perform_action")}
        </AlertDescription>
      </Alert>
    );
  }
  if (!isOwner) {
    return (
      <div className="pb-4">
        <Button size="sm" variant="destructive" disabled>
          {t("common.delete")}
        </Button>
      </div>
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
    // The organization is gone, so edits still open in its other cards have nothing left to be saved to.
    await navigate({ to: "/", ignoreBlocker: true });
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

/** forma2's CreateOrganizationCard, for whoever may create another organization: anyone where public sign-up is open, only the owner of an organization on an invite-only instance. */
const CreateOrganizationCard = () => {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const { data: canCreate } = useQuery(organizationCreationQuery);
  if (canCreate !== true) return null;
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
