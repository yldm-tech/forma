import { getReadableTextColor } from "@forma/types/colors";
import { DEFAULT_CARD_ARRANGEMENT } from "@forma/types/card-layout";
import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { OTPInput } from "../../components/ui/otp-input";
import { cn } from "../../lib/cn";
import { resumeSurveyPin, validateSurveyPin } from "../lib/api";
import { readEmbedFrameMode } from "../lib/embed-frame";
import { type TPinError, pinErrorKind } from "../lib/pin-error";
import { forgetPinToken, keepPinToken, readPinToken } from "../lib/pin-session";
import type { TPublicSurvey, TSurveyProps } from "../lib/state";
import { useAppLocale } from "../lib/use-app-locale";
import { LinkSurveyWrapper } from "./link-survey-wrapper";
import { StackedCardsContainer } from "./stacked-cards-container";
import { SurveyClientWrapper } from "./survey-client-wrapper";

const PIN_PATTERN = /^\d{4}$/;
/** How long a rejected PIN stays on screen, in red, before the boxes clear. */
const ERROR_RESET_MS = 2000;
/** How long a cardless gate waits for an image or video background before it shows anyway, in its default colors. Matches the runtime overlay's wait for a card's media. */
export const BACKGROUND_WAIT_MS = 3000;
/** The digit box surface when a cardless theme sets no input background: the admin Input is transparent, which leaves the digits to the survey background. */
const FALLBACK_INPUT_BG = "#ffffff";

/**
 * forma2's PinScreen: four digit boxes. Once all four are entered the server checks them, and a correct PIN returns the survey with the token its responses must carry; until then the survey never reaches the browser.
 *
 * When the survey saves respondents' progress, the token is kept for the tab's session (pin-session.ts), and a reload opens the gate with it instead of asking for the PIN again.
 *
 * The gate sits in the same page as the survey behind it (LinkSurveyWrapper: background, logo, legal footer) and on the survey's card, so the respondent sees the survey's branding from the first screen.
 */
