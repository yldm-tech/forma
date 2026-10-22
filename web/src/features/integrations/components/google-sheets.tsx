import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
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
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { PageContentWrapper, PageHeader } from "../../../components/ui/page";
import { ApiError } from "../../../lib/api";
import { toast } from "../../../lib/toast";
import {
  type LinkableSurvey,
  type SheetLink,
  integrationOf,
  integrationsQuery,
  googleSheetsValidationQuery,
  linkableSurveysQuery,
  spreadsheetName,
  useSaveLinks,
} from "../api";
import googleSheetsLogo from "../images/google-sheets.png";
import {
  DEFAULT_LINK_SETTINGS,
  type LinkSettings,
  constructGoogleSheetsUrl,
  elementSelection,
  extractSpreadsheetIdFromUrl,
  isValidGoogleSheetsUrl,
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

/** The codes Google Sheets failures keep from forma2 (lib/googleSheet/constants.ts). */
const INVALID_GRANT = "invalid_grant";
const INSUFFICIENT_PERMISSION = "insufficient_permission";

/** forma2's Google Sheets integration page: connect, or manage the links of the connected account. */
export const GoogleSheetsPage = ({
  workspaceId,
  oauthError,
}: {
  workspaceId: string;
  oauthError?: string;
}) => {
  const { t } = useTranslation();
  const { data: overview } = useSuspenseQuery(integrationsQuery(workspaceId));
  const surveys = useQuery(linkableSurveysQuery(workspaceId));
  const integration = integrationOf(overview, "googleSheets");
  const isConnected = !!integration?.connection.connected;
  const validation = useQuery({ ...googleSheetsValidationQuery(workspaceId), enabled: isConnected });
  const { connect, isConnecting } = useConnect(workspaceId, "googleSheets");
  const [modal, setModal] = useState<{ index?: number } | null>(null);

  // forma2 checked the credential on opening the page; one Google no longer refreshes gets the reconnect banner.
  const showReconnect = validation.error instanceof ApiError && validation.error.code === INVALID_GRANT;
  const links = integration?.config.data ?? [];

  return (
    <PageContentWrapper>
      <BackToIntegrations workspaceId={workspaceId} />
      <PageHeader pageTitle={t("workspace.integrations.google_sheets.google_sheets_integration")} />
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
                showReconnect && (
                  <ReconnectBanner
                    description={t("workspace.integrations.google_sheets.reconnect_button_description")}
                    label={t("workspace.integrations.google_sheets.reconnect_button")}
                    onReconnect={() => void connect()}
                  />
                )
              }
              reconnect={{
                label: t("workspace.integrations.google_sheets.reconnect_button"),
                tooltip: t("workspace.integrations.google_sheets.reconnect_button_tooltip"),
                onClick: () => void connect(),
              }}
              addLabel={t("workspace.integrations.google_sheets.link_new_sheet")}
              onAdd={() => setModal({})}
              links={links}
              columns={[
                { header: t("common.survey"), cell: (link) => link.surveyName },
                {
                  header: t("workspace.integrations.google_sheets.google_sheet_name"),
                  cell: (link) => link.spreadsheetName,
                },
                { header: t("common.questions"), cell: (link) => link.elements },
              ]}
              emptyText={t("workspace.integrations.google_sheets.no_integrations_yet")}
              onEdit={(index) => setModal({ index })}
              deleteWhat={t("workspace.integrations.google_sheets.google_connection")}
              deleteText={t("workspace.integrations.google_sheets.google_connection_deletion_description")}
            />
            {modal && (
              <SheetLinkModal
                workspaceId={workspaceId}
                surveys={surveys.data ?? []}
                links={links}
                index={modal.index}
                onClose={() => setModal(null)}
              />
            )}
          </>
        ) : (
          <ConnectIntegration
            type="googleSheets"
            isEnabled={overview.enabled.googleSheets}
            logo={googleSheetsLogo}
            onConnect={() => void connect()}
            isConnecting={isConnecting}
            oauthError={oauthError}
          />
        )}
      </div>
    </PageContentWrapper>
  );
};

