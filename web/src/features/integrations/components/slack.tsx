import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { CircleHelpIcon } from "lucide-react";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
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
  type IntegrationItem,
  type LinkableSurvey,
  type SlackLink,
  integrationOf,
  integrationsQuery,
  linkableSurveysQuery,
  slackChannelsQuery,
  useSaveLinks,
} from "../api";
import slackLogo from "../images/slack.png";
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
import { integrationErrorMessage, slackNeedsReconnect } from "./errors";
import {
  AdditionalSettings,
  BackToIntegrations,
  ConnectIntegration,
  ElementChecklist,
  ManageIntegration,
  SurveySelect,
  useConnect,
} from "./shared";

/** forma2's Slack integration page: connect, or manage which channels get a message for each finished response. */
export const SlackPage = ({ workspaceId, oauthError }: { workspaceId: string; oauthError?: string }) => {
  const { t } = useTranslation();
  const { data: overview } = useSuspenseQuery(integrationsQuery(workspaceId));
  const surveys = useQuery(linkableSurveysQuery(workspaceId));
  const integration = integrationOf(overview, "slack");
  const isConnected = !!integration?.connection.connected;
  const channels = useQuery({ ...slackChannelsQuery(workspaceId), enabled: isConnected });
  const { connect, isConnecting } = useConnect(workspaceId, "slack");
  const [modal, setModal] = useState<{ index?: number } | null>(null);
  const links = integration?.config.data ?? [];

  return (
    <PageContentWrapper>
      <BackToIntegrations workspaceId={workspaceId} />
      <PageHeader pageTitle={t("workspace.integrations.slack.slack_integration")} />
      <div className="h-[75vh] w-full">
        {integration && isConnected ? (
          <>
            <ManageIntegration
              workspaceId={workspaceId}
              integrationId={integration.id}
              connectedAs={t("workspace.integrations.slack.connected_with_team", {
                team: integration.connection.teamName ?? "",
              })}
              banner={
                slackNeedsReconnect(channels.error) && (
                  <div className="mb-4 flex w-full items-center justify-between gap-x-4">
                    <p className="text-amber-700">
                      <Trans
                        i18nKey="workspace.integrations.slack.slack_reconnect_button_description"
                        components={{ b: <b /> }}
                      />
                    </p>
                    <Button onClick={() => void connect()} variant="secondary">
                      {t("workspace.integrations.slack.slack_reconnect_button")}
                    </Button>
                  </div>
                )
              }
              addLabel={t("workspace.integrations.slack.link_channel")}
              onAdd={() => {
                void channels.refetch();
                setModal({});
              }}
              links={links}
              columns={[
                { header: t("common.survey"), cell: (link) => link.surveyName },
                { header: t("workspace.integrations.slack.channel_name"), cell: (link) => link.channelName },
                { header: t("common.questions"), cell: (link) => link.elements },
              ]}
              emptyText={t("workspace.integrations.slack.connect_your_first_slack_channel")}
              onEdit={(index) => setModal({ index })}
              deleteWhat={t("workspace.integrations.slack.slack_integration")}
              deleteText={t("workspace.integrations.delete_integration_confirmation")}
            />
            {modal && (
              <SlackLinkModal
                workspaceId={workspaceId}
                channels={channels.data ?? []}
                surveys={surveys.data ?? []}
                links={links}
                index={modal.index}
                onClose={() => setModal(null)}
              />
            )}
          </>
        ) : (
          <ConnectIntegration
            type="slack"
            isEnabled={overview.enabled.slack}
            logo={slackLogo}
            onConnect={() => void connect()}
            isConnecting={isConnecting}
            oauthError={oauthError}
          />
        )}
      </div>
    </PageContentWrapper>
  );
};

