import type { TSurvey } from "@forma/types/surveys/types";
import { CirclePlayIcon, CopyIcon } from "lucide-react";
import { type ChangeEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { AdvancedOptionToggle } from "../../../../components/ui/advanced-option-toggle";
import { Alert, AlertDescription, AlertTitle } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/input";
import { copyText } from "../../../../lib/clipboard";
import { toast } from "../../../../lib/toast";
import { analysisApi } from "../../api";
import { DisableLinkModal } from "./disable-link-modal";
import { ShareSurveyLink } from "./share-survey-link";
import { useUpdateSurveySettings } from "./use-survey-settings";

/** The links to a survey for a batch of single-use ids, one per line, as forma2's generated CSV listed them. */
export const singleUseLinksCsv = (surveyUrl: string, links: { suId: string; suToken?: string }[]): string =>
  links
    .map(({ suId, suToken }) => {
      const url = new URL(surveyUrl);
      url.searchParams.set("suId", suId);
      if (suToken) url.searchParams.set("suToken", suToken);
      return url.toString();
    })
    .join("\n");

type PendingDisable = { type: "multi-use" | "single-use"; action: () => Promise<void> };

/** forma2's AnonymousLinksTab: the survey's one multi-use link, or single-use links, generated in batches or with a custom id. */
export const AnonymousLinksTab = ({
  survey,
  surveyUrl,
  publicDomain,
  setSurveyUrl,
  locale,
  isReadOnly,
}: {
  survey: TSurvey;
  surveyUrl: string;
  publicDomain: string;
  setSurveyUrl: (url: string) => void;
  locale: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const updateSurvey = useUpdateSurveySettings(survey);
  const [isMultiUseLink, setIsMultiUseLink] = useState(!survey.singleUse?.enabled);
  const [isSingleUseLink, setIsSingleUseLink] = useState(survey.singleUse?.enabled ?? false);
  const [singleUseEncryption, setSingleUseEncryption] = useState(survey.singleUse?.isEncrypted ?? false);
  const [numberOfLinks, setNumberOfLinks] = useState<number | string>(1);
  const [customSingleUseId, setCustomSingleUseId] = useState("");
  const [pendingDisable, setPendingDisable] = useState<PendingDisable | null>(null);

  const resetState = () => {
    setIsMultiUseLink(!survey.singleUse?.enabled);
    setIsSingleUseLink(survey.singleUse?.enabled ?? false);
    setSingleUseEncryption(survey.singleUse?.isEncrypted ?? false);
  };

  const updateSingleUseSettings = async (enabled: boolean, isEncrypted: boolean) => {
    try {
      await updateSurvey.mutateAsync({ singleUse: { enabled, isEncrypted } });
    } catch {
      toast.error(t("common.something_went_wrong_please_try_again"));
      resetState();
    }
  };

  const switchToMultiUse = async () => {
    setIsMultiUseLink(true);
    setIsSingleUseLink(false);
    setSingleUseEncryption(false);
    await updateSingleUseSettings(false, false);
  };

  const switchToSingleUse = async () => {
    setIsMultiUseLink(false);
    setIsSingleUseLink(true);
    setSingleUseEncryption(true);
    await updateSingleUseSettings(true, true);
  };

  const handleMultiUseToggle = async (newValue: boolean) => {
    if (!newValue) {
      setPendingDisable({ type: "multi-use", action: switchToSingleUse });
    } else if (isSingleUseLink) {
      setPendingDisable({ type: "single-use", action: switchToMultiUse });
    } else {
      await switchToMultiUse();
    }
  };

  const handleSingleUseToggle = (newValue: boolean) => {
    setPendingDisable(
      newValue
        ? { type: "multi-use", action: switchToSingleUse }
        : { type: "single-use", action: switchToMultiUse }
    );
  };

  const handleSingleUseEncryptionToggle = async (newValue: boolean) => {
    setSingleUseEncryption(newValue);
    await updateSingleUseSettings(true, newValue);
  };

  const handleNumberOfLinksChange = (e: ChangeEvent<HTMLInputElement>) => {
    const inputValue = e.target.value;
    if (inputValue === "") {
      setNumberOfLinks("");
      return;
    }
    const value = Number(inputValue);
    if (!Number.isNaN(value)) setNumberOfLinks(value);
  };

  const handleGenerateLinks = async (count: number) => {
    try {
      const links = await analysisApi.singleUseLinks(survey.id, { isEncrypted: singleUseEncryption, count });
      if (links.length === 0) {
        toast.error(t("workspace.surveys.share.anonymous_links.generate_links_error"));
        return;
      }
      const blob = new Blob([singleUseLinksCsv(surveyUrl, links)], { type: "text/csv;charset=utf-8;" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `single-use-links-${survey.id}.csv`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    } catch {
      toast.error(t("workspace.surveys.share.anonymous_links.generate_links_error"));
    }
  };

  const handleCopyCustomSingleUseLink = async () => {
    const singleUseId = customSingleUseId.trim();
    if (!singleUseId) {
      toast.error(t("workspace.surveys.share.anonymous_links.custom_single_use_id_required"));
      return;
    }
    try {
      const [link] = await analysisApi.singleUseLinks(survey.id, {
        isEncrypted: false,
        count: 1,
        singleUseId,
      });
      if (!link) {
        toast.error(t("workspace.surveys.share.anonymous_links.generate_links_error"));
        return;
      }
      await copyText(singleUseLinksCsv(surveyUrl, [link]), t);
    } catch {
      toast.error(t("workspace.surveys.share.anonymous_links.generate_links_error"));
    }
  };

  return (
    <>
      <div className="flex h-full flex-col justify-between gap-y-4">
        <div className="flex w-full grow flex-col gap-6">
          <AdvancedOptionToggle
            htmlId="multi-use-link-switch"
            isChecked={isMultiUseLink}
            onToggle={(value) => void handleMultiUseToggle(value)}
            title={t("workspace.surveys.share.anonymous_links.multi_use_link")}
            description={t("workspace.surveys.share.anonymous_links.multi_use_link_description")}
            customContainerClass="pl-1 pr-0 py-0"
            disabled={isReadOnly}
            childBorder>
            <div className="flex w-full flex-col gap-4 overflow-hidden bg-white p-4">
              <ShareSurveyLink
                survey={survey}
                surveyUrl={surveyUrl}
                publicDomain={publicDomain}
                setSurveyUrl={setSurveyUrl}
                locale={locale}
                isReadOnly={isReadOnly}
              />
              <Alert variant="info" role="status">
                <AlertTitle>
                  {t("workspace.surveys.share.anonymous_links.multi_use_powers_other_channels_title")}
                </AlertTitle>
                <AlertDescription>
                  {t("workspace.surveys.share.anonymous_links.multi_use_powers_other_channels_description")}
                </AlertDescription>
              </Alert>
            </div>
          </AdvancedOptionToggle>

          <AdvancedOptionToggle
            htmlId="single-use-link-switch"
            isChecked={isSingleUseLink}
            onToggle={handleSingleUseToggle}
            title={t("workspace.surveys.share.anonymous_links.single_use_link")}
            description={t("workspace.surveys.share.anonymous_links.single_use_link_description")}
            customContainerClass="pl-1 pr-0 py-0"
            disabled={isReadOnly}
            childBorder>
            <div className="flex w-full flex-col gap-4 bg-white p-4">
              <AdvancedOptionToggle
                htmlId="single-use-encryption-switch"
                isChecked={singleUseEncryption}
                onToggle={(value) => void handleSingleUseEncryptionToggle(value)}
                title={t("workspace.surveys.share.anonymous_links.url_encryption_label")}
                description={t("workspace.surveys.share.anonymous_links.url_encryption_description")}
                customContainerClass="pl-1 pr-0 py-0"
                disabled={isReadOnly}
              />

              {singleUseEncryption ? (
                <div className="flex w-full flex-col gap-2">
                  <h3 className="text-sm font-medium text-slate-900">
                    {t("workspace.surveys.share.anonymous_links.number_of_links_label")}
                  </h3>
                  <div className="flex w-full items-center gap-2">
                    <div className="w-32">
                      <Input
                        type="number"
                        max={5000}
                        min={1}
                        className="bg-white focus:border focus:border-slate-900"
                        value={numberOfLinks}
                        onChange={handleNumberOfLinksChange}
                        aria-label={t("workspace.surveys.share.anonymous_links.number_of_links_label")}
                      />
                    </div>
                    <Button
                      onClick={() => void handleGenerateLinks(Number(numberOfLinks) || 1)}
                      disabled={isReadOnly || Number(numberOfLinks) < 1 || Number(numberOfLinks) > 5000}>
                      <CirclePlayIcon className="size-3.5 shrink-0 text-slate-50" />
                      <span className="text-sm text-slate-50">
                        {t("workspace.surveys.share.anonymous_links.generate_and_download_links")}
                      </span>
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex w-full flex-col gap-4">
                  <Alert variant="info">
                    <AlertTitle>
                      {t("workspace.surveys.share.anonymous_links.custom_single_use_id_title")}
                    </AlertTitle>
                    <AlertDescription>
                      {t("workspace.surveys.share.anonymous_links.custom_single_use_id_description")}
                    </AlertDescription>
                  </Alert>
                  <div className="grid w-full grid-cols-6 items-center gap-2">
                    <Input
                      className="col-span-5 bg-white focus:border focus:border-slate-900"
                      value={customSingleUseId}
                      onChange={(event) => setCustomSingleUseId(event.target.value)}
                      placeholder={t(
                        "workspace.surveys.share.anonymous_links.custom_single_use_id_placeholder"
                      )}
                      aria-label={t("workspace.surveys.share.anonymous_links.custom_single_use_id_title")}
                    />
                    <Button
                      variant="secondary"
                      disabled={isReadOnly || !customSingleUseId.trim()}
                      onClick={() => void handleCopyCustomSingleUseLink()}
                      className="col-span-1 gap-1 text-sm">
                      {t("common.copy")}
                      <CopyIcon />
                    </Button>
                  </div>
                </div>
              )}
            </div>
          </AdvancedOptionToggle>
        </div>
      </div>
      {pendingDisable && (
        <DisableLinkModal
          open
          onOpenChange={(open) => {
            if (!open) setPendingDisable(null);
          }}
          type={pendingDisable.type}
          onDisable={() => {
            void pendingDisable.action();
            setPendingDisable(null);
          }}
        />
      )}
    </>
  );
};
