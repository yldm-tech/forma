import { Link } from "@tanstack/react-router";
import { ArrowLeftIcon, RefreshCcwIcon, Trash2Icon } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Alert, AlertButton, AlertDescription } from "../../../components/ui/alert";
import { Button } from "../../../components/ui/button";
import { Checkbox } from "../../../components/ui/checkbox";
import { DeleteDialog } from "../../../components/ui/delete-dialog";
import { EmptyState } from "../../../components/ui/empty-state";
import { Label } from "../../../components/ui/label";
import { FormaLogo } from "../../../components/ui/logo";
import { Select } from "../../../components/ui/select";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../../components/ui/tooltip";
import { TransLink } from "../../../components/ui/trans-link";
import { errorMessage } from "../../../lib/errors";
import { timeSince } from "../../../lib/time";
import { toast } from "../../../lib/toast";
import { type IntegrationType, type LinkableSurvey, connectIntegration, useDeleteIntegration } from "../api";
import { type LinkSettings, headlineLabel } from "../links";

/** The link back to the integrations page every integration page starts with, forma2's GoBackButton. */
export const BackToIntegrations = ({ workspaceId }: { workspaceId: string }) => {
  const { t } = useTranslation();
  return (
    <Link
      to="/workspaces/$workspaceId/integrations"
      params={{ workspaceId }}
      className="inline-flex items-center gap-2 text-sm text-slate-600 hover:text-slate-900">
      <ArrowLeftIcon className="size-4" />
      {t("common.back")}
    </Link>
  );
};

/** Starts a provider's OAuth flow: the server answers the provider's authorize URL, and the browser leaves for it. */
export const useConnect = (workspaceId: string, type: IntegrationType) => {
  const { t } = useTranslation();
  const [isConnecting, setIsConnecting] = useState(false);
  const connect = async () => {
    setIsConnecting(true);
    try {
      globalThis.location.replace(await connectIntegration(workspaceId, type));
    } catch (error) {
      toast.error(errorMessage(t, error));
      setIsConnecting(false);
    }
  };
  return { connect, isConnecting };
};

const DETAILS = {
  googleSheets: {
    text: "workspace.integrations.google_sheets.google_sheets_integration_description",
    docsLink: "https://forma.yldm.ai/docs/integrations/google-sheets",
    connect: "workspace.integrations.google_sheets.connect_with_google_sheets",
    notConfigured: "workspace.integrations.google_sheets.google_sheet_integration_is_not_configured",
  },
  airtable: {
    text: "workspace.integrations.airtable.airtable_integration_description",
    docsLink: "https://forma.yldm.ai/docs/integrations/airtable",
    connect: "workspace.integrations.airtable.connect_with_airtable",
    notConfigured: "workspace.integrations.airtable.airtable_integration_is_not_configured",
  },
  notion: {
    text: "workspace.integrations.notion.notion_integration_description",
    docsLink: "https://forma.yldm.ai/docs/integrations/notion",
    connect: "workspace.integrations.notion.connect_with_notion",
    notConfigured: "workspace.integrations.notion.notion_integration_is_not_configured",
  },
  slack: {
    text: "workspace.integrations.slack.slack_integration_description",
    docsLink: "https://forma.yldm.ai/docs/integrations/slack",
    connect: "workspace.integrations.slack.connect_with_slack",
    notConfigured: "workspace.integrations.slack.slack_integration_is_not_configured",
  },
} as const;

/** forma2's ConnectIntegration: what the integration does and the button that starts its OAuth flow, disabled with a pointer to the docs when the instance has no credentials for it. A callback that came back with ?error says so once. */
export const ConnectIntegration = ({
  type,
  isEnabled,
  logo,
  onConnect,
  isConnecting,
  oauthError,
}: {
  type: IntegrationType;
  isEnabled: boolean;
  logo: string;
  onConnect: () => void;
  isConnecting: boolean;
  oauthError?: string;
}) => {
  const { t } = useTranslation();
  const details = DETAILS[type];
  const reported = useRef(false);

  useEffect(() => {
    if (!oauthError || reported.current) return;
    reported.current = true;
    toast.error(t("workspace.integrations.connecting_integration_failed_please_try_again"));
  }, [oauthError, t]);

  return (
    <div className="flex h-[75vh] w-full items-center justify-center">
      <div className="flex w-1/2 flex-col items-center justify-center rounded-lg bg-white p-8 shadow-sm">
        <div className="flex w-1/2 justify-center -space-x-4">
          <div className="flex size-32 items-center justify-center rounded-full bg-white p-6 shadow-md">
            <FormaLogo className="w-1/2" />
          </div>
          <div className="flex size-32 items-center justify-center rounded-full bg-white p-4 shadow-md">
            <img className="w-1/2" src={logo} alt="" />
          </div>
        </div>
        <p className="my-8">{t(details.text)}</p>
        {!isEnabled && (
          <p className="mb-8 rounded-sm border-slate-200 bg-slate-100 p-3 text-sm">
            {t(details.notConfigured)}
            <br />
            <Trans
              i18nKey="workspace.integrations.follow_these_docs_to_configure_it"
              components={{
                docsLink: (
                  <TransLink href={details.docsLink} target="_blank" rel="noreferrer" className="underline" />
                ),
              }}
            />
          </p>
        )}
        <Button loading={isConnecting} onClick={onConnect} disabled={!isEnabled}>
          {t(details.connect)}
        </Button>
      </div>
    </div>
  );
};

