import { useAutoAnimate } from "@formkit/auto-animate/react";
import { HashIcon, PlusIcon, SmileIcon, StarIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { type TSurveyCesElement, type TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";
import { Dropdown } from "./rating-type-dropdown";
import { AdvancedOptionToggle } from "../../../components/ui/advanced-option-toggle";
import { Button } from "../../../components/ui/button";
import { Label } from "../../../components/ui/label";

interface CESElementFormProps {
  localSurvey: TSurvey;
  element: TSurveyCesElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  isInvalid: boolean;
  locale: TUserLocale;
}

export const CESElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: CESElementFormProps) => {
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

      <div className="mt-3 flex justify-between gap-8">
        <div className="flex-1">
          <Label htmlFor="scale">{t("workspace.surveys.edit.scale")}</Label>
          <div className="mt-2">
            <Dropdown
              options={[
                { label: t("workspace.surveys.edit.number"), value: "number", icon: HashIcon },
                { label: t("workspace.surveys.edit.star"), value: "star", icon: StarIcon },
                { label: t("workspace.surveys.edit.smiley"), value: "smiley", icon: SmileIcon },
              ]}
              defaultValue={element.scale || "number"}
              onSelect={(option) => {
                if (option.value === "star") {
                  updateElement(elementIdx, { scale: option.value, isColorCodingEnabled: false });
                  return;
                }
                updateElement(elementIdx, { scale: option.value as "number" | "smiley" | "star" });
              }}
            />
          </div>
        </div>
        <div className="flex-1">
          <Label htmlFor="range">{t("workspace.surveys.edit.range")}</Label>
          <div className="mt-2">
            <Dropdown
              options={[
                { label: t("workspace.surveys.edit.five_points_recommended"), value: 5 },
                { label: t("workspace.surveys.edit.seven_points"), value: 7 },
              ]}
              defaultValue={element.range || 5}
              onSelect={(option) =>
                updateElement(elementIdx, { range: option.value as TSurveyCesElement["range"] })
              }
            />
          </div>
        </div>
      </div>

      <div className="flex justify-between gap-8">
        <div className="flex-1">
          <ElementFormInput
            id="lowerLabel"
            placeholder={t("templates.ces_lower_label")}
            value={element.lowerLabel}
            label={t("workspace.surveys.edit.lower_label")}
            localSurvey={localSurvey}
            elementIdx={elementIdx}
            isInvalid={isInvalid}
            updateElement={updateElement}
            locale={locale}
          />
        </div>
        <div className="flex-1">
          <ElementFormInput
            id="upperLabel"
            placeholder={t("templates.ces_upper_label")}
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

      {element.scale !== "star" && (
        <AdvancedOptionToggle
          isChecked={element.isColorCodingEnabled}
          onToggle={() => updateElement(elementIdx, { isColorCodingEnabled: !element.isColorCodingEnabled })}
          htmlId="isColorCodingEnabled"
          title={t("workspace.surveys.edit.add_color_coding")}
          description={t("workspace.surveys.edit.add_color_coding_description")}
          childBorder
          customContainerClass="p-0 mt-4"
        />
      )}
    </form>
  );
};
