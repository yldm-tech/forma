import { getLinkSurveyCardMaxWidth } from "@forma/types/styling";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { OTPInput } from "../../components/ui/otp-input";
import { cn } from "../../lib/cn";
import { resumeSurveyPin, validateSurveyPin } from "../lib/api";
import { type TPinError, pinErrorKind } from "../lib/pin-error";
import { forgetPinToken, keepPinToken, readPinToken } from "../lib/pin-session";
import type { TPublicSurvey, TSurveyProps } from "../lib/state";
import { useAppLocale } from "../lib/use-app-locale";
import { SurveyClientWrapper } from "./survey-client-wrapper";

const PIN_PATTERN = /^\d{4}$/;
/** How long a rejected PIN stays on screen, in red, before the boxes clear. */
const ERROR_RESET_MS = 2000;

/**
 * forma2's PinScreen: four digit boxes. Once all four are entered the server checks them, and a correct PIN returns the survey with the token its responses must carry; until then the survey never reaches the browser.
 *
 * When the survey saves respondents' progress, the token is kept for the tab's session (pin-session.ts), and a reload opens the gate with it instead of asking for the PIN again.
 */
export const PinScreen = (props: Readonly<TSurveyProps>) => {
  const { surveyId, styling, locale, isPreview } = props;
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
  const isCardless = styling.cardArrangement?.linkSurveys === "cardless";
  const linkSurveyCardMaxWidth = getLinkSurveyCardMaxWidth(styling.linkSurveyCardWidth);

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

  // The gate exists only to take the PIN, so the first box has focus as soon as it shows, and again once a refused PIN has cleared and the boxes are enabled again.
  useEffect(() => {
    if (!isLocaleReady || rejected || unlocked) return;
    boxesRef.current?.querySelector<HTMLInputElement>("input:not(:disabled)")?.focus();
  }, [isLocaleReady, rejected, unlocked]);

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
    // The gate is nothing but translated chrome, so it waits for its locale instead of painting in another first; and it does not show while a kept token may still open it.
    if (!isLocaleReady || isResuming) return null;
    return (
      <main
        className={cn(
          "mx-auto flex h-full w-full items-center justify-center",
          isCardless && "px-4 py-12 sm:px-6"
        )}
        style={{ maxWidth: linkSurveyCardMaxWidth }}>
        <div className="flex flex-col items-center justify-center px-4 text-center">
          <h1 className="my-4 font-semibold">{t("s.enter_pin")}</h1>
          <div ref={boxesRef} className="w-full">
            <OTPInput
              disabled={rejected || loading}
              value={pinEntry}
              onChange={handlePinChange}
              valueLength={4}
              inputBoxClassName={cn({ "border-red-400": rejected })}
            />
          </div>
          {/* Always rendered, so a screen reader announces the message when it appears. */}
          <p role="status" className="mt-3 min-h-5 text-sm text-red-700">
            {pinError ? pinErrorMessages[pinError] : null}
          </p>
        </div>
      </main>
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
