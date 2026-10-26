import { AUTO_OPENED_KEY_PREFIX, MODE_ATTRIBUTES, OPTION_ATTRIBUTES } from "@forma/embed/protocol";
import { CopyIcon } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { AdvancedOptionToggle } from "../../../../components/ui/advanced-option-toggle";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/input";
import { Label } from "../../../../components/ui/label";
import { OptionsSwitch } from "../../../../components/ui/options-switch";
import { copyText } from "../../../../lib/clipboard";
import { CodeBlock } from "./code-block";

/** The ways the share modal offers to put a link survey on a website: the embed script's four layouts (/js/embed.js, packages/embed), and the plain iframe for pages that cannot load a script. */
export const EMBED_LAYOUTS = ["standard", "popup", "slider", "side-tab", "iframe"] as const;
export type EmbedLayout = (typeof EMBED_LAYOUTS)[number];

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

/** What the snippet of one of the embed script's layouts says besides the survey's link. */
export interface ScriptEmbedOptions {
  layout: Exclude<EmbedLayout, "iframe">;
  /** Popup and slider: open on their own this many seconds after the page loads instead of from a button. */
  openDelay?: number;
  /** Popup and slider: the text of the button that opens them. */
  buttonText: string;
  /** Side tab: the text on the tab. */
  tabLabel: string;
  /** Slider and side tab. */
  position: "left" | "right";
}

const escapeHtml = (value: string) =>
  value.replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");

/** The HTML a page pastes to show the survey at surveyUrl in one of the embed script's layouts, loading the script from scriptUrl. */
export const scriptEmbedCode = (
  surveyUrl: string,
  scriptUrl: string,
  options: ScriptEmbedOptions
): string => {
  const attribute = (name: string, value: string | number) => ` ${name}="${escapeHtml(String(value))}"`;
  const survey = attribute(MODE_ATTRIBUTES[options.layout], surveyUrl);
  const position = options.position === "left" ? attribute(OPTION_ATTRIBUTES.position, "left") : "";
  const script = `<script src="${escapeHtml(scriptUrl)}" async></script>`;
  let element: string;
  switch (options.layout) {
    case "standard":
      element = `<div${survey} style="width:100%"></div>`;
      break;
    case "side-tab":
      element = `<div${survey}${attribute(OPTION_ATTRIBUTES.label, options.tabLabel)}${position}></div>`;
      break;
    default: {
      const slider = options.layout === "slider" ? position : "";
      element =
        options.openDelay === undefined
          ? `<button type="button"${survey}${slider}>${escapeHtml(options.buttonText)}</button>`
          : `<div${survey}${attribute(OPTION_ATTRIBUTES.openDelay, options.openDelay)}${slider}></div>`;
    }
  }
  return `${element}\n${script}`;
};

/** The survey's link for a preview: a preview neither counts a display nor stores a response. */
const withPreview = (url: string) => `${url}${url.includes("?") ? "&" : "?"}preview=true`;

/**
 * A sample page with the snippet on it, for the preview frame: the layouts that open over a page can only be shown on one. The frame shares this tab's session storage, where the embed script remembers a survey that already opened on its own; the page forgets that first, so a delayed popup opens again every time the preview is shown.
 */
const previewDocument = (snippet: string) =>
  `<!doctype html><html><head><meta charset="utf-8"><script>try{Object.keys(sessionStorage).forEach(function(k){if(k.indexOf("${AUTO_OPENED_KEY_PREFIX}")===0)sessionStorage.removeItem(k)})}catch(e){}</script><style>body{margin:0;padding:24px;font:14px/1.5 system-ui,sans-serif;color:#334155;background:#f8fafc}.line{height:12px;margin:0 0 12px;border-radius:6px;background:#e2e8f0}button[data-forma-popup],button[data-forma-slider]{padding:10px 16px;border:0;border-radius:8px;background:#0f172a;color:#fff;font:inherit;cursor:pointer}</style></head><body><div class="line" style="width:60%"></div><div class="line" style="width:85%"></div><div class="line" style="width:40%;margin-bottom:24px"></div>${snippet}</body></html>`;

