import { getLinkSurveyCardMaxWidth } from "@forma/types/styling";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { OTPInput } from "../../components/ui/otp-input";
import { cn } from "../../lib/cn";
import { validateSurveyPin } from "../lib/api";
import type { TPublicSurvey, TSurveyProps } from "../lib/state";
import { useAppLocale } from "../lib/use-app-locale";
import { SurveyClientWrapper } from "./survey-client-wrapper";

const PIN_PATTERN = /^\d{4}$/;
/** How long a rejected PIN stays on screen, in red, before the boxes clear. */
const ERROR_RESET_MS = 2000;

/**
 * forma2's PinScreen: four digit boxes. Once all four are entered the server checks them, and a correct PIN returns the survey with the token its responses must carry; until then the survey never reaches the browser.
 */
export const PinScreen = (props: Readonly<TSurveyProps>) => {
  const { surveyId, styling, locale } = props;
  const { t } = useTranslation();
  const isLocaleReady = useAppLocale(locale);
  const [pinEntry, setPinEntry] = useState("");
  const [loading, setLoading] = useState(false);
  // Whether the last PIN was refused (wrong, or the attempt limit reached): the boxes turn red and clear shortly after.
  const [rejected, setRejected] = useState(false);
  const [unlocked, setUnlocked] = useState<{ survey: TPublicSurvey; pinAuthToken: string }>();
  const isCardless = styling.cardArrangement?.linkSurveys === "cardless";
  const linkSurveyCardMaxWidth = getLinkSurveyCardMaxWidth(styling.linkSurveyCardWidth);

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

  // The PIN is checked as soon as all four digits are in.
  const handlePinChange = (value: string) => {
    setPinEntry(value);
    if (!PIN_PATTERN.test(value)) return;
    setLoading(true);
    validateSurveyPin(surveyId, value)
      .then(setUnlocked)
      .catch(() => setRejected(true))
      .finally(() => setLoading(false));
  };

  if (!unlocked) {
    // The gate is nothing but translated chrome, so it waits for its locale instead of painting in another first.
    if (!isLocaleReady) return null;
    return (
      <div
        className={cn(
          "mx-auto flex h-full w-full items-center justify-center",
          isCardless && "px-4 py-12 sm:px-6"
        )}
        style={{ maxWidth: linkSurveyCardMaxWidth }}>
        <div className="flex flex-col items-center justify-center">
          <div className="my-4 font-semibold">
            <h4>{t("s.enter_pin")}</h4>
          </div>
          <OTPInput
            disabled={rejected || loading}
            value={pinEntry}
            onChange={handlePinChange}
            valueLength={4}
            inputBoxClassName={cn({ "border-red-400": rejected })}
          />
        </div>
      </div>
    );
  }

  return <SurveyClientWrapper {...props} survey={unlocked.survey} pinAuthToken={unlocked.pinAuthToken} />;
};
