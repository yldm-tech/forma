import { getLinkSurveyCardMaxWidth } from "@forma/types/styling";
import type { TSurveyStyling, TSurveyType } from "@forma/types/surveys/types";
import type { TWorkspaceStyling } from "@forma/types/workspace";
import { type JSX, useState } from "react";
import { useTranslation } from "react-i18next";
import { CardlessPreviewLogo } from "../../components/ui/cardless-preview-logo";
import { ClientLogo } from "../../components/ui/client-logo";
import { MediaBackground } from "../../components/ui/media-background";
import { ResetProgressButton } from "../../components/ui/reset-progress-button";
import { cn } from "../../lib/cn";
import { getFooterLinkStyle } from "../lib/footer-link-color";
import type { TSurveyWorkspace } from "../lib/state";
import { LegalFooter } from "./legal-footer";
import { SurveyLoadingAnimation } from "./survey-loading-animation";

interface LinkSurveyWrapperProps {
  children: JSX.Element;
  workspace: Pick<TSurveyWorkspace, "logo" | "linkSurveyBranding">;
  workspaceId: string;
  isWelcomeCardEnabled: boolean;
  surveyId: string;
  surveyType: TSurveyType;
  isPreview: boolean;
  isEmbed: boolean;
  styling: TSurveyStyling | TWorkspaceStyling;
  handleResetSurvey: () => void;
  imprintUrl?: string;
  privacyUrl?: string;
  termsUrl?: string;
  isFormaCloud: boolean;
  publicDomain: string;
  isBrandingEnabled: boolean;
  dir?: "ltr" | "rtl" | "auto";
}

/** forma2's LinkSurveyWrapper: the page around the survey (background, logo, preview bar, legal footer), or nothing but the survey when it is embedded. */
export const LinkSurveyWrapper = ({
  children,
  workspace,
  workspaceId,
  isWelcomeCardEnabled,
  surveyType,
  surveyId,
  isPreview,
  isEmbed,
  styling,
  handleResetSurvey,
  imprintUrl,
  privacyUrl,
  termsUrl,
  isFormaCloud,
  publicDomain,
  isBrandingEnabled,
  dir = "auto",
}: Readonly<LinkSurveyWrapperProps>) => {
  const { t } = useTranslation();
  const [isBackgroundLoaded, setIsBackgroundLoaded] = useState(false);
  const handleBackgroundLoaded = (isLoaded: boolean) => {
    if (isLoaded) setIsBackgroundLoaded(true);
  };
  // The footer links sit on the survey background: an AA-compliant colour, and a backdrop on a non-solid background. An explicit footer link colour wins and drops the backdrop.
  const footerLinkStyle = getFooterLinkStyle(styling);
  const explicitFooterLinkColor = styling.footerLinkColor?.light;
  const footerLinkColor = explicitFooterLinkColor ?? footerLinkStyle.textColor;
  const footerLinkBackdropColor = explicitFooterLinkColor ? undefined : footerLinkStyle.backdropColor;
  const isCardless = styling.cardArrangement?.linkSurveys === "cardless";
  const linkSurveyCardMaxWidth = getLinkSurveyCardMaxWidth(styling.linkSurveyCardWidth);
  const hasLogo = !styling.isLogoHidden && !!(workspace.logo?.url || styling.logo?.url);
  const showCardlessLogo = isCardless && hasLogo;
  // Cardless surveys span the full width; card-based surveys are capped to the configured width.
  const cardMaxWidthStyle = isCardless ? undefined : { maxWidth: linkSurveyCardMaxWidth };
  const surveyUrl = `${publicDomain}/s/${surveyId}`;

  if (isEmbed) {
    return (
      <div className={cn("h-full w-full overflow-auto", !isCardless && "overflow-clip")}>
        <SurveyLoadingAnimation
          isWelcomeCardEnabled={isWelcomeCardEnabled}
          isBrandingEnabled={isBrandingEnabled}
        />
        <main
          className={cn(
            "flex h-full w-full flex-col",
            isCardless && "overflow-hidden",
            !isCardless && "mx-auto",
            styling.cardArrangement?.linkSurveys === "straight" && "pt-6",
            styling.cardArrangement?.linkSurveys === "casual" && "px-6 py-10"
          )}
          style={cardMaxWidthStyle}>
          {children}
        </main>
      </div>
    );
  }

  return (
    <div>
      <SurveyLoadingAnimation
        isWelcomeCardEnabled={isWelcomeCardEnabled}
        isBackgroundLoaded={isBackgroundLoaded}
        isBrandingEnabled={isBrandingEnabled}
      />
      <MediaBackground
        surveyType={surveyType}
        styling={styling}
        onBackgroundLoaded={handleBackgroundLoaded}
        useNaturalHeight={isCardless}>
        <div
          className={cn(
            "flex w-full justify-center",
            isCardless
              ? "h-full min-h-0 flex-1 flex-col items-stretch overflow-hidden"
              : "max-h-dvh min-h-dvh items-center overflow-clip"
          )}>
          {!showCardlessLogo && hasLogo && (
            <header>
              <ClientLogo
                workspaceLogo={workspace.logo}
                workspaceId={workspaceId}
                surveyLogo={styling.logo}
                dir={dir}
              />
            </header>
          )}
          <div
            className={cn(
              "w-full",
              isCardless ? "flex min-h-0 w-full flex-1 flex-col" : "mx-auto h-full space-y-6 px-1.5"
            )}
            style={cardMaxWidthStyle}>
            {isPreview && (
              <div className="fixed top-0 left-0 flex w-full items-center justify-between bg-slate-600 p-2 px-4 text-center text-sm text-white shadow-xs">
                <div />
                {t("s.survey_preview")}
                <ResetProgressButton onClick={handleResetSurvey} />
              </div>
            )}
            <main
              className={cn(
                "flex min-h-0 w-full flex-1 flex-col",
                isPreview && isCardless && "pt-8",
                !isCardless && "justify-center"
              )}>
              {children}
            </main>
            {isCardless && (
              <LegalFooter
                imprintUrl={imprintUrl}
                privacyUrl={privacyUrl}
                termsUrl={termsUrl}
                isFormaCloud={isFormaCloud}
                surveyUrl={surveyUrl}
                linkColor={footerLinkColor}
                backdropColor={footerLinkBackdropColor}
                isInFlow
              />
            )}
          </div>
        </div>
      </MediaBackground>
      {showCardlessLogo && (
        <CardlessPreviewLogo
          workspaceLogo={workspace.logo}
          workspaceId={workspaceId}
          surveyLogo={styling.logo}
          previewSurvey={false}
        />
      )}
      {!isCardless && (
        <LegalFooter
          imprintUrl={imprintUrl}
          privacyUrl={privacyUrl}
          termsUrl={termsUrl}
          isFormaCloud={isFormaCloud}
          surveyUrl={surveyUrl}
          linkColor={footerLinkColor}
          backdropColor={footerLinkBackdropColor}
        />
      )}
    </div>
  );
};
