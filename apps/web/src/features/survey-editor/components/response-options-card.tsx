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
  getSurveySchedulingTimeLabel,
} from "../lib/scheduling-config";
import { createSurveySchedulingDateUtils } from "../lib/scheduling-date-utils";
import { SURVEY_PIN_PATTERN, generateSurveyPin } from "../lib/survey-pin";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Alert, AlertTitle } from "../../../components/ui/alert";
import { DatePicker } from "../../../components/ui/date-picker/index";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { Slider } from "../../../components/ui/slider";

interface ResponseOptionsCardProps {
  localSurvey: TSurvey;
  setLocalSurvey: (survey: TSurvey | ((prev: TSurvey) => TSurvey)) => void;
  finishedResponseCount: number;
  isSpamProtectionAllowed: boolean;
  surveySchedulingConfig: TSurveySchedulingConfig;
  locale: TUserLocale;
}

export const ResponseOptionsCard = ({
  localSurvey,
  setLocalSurvey,
  finishedResponseCount,
  isSpamProtectionAllowed,
  surveySchedulingConfig,
  locale,
}: Readonly<ResponseOptionsCardProps>) => {
  const { t } = useTranslation();
  const {
    toCalendarDate,
    getMinimumSurveySchedulingCalendarDate,
    normalizeDateOnlySelectionToSurveySchedulingDateTime,
    toSurveySchedulingDateTime,
  } = useMemo(() => createSurveySchedulingDateUtils(surveySchedulingConfig), [surveySchedulingConfig]);
  const surveySchedulingTimeLabel = getSurveySchedulingTimeLabel(surveySchedulingConfig);
  const surveySchedulingTimeZoneLabel = surveySchedulingConfig.timeZone;
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
  const minimumSchedulingDate = getMinimumSurveySchedulingCalendarDate();
  const minPublishDate = minimumSchedulingDate;
  const minCloseDate = (() => {
    if (!publishOn) {
      return minimumSchedulingDate;
    }

    const nextCalendarDay = new Date(publishOn);
    nextCalendarDay.setDate(nextCalendarDay.getDate() + 1);

    return nextCalendarDay.getTime() > minimumSchedulingDate.getTime()
      ? nextCalendarDay
      : minimumSchedulingDate;
  })();
  /** The moment a picked date stands for, in the scheduling zone and, when it differs, in the browser's. */
  const scheduledMoment = (date: Date | null) => {
    const instant = normalizeDateOnlySelectionToSurveySchedulingDateTime(date);
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

  useEffect(() => {
    if (!publishOn || !closeOn) {
      return;
    }

    if (closeOn.getTime() > publishOn.getTime()) {
      return;
    }

    setLocalSurvey((currentSurvey) => {
      if (!currentSurvey.closeOn || !currentSurvey.publishOn) {
        return currentSurvey;
      }

      const currentCloseOn = toCalendarDate(currentSurvey.closeOn);
      const currentPublishOn = toCalendarDate(currentSurvey.publishOn);

      if (currentCloseOn.getTime() > currentPublishOn.getTime()) {
        return currentSurvey;
      }

      return {
        ...currentSurvey,
        closeOn: null,
      };
    });
  }, [closeOn, publishOn, setLocalSurvey, toCalendarDate]);

  const togglePublishOnDate = () => {
    if (isPublishOnDateEnabled) {
      setLocalSurvey((currentSurvey) => ({
        ...currentSurvey,
        publishOn: null,
      }));
      return;
    }

    const nextPublishOn = toSurveySchedulingDateTime(minPublishDate);
    const nextPublishCalendarDate = toCalendarDate(nextPublishOn);

    setLocalSurvey((currentSurvey) => ({
      ...currentSurvey,
      closeOn:
        currentSurvey.closeOn &&
        toCalendarDate(currentSurvey.closeOn).getTime() <= nextPublishCalendarDate.getTime()
          ? null
          : currentSurvey.closeOn,
      publishOn: nextPublishOn,
    }));
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
      closeOn: toSurveySchedulingDateTime(minCloseDate),
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

  const handleRecaptchaToggle = () => {
    if (!isSpamProtectionAllowed) return;
    if (recaptchaToggle) {
      setRecaptchaToggle(false);
      if (localSurvey.recaptcha?.enabled) {
        setRecaptchaThreshold(0.1);
        setLocalSurvey({ ...localSurvey, recaptcha: { enabled: false, threshold: 0.1 } });
      }
    } else {
      setRecaptchaToggle(true);
      setLocalSurvey({ ...localSurvey, recaptcha: { enabled: true, threshold: 0.1 } });
    }
  };

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
            description={t("workspace.surveys.edit.survey_will_be_published_at_midnight_cet", {
              time: surveySchedulingTimeLabel,
              timeZone: surveySchedulingTimeZoneLabel,
            })}
            childBorder={true}>
            <div className="p-4">
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
                onChange={(date) => {
                  const nextPublishOn = toSurveySchedulingDateTime(date);
                  const nextPublishCalendarDate = toCalendarDate(nextPublishOn);

                  setLocalSurvey((currentSurvey) => ({
                    ...currentSurvey,
                    closeOn:
                      currentSurvey.closeOn &&
                      toCalendarDate(currentSurvey.closeOn).getTime() <= nextPublishCalendarDate.getTime()
                        ? null
                        : currentSurvey.closeOn,
                    publishOn: nextPublishOn,
                  }));
                }}
              />
              {scheduledMoment(localSurvey.publishOn)}
            </div>
          </AdvancedOptionToggle>
          <AdvancedOptionToggle
            htmlId="closeSurveyOnDate"
            isChecked={isCloseOnDateEnabled}
            onToggle={toggleCloseOnDate}
            title={t("workspace.surveys.edit.close_survey_on_date")}
            description={t("workspace.surveys.edit.survey_will_be_closed_at_midnight_cet", {
              time: surveySchedulingTimeLabel,
              timeZone: surveySchedulingTimeZoneLabel,
            })}
            childBorder={true}>
            <div className="p-4">
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
                  setLocalSurvey((currentSurvey) => ({
                    ...currentSurvey,
                    closeOn: toSurveySchedulingDateTime(date),
                  }));
                }}
              />
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
          {isSpamProtectionAllowed && (
            <AdvancedOptionToggle
              htmlId="recaptchaToggle"
              isChecked={recaptchaToggle}
              onToggle={handleRecaptchaToggle}
              title={t("workspace.surveys.edit.enable_spam_protection")}
              description={t("workspace.surveys.edit.enable_recaptcha_to_protect_your_survey_from_spam")}
              childBorder={true}>
              <div className="w-full px-2 py-4">
                <p className="text-sm font-semibold text-slate-800">
                  {t("workspace.surveys.edit.spam_protection_threshold_heading")} : {recaptchaThreshold}
                </p>
                <p className="mb-2 text-xs text-slate-500">
                  {t("workspace.surveys.edit.spam_protection_threshold_description")}
                </p>
                <div className="flex w-full items-center gap-1">
                  <div className="text-center">
                    <p className="mx-2">0.1</p>
                    <p className="mx-2 text-xs text-slate-500">Lenient</p>
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
                    <p className="mx-2 text-xs text-slate-500">Strict</p>
                  </div>
                </div>
                <Alert variant="warning" size="default" className="w-fill mt-2 text-sm" role="status">
                  <AlertTitle>{t("workspace.surveys.edit.spam_protection_note")}</AlertTitle>
                </Alert>
              </div>
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
