import { useAutoAnimate } from "@formkit/auto-animate/react";
import { PlusIcon } from "lucide-react";
import { type JSX } from "react";
import { useTranslation } from "react-i18next";
import { type TSurveyElement, TSurveyNPSElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Button } from "../../../components/ui/button";

interface NPSElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyNPSElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const NPSElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: NPSElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);
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

      <div className="flex justify-between gap-x-2">
        <div className="w-full">
          <ElementFormInput
            id="lowerLabel"
            value={element.lowerLabel}
            label={t("workspace.surveys.edit.lower_label")}
            localSurvey={localSurvey}
            elementIdx={elementIdx}
            isInvalid={isInvalid}
            updateElement={updateElement}
            locale={locale}
          />
        </div>
        <div className="w-full">
          <ElementFormInput
            id="upperLabel"
            value={element.upperLabel}
            label={t("workspace.surveys.edit.upper_label")}
            localSurvey={localSurvey}
            elementIdx={elementIdx}
            isInvalid={isInvalid}
            updateElement={updateElement}
            locale={locale}
          />
        </div>
      </div>

      <AdvancedOptionToggle
        isChecked={element.isColorCodingEnabled}
        onToggle={() => updateElement(elementIdx, { isColorCodingEnabled: !element.isColorCodingEnabled })}
        htmlId="isColorCodingEnabled"
        title={t("workspace.surveys.edit.add_color_coding")}
        description={t("workspace.surveys.edit.add_color_coding_description")}
        childBorder
        customContainerClass="p-0 mt-4"
      />
    </form>
  );
};