export const PinScreen = (props: Readonly<TSurveyProps>) => {
  const { surveyId, styling, locale, isPreview, isEmbed, workspace } = props;
  const { t } = useTranslation();
  const isLocaleReady = useAppLocale(locale);
  const [pinEntry, setPinEntry] = useState("");
  const [loading, setLoading] = useState(false);
  // Whether the last PIN was refused (wrong, or the attempt limit reached): the boxes turn red and clear shortly after.
  const [rejected, setRejected] = useState(false);
  // Why it was refused; the message stays until the respondent types again, after the boxes have cleared.
  const [pinError, setPinError] = useState<TPinError>();
  const boxesRef = useRef<HTMLDivElement>(null);
  const [unlocked, setUnlocked] = useState<{ survey: TPublicSurvey; pinAuthToken: string }>();
  // A token kept from an earlier visit in this tab is tried before the gate shows.
  const [resumeToken] = useState(() => (isPreview ? null : readPinToken(surveyId)));
  const [isResuming, setIsResuming] = useState(resumeToken !== null);
  const cardArrangement = styling.cardArrangement?.linkSurveys ?? DEFAULT_CARD_ARRANGEMENT.linkSurveys;
  const isCardless = cardArrangement === "cardless";
  const isAutoHeight = isEmbed && readEmbedFrameMode(globalThis.location.search).isAutoHeight;
  // An embed draws no background of its own, so there is nothing to wait for.
  const [isBackgroundLoaded, setIsBackgroundLoaded] = useState(isEmbed);
  const [isBackgroundWaitOver, setIsBackgroundWaitOver] = useState(false);
  const handleBackgroundLoaded = useCallback(() => setIsBackgroundLoaded(true), []);
  const isGateRendered = isLocaleReady && !isResuming && !unlocked;
  // Without a card the gate sits on the survey background, and the theme's colors are chosen for that background: until it has loaded (an image or video still on the light placeholder) the gate stays hidden, and if it never loads the gate shows in its default colors, which read on that placeholder.
  const isGateShown = !isCardless || isBackgroundLoaded || isBackgroundWaitOver;
  const isThemed = isCardless && isBackgroundLoaded;

  useEffect(() => {
    if (resumeToken === null) return;
    let cancelled = false;
    resumeSurveyPin(surveyId, resumeToken)
      .then((result) => {
        if (!cancelled) setUnlocked(result);
      })
      .catch(() => {
        // Expired, or the survey stopped saving progress: the PIN is asked for again.
        forgetPinToken(surveyId);
      })
      .finally(() => {
        if (!cancelled) setIsResuming(false);
      });
    return () => {
      cancelled = true;
    };
  }, [resumeToken, surveyId]);

  // Once the final answer is in, a reload asks for the PIN again: the next person at this device has to know it too.
  const handleFinished = useCallback(() => forgetPinToken(surveyId), [surveyId]);

  const resetState = useCallback(() => {
    setRejected(false);
    setLoading(false);
    setPinEntry("");
  }, []);

  useEffect(() => {
    if (!rejected) return;
    const timeout = setTimeout(resetState, ERROR_RESET_MS);
    return () => clearTimeout(timeout);
  }, [rejected, resetState]);

  useEffect(() => {
    if (!isGateRendered || isGateShown) return;
    const timeout = setTimeout(() => setIsBackgroundWaitOver(true), BACKGROUND_WAIT_MS);
    return () => clearTimeout(timeout);
  }, [isGateRendered, isGateShown]);

  // The gate exists only to take the PIN, so the first box has focus as soon as it shows, and again once a refused PIN has cleared and the boxes are enabled again.
  useEffect(() => {
    if (!isLocaleReady || !isGateShown || rejected || unlocked) return;
    boxesRef.current?.querySelector<HTMLInputElement>("input:not(:disabled)")?.focus();
  }, [isLocaleReady, isGateShown, rejected, unlocked]);

  // The PIN is checked as soon as all four digits are in.
  const handlePinChange = (value: string) => {
    setPinEntry(value);
    setPinError(undefined);
    if (!PIN_PATTERN.test(value)) return;
    setLoading(true);
    validateSurveyPin(surveyId, value)
      .then((result) => {
        if (!isPreview && result.survey.isProgressSavingEnabled !== false) {
          keepPinToken(surveyId, result.pinAuthToken);
        }
        setUnlocked(result);
      })
      .catch((error: unknown) => {
        setPinError(pinErrorKind(error));
        setRejected(true);
      })
      .finally(() => setLoading(false));
  };

  const pinErrorMessages: Record<TPinError, string> = {
    incorrect: t("s.pin_incorrect"),
    rateLimited: t("s.pin_too_many_attempts"),
    failed: t("common.something_went_wrong_please_try_again"),
  };

  if (!unlocked) {
    const headlineColor = isThemed ? styling.elementHeadlineColor?.light : undefined;
    const inputBg = styling.inputBgColor?.light ?? FALLBACK_INPUT_BG;
    // The theme's input colors, as the runtime's own inputs use them, so the digits never sit on the bare background; a refused PIN keeps its red border.
    const inputBoxStyle: CSSProperties | undefined = isThemed
      ? {
          backgroundColor: inputBg,
          color: styling.inputTextColor?.light ?? getReadableTextColor(inputBg),
          borderColor: rejected ? undefined : styling.inputBorderColor?.light,
        }
      : undefined;
    // The gate is nothing but translated chrome, so it waits for its locale instead of painting in another first; and it does not show while a kept token may still open it.
    if (!isLocaleReady || isResuming) return null;
    return (
      <LinkSurveyWrapper
        workspace={workspace}
        // Only the logo's link to the Look settings reads the workspace id, and that link shows in the editor's preview, never on this page; the gate has no id before the PIN is entered.
        workspaceId=""
        isWelcomeCardEnabled={false}
        // Nothing to wait for: the runtime only loads behind the gate.
        showLoadingOverlay={false}
        onBackgroundLoaded={handleBackgroundLoaded}
        surveyId={surveyId}
        surveyType="link"
        isPreview={isPreview}
        isEmbed={isEmbed}
        isAutoHeight={isAutoHeight}
        styling={styling}
        handleResetSurvey={resetState}
        imprintUrl={props.imprintUrl}
        privacyUrl={props.privacyUrl}
        termsUrl={props.termsUrl}
        isFormaCloud={props.isFormaCloud}
        publicDomain={props.publicDomain}
        isBrandingEnabled={workspace.linkSurveyBranding}>
        <div
          className={cn(
            "flex w-full flex-1 flex-col items-center justify-center",
            isCardless ? "px-4 py-12 sm:px-6" : "py-10"
          )}>
          <StackedCardsContainer
            cardArrangement={cardArrangement}
            linkSurveyCardWidth={styling.linkSurveyCardWidth}>
            {/* The card pads its own content; only the cardless prompt needs room from the screen edge. */}
            <div
              data-pin-gate=""
              className={cn(
                "flex flex-col items-center justify-center text-center transition-opacity duration-500",
                isCardless && "px-4",
                !isGateShown && "opacity-0"
              )}
              style={isGateShown ? undefined : { visibility: "hidden" }}>
              {/* Without a card the prompt sits on the survey background, where the theme's headline color is the one chosen to be read. */}
              <h1 className="my-4 font-semibold" style={headlineColor ? { color: headlineColor } : undefined}>
                {t("s.enter_pin")}
              </h1>
              <div ref={boxesRef} className="w-full">
                <OTPInput
                  disabled={rejected || loading}
                  value={pinEntry}
                  onChange={handlePinChange}
                  valueLength={4}
                  inputBoxClassName={cn({ "border-red-400": rejected })}
                  inputBoxStyle={inputBoxStyle}
                />
              </div>
              {/* Always rendered, so a screen reader announces the message when it appears. On the survey background red-700 can vanish (a dark theme), so the message takes the headline color there; the boxes' red border still marks the refusal. */}
              <p
                role="status"
                className="mt-3 min-h-5 text-sm text-red-700"
                style={headlineColor ? { color: headlineColor } : undefined}>
                {pinError ? pinErrorMessages[pinError] : null}
              </p>
            </div>
          </StackedCardsContainer>
        </div>
      </LinkSurveyWrapper>
    );
  }

  return (
    <SurveyClientWrapper
      {...props}
      survey={unlocked.survey}
      pinAuthToken={unlocked.pinAuthToken}
      onFinished={handleFinished}
    />
  );
};
