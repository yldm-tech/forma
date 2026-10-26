import { DndContext } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { createId } from "@paralleldrive/cuid2";
import { PlusIcon } from "lucide-react";
import { type JSX, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { getLanguageLabel } from "@forma/i18n-utils/utils";
import {
  TMultipleChoiceOptionDisplayType,
  TSurveyElementChoice,
  TSurveyElementTypeEnum,
  TSurveyMultipleChoiceElement,
} from "@forma/types/surveys/elements";
import { isScoredElement } from "@forma/types/surveys/scoring";
import { TShuffleOption, TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import type { TElementUpdate, TUpdateElement } from "../types/editor";
import { ElementFormInput } from "./element-form-input/index";
import { BulkEditOptionsModal } from "./bulk-edit-options-modal";
import { ChoiceScoringToggle } from "./choice-scoring";
import { ElementOptionChoice } from "./element-option-choice";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { findOptionUsedInLogic } from "../lib/utils";
import { Button } from "../../../components/ui/button";
import { Label } from "../../../components/ui/label";
import { OptionsSwitch } from "../../../components/ui/options-switch";
import { ShuffleOptionSelect } from "../../../components/ui/shuffle-option-select";

interface MultipleChoiceElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyMultipleChoiceElement;
  elementIdx: number;
  updateElement: TUpdateElement;
  selectedLanguageCode: string;
  isInvalid: boolean;
  locale: TUserLocale;
  setLocalSurvey?: (survey: TSurvey) => void;
}

export const MultipleChoiceElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  selectedLanguageCode,
  locale,
  setLocalSurvey,
}: MultipleChoiceElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const lastChoiceRef = useRef<HTMLInputElement>(null);
  const [isNew, setIsNew] = useState(true);
  const [isInvalidValue, setisInvalidValue] = useState<string | null>(null);
  const [isBulkEditOpen, setIsBulkEditOpen] = useState(false);

  const elementRef = useRef<HTMLInputElement>(null);
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);
  const surveyLanguages = localSurvey.languages ?? [];
  const shuffleOptionsTypes: Record<TShuffleOption, { id: TShuffleOption; label: string; show: boolean }> = {
    none: {
      id: "none",
      label: t("workspace.surveys.edit.keep_current_order"),
      show: true,
    },
    all: {
      id: "all",
      label: t("workspace.surveys.edit.randomize_all"),
      show: element.choices.every((c) => c.id !== "other" && c.id !== "none"),
    },
    exceptLast: {
      id: "exceptLast",
      label: t("workspace.surveys.edit.randomize_all_except_last"),
      show: true,
    },
    reverseOrderOccasionally: {
      id: "reverseOrderOccasionally",
      label: t("workspace.surveys.edit.reverse_order_occasionally"),
      show: element.choices.every((c) => c.id !== "other" && c.id !== "none"),
    },
    reverseOrderExceptLast: {
      id: "reverseOrderExceptLast",
      label: t("workspace.surveys.edit.reverse_order_occasionally_except_last"),
      show: true,
    },
  };

  const multipleChoiceOptionDisplayTypeOptions = [
    { value: "list", label: t("workspace.surveys.edit.list") },
    { value: "dropdown", label: t("workspace.surveys.edit.dropdown") },
  ];

  // Choice edits are computed from the element's latest choices, not this render's: an option's label is committed 100 ms after its last keystroke, after Enter may already have added the next row.
  const updateCurrentElement = (compute: (current: TSurveyMultipleChoiceElement) => TElementUpdate) =>
    updateElement(elementIdx, (current) =>
      current.type === TSurveyElementTypeEnum.MultipleChoiceSingle ||
      current.type === TSurveyElementTypeEnum.MultipleChoiceMulti
        ? compute(current)
        : {}
    );

  const updateChoice = (choiceIdx: number, updatedAttributes: Partial<TSurveyElementChoice>) => {
    // The index is this render's; the choice it names is found by id in the latest choices.
    const choiceId = element.choices[choiceIdx]?.id;
    if (!choiceId) return;

    updateCurrentElement((current) => ({
      choices: current.choices.map((choice) =>
        choice.id === choiceId ? { ...choice, ...updatedAttributes } : choice
      ),
    }));
  };

  const isScored = isScoredElement(element);

  const regularChoices = useMemo(
    () => element.choices?.filter((c) => c.id !== "other" && c.id !== "none"),
    [element.choices]
  );

  // Get the display name for the selected language (for multi-language surveys)
  const bulkEditButtonLabel = useMemo(() => {
    if (localSurvey.languages.length <= 1) {
      return t("workspace.surveys.edit.bulk_edit");
    }

    const languageCode =
      selectedLanguageCode === "default"
        ? localSurvey.languages.find((lang) => lang.default)?.language.code
        : selectedLanguageCode;

    const languageName = languageCode ? getLanguageLabel(languageCode, locale) : "";
    return `${t("workspace.surveys.edit.bulk_edit")} (${languageName})`;
  }, [localSurvey.languages, selectedLanguageCode, locale, t]);

  const ensureSpecialChoicesOrder = (choices: TSurveyMultipleChoiceElement["choices"]) => {
    const regularChoicesFromInput = choices.filter((c) => c.id !== "other" && c.id !== "none");
    const otherChoice = choices.find((c) => c.id === "other");
    const noneChoice = choices.find((c) => c.id === "none");
    // [regularChoices, otherChoice, noneChoice]
    return [
      ...regularChoicesFromInput,
      ...(otherChoice ? [otherChoice] : []),
      ...(noneChoice ? [noneChoice] : []),
    ];
  };

  const addChoice = (choiceIdx?: number) => {
    setIsNew(false);

    // Created outside the updater, which React may call more than once.
    const newChoice = {
      id: createId(),
      label: createI18nString("", surveyLanguageCodes),
    };
    // The new row goes below the choice the author is on (found by id), or at the end of the regular choices.
    const afterChoiceId = choiceIdx === undefined ? undefined : element.choices[choiceIdx]?.id;

    updateCurrentElement((current) => {
      const regular = current.choices.filter((c) => c.id !== "other" && c.id !== "none");
      const afterIdx = afterChoiceId === undefined ? -1 : regular.findIndex((c) => c.id === afterChoiceId);
      const withNew =
        afterIdx === -1
          ? [...regular, newChoice]
          : [...regular.slice(0, afterIdx + 1), newChoice, ...regular.slice(afterIdx + 1)];

      return {
        choices: ensureSpecialChoicesOrder([
          ...withNew,
          ...current.choices.filter((c) => c.id === "other" || c.id === "none"),
        ]),
      };
    });
  };

  const addSpecialChoice = (choiceId: "other" | "none", labelText: string) => {
    if (element.choices.some((c) => c.id === choiceId)) return;

    const newChoice = {
      id: choiceId,
      label: createI18nString(labelText, surveyLanguageCodes),
    };

    updateCurrentElement((current) => {
      if (current.choices.some((c) => c.id === choiceId)) return {};

      return {
        choices: ensureSpecialChoicesOrder([...current.choices, newChoice]),
        ...(choiceId === "other" &&
          !current.otherOptionPlaceholder && {
            otherOptionPlaceholder: createI18nString(
              t("workspace.surveys.edit.please_specify"),
              surveyLanguageCodes
            ),
          }),
        ...(current.shuffleOption === shuffleOptionsTypes.all.id && {
          shuffleOption: shuffleOptionsTypes.exceptLast.id,
        }),
        ...(current.shuffleOption === shuffleOptionsTypes.reverseOrderOccasionally.id && {
          shuffleOption: shuffleOptionsTypes.reverseOrderExceptLast.id,
        }),
      };
    });
  };

  const deleteChoice = (choiceIdx: number) => {
    const choiceToDelete = element.choices[choiceIdx]?.id;
    if (!choiceToDelete) return;

    if (choiceToDelete !== "other" && choiceToDelete !== "none") {
      const idx = findOptionUsedInLogic(localSurvey, element.id, choiceToDelete);
      if (idx !== -1) {
        toast.error(
          t("workspace.surveys.edit.option_used_in_logic_error", {
            questionIndex: idx + 1,
          })
        );
        return;
      }
    }

    const choiceValue = element.choices[choiceIdx]?.label[selectedLanguageCode];
    if (isInvalidValue === choiceValue) {
      setisInvalidValue(null);
    }

    updateCurrentElement((current) => {
      const newChoices = current.choices.filter((c) => c.id !== choiceToDelete);
      const hasRemainingSpecialChoices = newChoices.some((c) => c.id === "other" || c.id === "none");

      return {
        choices: newChoices,
        ...(!hasRemainingSpecialChoices &&
          current.shuffleOption === "reverseOrderExceptLast" && {
            shuffleOption: "reverseOrderOccasionally",
          }),
        ...(!hasRemainingSpecialChoices &&
          current.shuffleOption === "exceptLast" && {
            shuffleOption: "all",
          }),
      };
    });
  };

  useEffect(() => {
    if (lastChoiceRef.current) {
      lastChoiceRef.current?.focus();
    }
  }, [element.choices?.length]);

  // This effect will run once on initial render, setting focus to the element input.
  useEffect(() => {
    if (isNew && elementRef.current) {
      elementRef.current.focus();
    }
  }, [isNew]);

  const specialChoices = [
    {
      id: "other",
      label: t("common.other"),
      addChoice: () => addSpecialChoice("other", t("common.other")),
      addButtonText: t("workspace.surveys.edit.add_other"),
    },
    {
      id: "none",
      label: t("common.none_of_the_above"),
      addChoice: () => addSpecialChoice("none", t("common.none_of_the_above")),
      addButtonText: t("workspace.surveys.edit.add_none_of_the_above"),
    },
  ];

  // Auto animate
  const [parent] = useAutoAnimate();

  return (
    <form>
      <ElementFormInput
        id="headline"
        value={element.headline}
        label={t("workspace.surveys.edit.question") + "*"}
        localSurvey={localSurvey}
        elementIdx={elementIdx}
        isInvalid={isInvalid}
        updateElement={updateElement}
        locale={locale}
        autoFocus={!element.headline?.default || element.headline.default.trim() === ""}
      />

      <div ref={parent}>
        {element.subheader !== undefined && (
          <div className="inline-flex w-full items-center">
            <div className="w-full">
              <ElementFormInput
                id="subheader"
                value={element.subheader}
                label={t("common.description")}
                localSurvey={localSurvey}
                elementIdx={elementIdx}
                isInvalid={isInvalid}
                updateElement={updateElement}
                locale={locale}
                autoFocus={!element.subheader?.default || element.subheader.default.trim() === ""}
              />
            </div>
          </div>
        )}
        {element.subheader === undefined && (
          <Button
            size="sm"
            variant="secondary"
            className="mt-3"
            type="button"
            onClick={() => {
              updateElement(elementIdx, {
                subheader: createI18nString("", surveyLanguageCodes),
              });
            }}>
            <PlusIcon className="mr-1 size-4" />
            {t("workspace.surveys.edit.add_description")}
          </Button>
        )}
      </div>

      <div className="mt-3">
        <Label htmlFor="choices">{t("workspace.surveys.edit.options")}</Label>
        <div className="mt-2" id="choices">
          <DndContext
            id="multi-choice-choices"
            onDragEnd={(event) => {
              const { active, over } = event;

              if (
                active.id === "other" ||
                over?.id === "other" ||
                active.id === "none" ||
                over?.id === "none"
              ) {
                return;
              }

              if (!active || !over) {
                return;
              }

              const activeIndex = element.choices.findIndex((choice) => choice.id === active.id);
              const overIndex = element.choices.findIndex((choice) => choice.id === over.id);

              const newChoices = [...element.choices];

              const [movedChoice] = newChoices.splice(activeIndex, 1);
              if (!movedChoice) return;
              newChoices.splice(overIndex, 0, movedChoice);

              updateElement(elementIdx, { choices: newChoices });
            }}>
            <SortableContext items={element.choices} strategy={verticalListSortingStrategy}>
              <div className="flex max-h-[25dvh] flex-col gap-2 overflow-y-auto py-1 pr-1" ref={parent}>
                {element.choices?.map((choice, choiceIdx) => (
                  <ElementOptionChoice
                    key={choice.id}
                    choice={choice}
                    choiceIdx={choiceIdx}
                    elementIdx={elementIdx}
                    updateChoice={updateChoice}
                    deleteChoice={deleteChoice}
                    addChoice={addChoice}
                    isInvalid={isInvalid}
                    localSurvey={localSurvey}
                    surveyLanguages={surveyLanguages}
                    element={element}
                    updateElement={updateElement}
                    surveyLanguageCodes={surveyLanguageCodes}
                    locale={locale}
                    showScore={isScored}
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>
        </div>

        <div className="mt-2">
          <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap gap-2">
              {specialChoices.map((specialChoice) => {
                if (element.choices.some((c) => c.id === specialChoice.id)) return null;
                return (
                  <Button
                    size="sm"
                    key={specialChoice.id}
                    variant="secondary"
                    type="button"
                    onClick={() => specialChoice.addChoice()}>
                    {specialChoice.addButtonText}
                  </Button>
                );
              })}
              <Button size="sm" variant="secondary" type="button" onClick={() => setIsBulkEditOpen(true)}>
                {bulkEditButtonLabel}
              </Button>
            </div>
            <Button
              size="sm"
              variant="secondary"
              type="button"
              onClick={() => {
                updateElement(elementIdx, {
                  type:
                    element.type === TSurveyElementTypeEnum.MultipleChoiceMulti
                      ? TSurveyElementTypeEnum.MultipleChoiceSingle
                      : TSurveyElementTypeEnum.MultipleChoiceMulti,
                });
              }}>
              {element.type === TSurveyElementTypeEnum.MultipleChoiceSingle
                ? t("workspace.surveys.edit.convert_to_multiple_choice")
                : t("workspace.surveys.edit.convert_to_single_choice")}
            </Button>

            <div className="flex flex-1 items-center justify-end gap-2">
              <ShuffleOptionSelect
                elementIdx={elementIdx}
                shuffleOption={element.shuffleOption}
                updateElement={updateElement}
                shuffleOptionsTypes={shuffleOptionsTypes}
              />
            </div>
          </div>
        </div>
      </div>

      <ChoiceScoringToggle
        localSurvey={localSurvey}
        element={element}
        elementIdx={elementIdx}
        updateElement={updateElement}
        setLocalSurvey={setLocalSurvey}
      />

      <div className="mt-3">
        <Label>{t("workspace.surveys.edit.display_type")}</Label>
        <div className="mt-2">
          <OptionsSwitch
            options={multipleChoiceOptionDisplayTypeOptions}
            currentOption={element.displayType ?? "list"}
            handleOptionChange={(value: string) =>
              updateElement(elementIdx, { displayType: value as TMultipleChoiceOptionDisplayType })
            }
          />
        </div>
      </div>

      <BulkEditOptionsModal
        isOpen={isBulkEditOpen}
        onClose={() => setIsBulkEditOpen(false)}
        regularChoices={regularChoices}
        onSave={(updatedChoices) => {
          updateCurrentElement((current) => ({
            choices: ensureSpecialChoicesOrder([
              ...updatedChoices,
              ...current.choices.filter((c) => c.id === "other" || c.id === "none"),
            ]),
          }));
        }}
        element={element}
        localSurvey={localSurvey}
        surveyLanguageCodes={surveyLanguageCodes}
        locale={locale}
      />

      {element.type === TSurveyElementTypeEnum.MultipleChoiceMulti && (
        <ValidationRulesEditor
          elementType={element.type}
          validation={element.validation}
          onUpdateValidation={(validation) => {
            updateElement(elementIdx, {
              validation,
            });
          }}
          element={element}
        />
      )}
    </form>
  );
};