/** forma2's AddChannelMappingModal: link a survey to a channel, or edit or delete the link at index. */
export const SlackLinkModal = ({
  workspaceId,
  channels,
  surveys,
  links,
  index,
  onClose,
}: {
  workspaceId: string;
  channels: readonly IntegrationItem[];
  surveys: readonly LinkableSurvey[];
  links: readonly SlackLink[];
  index?: number;
  onClose: () => void;
}) => {
  const { t } = useTranslation();
  const editing = index === undefined ? undefined : links[index];
  const save = useSaveLinks(workspaceId, "slack");
  const [channel, setChannel] = useState<IntegrationItem | null>(
    editing ? { id: editing.channelId, name: editing.channelName } : null
  );
  const [surveyId, setSurveyId] = useState(editing?.surveyId ?? "");
  const [elementIds, setElementIds] = useState<string[]>(editing?.elementIds ?? []);
  const [settings, setSettings] = useState<LinkSettings>(
    editing ? linkSettingsOf(editing) : DEFAULT_LINK_SETTINGS
  );
  const [isLinking, setIsLinking] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const survey = surveys.find((s) => s.id === surveyId);
  // The edited link's channel stays choosable even when the bot has since left it.
  const channelItems =
    channel && !channels.some((c) => c.id === channel.id) ? [channel, ...channels] : channels;
  const alreadyLinked = !editing && !!channel && links.some((link) => link.channelId === channel.id);

  const chooseSurvey = (id: string) => {
    setSurveyId(id);
    setElementIds(surveys.find((s) => s.id === id)?.elements.map((el) => el.id) ?? []);
  };

  const linkChannel = async () => {
    if (!channel) {
      toast.error(t("workspace.integrations.slack.please_select_a_channel"));
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
    const link: SlackLink = {
      ...linkBase(survey, settings),
      ...elementSelection(t, survey, elementIds),
      channelId: channel.id,
      channelName: channel.name,
    };
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
        <div className="flex items-center gap-x-2">
          <img
            className="size-8 object-contain"
            src={slackLogo}
            alt={t("workspace.integrations.slack.slack_logo")}
          />
          <div className="space-y-0.5">
            <DialogTitle>{t("workspace.integrations.slack.link_slack_channel")}</DialogTitle>
            <DialogDescription>
              {t("workspace.integrations.slack.slack_integration_description")}
            </DialogDescription>
          </div>
        </div>
      </DialogHeader>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault();
          void linkChannel();
        }}>
        <DialogBody>
          <div className="w-full space-y-4">
            <div>
              <Label htmlFor="slack-channel">{t("workspace.integrations.slack.select_channel")}</Label>
              <div className="mt-1">
                <Select
                  id="slack-channel"
                  value={channel?.id ?? ""}
                  disabled={channelItems.length === 0}
                  onChange={(e) => setChannel(channelItems.find((c) => c.id === e.target.value) ?? null)}>
                  <option value="" disabled>
                    {t("workspace.integrations.slack.select_channel")}
                  </option>
                  {channelItems.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </Select>
              </div>
              <Button variant="ghost" size="sm" className="my-2" type="button" asChild>
                <a
                  href="https://forma.yldm.ai/docs/developer-docs/integrations/slack"
                  target="_blank"
                  rel="noreferrer"
                  className="text-xs">
                  <CircleHelpIcon className="size-4" />
                  {t("workspace.integrations.slack.dont_see_your_channel")}
                </a>
              </Button>
              {alreadyLinked && (
                <p className="text-xs text-amber-700">
                  <strong>{t("common.note")}:</strong>{" "}
                  {t("workspace.integrations.slack.already_connected_another_survey")}
                </p>
              )}
              {channels.length === 0 && (
                <p className="m-1 text-xs text-slate-500">
                  {t("workspace.integrations.slack.create_at_least_one_channel_error")}
                </p>
              )}
            </div>
            <SurveySelect surveys={surveys} value={surveyId} onChange={chooseSurvey} />
            {survey && (
              <div>
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
            {editing ? t("common.update") : t("workspace.integrations.slack.link_channel")}
          </Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
};
