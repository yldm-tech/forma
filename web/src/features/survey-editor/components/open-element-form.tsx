import { useAutoAnimate } from "@formkit/auto-animate/react";
import { PlusIcon } from "lucide-react";
import { JSX } from "react";
import { useTranslation } from "react-i18next";
import type {
  TSurveyElement,
  TSurveyOpenTextElement,
  TSurveyOpenTextElementInputType,
} from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { ValidationRulesEditor } from "./validation-rules-editor";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Button } from "../../../components/ui/button";

interface OpenElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyOpenTextElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  lastElement: boolean;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const OpenElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: OpenElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const defaultPlaceholder = getPlaceholderByInputType(element.inputType ?? "text");
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages ?? []);

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
      <div className="mt-2">
        <ElementFormInput
          id="placeholder"
          value={
            element.placeholder
              ? element.placeholder
              : createI18nString(defaultPlaceholder, surveyLanguageCodes)
          }
          localSurvey={localSurvey}
          elementIdx={elementIdx}
          isInvalid={isInvalid}
          updateElement={updateElement}
          label={t("common.placeholder")}
          locale={locale}
        />
      </div>

      <div className="mt-6 space-y-6">
        <div className="mt-4">
          <AdvancedOptionToggle
            isChecked={element.longAnswer !== false}
            onToggle={(checked: boolean) => {
              updateElement(elementIdx, {
                longAnswer: checked,
              });
            }}
            htmlId={`longAnswer-${element.id}`}
            title={t("workspace.surveys.edit.long_answer")}
            description={t("workspace.surveys.edit.long_answer_toggle_description")}
            disabled={element.inputType !== "text"}
            customContainerClass="p-0"
          />
        </div>

        <ValidationRulesEditor
          elementType={element.type}
          validation={element.validation}
          onUpdateValidation={(validation) => {
            updateElement(elementIdx, {
              validation,
            });
          }}
          inputType={element.inputType ?? "text"}
          onUpdateInputType={(newInputType) => {
            updateElement(elementIdx, {
              inputType: newInputType,
              longAnswer: newInputType === "text",
            });
          }}
        />
      </div>
    </form>
  );
};

const getPlaceholderByInputType = (inputType: TSurveyOpenTextElementInputType) => {
  switch (inputType) {
    case "email":
      return "example@email.com";
    case "url":
      return "https://...";
    case "number":
      return "42";
    case "phone":
      return "+1 123 456 789";
    default:
      return "Type your answer here...";
  }
};