/** forma2's Google Sheets AddIntegrationModal: link a survey to a spreadsheet, or edit or delete the link at index. */
export const SheetLinkModal = ({
  workspaceId,
  surveys,
  links,
  index,
  onClose,
}: {
  workspaceId: string;
  surveys: readonly LinkableSurvey[];
  links: readonly SheetLink[];
  index?: number;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const editing = index === undefined ? undefined : links[index];
  const save = useSaveLinks(workspaceId, "googleSheets");
  const [spreadsheetUrl, setSpreadsheetUrl] = useState(
    editing ? constructGoogleSheetsUrl(editing.spreadsheetId) : ""
  );
  const [surveyId, setSurveyId] = useState(editing?.surveyId ?? "");
  const [elementIds, setElementIds] = useState<string[]>(editing?.elementIds ?? []);
  const [settings, setSettings] = useState<LinkSettings>(
    editing ? linkSettingsOf(editing) : DEFAULT_LINK_SETTINGS
  );
  const [isLinking, setIsLinking] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const survey = surveys.find((s) => s.id === surveyId);

  // Choosing a survey selects all of its questions, as forma2 did.
  const chooseSurvey = (id: string) => {
    setSurveyId(id);
    setElementIds(surveys.find((s) => s.id === id)?.elements.map((el) => el.id) ?? []);
  };

  const linkSheet = async () => {
    if (!isValidGoogleSheetsUrl(spreadsheetUrl)) {
      toast.error(t("workspace.integrations.google_sheets.enter_a_valid_spreadsheet_url_error"));
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
    setIsLinking(true);
    try {
      const spreadsheetId = extractSpreadsheetIdFromUrl(spreadsheetUrl);
      let name: string;
      try {
        name = await spreadsheetName(workspaceId, spreadsheetId);
      } catch (error) {
        if (error instanceof ApiError && error.code === INVALID_GRANT) {
          toast.error(t("workspace.integrations.google_sheets.token_expired_error"));
        } else if (error instanceof ApiError && error.code === INSUFFICIENT_PERMISSION) {
          toast.error(t("workspace.integrations.google_sheets.spreadsheet_permission_error"));
        } else {
          toast.error(integrationErrorMessage(t, error));
        }
        return;
      }
      const link: SheetLink = {
        ...linkBase(survey, settings),
        ...elementSelection(t, survey, elementIds),
        spreadsheetId,
        spreadsheetName: name,
      };
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
        <div className="flex items-center gap-x-2">
          <img
            className="size-8 object-contain"
            src={googleSheetsLogo}
            alt={t("workspace.integrations.google_sheets.google_sheet_logo")}
          />
          <div className="space-y-0.5">
            <DialogTitle>{t("workspace.integrations.google_sheets.link_google_sheet")}</DialogTitle>
            <DialogDescription>
              {t("workspace.integrations.google_sheets.google_sheets_integration_description")}
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void linkSheet();
        }}>
        <DialogBody>
          <div className="w-full space-y-4">
            <div>
              <Label htmlFor="spreadsheet-url">
                {t("workspace.integrations.google_sheets.spreadsheet_url")}
              </Label>
              <Input
                id="spreadsheet-url"
                value={spreadsheetUrl}
                onChange={(e) => setSpreadsheetUrl(e.target.value)}
                placeholder="https://docs.google.com/spreadsheets/d/<your-spreadsheet-id>"
                className="mt-1"
              />
            </div>
            <SurveySelect surveys={surveys} value={surveyId} onChange={chooseSurvey} />
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
          <Button type="submit" loading={isLinking}>
            {editing ? t("common.update") : t("workspace.integrations.google_sheets.link_google_sheet")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
