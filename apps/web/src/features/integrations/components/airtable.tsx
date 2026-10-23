import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../../components/ui/alert";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { Label } from "../../../components/ui/label";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { Select } from "../../../components/ui/select";
import { toast } from "../../../lib/toast";
import {
  type AirtableLink,
  type IntegrationItem,
  type LinkableSurvey,
  airtableBasesQuery,
  airtableTablesQuery,
  integrationOf,
  integrationsQuery,
  linkableSurveysQuery,
  useSaveLinks,
} from "../api";
import airtableLogo from "../images/airtable.svg";
import {
  DEFAULT_LINK_SETTINGS,
  type LinkSettings,
  elementSelection,
  linkBase,
  linkSettingsOf,
  toggled,
  withLink,
  withoutLink,
} from "../links";
import { integrationErrorMessage } from "./errors";
import {
  AdditionalSettings,
  BackToIntegrations,
  ConnectIntegration,
  ElementChecklist,
  ManageIntegration,
  ReconnectBanner,
  SurveySelect,
  useConnect,
} from "./shared";

/** forma2's Airtable integration page: connect, or manage the links of the connected account. A connection whose bases cannot be listed (an expired or revoked token) offers reconnecting. */
export const AirtablePage = ({ workspaceId, oauthError }: { workspaceId: string; oauthError?: string }) => {
  const { t } = useTranslation();
  const { data: overview } = useSuspenseQuery(integrationsQuery(workspaceId));
  const surveys = useQuery(linkableSurveysQuery(workspaceId));
  const integration = integrationOf(overview, "airtable");
  const isConnected = !!integration?.connection.connected;
  const bases = useQuery({ ...airtableBasesQuery(workspaceId), enabled: isConnected });
  const { connect, isConnecting } = useConnect(workspaceId, "airtable");
  const [modal, setModal] = useState<{ index?: number } | null>(null);
  const links = integration?.config.data ?? [];

  return (
    <PageContentWrapper>
      <BackToIntegrations workspaceId={workspaceId} />
      <PageHeader pageTitle={t("workspace.integrations.airtable.airtable_integration")} />
      <div className="h-[75vh] w-full">
        {integration && isConnected ? (
          <>
            <ManageIntegration
              workspaceId={workspaceId}
              integrationId={integration.id}
              connectedAs={t("workspace.integrations.connected_with_email", {
                email: integration.connection.email ?? "",
              })}
              banner={
                bases.isError && (
                  <ReconnectBanner
                    description={t("workspace.integrations.reconnect_button_description")}
                    label={t("workspace.integrations.reconnect_button")}
                    onReconnect={() => void connect()}
                  />
                )
              }
              reconnect={{
                label: t("workspace.integrations.reconnect_button"),
                tooltip: t("workspace.integrations.reconnect_button_tooltip"),
                onClick: () => void connect(),
              }}
              addLabel={t("workspace.integrations.airtable.link_new_table")}
              onAdd={() => setModal({})}
              links={links}
              columns={[
                { header: t("common.survey"), cell: (link) => link.surveyName },
                { header: t("workspace.integrations.airtable.table_name"), cell: (link) => link.tableName },
                { header: t("common.questions"), cell: (link) => link.elements },
              ]}
              emptyText={t("workspace.integrations.airtable.no_integrations_yet")}
              onEdit={(index) => setModal({ index })}
              deleteWhat={t("workspace.integrations.airtable.airtable_integration")}
              deleteText={t("workspace.integrations.delete_integration_confirmation")}
            />
            {modal && (
              <AirtableLinkModal
                workspaceId={workspaceId}
                bases={bases.data ?? []}
                surveys={surveys.data ?? []}
                links={links}
                index={modal.index}
                onClose={() => setModal(null)}
              />
            )}
          </>
        ) : (
          <ConnectIntegration
            type="airtable"
            isEnabled={overview.enabled.airtable}
            logo={airtableLogo}
            onConnect={() => void connect()}
            isConnecting={isConnecting}
            oauthError={oauthError}
          />
        )}
      </div>
    </PageContentWrapper>
  );
};

