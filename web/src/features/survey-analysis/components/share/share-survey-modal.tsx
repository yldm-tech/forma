import type { TSurvey } from "@forma/types/surveys/types";
import { Code2Icon, CodeIcon, Link2Icon, QrCodeIcon, Settings, Share2Icon, SquareStack } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Dialog, DialogTitle } from "../../../../components/ui/dialog";
import type { Me } from "../../../../lib/queries/auth";
import type { WorkspaceDetail } from "../../../../lib/queries/workspaces";
import { AnonymousLinksTab } from "./anonymous-links-tab";
import { AppTab } from "./app-tab";
import { CustomHtmlTab } from "./custom-html-tab";
import { DynamicPopupTab } from "./dynamic-popup-tab";
import { LinkSettingsTab } from "./link-settings-tab";
import { PrettyUrlTab } from "./pretty-url-tab";
import { QRCodeTab } from "./qr-code-tab";
import { SocialMediaTab } from "./social-media-tab";
import { getSurveyUrl } from "./share-survey-link";
import { LinkTabsType, ShareSettingsType, ShareViaType } from "./share-types";
import { type ShareTab, ShareView } from "./share-view";
import { SuccessView } from "./success-view";
import { TabContainer } from "./tab-container";
import { WebsiteEmbedTab } from "./website-embed-tab";

type ModalView = "start" | "share";

/**
 * forma2's ShareSurveyModal: after publishing it opens on the start view; from the header it opens on the share channels. A link survey offers its links, embed code, social networks, QR code and pop-up, plus its link settings, pretty URL and custom HTML; an app survey explains how it reaches respondents. The channels a single-use survey cannot use are disabled.
 */
export const ShareSurveyModal = ({
  survey,
  workspace,
  publicDomain,
  open,
  modalView,
  setOpen,
  user,
  isReadOnly,
}: {
  survey: TSurvey;
  workspace: WorkspaceDetail;
  publicDomain: string;
  open: boolean;
  modalView: ModalView;
  setOpen: (open: boolean) => void;
  user: Me;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [surveyUrl, setSurveyUrl] = useState(() => getSurveyUrl(survey, publicDomain, "default"));
  const [showView, setShowView] = useState<ModalView>(modalView);
  const defaultActiveId = survey.type === "link" ? ShareViaType.ANON_LINKS : ShareViaType.APP;
  const [activeId, setActiveId] = useState<ShareTab["id"]>(defaultActiveId);
  const isSingleUse = Boolean(survey.singleUse?.enabled);

  // Each opening starts on the view it was opened for.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setShowView(modalView);
  }

  const tabs: ShareTab[] = [
    {
      id: ShareViaType.ANON_LINKS,
      type: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.share.anonymous_links.nav_title"),
      icon: Link2Icon,
      title: t("workspace.surveys.share.anonymous_links.nav_title"),
      description: t("workspace.surveys.share.anonymous_links.description"),
      content: (
        <AnonymousLinksTab
          survey={survey}
          surveyUrl={surveyUrl}
          publicDomain={publicDomain}
          setSurveyUrl={setSurveyUrl}
          locale={user.locale}
          isReadOnly={isReadOnly}
        />
      ),
    },
    {
      id: ShareViaType.WEBSITE_EMBED,
      type: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.share.embed_on_website.nav_title"),
      icon: Code2Icon,
      title: t("workspace.surveys.share.embed_on_website.nav_title"),
      description: t("workspace.surveys.share.embed_on_website.description"),
      content: <WebsiteEmbedTab surveyUrl={surveyUrl} />,
      disabled: isSingleUse,
    },
    {
      id: ShareViaType.SOCIAL_MEDIA,
      type: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.share.social_media.title"),
      icon: Share2Icon,
      title: t("workspace.surveys.share.social_media.title"),
      description: t("workspace.surveys.share.social_media.description"),
      content: <SocialMediaTab surveyUrl={surveyUrl} surveyTitle={survey.name} />,
      disabled: isSingleUse,
    },
    {
      id: ShareViaType.QR_CODE,
      type: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.summary.qr_code"),
      icon: QrCodeIcon,
      title: t("workspace.surveys.summary.qr_code"),
      description: t("workspace.surveys.summary.qr_code_description"),
      content: <QRCodeTab surveyUrl={surveyUrl} />,
      disabled: isSingleUse,
    },
    {
      id: ShareViaType.DYNAMIC_POPUP,
      type: LinkTabsType.SHARE_VIA,
      label: t("workspace.surveys.share.dynamic_popup.nav_title"),
      icon: SquareStack,
      title: t("workspace.surveys.share.dynamic_popup.nav_title"),
      description: t("workspace.surveys.share.dynamic_popup.description"),
      content: <DynamicPopupTab surveyId={survey.id} workspaceId={workspace.id} />,
    },
    {
      id: ShareSettingsType.LINK_SETTINGS,
      type: LinkTabsType.SHARE_SETTING,
      label: t("workspace.surveys.share.link_settings.title"),
      icon: Settings,
      title: t("workspace.surveys.share.link_settings.title"),
      description: t("workspace.surveys.share.link_settings.description"),
      content: <LinkSettingsTab survey={survey} isReadOnly={isReadOnly} locale={user.locale} />,
    },
    {
      id: ShareSettingsType.PRETTY_URL,
      type: LinkTabsType.SHARE_SETTING,
      label: t("workspace.surveys.share.pretty_url.title"),
      icon: Link2Icon,
      title: t("workspace.surveys.share.pretty_url.title"),
      description: t("workspace.surveys.share.pretty_url.description"),
      content: <PrettyUrlTab survey={survey} publicDomain={publicDomain} isReadOnly={isReadOnly} />,
    },
    {
      id: ShareSettingsType.CUSTOM_HTML,
      type: LinkTabsType.SHARE_SETTING,
      label: t("workspace.surveys.share.custom_html.nav_title"),
      icon: CodeIcon,
      title: t("workspace.surveys.share.custom_html.nav_title"),
      description: t("workspace.surveys.share.custom_html.description"),
      content: (
        <CustomHtmlTab
          survey={survey}
          workspaceCustomScripts={workspace.customHeadScripts}
          isReadOnly={isReadOnly}
        />
      ),
    },
  ];

  // A tab the survey's link mode disables falls back to the default one.
  const activeTab = tabs.find((tab) => tab.id === activeId);
  const effectiveActiveId = activeTab?.disabled ? defaultActiveId : activeId;

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (!next) {
      setShowView("start");
      setActiveId(defaultActiveId);
    }
  };

  const renderContent = () => {
    if (showView === "start") {
      return (
        <SuccessView
          survey={survey}
          workspaceId={workspace.id}
          surveyUrl={surveyUrl}
          publicDomain={publicDomain}
          setSurveyUrl={setSurveyUrl}
          locale={user.locale}
          onShare={() => setShowView("share")}
          isReadOnly={isReadOnly}
        />
      );
    }
    if (survey.type === "link") {
      return <ShareView tabs={tabs} activeId={effectiveActiveId} setActiveId={setActiveId} />;
    }
    return (
      <div className="h-full w-full rounded-lg bg-slate-50 p-6">
        <TabContainer
          title={t("workspace.surveys.summary.in_app.title")}
          description={t("workspace.surveys.summary.in_app.description")}>
          <AppTab survey={survey} workspace={workspace} />
        </TabContainer>
      </div>
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      width={survey.type === "link" ? "wide" : "default"}
      className="lg:h-[700px]">
      <DialogTitle className="sr-only">{t("workspace.surveys.summary.share_survey")}</DialogTitle>
      {renderContent()}
    </Dialog>
  );
};
