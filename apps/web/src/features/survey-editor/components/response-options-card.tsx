import * as Collapsible from "@radix-ui/react-collapsible";
import { SlidersHorizontalIcon } from "lucide-react";
import { KeyboardEventHandler, useEffect, useMemo, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { cn } from "../../../lib/cn";
import {
  type TSurveySchedulingConfig,
  browserTimeZone,
  describeScheduledMoment,
} from "../lib/scheduling-config";
import { createSurveySchedulingDateUtils } from "../lib/scheduling-date-utils";
import { SURVEY_PIN_PATTERN, generateSurveyPin } from "../lib/survey-pin";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Alert, AlertTitle } from "../../../components/ui/alert";
import { DatePicker } from "../../../components/ui/date-picker/index";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { Select } from "../../../components/ui/select";
import { Slider } from "../../../components/ui/slider";

interface ResponseOptionsCardProps {
  localSurvey: TSurvey;
  setLocalSurvey: (survey: TSurvey | ((prev: TSurvey) => TSurvey)) => void;
  finishedResponseCount: number;
  isSpamProtectionAllowed: boolean;
  /** The service the instance's spam protection uses. Turnstile has no score, so the threshold is reCAPTCHA's alone. */
  spamProtectionProvider?: "turnstile" | "recaptcha";
  surveySchedulingConfig: TSurveySchedulingConfig;
  locale: TUserLocale;
}

