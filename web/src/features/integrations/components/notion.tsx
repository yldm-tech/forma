import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { PlusIcon, TrashIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { attributeKeysQuery } from "../../../lib/queries/contacts";
import { toast } from "../../../lib/toast";
import { getElementTypes } from "../../survey-editor/lib/elements";
import {
  type LinkableSurvey,
  type NotionDatabase,
  type NotionLink,
  type NotionRef,
  integrationOf,
  integrationsQuery,
  linkableSurveysQuery,
  notionDatabasesQuery,
  useSaveLinks,
} from "../api";
import notionLogo from "../images/notion.png";
import {
  type MappingRow,
  NOTION_MAPPING_ERRORS,
  NOTION_TYPE_MAPPING,
  emptyMappingRow,
  mappingErrorOf,
  mappingRowsOf,
  notionElementItems,
  notionLinkProblem,
  notionLink,
  unusedBy,
  withLink,
  withoutLink,
} from "../links";
import { integrationErrorMessage } from "./errors";
import {
  BackToIntegrations,
  ConnectIntegration,
  ManageIntegration,
  SurveySelect,
  useConnect,
} from "./shared";

/** forma2's Notion integration page: connect, or manage which databases get a page for each finished response. */
export const NotionPage = ({ workspaceId, oauthError }: { workspaceId: string; oauthError?: string }) => {
  const { t } = useTranslation();
  const { data: overview } = useSuspenseQuery(integrationsQuery(workspaceId));
  const surveys = useQuery(linkableSurveysQuery(workspaceId));
  const attributeKeys = useQuery(attributeKeysQuery(workspaceId));
  const integration = integrationOf(overview, "notion");
  const isConnected = !!integration?.connection.connected;
  const databases = useQuery({ ...notionDatabasesQuery(workspaceId), enabled: isConnected });
  const { connect, isConnecting } = useConnect(workspaceId, "notion");
  const [modal, setModal] = useState<{ index?: number } | null>(null);
  const links = integration?.config.data ?? [];

  return (
    <PageContentWrapper>
      <BackToIntegrations workspaceId={workspaceId} />
      <PageHeader pageTitle={t("workspace.integrations.notion.notion_integration")} />
      {integration && isConnected ? (
        <>
          <ManageIntegration
            workspaceId={workspaceId}
            integrationId={integration.id}
            connectedAs={t("workspace.integrations.notion.connected_with_workspace", {
              workspace: integration.connection.workspaceName ?? "",
            })}
            reconnect={{
              label: t("workspace.integrations.notion.update_connection"),
              tooltip: t("workspace.integrations.notion.update_connection_tooltip"),
              onClick: () => void connect(),
            }}
            addLabel={t("workspace.integrations.notion.link_new_database")}
            onAdd={() => setModal({})}
            links={links}
            columns={[
              { header: t("common.survey"), cell: (link) => link.surveyName },
              { header: t("workspace.integrations.notion.database_name"), cell: (link) => link.databaseName },
            ]}
            emptyText={t("workspace.integrations.notion.no_databases_found")}
            onEdit={(index) => setModal({ index })}
            deleteWhat={t("workspace.integrations.notion.notion_integration")}
            deleteText={t("workspace.integrations.delete_integration_confirmation")}
          />
          {modal && (
            <NotionLinkModal
              workspaceId={workspaceId}
              databases={databases.data ?? []}
              surveys={surveys.data ?? []}
              attributeKeys={attributeKeys.data ?? []}
              links={links}
              index={modal.index}
              onClose={() => setModal(null)}
            />
          )}
        </>
      ) : (
        <ConnectIntegration
          type="notion"
          isEnabled={overview.enabled.notion}
          logo={notionLogo}
          onConnect={() => void connect()}
          isConnecting={isConnecting}
          oauthError={oauthError}
        />
      )}
    </PageContentWrapper>
  );
};

/** forma2's Notion AddIntegrationModal: link a survey to a database by mapping what the survey collects onto the database's properties, or edit or delete the link at index. */
export const NotionLinkModal = ({
  workspaceId,
  databases,
  surveys,
  attributeKeys,
  links,
  index,
  onClose,
}: {
  workspaceId: string;
  databases: readonly NotionDatabase[];
  surveys: readonly LinkableSurvey[];
  attributeKeys: readonly { key: string; name: string | null }[];
  links: readonly NotionLink[];
  index?: number;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const editing = index === undefined ? undefined : links[index];
  const save = useSaveLinks(workspaceId, "notion");
  const [databaseId, setDatabaseId] = useState(editing?.databaseId ?? "");
  const [surveyId, setSurveyId] = useState(editing?.surveyId ?? "");
  const [rows, setRows] = useState<MappingRow[]>(() =>
    editing && editing.mapping.length > 0 ? mappingRowsOf(editing.mapping) : [emptyMappingRow()]
  );
  const [isLinking, setIsLinking] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const database = databases.find((db) => db.id === databaseId);
  const survey = surveys.find((s) => s.id === surveyId);
  const elementItems = useMemo(
    () => (survey ? notionElementItems(t, survey, attributeKeys) : []),
    [t, survey, attributeKeys]
  );
  const columnItems = database?.properties ?? [];
  const alreadyLinked = !!database && links.some((link) => link.databaseId === database.id);
  const hasErrors = rows.some((row) => mappingErrorOf(row));

  const setRow = (rowIndex: number, side: "element" | "column", ref: NotionRef) =>
    setRows((current) => current.map((row, i) => (i === rowIndex ? { ...row, [side]: ref } : row)));

  const linkDatabase = async () => {
    const problem = notionLinkProblem(t, { hasDatabase: !!database, hasSurvey: !!survey, rows });
    if (problem || !database || !survey) {
      if (problem) toast.error(problem);
      return;
    }
    const link = notionLink(survey, database, rows);
    setIsLinking(true);
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
      setIsLinking(false);
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
        <div className="mb-4 flex items-start gap-x-2">
          <img
            className="size-8 object-contain"
            src={notionLogo}
            alt={t("workspace.integrations.notion.notion_logo")}
          />
          <div className="space-y-0.5">
            <DialogTitle>{t("workspace.integrations.notion.link_notion_database")}</DialogTitle>
            <DialogDescription>
              {t("workspace.integrations.notion.notion_integration_description")}
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <form
        className="contents space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void linkDatabase();
        }}>
        <DialogBody>
          <div className="w-full space-y-4">
            <div>
              <Label htmlFor="notion-database">{t("workspace.integrations.notion.select_a_database")}</Label>
              <div className="mt-1">
                <Select
                  id="notion-database"
                  value={databaseId}
                  disabled={databases.length === 0}
                  onChange={(e) => setDatabaseId(e.target.value)}>
                  <option value="" disabled>
                    {t("workspace.integrations.notion.select_a_database")}
                  </option>
                  {databases.map((db) => (
                    <option key={db.id} value={db.id}>
                      {db.name}
                    </option>
                  ))}
                </Select>
              </div>
              {alreadyLinked && (
                <p className="text-xs text-amber-700">
                  <strong>{t("common.warning")}:</strong>{" "}
                  {t("workspace.integrations.notion.duplicate_connection_warning")}
                </p>
              )}
              {databases.length === 0 && (
                <p className="m-1 text-xs text-slate-500">
                  {t("workspace.integrations.notion.create_at_least_one_database_to_setup_this_integration")}
                </p>
              )}
            </div>
            <SurveySelect surveys={surveys} value={surveyId} onChange={setSurveyId} />
            {database && survey && (
              <div>
                <Label>{t("workspace.integrations.notion.map_forma_fields_to_notion_property")}</Label>
                <div className="mt-1 space-y-2 overflow-y-auto">
                  {rows.map((row, rowIndex) => (
                    <NotionMappingRow
                      key={row.key}
                      row={row}
                      elementItems={unusedBy(elementItems, rows, rowIndex, "element")}
                      columnItems={unusedBy(columnItems, rows, rowIndex, "column")}
                      canDelete={rows.length > 1}
                      onElement={(ref) => setRow(rowIndex, "element", ref)}
                      onColumn={(ref) => setRow(rowIndex, "column", ref)}
                      onAdd={() => setRows((current) => [...current, emptyMappingRow()])}
                      onDelete={() => setRows((current) => current.filter((_, i) => i !== rowIndex))}
                    />
                  ))}
                </div>
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
            <Button type="button" variant="secondary" onClick={onClose}>
              {t("common.cancel")}
            </Button>
          )}
          <Button type="submit" loading={isLinking} disabled={hasErrors}>
            {editing ? t("common.update") : t("workspace.integrations.notion.link_database")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};

/** forma2's MappingRow: one element-to-property pair, with the reason it cannot be written when it cannot. */
const NotionMappingRow = ({
  row,
  elementItems,
  columnItems,
  canDelete,
  onElement,
  onColumn,
  onAdd,
  onDelete,
}: {
  row: MappingRow;
  elementItems: NotionRef[];
  columnItems: NotionRef[];
  canDelete: boolean;
  onElement: (ref: NotionRef) => void;
  onColumn: (ref: NotionRef) => void;
  onAdd: () => void;
  onDelete: () => void;
}) => {
  const { t } = useTranslation();
  const error = mappingErrorOf(row);
  const elementType = getElementTypes(t).find((type) => type.id === row.element.type);

  return (
    <div className="w-full">
      {error && (
        <div role="alert" className="my-4 w-full rounded-lg bg-red-100 p-4 text-sm text-red-800">
          <span className="mb-2 block">{error}</span>
          {error === NOTION_MAPPING_ERRORS.UNSUPPORTED_TYPE && (
            <>
              -{" "}
              {t("workspace.integrations.notion.col_name_of_type_is_not_supported", {
                col_name: row.column.name,
                type: row.column.type,
              })}
            </>
          )}
          {error === NOTION_MAPPING_ERRORS.MAPPING &&
            elementType &&
            t("workspace.integrations.notion.que_name_of_type_cant_be_mapped_to", {
              que_name: row.element.name,
              question_label: elementType.label,
              col_name: row.column.name,
              col_type: row.column.type,
              mapped_type: (NOTION_TYPE_MAPPING[elementType.id] ?? []).join(" ,"),
            })}
        </div>
      )}
      <div className="flex w-full items-center gap-x-2">
        <div className="flex w-full items-center">
          <div className="max-w-full flex-1">
            <Select
              aria-label={t("workspace.integrations.notion.select_a_survey_question")}
              value={row.element.id}
              disabled={elementItems.length === 0}
              onChange={(e) => {
                const ref = elementItems.find((item) => item.id === e.target.value);
                if (ref) onElement(ref);
              }}>
              <option value="" disabled>
                {t("workspace.integrations.notion.select_a_survey_question")}
              </option>
              {elementItems.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </Select>
          </div>
          <div className="h-px w-4 border-t border-t-slate-300" />
          <div className="max-w-full flex-1">
            <Select
              aria-label={t("workspace.integrations.notion.select_a_field_to_map")}
              value={row.column.id}
              disabled={columnItems.length === 0}
              onChange={(e) => {
                const ref = columnItems.find((item) => item.id === e.target.value);
                if (ref) onColumn(ref);
              }}>
              <option value="" disabled>
                {t("workspace.integrations.notion.select_a_field_to_map")}
              </option>
              {columnItems.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </Select>
          </div>
        </div>
        <div className="flex gap-x-2">
          {canDelete && (
            <Button
              variant="secondary"
              size="icon"
              className="size-10"
              onClick={onDelete}
              type="button"
              aria-label={t("common.delete")}>
              <TrashIcon />
            </Button>
          )}
          <Button
            variant="secondary"
            size="icon"
            className="size-10"
            onClick={onAdd}
            type="button"
            aria-label={t("common.add")}>
            <PlusIcon />
          </Button>
        </div>
      </div>
    </div>
  );
};