/** The website embed tab: one of the embed script's layouts (or a plain iframe), its options, the code to copy, and a live preview. */
export const WebsiteEmbedTab = ({
  surveyUrl,
  publicDomain,
  layout,
  onLayoutChange,
}: {
  surveyUrl: string;
  publicDomain: string;
  layout: EmbedLayout;
  onLayoutChange: (layout: EmbedLayout) => void;
}) => {
  const { t } = useTranslation();
  const id = useId();
  const [embedModeEnabled, setEmbedModeEnabled] = useState(false);
  const [opensAfterDelay, setOpensAfterDelay] = useState(false);
  const [delaySeconds, setDelaySeconds] = useState("5");
  const [buttonText, setButtonText] = useState(() =>
    t("workspace.surveys.share.embed_on_website.button_text_default")
  );
  const [tabLabel, setTabLabel] = useState(() =>
    t("workspace.surveys.share.embed_on_website.tab_label_default")
  );
  const [position, setPosition] = useState<"left" | "right">("right");

  const layoutOptions: { value: EmbedLayout; label: string; description: string }[] = [
    {
      value: "standard",
      label: t("workspace.surveys.share.embed_on_website.layout_standard"),
      description: t("workspace.surveys.share.embed_on_website.layout_standard_description"),
    },
    {
      value: "popup",
      label: t("workspace.surveys.share.embed_on_website.layout_popup"),
      description: t("workspace.surveys.share.embed_on_website.layout_popup_description"),
    },
    {
      value: "slider",
      label: t("workspace.surveys.share.embed_on_website.layout_slider"),
      description: t("workspace.surveys.share.embed_on_website.layout_slider_description"),
    },
    {
      value: "side-tab",
      label: t("workspace.surveys.share.embed_on_website.layout_side_tab"),
      description: t("workspace.surveys.share.embed_on_website.layout_side_tab_description"),
    },
    {
      value: "iframe",
      label: t("workspace.surveys.share.embed_on_website.layout_iframe"),
      description: t("workspace.surveys.share.embed_on_website.layout_iframe_description"),
    },
  ];
  const layoutDescription = layoutOptions.find((option) => option.value === layout)?.description;

  const delay = Number(delaySeconds);
  const isDelayValid = delaySeconds.trim() !== "" && Number.isFinite(delay) && delay >= 0;
  const scriptOptions = (current: Exclude<EmbedLayout, "iframe">): ScriptEmbedOptions => ({
    layout: current,
    openDelay: opensAfterDelay && isDelayValid ? delay : undefined,
    buttonText: buttonText.trim() || t("workspace.surveys.share.embed_on_website.button_text_default"),
    tabLabel: tabLabel.trim() || t("workspace.surveys.share.embed_on_website.tab_label_default"),
    position,
  });
  const scriptUrl = `${publicDomain}/js/embed.js`;

  let code: string;
  let preview: { src: string } | { srcDoc: string };
  if (layout === "iframe") {
    const iframe = websiteEmbedCode(surveyUrl, embedModeEnabled);
    code = iframe.code;
    preview = { src: withPreview(iframe.iframeSrc) };
  } else {
    code = scriptEmbedCode(surveyUrl, scriptUrl, scriptOptions(layout));
    preview = {
      srcDoc: previewDocument(scriptEmbedCode(withPreview(surveyUrl), scriptUrl, scriptOptions(layout))),
    };
  }

  const opensFromButton = (layout === "popup" || layout === "slider") && !opensAfterDelay;

  return (
    <>
      <div className="flex flex-col gap-2">
        <p id={`${id}-layout`} className="text-sm font-medium text-slate-800">
          {t("workspace.surveys.share.embed_on_website.layout_label")}
        </p>
        <OptionsSwitch
          aria-labelledby={`${id}-layout`}
          options={layoutOptions.map(({ value, label }) => ({ value, label }))}
          currentOption={layout}
          handleOptionChange={(value) => onLayoutChange(value as EmbedLayout)}
        />
        <p className="text-sm text-slate-500">{layoutDescription}</p>
      </div>

      {(layout === "popup" || layout === "slider") && (
        <div className="flex flex-col gap-2">
          <AdvancedOptionToggle
            htmlId={`${id}-delay`}
            isChecked={opensAfterDelay}
            onToggle={setOpensAfterDelay}
            title={t("workspace.surveys.share.embed_on_website.open_after_delay")}
            description={t("workspace.surveys.share.embed_on_website.open_after_delay_description")}
            customContainerClass="pl-1 pr-0 py-0"
            childrenContainerClass="p-3">
            <div className="flex items-center gap-2">
              <Input
                id={`${id}-delay-seconds`}
                type="number"
                min={0}
                step={1}
                className="w-24"
                value={delaySeconds}
                isInvalid={!isDelayValid}
                onChange={(event) => setDelaySeconds(event.target.value)}
              />
              <Label htmlFor={`${id}-delay-seconds`}>
                {t("workspace.surveys.share.embed_on_website.delay_seconds")}
              </Label>
            </div>
          </AdvancedOptionToggle>
        </div>
      )}

      {opensFromButton && (
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${id}-button-text`}>
            {t("workspace.surveys.share.embed_on_website.button_text")}
          </Label>
          <Input
            id={`${id}-button-text`}
            value={buttonText}
            maxLength={60}
            onChange={(event) => setButtonText(event.target.value)}
          />
        </div>
      )}

      {layout === "side-tab" && (
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${id}-tab-label`}>{t("workspace.surveys.share.embed_on_website.tab_label")}</Label>
          <Input
            id={`${id}-tab-label`}
            value={tabLabel}
            maxLength={40}
            onChange={(event) => setTabLabel(event.target.value)}
          />
        </div>
      )}

      {(layout === "slider" || layout === "side-tab") && (
        <div className="flex flex-col gap-2">
          <p id={`${id}-position`} className="text-sm font-medium text-slate-800">
            {t("workspace.surveys.share.embed_on_website.position")}
          </p>
          <OptionsSwitch
            aria-labelledby={`${id}-position`}
            options={[
              { value: "left", label: t("workspace.surveys.share.embed_on_website.position_left") },
              { value: "right", label: t("workspace.surveys.share.embed_on_website.position_right") },
            ]}
            currentOption={position}
            handleOptionChange={(value) => setPosition(value === "left" ? "left" : "right")}
          />
        </div>
      )}

      <CodeBlock>{code}</CodeBlock>
      {layout === "iframe" && (
        <AdvancedOptionToggle
          htmlId="enableEmbedMode"
          isChecked={embedModeEnabled}
          onToggle={setEmbedModeEnabled}
          title={t("workspace.surveys.share.embed_on_website.embed_mode")}
          description={t("workspace.surveys.share.embed_on_website.embed_mode_description")}
          customContainerClass="pl-1 pr-0 py-0"
        />
      )}
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
      {layout !== "iframe" && layout !== "standard" && (
        <p className="-mt-2 text-sm text-slate-500">
          {t("workspace.surveys.share.embed_on_website.preview_sample_page")}
        </p>
      )}
      <div className="relative h-[500px] w-full overflow-hidden rounded-lg border border-slate-300">
        <iframe
          // A new document for every change of the code, so a popup's delay starts over and the preview never shows a stale layout.
          key={code}
          title={t("common.preview")}
          {...preview}
          className="absolute inset-0 h-full w-full border-0"
        />
      </div>
    </>
  );
};