export const ResponseOptionsCard = ({
  localSurvey,
  setLocalSurvey,
  finishedResponseCount,
  isSpamProtectionAllowed,
  spamProtectionProvider,
  surveySchedulingConfig,
  locale,
}: Readonly<ResponseOptionsCardProps>) => {
  const { t } = useTranslation();
  const {
    toCalendarDate,
    getMinimumSurveySchedulingCalendarDate,
    toSurveySchedulingDateTime,
    toSurveySchedulingDateTimeAt,
    getSurveySchedulingTimeOfDay,
    getSurveySchedulingTimeOptions,
    getFirstSurveySchedulingCalendarDate,
  } = useMemo(() => createSurveySchedulingDateUtils(surveySchedulingConfig), [surveySchedulingConfig]);
  const surveySchedulingTimeZoneLabel = surveySchedulingConfig.timeZone;
  const timeOfDayFormat = useMemo(
    () =>
      new Intl.DateTimeFormat(locale, {
        hour: "numeric",
        minute: "2-digit",
        timeZone: surveySchedulingConfig.timeZone,
      }),
    [locale, surveySchedulingConfig.timeZone]
  );
  const [open, setOpen] = useState(localSurvey.type === "link");
  const autoComplete = localSurvey.autoComplete !== null;
  const [surveyClosedMessageToggle, setSurveyClosedMessageToggle] = useState(false);
  const [verifyEmailToggle, setVerifyEmailToggle] = useState(localSurvey.isVerifyEmailEnabled);
  const [recaptchaToggle, setRecaptchaToggle] = useState(localSurvey.recaptcha?.enabled ?? false);
  const [captureIpToggle, setCaptureIpToggle] = useState(localSurvey.isCaptureIpEnabled);
  const [anonymizeResponsesToggle, setAnonymizeResponsesToggle] = useState(
    localSurvey.isAnonymizeResponsesEnabled
  );

  const [surveyClosedMessage, setSurveyClosedMessage] = useState({
    heading: t("workspace.surveys.edit.survey_completed_heading"),
    subheading: t("workspace.surveys.edit.survey_completed_subheading"),
  });

  const [recaptchaThreshold, setRecaptchaThreshold] = useState<number>(localSurvey.recaptcha?.threshold ?? 0);
  const publishOn = localSurvey.publishOn ? toCalendarDate(localSurvey.publishOn) : null;
  const closeOn = localSurvey.closeOn ? toCalendarDate(localSurvey.closeOn) : null;
  const now = new Date();
  const minimumSchedulingDate = getMinimumSurveySchedulingCalendarDate(now);
  // The calendars offer every day that still has a run after now (for the close date, after the publish date); the time selects offer that day's runs.
  const closeLowerBound =
    localSurvey.publishOn && localSurvey.publishOn.getTime() > now.getTime() ? localSurvey.publishOn : now;
  const minPublishDate = getFirstSurveySchedulingCalendarDate(now);
  const minCloseDate = getFirstSurveySchedulingCalendarDate(closeLowerBound);
  /** Where a newly enabled close date starts: the day after the publish date, or the first day the schedule runs, at the schedule's time, as before times could be picked. */
  const defaultCloseDate = (() => {
    if (!publishOn) {
      return minimumSchedulingDate;
    }

    const nextCalendarDay = new Date(publishOn);
    nextCalendarDay.setDate(nextCalendarDay.getDate() + 1);

    return nextCalendarDay.getTime() > minimumSchedulingDate.getTime()
      ? nextCalendarDay
      : minimumSchedulingDate;
  })();

  /** The picked day at the time the field already has (the schedule's time when it has none), or the day's first run after `after` when that time has passed. */
  const atKeptTime = (calendarDate: Date, current: Date | null, after: Date): Date => {
    const { hour, minute } = current
      ? getSurveySchedulingTimeOfDay(current)
      : { hour: surveySchedulingConfig.localHour, minute: surveySchedulingConfig.localMinute };
    const instant = toSurveySchedulingDateTimeAt(calendarDate, hour, minute);
    if (instant.getTime() > after.getTime()) return instant;
    return getSurveySchedulingTimeOptions(calendarDate, after)[0]?.instant ?? instant;
  };

  const timeValue = (instant: Date): string => {
    const { hour, minute } = getSurveySchedulingTimeOfDay(instant);
    return `${hour}:${minute}`;
  };

  /** The times a field offers on its day, with the time it has kept in the list even when it is no longer offered. */
  const timeOptions = (instant: Date, after: Date): { value: string; label: string }[] => {
    const options = getSurveySchedulingTimeOptions(toCalendarDate(instant), after).map(
      (option) => option.instant
    );
    if (!options.some((option) => timeValue(option) === timeValue(instant))) options.push(instant);
    return options
      .sort((a, b) => a.getTime() - b.getTime())
      .map((option) => ({ value: timeValue(option), label: timeOfDayFormat.format(option) }));
  };

  /** The field's day at the time a select picked. */
  const atSelectedTime = (instant: Date, value: string): Date => {
    const [hour = 0, minute = 0] = value.split(":").map(Number);
    return toSurveySchedulingDateTimeAt(toCalendarDate(instant), hour, minute);
  };

  /** A publish date, with the close date dropped when it no longer comes after it. */
  const setPublishOn = (nextPublishOn: Date) => {
    setLocalSurvey((currentSurvey) => ({
      ...currentSurvey,
      closeOn:
        currentSurvey.closeOn && currentSurvey.closeOn.getTime() <= nextPublishOn.getTime()
          ? null
          : currentSurvey.closeOn,
      publishOn: nextPublishOn,
    }));
  };

  /** The moment a scheduled date stands for, in the scheduling zone and, when it differs, in the browser's. */
  const scheduledMoment = (instant: Date | null) => {
    if (!instant) return null;
    const { scheduled, browser } = describeScheduledMoment(
      instant,
      surveySchedulingConfig.timeZone,
      browserTimeZone(),
      locale
    );
    return (
      <div className="mt-2 space-y-0.5 text-xs text-slate-500" aria-live="polite">
        <p>
          {t("workspace.surveys.edit.scheduled_moment", {
            moment: scheduled.moment,
            timeZone: scheduled.timeZone,
          })}
        </p>
        {browser && (
          <p>
            {t("workspace.surveys.edit.scheduled_moment_in_your_time_zone", {
              moment: browser.moment,
              timeZone: browser.timeZone,
            })}
          </p>
        )}
      </div>
    );
  };
  const isPublishOnDateEnabled = localSurvey.publishOn !== null;
  const isCloseOnDateEnabled = localSurvey.closeOn !== null;

  const isPinProtectionEnabled = localSurvey.pin !== null;

  const [verifyProtectWithPinError, setVerifyProtectWithPinError] = useState<string | null>(null);
  const [responseLimitError, setResponseLimitError] = useState<string | null>(null);
  const pinInputRef = useRef<HTMLInputElement>(null);

  const handleProtectSurveyWithPinToggle = () => {
    setVerifyProtectWithPinError(null);
    if (isPinProtectionEnabled) {
      setLocalSurvey((prevSurvey) => ({ ...prevSurvey, pin: null }));
      return;
    }
    // A random PIN, selected, so the author sees it and can type their own over it.
    setLocalSurvey((prevSurvey) => ({ ...prevSurvey, pin: generateSurveyPin() }));
    requestAnimationFrame(() => {
      pinInputRef.current?.focus();
      pinInputRef.current?.select();
    });
  };

  const pinError = (pin: string): string | null =>
    SURVEY_PIN_PATTERN.test(pin) ? null : t("workspace.surveys.edit.pin_must_be_a_four_digit_number");

  const handleProtectSurveyPinChange = (pin: string) => {
    // Digits only; clearing the field is allowed, so a new PIN can be typed from scratch.
    if (!/^\d*$/.test(pin)) {
      setVerifyProtectWithPinError(t("workspace.surveys.edit.pin_can_only_contain_numbers"));
      return;
    }
    setLocalSurvey({ ...localSurvey, pin });
    // Once an error shows, it follows the typing, so it goes away as soon as the PIN is valid.
    if (verifyProtectWithPinError) setVerifyProtectWithPinError(pinError(pin));
  };

  const handleProtectSurveyPinBlurEvent = () => {
    setVerifyProtectWithPinError(localSurvey.pin == null ? null : pinError(localSurvey.pin));
  };

  const handleSurveyPinInputKeyDown: KeyboardEventHandler<HTMLInputElement> = (e) => {
    const exceptThisSymbols = ["e", "E", "+", "-", "."];
    if (exceptThisSymbols.includes(e.key)) e.preventDefault();
  };

  const handleCloseSurveyMessageToggle = () => {
    setSurveyClosedMessageToggle((prev) => !prev);

    if (surveyClosedMessageToggle && localSurvey.surveyClosedMessage) {
      setLocalSurvey({ ...localSurvey, surveyClosedMessage: null });
    }
  };

  const handleVerifyEmailToogle = () => {
    setVerifyEmailToggle(!verifyEmailToggle);
    setLocalSurvey({ ...localSurvey, isVerifyEmailEnabled: !localSurvey.isVerifyEmailEnabled });
  };

  const handleClosedSurveyMessageChange = ({
    heading,
    subheading,
  }: {
    heading?: string;
    subheading?: string;
  }) => {
    const message = {
      heading: heading ?? surveyClosedMessage.heading,
      subheading: subheading ?? surveyClosedMessage.subheading,
    };

    setSurveyClosedMessage(message);
    setLocalSurvey({ ...localSurvey, surveyClosedMessage: message });
  };

  // On unless turned off; a survey loaded before the setting existed has it on.
  const isProgressSavingEnabled = localSurvey.isProgressSavingEnabled !== false;

  const handleProgressSavingToggle = () => {
    setLocalSurvey((prevSurvey) => ({
      ...prevSurvey,
      isProgressSavingEnabled: prevSurvey.isProgressSavingEnabled === false,
    }));
  };

  // D-FG03: off unless turned on; per email needs email verification, which stamps the address the server counts.
  const isSingleResponsePerBrowserEnabled = localSurvey.isSingleResponsePerBrowserEnabled === true;
  const isSingleResponsePerEmailEnabled = localSurvey.isSingleResponsePerEmailEnabled === true;

  const handleSingleResponsePerBrowserToggle = () => {
    setLocalSurvey((prevSurvey) => ({
      ...prevSurvey,
      isSingleResponsePerBrowserEnabled: prevSurvey.isSingleResponsePerBrowserEnabled !== true,
    }));
  };

  const handleSingleResponsePerEmailToggle = () => {
    setLocalSurvey((prevSurvey) => ({
      ...prevSurvey,
      isSingleResponsePerEmailEnabled: prevSurvey.isSingleResponsePerEmailEnabled !== true,
    }));
  };

  const handleHideBackButtonToggle = () => {
    setLocalSurvey({ ...localSurvey, isBackButtonHidden: !localSurvey.isBackButtonHidden });
  };

  const handleAutoProgressToggle = () => {
    setLocalSurvey({ ...localSurvey, isAutoProgressingEnabled: !localSurvey.isAutoProgressingEnabled });
  };

  const handleCaptureIpToggle = () => {
    setCaptureIpToggle(!captureIpToggle);
    setLocalSurvey({ ...localSurvey, isCaptureIpEnabled: !localSurvey.isCaptureIpEnabled });
  };

  const handleAnonymizeResponsesToggle = () => {
    setAnonymizeResponsesToggle(!anonymizeResponsesToggle);
    setLocalSurvey({
      ...localSurvey,
      isAnonymizeResponsesEnabled: !localSurvey.isAnonymizeResponsesEnabled,
    });
  };

  useEffect(() => {
    if (localSurvey.surveyClosedMessage) {
      setSurveyClosedMessage({
        heading: localSurvey.surveyClosedMessage.heading ?? surveyClosedMessage.heading,
        subheading: localSurvey.surveyClosedMessage.subheading ?? surveyClosedMessage.subheading,
      });
      setSurveyClosedMessageToggle(true);
    }
  }, [localSurvey, surveyClosedMessage.heading, surveyClosedMessage.subheading]);

  // A survey loaded with a close date not after its publish date (the server refuses to save one) loses the close date.
  const publishOnTime = localSurvey.publishOn?.getTime() ?? null;
  const closeOnTime = localSurvey.closeOn?.getTime() ?? null;
  useEffect(() => {
    if (publishOnTime === null || closeOnTime === null || closeOnTime > publishOnTime) {
      return;
    }

    setLocalSurvey((currentSurvey) => {
      if (
        !currentSurvey.closeOn ||
        !currentSurvey.publishOn ||
        currentSurvey.closeOn.getTime() > currentSurvey.publishOn.getTime()
      ) {
        return currentSurvey;
      }

      return {
        ...currentSurvey,
        closeOn: null,
      };
    });
  }, [closeOnTime, publishOnTime, setLocalSurvey]);

  const togglePublishOnDate = () => {
    if (isPublishOnDateEnabled) {
      setLocalSurvey((currentSurvey) => ({
        ...currentSurvey,
        publishOn: null,
      }));
      return;
    }

    setPublishOn(toSurveySchedulingDateTime(minimumSchedulingDate));
  };

  const toggleCloseOnDate = () => {
    if (isCloseOnDateEnabled) {
      setLocalSurvey((currentSurvey) => ({
        ...currentSurvey,
        closeOn: null,
      }));
      return;
    }

    setLocalSurvey((currentSurvey) => ({
      ...currentSurvey,
      closeOn: toSurveySchedulingDateTime(defaultCloseDate),
    }));
  };

  const toggleAutocomplete = () => {
    if (autoComplete) {
      const updatedSurvey = { ...localSurvey, autoComplete: null };
      setLocalSurvey(updatedSurvey);
    } else {
      const updatedSurvey = { ...localSurvey, autoComplete: Math.max(25, finishedResponseCount + 5) };
      setLocalSurvey(updatedSurvey);
    }
  };

  const responseLimitIssue = (limit: number): string | null => {
    if (limit === 0) return t("workspace.surveys.edit.response_limit_can_t_be_set_to_0");
    if (limit <= finishedResponseCount) {
      return t("workspace.surveys.edit.response_limit_needs_to_exceed_number_of_received_responses", {
        responseCount: finishedResponseCount,
      });
    }
    return null;
  };

  const handleInputResponse = (e: React.ChangeEvent<HTMLInputElement>) => {
    let value = Number.parseInt(e.target.value);

    if (Number.isNaN(value) || value < 1) {
      value = 1;
    }

    const updatedSurvey = { ...localSurvey, autoComplete: value };
    setLocalSurvey(updatedSurvey);
    if (responseLimitError) setResponseLimitError(responseLimitIssue(value));
  };

  // Shown under the field, like the PIN's error, rather than as a toast that disappears before it can be fixed.
  const handleInputResponseBlur = (e: React.FocusEvent<HTMLInputElement>) => {
    setResponseLimitError(responseLimitIssue(Number.parseInt(e.target.value)));
  };

  // Turning it off is always allowed: a survey saved with it on keeps it after the instance loses its spam protection keys, and the server refuses to save it on until it is switched off.
  const handleRecaptchaToggle = () => {
    if (recaptchaToggle) {
      setRecaptchaToggle(false);
      if (localSurvey.recaptcha?.enabled) {
        setRecaptchaThreshold(0.1);
        setLocalSurvey({ ...localSurvey, recaptcha: { enabled: false, threshold: 0.1 } });
      }
    } else if (isSpamProtectionAllowed) {
      setRecaptchaToggle(true);
      setLocalSurvey({ ...localSurvey, recaptcha: { enabled: true, threshold: 0.1 } });
    }
  };

  const isTurnstile = isSpamProtectionAllowed && spamProtectionProvider === "turnstile";
  // Names the service in use: Turnstile, or reCAPTCHA with its score threshold.
  let spamProtectionDescription = t("workspace.surveys.edit.spam_protection_unavailable");
  if (isTurnstile) {
    spamProtectionDescription = t("workspace.surveys.edit.turnstile_spam_protection.description");
  } else if (isSpamProtectionAllowed) {
    spamProtectionDescription = t("workspace.surveys.edit.enable_recaptcha_to_protect_your_survey_from_spam");
  }

  const handleThresholdChange = (value: number) => {
    setRecaptchaThreshold(value);
    setLocalSurvey((prevSurvey: TSurvey): TSurvey => ({
      ...prevSurvey,
      recaptcha: {
        enabled: prevSurvey.recaptcha?.enabled ?? false,
        threshold: value,
      },
    }));
  };

  return (
    <Collapsible.Root
      open={open}
      onOpenChange={setOpen}
      className={cn(
        open ? "" : "hover:bg-slate-50",
        "w-full space-y-2 rounded-lg border border-slate-300 bg-white"
      )}>
      <Collapsible.CollapsibleTrigger className="h-full w-full cursor-pointer text-left focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-slate-700">
        <div className="inline-flex px-4 py-4">
          <div className="flex items-center pr-5 pl-2">
            <SlidersHorizontalIcon
              aria-hidden="true"
              className="size-7 rounded-full border border-slate-200 bg-slate-100 p-1.5 text-slate-600"
            />
          </div>
          <div>
            <p className="font-semibold text-slate-800">{t("workspace.surveys.edit.response_options")}</p>
            <p className="mt-1 text-sm text-slate-500">
              {t("workspace.surveys.edit.response_limits_redirections_and_more")}
            </p>
          </div>
        </div>
      </Collapsible.CollapsibleTrigger>
      <Collapsible.CollapsibleContent className="data-[state=closed]:animate-collapsible-up data-[state=open]:animate-collapsible-down flex flex-col overflow-hidden">
        <hr className="py-1 text-slate-600" />
        <div className="p-3">
          <AdvancedOptionToggle
            htmlId="publishSurveyOnDate"
            isChecked={isPublishOnDateEnabled}
            onToggle={togglePublishOnDate}
            title={t("workspace.surveys.edit.publish_survey_on_date")}
            description={t("workspace.surveys.edit.survey_will_be_published_at_the_selected_time", {
              timeZone: surveySchedulingTimeZoneLabel,
            })}
            childBorder={true}>
            <div className="p-4">
              <div className="flex flex-wrap items-center gap-2">
                <DatePicker
                  clearButtonId="clear-publish-on-date"
                  clearButtonLabel={t("workspace.surveys.edit.clear_publish_on_date")}
                  value={publishOn}
                  locale={locale}
                  minDate={minPublishDate}
                  onClear={() => {
                    setLocalSurvey((currentSurvey) => ({
                      ...currentSurvey,
                      publishOn: null,
                    }));
                  }}
                  onChange={(date) => setPublishOn(atKeptTime(date, localSurvey.publishOn, now))}
                />
                {localSurvey.publishOn && (
                  <Select
                    aria-label={t("workspace.surveys.edit.publish_time")}
                    containerClassName="w-36 shrink-0"
                    value={timeValue(localSurvey.publishOn)}
                    onChange={(e) => {
                      if (localSurvey.publishOn)
                        setPublishOn(atSelectedTime(localSurvey.publishOn, e.target.value));
                    }}>
                    {timeOptions(localSurvey.publishOn, now).map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </Select>
                )}
              </div>
              {scheduledMoment(localSurvey.publishOn)}
            </div>
          </AdvancedOptionToggle>
          <AdvancedOptionToggle
            htmlId="closeSurveyOnDate"
            isChecked={isCloseOnDateEnabled}
            onToggle={toggleCloseOnDate}
            title={t("workspace.surveys.edit.close_survey_on_date")}
            description={t("workspace.surveys.edit.survey_will_be_closed_at_the_selected_time", {
              timeZone: surveySchedulingTimeZoneLabel,
            })}
            childBorder={true}>
            <div className="p-4">
              <div className="flex flex-wrap items-center gap-2">
                <DatePicker
                  clearButtonId="clear-close-on-date"
                  clearButtonLabel={t("workspace.surveys.edit.clear_close_on_date")}
                  value={closeOn}
                  locale={locale}
                  minDate={minCloseDate}
                  onClear={() => {
                    setLocalSurvey((currentSurvey) => ({
                      ...currentSurvey,
                      closeOn: null,
                    }));
                  }}
                  onChange={(date) => {
                    const nextCloseOn = atKeptTime(date, localSurvey.closeOn, closeLowerBound);
                    setLocalSurvey((currentSurvey) => ({ ...currentSurvey, closeOn: nextCloseOn }));
                  }}
                />
                {localSurvey.closeOn && (
                  <Select
                    aria-label={t("workspace.surveys.edit.close_time")}
                    containerClassName="w-36 shrink-0"
                    value={timeValue(localSurvey.closeOn)}
                    onChange={(e) => {
                      if (!localSurvey.closeOn) return;
                      const nextCloseOn = atSelectedTime(localSurvey.closeOn, e.target.value);
                      setLocalSurvey((currentSurvey) => ({ ...currentSurvey, closeOn: nextCloseOn }));
                    }}>
                    {timeOptions(localSurvey.closeOn, closeLowerBound).map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </Select>
                )}
              </div>
              {scheduledMoment(localSurvey.closeOn)}
            </div>
          </AdvancedOptionToggle>
          {/* Close Survey on Limit */}
          <AdvancedOptionToggle
            htmlId="closeOnNumberOfResponse"
            isChecked={autoComplete}
            onToggle={toggleAutocomplete}
            title={t("workspace.surveys.edit.close_survey_on_response_limit")}
            description={t(
              "workspace.surveys.edit.automatically_close_the_survey_after_a_certain_number_of_responses"
            )}
            childBorder={true}>
            <label htmlFor="autoCompleteResponses" className="cursor-pointer bg-slate-50 p-4">
              <p className="text-sm font-semibold text-slate-700">
                <Trans
                  i18nKey="workspace.surveys.edit.automatically_mark_complete_after_n_responses"
                  components={{
                    autoCompleteInput: (
                      <Input
                        autoFocus
                        type="number"
                        min={finishedResponseCount ? (finishedResponseCount + 1).toString() : "1"}
                        id="autoCompleteResponses"
                        value={localSurvey.autoComplete?.toString()}
                        onChange={handleInputResponse}
                        onBlur={handleInputResponseBlur}
                        isInvalid={Boolean(responseLimitError)}
                        aria-describedby={responseLimitError ? "autoCompleteResponsesError" : undefined}
                        className="mr-2 ml-2 inline w-20 bg-white text-center text-sm"
                      />
                    ),
                  }}
                />
              </p>
              {responseLimitError && (
                <p id="autoCompleteResponsesError" className="pt-2 text-sm font-normal text-red-700">
                  {responseLimitError}
                </p>
              )}
            </label>
          </AdvancedOptionToggle>

          {/* recaptcha for spam protection */}
          {(isSpamProtectionAllowed || recaptchaToggle) && (
            <AdvancedOptionToggle
              htmlId="recaptchaToggle"
              isChecked={recaptchaToggle}
              onToggle={handleRecaptchaToggle}
              title={t("workspace.surveys.edit.enable_spam_protection")}
              description={spamProtectionDescription}
              childBorder={true}>
              {isTurnstile ? (
                <div className="w-full px-2 py-4">
                  <Alert variant="warning" size="default" className="w-fill text-sm" role="status">
                    <AlertTitle>{t("workspace.surveys.edit.turnstile_spam_protection.web_note")}</AlertTitle>
                  </Alert>
                </div>
              ) : (
                <div className="w-full px-2 py-4">
                  <p className="text-sm font-semibold text-slate-800">
                    {t("workspace.surveys.edit.spam_protection_threshold_heading")} : {recaptchaThreshold}
                  </p>
                  <p className="mb-2 text-xs text-slate-500">
                    {t("workspace.surveys.edit.spam_protection_threshold_range_description")}
                  </p>
                  <div className="flex w-full items-center gap-1">
                    <div className="text-center">
                      <p className="mx-2">0.1</p>
                      <p className="mx-2 text-xs text-slate-500">
                        {t("workspace.surveys.edit.spam_protection_lenient")}
                      </p>
                    </div>

                    <Slider
                      value={[recaptchaThreshold]}
                      className="grow"
                      max={0.9}
                      min={0.1}
                      step={0.1}
                      onValueChange={(value) => {
                        const [threshold] = value;
                        if (threshold !== undefined) handleThresholdChange(threshold);
                      }}
                    />
                    <div className="text-center">
                      <p className="mx-2">0.9</p>
                      <p className="mx-2 text-xs text-slate-500">
                        {t("workspace.surveys.edit.spam_protection_strict")}
                      </p>
                    </div>
                  </div>
                  <Alert variant="warning" size="default" className="w-fill mt-2 text-sm" role="status">
                    <AlertTitle>{t("workspace.surveys.edit.spam_protection_web_note")}</AlertTitle>
                  </Alert>
                </div>
              )}
            </AdvancedOptionToggle>
          )}

          {localSurvey.type === "link" && (
            <>
              {/* Adjust Survey Closed Message */}
              <AdvancedOptionToggle
                htmlId="adjustSurveyClosedMessage"
                isChecked={surveyClosedMessageToggle}
                onToggle={handleCloseSurveyMessageToggle}
                title={t("workspace.surveys.edit.adjust_survey_closed_message")}
                description={t("workspace.surveys.edit.adjust_survey_closed_message_description")}
                childBorder={true}>
                <div className="flex w-full items-center gap-x-1 p-4 pb-4">
                  <div className="w-full cursor-pointer items-center bg-slate-50">
                    <Label htmlFor="headline">{t("workspace.surveys.edit.heading")}</Label>
                    <Input
                      autoFocus
                      id="heading"
                      className="mt-2 mb-4 bg-white"
                      name="heading"
                      defaultValue={surveyClosedMessage.heading}
                      onChange={(e) => handleClosedSurveyMessageChange({ heading: e.target.value })}
                    />

                    <Label htmlFor="headline">{t("workspace.surveys.edit.subheading")}</Label>
                    <Input
                      className="mt-2 bg-white"
                      id="subheading"
                      name="subheading"
                      defaultValue={surveyClosedMessage.subheading}
                      onChange={(e) => handleClosedSurveyMessageChange({ subheading: e.target.value })}
                    />
                  </div>
                </div>
              </AdvancedOptionToggle>

              {/* Verify Email Section */}
              <AdvancedOptionToggle
                htmlId="verifyEmailBeforeSubmission"
                isChecked={verifyEmailToggle}
                onToggle={handleVerifyEmailToogle}
                title={t("workspace.surveys.edit.verify_email_before_submission")}
                description={t("workspace.surveys.edit.verify_email_before_submission_description")}
              />

              {/* Duplicate-submission limits (D-FG03). Per email is the server's to enforce and needs the verified address; the switch keeps its stored value while verification is off, and applies again when it is turned back on. */}
              <AdvancedOptionToggle
                htmlId="singleResponsePerEmail"
                isChecked={verifyEmailToggle && isSingleResponsePerEmailEnabled}
                onToggle={handleSingleResponsePerEmailToggle}
                disabled={!verifyEmailToggle}
                title={t("workspace.surveys.edit.one_response_per_email")}
                description={
                  verifyEmailToggle
                    ? t("workspace.surveys.edit.one_response_per_email_description")
                    : t("workspace.surveys.edit.one_response_per_email_requires_verification")
                }
              />
              <AdvancedOptionToggle
                htmlId="singleResponsePerBrowser"
                isChecked={isSingleResponsePerBrowserEnabled}
                onToggle={handleSingleResponsePerBrowserToggle}
                title={t("workspace.surveys.edit.one_response_per_browser")}
                description={t("workspace.surveys.edit.one_response_per_browser_description")}
              />

              {/* Protect Survey with Pin */}
              <AdvancedOptionToggle
                htmlId="protectSurveyWithPin"
                isChecked={isPinProtectionEnabled}
                onToggle={handleProtectSurveyWithPinToggle}
                title={t("workspace.surveys.edit.protect_survey_with_pin")}
                description={t("workspace.surveys.edit.protect_survey_with_pin_description")}
                childBorder={true}>
                <div className="p-4">
                  <Label htmlFor="pin" className="sr-only">
                    {t("workspace.surveys.edit.add_pin")}
                  </Label>
                  <Input
                    ref={pinInputRef}
                    id="pin"
                    isInvalid={Boolean(verifyProtectWithPinError)}
                    aria-describedby={verifyProtectWithPinError ? "pinError" : undefined}
                    className="bg-white"
                    name="pin"
                    inputMode="numeric"
                    autoComplete="off"
                    placeholder={t("workspace.surveys.edit.add_a_four_digit_pin")}
                    onBlur={handleProtectSurveyPinBlurEvent}
                    value={localSurvey.pin ?? ""}
                    onKeyDown={handleSurveyPinInputKeyDown}
                    onChange={(e) => handleProtectSurveyPinChange(e.target.value)}
                    maxLength={4}
                  />
                  {verifyProtectWithPinError && (
                    <p id="pinError" className="pt-1 text-sm text-red-700">
                      {verifyProtectWithPinError}
                    </p>
                  )}
                </div>
              </AdvancedOptionToggle>

              {/* Save respondents' progress (D-R03: on by default, off for kiosks and shared devices) */}
              <AdvancedOptionToggle
                htmlId="saveRespondentProgress"
                isChecked={isProgressSavingEnabled}
                onToggle={handleProgressSavingToggle}
                title={t("workspace.surveys.edit.save_respondent_progress")}
                description={t("workspace.surveys.edit.save_respondent_progress_description")}
              />
            </>
          )}
          <AdvancedOptionToggle
            htmlId="autoProgressRatingNps"
            isChecked={Boolean(localSurvey.isAutoProgressingEnabled)}
            onToggle={handleAutoProgressToggle}
            title={t("workspace.surveys.edit.auto_progress_rating_and_nps")}
            description={t("workspace.surveys.edit.auto_progress_rating_and_nps_description")}
          />
          <AdvancedOptionToggle
            htmlId="hideBackButton"
            isChecked={localSurvey.isBackButtonHidden}
            onToggle={handleHideBackButtonToggle}
            title={t("workspace.surveys.edit.hide_back_button")}
            description={t("workspace.surveys.edit.hide_back_button_description")}
          />
          {/*
           * Disabled while anonymizing, because anonymize overrides it: the ingest gate drops
           * `ipAddress` on a `privacy: "drop"` field regardless of `isCaptureIpEnabled`. Leaving the
           * switch live would let an author turn IP capture "on" and see nothing captured. The stored
           * value is left untouched rather than forced off, so turning anonymize back off restores
           * whatever the author had chosen.
           *
           * The description says so while it is disabled. Greying the switch out alone left the author
           * with no reason for it — two adjacent toggles that both decide whether the IP is stored, one
           * of them inert, and nothing on screen saying which wins.
           */}
          <AdvancedOptionToggle
            htmlId="captureIp"
            isChecked={captureIpToggle}
            onToggle={handleCaptureIpToggle}
            disabled={anonymizeResponsesToggle}
            title={t("workspace.surveys.edit.capture_ip_address")}
            description={
              anonymizeResponsesToggle
                ? t("workspace.surveys.edit.capture_ip_address_disabled_by_anonymize")
                : t("workspace.surveys.edit.capture_ip_address_description")
            }
          />
          <AdvancedOptionToggle
            htmlId="anonymizeResponses"
            isChecked={anonymizeResponsesToggle}
            onToggle={handleAnonymizeResponsesToggle}
            title={t("workspace.surveys.edit.anonymize_responses")}
            description={t("workspace.surveys.edit.anonymize_responses_description")}
          />
        </div>
      </Collapsible.CollapsibleContent>
    </Collapsible.Root>
  );
};