/** forma2's Airtable AddIntegrationModal: link a survey to a table of a base, or edit or delete the link at index. */
export const AirtableLinkModal = ({
  workspaceId,
  bases,
  surveys,
  links,
  index,
  onClose,
}: {
  workspaceId: string;
  bases: readonly IntegrationItem[];
  surveys: readonly LinkableSurvey[];
  links: readonly AirtableLink[];
  index?: number;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const editing = index === undefined ? undefined : links[index];
  const save = useSaveLinks(workspaceId, "airtable");
  const [baseId, setBaseId] = useState(editing?.baseId ?? "");
  const [tableId, setTableId] = useState(editing?.tableId ?? "");
  const [surveyId, setSurveyId] = useState(editing?.surveyId ?? "");
  const [elementIds, setElementIds] = useState<string[]>(editing?.elementIds ?? []);
  const [settings, setSettings] = useState<LinkSettings>(
    editing ? linkSettingsOf(editing) : DEFAULT_LINK_SETTINGS
  );
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const tables = useQuery({ ...airtableTablesQuery(workspaceId, baseId), enabled: baseId !== "" });
  const tableItems = tables.data ?? [];
  const survey = surveys.find((s) => s.id === surveyId);

  const submit = async () => {
    if (!baseId) {
      toast.error(t("workspace.integrations.airtable.please_select_a_base"));
      return;
    }
    if (!tableId) {
      toast.error(t("workspace.integrations.airtable.please_select_a_table"));
      return;
    }
    if (!survey) {
      toast.error(t("workspace.integrations.please_select_a_survey_error"));
      return;
    }
    if (elementIds.length === 0) {
      toast.error(t("workspace.integrations.select_at_least_one_question_error"));
      return;
    }
    const link: AirtableLink = {
      ...linkBase(survey, settings),
      ...elementSelection(t, survey, elementIds),
      baseId,
      tableId,
      tableName: tableItems.find((table) => table.id === tableId)?.name ?? "",
    };
    setIsSubmitting(true);
    try {
      await save.mutateAsync(withLink(links, link, index));
      toast.success(
        editing
          ? t("workspace.integrations.integration_updated_successfully")
          : t("workspace.integrations.integration_added_successfully")
      );
      onClose();
    } catch (error) {
      toast.error(integrationErrorMessage(t, error));
    } finally {
      setIsSubmitting(false);
    }
  };

  const deleteLink = async () => {
    if (index === undefined) return;
    setIsDeleting(true);
    try {
      await save.mutateAsync(withoutLink(links, index));
      toast.success(t("workspace.integrations.integration_removed_successfully"));
      onClose();
    } catch (error) {
      toast.error(integrationErrorMessage(t, error));
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogHeader>
        <div className="flex items-center gap-x-2">
          <img
            className="size-8 object-contain"
            src={airtableLogo}
            alt={t("workspace.integrations.airtable.airtable_logo")}
          />
          <div className="space-y-0.5">
            <DialogTitle>{t("workspace.integrations.airtable.link_airtable_table")}</DialogTitle>
            <DialogDescription>
              {t("workspace.integrations.airtable.sync_responses_with_airtable")}
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}>
        <DialogBody>
          <div className="flex w-full flex-col gap-y-4">
            {bases.length ? (
              <div className="flex w-full flex-col">
                <Label htmlFor="airtable-base">{t("workspace.integrations.airtable.airtable_base")}</Label>
                <div className="mt-1">
                  <Select
                    id="airtable-base"
                    value={baseId}
                    disabled={tables.isFetching}
                    onChange={(e) => {
                      setBaseId(e.target.value);
                      setTableId("");
                    }}>
                    <option value="" disabled />
                    {bases.map((item) => (
                      <option key={item.id} value={item.id}>
                        {item.name}
                      </option>
                    ))}
                  </Select>
                </div>
              </div>
            ) : (
              <Alert role="status">
                <AlertTitle>{t("workspace.integrations.airtable.no_bases_found")}</AlertTitle>
                <AlertDescription>
                  {t("workspace.integrations.airtable.please_create_a_base")}
                </AlertDescription>
              </Alert>
            )}
            <div className="flex w-full flex-col">
              <Label htmlFor="airtable-table">{t("workspace.integrations.airtable.table_name")}</Label>
              <div className="mt-1">
                <Select
                  id="airtable-table"
                  value={tableId}
                  disabled={tableItems.length === 0}
                  onChange={(e) => setTableId(e.target.value)}>
                  <option value="" disabled />
                  {tableItems.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </Select>
              </div>
            </div>
            <SurveySelect
              surveys={surveys}
              value={surveyId}
              onChange={(id) => {
                setSurveyId(id);
                setElementIds([]);
              }}
            />
            {survey && (
              <div className="space-y-4">
                <ElementChecklist
                  survey={survey}
                  selected={elementIds}
                  onToggle={(id) => setElementIds((ids) => toggled(ids, id))}
                />
                <AdditionalSettings settings={settings} onChange={setSettings} />
              </div>
            )}
          </div>
        </DialogBody>
        <DialogFooter>
          {editing ? (
            <Button
              type="button"
              variant="destructive"
              loading={isDeleting}
              onClick={() => void deleteLink()}>
              {t("common.delete")}
            </Button>
          ) : (
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
          )}
          <Button type="submit" loading={isSubmitting}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
