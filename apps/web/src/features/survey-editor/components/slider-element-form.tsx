import { useAutoAnimate } from "@formkit/auto-animate/react";
import { PlusIcon } from "lucide-react";
import { type JSX, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TSurveyElement, TSurveySliderElement } from "@forma/types/surveys/elements";
import {
  SLIDER_MAX_STEPS,
  type TSliderScaleIssueCode,
  getSliderScaleIssue,
} from "@forma/types/surveys/slider";
import { TSurvey } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { createI18nString, extractLanguageCodes } from "../../../lib/survey/i18n";
import { ElementFormInput } from "./element-form-input/index";

interface SliderElementFormProps {
  localSurvey: TSurvey;
  element: TSurveySliderElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: Partial<TSurveyElement>) => void;
  isInvalid: boolean;
  locale: TUserLocale;
}

type TScaleField = "min" | "max" | "step";

const SCALE_FIELDS: TScaleField[] = ["min", "max", "step"];

/** The slider's own settings: the question, its scale (minimum, maximum and step) and the labels at either end. */
export const SliderElementForm = ({
  element,
  elementIdx,
  updateElement,
  isInvalid,
  localSurvey,
  locale,
}: SliderElementFormProps): JSX.Element => {
  const { t } = useTranslation();
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);
  const [parent] = useAutoAnimate();
  // What is typed in each number box. A half-typed number ("-", "0.") is kept here and only a complete one is saved, so typing is never undone under the cursor.
  const [drafts, setDrafts] = useState<Record<TScaleField, string>>(() => ({
    min: String(element.min),
    max: String(element.max),
    step: String(element.step),
  }));

  const scaleLabels: Record<TScaleField, string> = {
    min: t("workspace.surveys.edit.slider_min"),
    max: t("workspace.surveys.edit.slider_max"),
    step: t("workspace.surveys.edit.slider_step"),
  };

  const issueMessages: Record<TSliderScaleIssueCode, string> = {
    notNumbers: t("workspace.surveys.edit.slider_scale_numbers_required"),
    stepNotPositive: t("workspace.surveys.edit.slider_step_must_be_positive"),
    maxNotAboveMin: t("workspace.surveys.edit.slider_max_must_exceed_min"),
    stepUneven: t("workspace.surveys.edit.slider_step_must_divide_range"),
    tooManySteps: t("workspace.surveys.edit.slider_too_many_steps", { max: SLIDER_MAX_STEPS }),
  };

  const isNumberText = (text: string): boolean => text.trim() !== "" && Number.isFinite(Number(text));

  // A box that is blank or holds no number is not saved, so the element still has the number from before; the issue shown is about that box, not about the hidden number.
  const blankField = SCALE_FIELDS.find((field) => !isNumberText(drafts[field]));
  const issue: { code: TSliderScaleIssueCode; field: TScaleField } | null = blankField
    ? { code: "notNumbers", field: blankField }
    : getSliderScaleIssue(element);

  const updateScaleField = (field: TScaleField, text: string) => {
    setDrafts((current) => ({ ...current, [field]: text }));
    if (isNumberText(text)) {
      updateElement(elementIdx, { [field]: Number(text) });
    }
  };

  // Leaving a box that holds no number puts back the number that is saved, so the box never shows something other than what the survey will use.
  const restoreScaleField = (field: TScaleField) => {
    if (!isNumberText(drafts[field])) {
      setDrafts((current) => ({ ...current, [field]: String(element[field]) }));
    }
  };

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

      <div className="mt-3 grid grid-cols-3 gap-x-2">
        {SCALE_FIELDS.map((field) => {
          const inputId = `slider-${element.id}-${field}`;
          const isFieldInvalid = issue?.field === field;
          return (
            <div key={field} className="flex flex-col gap-y-2">
              <Label htmlFor={inputId}>{scaleLabels[field]}</Label>
              <Input
                id={inputId}
                type="number"
                inputMode="decimal"
                step="any"
                min={field === "step" ? 0 : undefined}
                value={drafts[field]}
                onChange={(e) => {
                  updateScaleField(field, e.target.value);
                }}
                onBlur={() => {
                  restoreScaleField(field);
                }}
                aria-invalid={isFieldInvalid}
                aria-describedby={isFieldInvalid ? `slider-${element.id}-scale-issue` : undefined}
                className={isFieldInvalid ? "border-red-400" : undefined}
              />
            </div>
          );
        })}
      </div>
      {issue ? (
        <p id={`slider-${element.id}-scale-issue`} role="alert" className="mt-2 text-sm text-red-600">
          {issueMessages[issue.code]}
        </p>
      ) : null}

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
    </form>
  );
};
