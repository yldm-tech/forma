import { DndContext } from "@dnd-kit/core";
import { SortableContext, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import { createId } from "@paralleldrive/cuid2";
import { PlusIcon } from "lucide-react";
import { type JSX, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  TSurveyElement,
  TSurveyElementChoice,
  TSurveyRankingElement,
} from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { ElementOptionChoice } from "./element-option-choice";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { Button } from "../../../components/ui/button";
import { Label } from "../../../components/ui/label";
import { ShuffleOptionSelect } from "../../../components/ui/shuffle-option-select";

interface RankingElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyRankingElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  selectedLanguageCode: string;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const RankingElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  selectedLanguageCode,
  locale,
}: RankingElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const lastChoiceRef = useRef<HTMLInputElement>(null);
  const [isInvalidValue, setIsInvalidValue] = useState<string | null>(null);

  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);
  const surveyLanguages = localSurvey.languages ?? [];

  const updateChoice = (choiceIdx: number, updatedAttributes: Partial<TSurveyElementChoice>) => {
    if (element.choices) {
      const newChoices = element.choices.map((choice, idx) => {
        if (idx !== choiceIdx) return choice;
        return { ...choice, ...updatedAttributes };
      });

      updateElement(elementIdx, { choices: newChoices });
    }
  };

  const addChoice = (choiceIdx: number) => {
    let newChoices = !element.choices ? [] : element.choices;

    const newChoice = {
      id: createId(),
      label: createI18nString("", surveyLanguageCodes),
    };

    updateElement(elementIdx, {
      choices: [...newChoices.slice(0, choiceIdx + 1), newChoice, ...newChoices.slice(choiceIdx + 1)],
    });
  };

  const addOption = () => {
    const choices = !element.choices ? [] : element.choices;

    if (choices.length >= 25) {
      return;
    }

    const newChoice = {
      id: createId(),
      label: createI18nString("", surveyLanguageCodes),
    };

    updateElement(elementIdx, { choices: [...choices, newChoice] });
  };

  const deleteChoice = (choiceIdx: number) => {
    const newChoices = element.choices.filter((_, idx) => idx !== choiceIdx);
    const choiceValue = element.choices[choiceIdx]?.label[selectedLanguageCode];

    if (isInvalidValue === choiceValue) {
      setIsInvalidValue(null);
    }

    updateElement(elementIdx, { choices: newChoices });
  };

  const shuffleOptionsTypes = {
    none: {
      id: "none",
      label: t("workspace.surveys.edit.keep_current_order"),
      show: true,
    },
    all: {
      id: "all",
      label: t("workspace.surveys.edit.randomize_all"),
      show: element.choices.length > 0,
    },
    exceptLast: {
      id: "exceptLast",
      label: t("workspace.surveys.edit.randomize_all_except_last"),
      show: true,
    },
    reverseOrderOccasionally: {
      id: "reverseOrderOccasionally",
      label: t("workspace.surveys.edit.reverse_order_occasionally"),
      show: true,
    },
    reverseOrderExceptLast: {
      id: "reverseOrderExceptLast",
      label: t("workspace.surveys.edit.reverse_order_occasionally_except_last"),
      show: true,
    },
  };

  useEffect(() => {
    if (lastChoiceRef.current) {
      lastChoiceRef.current?.focus();
    }
  }, [element.choices?.length]);

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
            id="ranking-choices"
            onDragEnd={(event) => {
              const { active, over } = event;

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
              <div className="flex flex-col gap-2" ref={parent}>
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
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>

          <div className="mt-2 flex flex-1 items-center justify-between gap-2">
            <Button
              size="sm"
              variant="secondary"
              type="button"
              disabled={element.choices?.length >= 25}
              onClick={() => addOption()}>
              {t("workspace.surveys.edit.add_option")}
              <PlusIcon />
            </Button>
            <ShuffleOptionSelect
              shuffleOptionsTypes={shuffleOptionsTypes}
              updateElement={updateElement}
              shuffleOption={element.shuffleOption}
              elementIdx={elementIdx}
            />
          </div>
        </div>
      </div>

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
    </form>
  );
};
