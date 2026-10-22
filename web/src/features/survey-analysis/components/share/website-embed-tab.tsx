import { CopyIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AdvancedOptionToggle } from "../../../../components/ui/advanced-option-toggle";
import { Button } from "../../../../components/ui/button";
import { copyText } from "../../../../lib/clipboard";
import { CodeBlock } from "./code-block";

/** The iframe snippet that embeds a link survey in a page; embed mode drops the survey page's own chrome. */
export const websiteEmbedCode = (surveyUrl: string, embedMode: boolean) => {
  const separator = surveyUrl.includes("?") ? "&" : "?";
  const iframeSrc = embedMode ? `${surveyUrl}${separator}embed=true` : surveyUrl;
  return {
    iframeSrc,
    code: `<div style="position: relative; height:80dvh; overflow:auto;">
  <iframe
    src="${iframeSrc}"
    frameborder="0" style="position: absolute; left:0; top:0; width:100%; height:100%; border:0;">
  </iframe>
</div>`,
  };
};

/** forma2's WebsiteEmbedTab: the iframe snippet, its embed mode, and a live preview. */
export const WebsiteEmbedTab = ({ surveyUrl }: { surveyUrl: string }) => {
  const [embedModeEnabled, setEmbedModeEnabled] = useState(false);
  const { t } = useTranslation();
  const { iframeSrc, code } = websiteEmbedCode(surveyUrl, embedModeEnabled);
  const previewSrc = `${iframeSrc}${iframeSrc.includes("?") ? "&" : "?"}preview=true`;

  return (
    <>
      <CodeBlock>{code}</CodeBlock>
      <AdvancedOptionToggle
        htmlId="enableEmbedMode"
        isChecked={embedModeEnabled}
        onToggle={setEmbedModeEnabled}
        title={t("workspace.surveys.share.embed_on_website.embed_mode")}
        description={t("workspace.surveys.share.embed_on_website.embed_mode_description")}
        customContainerClass="pl-1 pr-0 py-0"
      />
      <Button
        className="self-start"
        title={t("common.copy_code")}
        aria-label={t("common.copy_code")}
        onClick={() =>
          void copyText(code, t, t("workspace.surveys.share.embed_on_website.embed_code_copied_to_clipboard"))
        }>
        {t("common.copy_code")}
        <CopyIcon />
      </Button>
      <p className="text-base font-medium text-slate-800">{t("common.preview")}</p>
      <div className="relative h-[500px] w-full overflow-hidden rounded-lg border border-slate-300">
        <iframe
          title={t("common.preview")}
          src={previewSrc}
          className="absolute inset-0 h-full w-full border-0"
        />
      </div>
    </>
  );
};
