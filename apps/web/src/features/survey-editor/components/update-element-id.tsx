import { useState } from "react";
import type { TElementUpdate } from "../types/editor";
import { toast } from "../../../lib/toast";
import { useTranslation } from "react-i18next";
import { TSurveyElement } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { validateId } from "@forma/types/surveys/validation";
import { getValidateIdErrorMessage } from "../lib/validation";
import { getElementsFromBlocks } from "../lib/client-utils";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";

interface UpdateElementIdProps {
  localSurvey: TSurvey;
  element: TSurveyElement;
  elementIdx: number;
  updateElement: (elementIdx: number, updatedAttributes: TElementUpdate) => void;
}

export const UpdateElementId = ({
  localSurvey,
  element,
  elementIdx,
  updateElement,
}: UpdateElementIdProps) => {
  const { t } = useTranslation();
  const [currentValue, setCurrentValue] = useState(element.id);
  const [prevValue, setPrevValue] = useState(element.id);
  const [isInputInvalid, setIsInputInvalid] = useState(
    currentValue.trim() === "" || currentValue.includes(" ")
  );

  const saveAction = () => {
    // return early if the input value was not changed
    if (currentValue === prevValue) {
      return;
    }

    const elements = getElementsFromBlocks(localSurvey.blocks);
    const elementIds = elements.map((q) => q.id);
    const endingCardIds = localSurvey.endings.map((e) => e.id);
    const hiddenFieldIds = localSurvey.hiddenFields.fieldIds ?? [];

    const validateIdError = validateId(currentValue, elementIds, endingCardIds, hiddenFieldIds);

    if (validateIdError) {
      setIsInputInvalid(true);
      toast.error(getValidateIdErrorMessage(validateIdError, "question", t));
      setCurrentValue(prevValue);
      return;
    }

    setIsInputInvalid(false);
    toast.success(t("workspace.surveys.edit.question_id_updated"));
    updateElement(elementIdx, { id: currentValue });
    setPrevValue(currentValue); // after successful update, set current value as previous value
  };

  const isButtonDisabled = () => {
    if (currentValue === element.id || currentValue.trim() === "") return true;
    else return false;
  };

  return (
    <div>
      <Label htmlFor="elementId">{t("common.question_id")}</Label>
      <div className="mt-2 inline-flex w-full items-center gap-x-2">
        <Input
          id="elementId"
          name="elementId"
          value={currentValue}
          onChange={(e) => {
            setCurrentValue(e.target.value);
          }}
          aria-describedby="elementId-help"
          dir="auto"
          disabled={localSurvey.status !== "draft" && !element.isDraft}
          className={`h-10 ${isInputInvalid ? "border-red-300 focus:border-red-300" : ""}`}
        />
        <Button size="sm" onClick={saveAction} disabled={isButtonDisabled()} className="h-10">
          {t("common.save")}
        </Button>
      </div>
      <p id="elementId-help" className="mt-1 text-xs text-slate-500">
        {t("workspace.surveys.edit.question_id_help")}
      </p>
    </div>
  );
};
