import { useMemo, useState } from "preact/hooks";
import { useTranslation } from "react-i18next";
import { Ranking, type RankingChange, type RankingOption } from "@forma/survey-ui";
import { type TResponseData, type TResponseTtc } from "@forma/types/responses";
import type { TSurveyRankingElement } from "@forma/types/surveys/elements";
import { getLocalizedValue } from "@/lib/i18n";
import { announceToLiveRegion } from "@/lib/live-region";
import { getUpdatedTtc, useTtc } from "@/lib/ttc";
import { getShuffledChoicesIds } from "@/lib/utils";

interface RankingElementProps {
  element: TSurveyRankingElement;
  value: string[];
  onChange: (responseData: TResponseData) => void;
  languageCode: string;
  ttc: TResponseTtc;
  setTtc: (ttc: TResponseTtc) => void;
  autoFocusEnabled: boolean;
  currentElementId: string;
  errorMessage?: string;
  dir?: "ltr" | "rtl" | "auto";
}

export function RankingElement({
  element,
  value,
  onChange,
  languageCode,
  ttc,
  setTtc,
  currentElementId,
  errorMessage,
  dir = "auto",
}: Readonly<RankingElementProps>) {
  const [startTime, setStartTime] = useState(performance.now());
  const isCurrent = element.id === currentElementId;
  const isRequired = element.required;
  const { t } = useTranslation();

  useTtc(element.id, ttc, setTtc, startTime, setStartTime, isCurrent);

  const shuffledChoicesIds = useMemo(() => {
    if (element.shuffleOption) {
      return getShuffledChoicesIds(element.choices, element.shuffleOption);
    }
    return element.choices.map((choice) => choice.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [element.shuffleOption, element.choices.length]);

  const elementChoices = useMemo(() => {
    if (!element.choices.length) {
      return [];
    }
    if (element.shuffleOption === "none") {
      return element.choices;
    }
    return shuffledChoicesIds
      .map((shuffledIdx) => {
        const found = element.choices.find((c) => c.id === shuffledIdx);
        return found;
      })
      .filter(Boolean);
  }, [element.shuffleOption, element.choices, shuffledChoicesIds]);

  // Convert choices to RankingOption format
  const options: RankingOption[] = useMemo(() => {
    return elementChoices
      .filter((choice): choice is NonNullable<typeof choice> => choice !== undefined)
      .map((choice) => ({
        id: choice.id,
        label: getLocalizedValue(choice.label, languageCode),
      }));
  }, [elementChoices, languageCode]);

  // For the survey-ui component, we need to map labels to IDs
  const selectedValues = useMemo(() => {
    if (!value || !Array.isArray(value)) return [];

    const selected: string[] = [];
    value.forEach((val) => {
      // Backwards-compat: if value is already an option ID
      const idMatch = options.find((opt) => opt.id === val);
      if (idMatch) {
        selected.push(idMatch.id);
        return;
      }

      // Normal: value is a label
      const labelMatch = options.find((opt) => opt.label === val);
      if (labelMatch) selected.push(labelMatch.id);
    });

    return selected;
  }, [value, options]);

  // Handle selection changes - store labels directly instead of IDs
  const handleChange = (selectedIds: string[]) => {
    const nextLabels: string[] = [];
    selectedIds.forEach((id) => {
      const matchingOption = options.find((opt) => opt.id === id);
      if (matchingOption) nextLabels.push(matchingOption.label);
    });

    onChange({ [element.id]: nextLabels });

    const updatedTtcObj = getUpdatedTtc(ttc, element.id, performance.now() - startTime);
    setTtc(updatedTtcObj);
  };

  const handleSubmit = (e: Event) => {
    e.preventDefault();
    // Update TTC when form is submitted (for TTC collection)
    const updatedTtcObj = getUpdatedTtc(ttc, element.id, performance.now() - startTime);
    setTtc(updatedTtcObj);
  };

  // The rank an option lands at is otherwise painted only in a <span>, so a keyboard user gets no
  // confirmation that the keypress did anything.
  const handleAnnounce = (change: RankingChange) => {
    const { label, position, total } = change;
    if (change.type === "remove") {
      announceToLiveRegion(t("common.ranking_removed", { label }));
      return;
    }
    // Both keys spelled out rather than selected into a variable: `scan-translations` matches literal
    // `t("...")` arguments, so a computed key reads as unused and fails the translation gate.
    announceToLiveRegion(
      change.type === "add"
        ? t("common.ranking_added", { label, position, total })
        : t("common.ranking_moved", { label, position, total })
    );
  };

  return (
    <form onSubmit={handleSubmit} className="w-full">
      <Ranking
        dir={dir}
        elementId={element.id}
        inputId={element.id}
        headline={getLocalizedValue(element.headline, languageCode)}
        description={element.subheader ? getLocalizedValue(element.subheader, languageCode) : undefined}
        options={options}
        value={selectedValues}
        onChange={handleChange}
        required={isRequired}
        requiredLabel={t("common.required")}
        addLabel={(label) => t("common.add_x_to_ranking", { label })}
        removeLabel={(label) => t("common.remove_x_from_ranking", { label })}
        moveUpLabel={(label) => t("common.move_x_up", { label })}
        moveDownLabel={(label) => t("common.move_x_down", { label })}
        legendLabel={t("common.ranking_options")}
        onAnnounce={handleAnnounce}
        errorMessage={errorMessage}
        imageUrl={element.imageUrl}
        videoUrl={element.videoUrl}
      />
    </form>
  );
}
