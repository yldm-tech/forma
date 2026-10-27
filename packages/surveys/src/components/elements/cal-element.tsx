import { useCallback, useState } from "preact/hooks";
import { useTranslation } from "react-i18next";
import { ElementHeader } from "@forma/survey-ui";
import { type TResponseData, type TResponseTtc } from "@forma/types/responses";
import type { TSurveyCalElement } from "@forma/types/surveys/elements";
import { CalEmbed } from "@/components/general/cal-embed";
import { getLocalizedValue } from "@/lib/i18n";
import { getUpdatedTtc, useTtc } from "@/lib/ttc";

interface CalElementProps {
  element: TSurveyCalElement;
  value: string;
  onChange: (responseData: TResponseData) => void;
  languageCode: string;
  ttc: TResponseTtc;
  setTtc: (ttc: TResponseTtc) => void;
  currentElementId: string;
  errorMessage?: string;
}

export function CalElement({
  element,
  value,
  onChange,
  languageCode,
  ttc,
  setTtc,
  currentElementId,
  errorMessage,
}: Readonly<CalElementProps>) {
  const [startTime, setStartTime] = useState(performance.now());
  const { t } = useTranslation();
  useTtc(element.id, ttc, setTtc, startTime, setStartTime, element.id === currentElementId);

  const onSuccessfulBooking = useCallback(() => {
    onChange({ [element.id]: "booked" });
    const updatedttc = getUpdatedTtc(ttc, element.id, performance.now() - startTime);
    setTtc(updatedttc);
  }, [onChange, element.id, setTtc, startTime, ttc]);

  return (
    <form
      key={element.id}
      onSubmit={(e) => {
        e.preventDefault();
        // Validation is handled by centralized system, just update TTC
        const updatedttc = getUpdatedTtc(ttc, element.id, performance.now() - startTime);
        setTtc(updatedttc);

        onChange({ [element.id]: value });
      }}
      className="w-full">
      <div className="space-y-4">
        {/* The same header as every other question (survey-ui), so the prompt takes the theme's headline font, size and weight instead of the old bold one. */}
        <ElementHeader
          headline={getLocalizedValue(element.headline, languageCode)}
          description={element.subheader ? getLocalizedValue(element.subheader, languageCode) : undefined}
          required={element.required}
          requiredLabel={t("common.required")}
          imageUrl={element.imageUrl}
          videoUrl={element.videoUrl}
        />
        <CalEmbed key={element.id} element={element} onSuccessfulBooking={onSuccessfulBooking} />
        {errorMessage ? (
          <span className="text-red-500" role="alert" aria-live="assertive" aria-atomic="true">
            {errorMessage}
          </span>
        ) : null}
      </div>
    </form>
  );
}