/** The survey picker of a link modal; the modals offer the surveys that are not completed or archived. */
export const SurveySelect = ({
  surveys,
  value,
  onChange,
}: {
  surveys: readonly LinkableSurvey[];
  value: string;
  onChange: (surveyId: string) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div className="flex w-full flex-col">
      <Label htmlFor="integration-survey">{t("common.select_survey")}</Label>
      <div className="mt-1">
        <Select
          id="integration-survey"
          value={value}
          disabled={surveys.length === 0}
          onChange={(e) => onChange(e.target.value)}>
          <option value="" disabled>
            {t("common.select_survey")}
          </option>
          {surveys.map((survey) => (
            <option key={survey.id} value={survey.id}>
              {survey.name}
            </option>
          ))}
        </Select>
      </div>
      {surveys.length === 0 && (
        <p className="m-1 text-xs text-slate-500">{t("workspace.integrations.create_survey_warning")}</p>
      )}
    </div>
  );
};

/** The survey's elements to pick the columns from, in survey order. */
export const ElementChecklist = ({
  survey,
  selected,
  onToggle,
}: {
  survey: LinkableSurvey;
  selected: readonly string[];
  onToggle: (elementId: string) => void;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      <Label>{t("common.questions")}</Label>
      <div className="mt-1 max-h-[15vh] overflow-x-hidden overflow-y-auto rounded-lg border border-slate-200">
        <div className="grid content-center rounded-lg bg-slate-50 p-3 text-left text-sm text-slate-900">
          {survey.elements.map((element) => (
            <div key={element.id} className="my-1 flex items-center gap-x-2">
              <label htmlFor={`element-${element.id}`} className="flex cursor-pointer items-center">
                <Checkbox
                  id={`element-${element.id}`}
                  className="bg-white"
                  checked={selected.includes(element.id)}
                  onCheckedChange={() => onToggle(element.id)}
                />
                <span className="ml-2 w-120 truncate">{headlineLabel(survey, element.headline)}</span>
              </label>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

/** forma2's AdditionalIntegrationSettings: the extra columns a link writes. */
export const AdditionalSettings = ({
  settings,
  onChange,
}: {
  settings: LinkSettings;
  onChange: (settings: LinkSettings) => void;
}) => {
  const { t } = useTranslation();
  const checkboxes: { id: keyof LinkSettings; label: string }[] = [
    { id: "includeCreatedAt", label: t("workspace.integrations.include_created_at") },
    { id: "includeVariables", label: t("workspace.integrations.include_variables") },
    { id: "includeHiddenFields", label: t("workspace.integrations.include_hidden_fields") },
    { id: "includeMetadata", label: t("workspace.integrations.include_metadata") },
    { id: "includeContactAttributes", label: t("workspace.integrations.include_contact_attributes") },
  ];
  return (
    <div className="mt-4">
      <Label>{t("workspace.integrations.additional_settings")}</Label>
      <div className="text-sm">
        {checkboxes.map(({ id, label }) => (
          <div key={id} className="my-1 flex items-center gap-x-2">
            <label htmlFor={id} className="flex cursor-pointer items-center">
              <Checkbox
                id={id}
                className="bg-white"
                checked={settings[id]}
                onCheckedChange={() => onChange({ ...settings, [id]: !settings[id] })}
              />
              <span className="ml-2 w-120 truncate">{label}</span>
            </label>
          </div>
        ))}
      </div>
    </div>
  );
};

/** One column of a manage page's link table: its heading, and what a link shows in it. */
export interface LinkColumn<T> {
  header: string;
  cell: (link: T) => ReactNode;
}

/** forma2's ManageIntegration pages, shared by the four providers: who the integration is connected as, reconnecting, the links (a click edits one), adding a link, and deleting the whole integration. */
export const ManageIntegration = <T extends { createdAt: string }>({
  workspaceId,
  integrationId,
  connectedAs,
  banner,
  reconnect,
  addLabel,
  onAdd,
  links,
  columns,
  emptyText,
  onEdit,
  deleteWhat,
  deleteText,
}: {
  workspaceId: string;
  integrationId: string;
  connectedAs: string;
  /** A warning above the page, when the connection needs renewing. */
  banner?: ReactNode;
  /** The reconnect button, with its tooltip; Slack has none. */
  reconnect?: { label: string; tooltip: string; onClick: () => void };
  addLabel: string;
  onAdd: () => void;
  links: readonly T[];
  columns: LinkColumn<T>[];
  emptyText: string;
  onEdit: (index: number) => void;
  deleteWhat: string;
  deleteText: string;
}) => {
  const { t, i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en-US";
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);
  const deleteIntegration = useDeleteIntegration(workspaceId);
  const span = columns.length + 1;
  const gridCols = span === 4 ? "grid-cols-8" : "grid-cols-6";

  const remove = async () => {
    try {
      await deleteIntegration.mutateAsync(integrationId);
      toast.success(t("workspace.integrations.integration_removed_successfully"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    } finally {
      setIsDeleteOpen(false);
    }
  };

  return (
    <div className="mt-6 flex w-full flex-col items-center justify-center p-6">
      {banner}
      <div className="flex w-full justify-end gap-x-2">
        <div className="mr-6 flex items-center">
          <span className="mr-4 size-4 rounded-full bg-green-600" />
          <span className="text-slate-500">{connectedAs}</span>
        </div>
        {reconnect && (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="outline" onClick={reconnect.onClick}>
                  <RefreshCcwIcon className="mr-2 size-4" />
                  {reconnect.label}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{reconnect.tooltip}</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        )}
        <Button onClick={onAdd}>{addLabel}</Button>
      </div>
      {links.length === 0 ? (
        <div className="mt-4 w-full">
          <EmptyState text={emptyText} />
        </div>
      ) : (
        <div className="mt-4 flex w-full flex-col items-center justify-center">
          <div className="mt-6 w-full rounded-lg border border-slate-200">
            <div
              className={`grid h-12 ${gridCols} content-center rounded-lg bg-slate-100 text-left text-sm font-semibold text-slate-900`}>
              {columns.map((column) => (
                <div key={column.header} className="col-span-2 hidden text-center sm:block">
                  {column.header}
                </div>
              ))}
              <div className="col-span-2 hidden text-center sm:block">{t("common.updated_at")}</div>
            </div>
            {links.map((link, index) => (
              <button
                type="button"
                // A link has no id of its own; its place in the list is what a save addresses.
                key={`${index}-${link.createdAt}`}
                className={`grid h-16 w-full cursor-pointer ${gridCols} content-center rounded-lg p-2 hover:bg-slate-100`}
                onClick={() => onEdit(index)}>
                {columns.map((column) => (
                  <div key={column.header} className="col-span-2 text-center">
                    {column.cell(link)}
                  </div>
                ))}
                <div className="col-span-2 text-center">{timeSince(link.createdAt, locale)}</div>
              </button>
            ))}
          </div>
        </div>
      )}
      <Button variant="ghost" onClick={() => setIsDeleteOpen(true)} className="mt-4">
        <Trash2Icon />
        {t("workspace.integrations.delete_integration")}
      </Button>
      <DeleteDialog
        open={isDeleteOpen}
        setOpen={setIsDeleteOpen}
        deleteWhat={deleteWhat}
        onDelete={() => void remove()}
        text={deleteText}
        isDeleting={deleteIntegration.isPending}
      />
    </div>
  );
};

/** forma2's reconnect warning above a manage page. */
export const ReconnectBanner = ({
  description,
  label,
  onReconnect,
}: {
  description: string;
  label: string;
  onReconnect: () => void;
}) => (
  <Alert variant="warning" size="small" className="mb-4 w-full" role="status">
    <AlertDescription>{description}</AlertDescription>
    <AlertButton onClick={onReconnect}>{label}</AlertButton>
  </Alert>
);
