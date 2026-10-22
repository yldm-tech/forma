import { TSurveyBlockLogic } from "@forma/types/surveys/blocks";
import type { TElementUpdate } from "../types/editor";
import { TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurvey } from "@forma/types/surveys/types";
import { OptionIds } from "./option-ids";
import { UpdateElementId } from "./update-element-id";

interface AdvancedSettingsProps {
  element: TSurveyElement;
  elementIdx: number;
  localSurvey: TSurvey;
  updateElement: (elementIdx: number, updatedAttributes: TElementUpdate) => void;
  updateBlockLogic: (elementIdx: number, logic: TSurveyBlockLogic[]) => void;
  updateBlockLogicFallback: (elementIdx: number, logicFallback: string | undefined) => void;
}

export const AdvancedSettings = ({
  element,
  elementIdx,
  localSurvey,
  updateElement,
}: AdvancedSettingsProps) => {
  const showOptionIds =
    element.type === TSurveyElementTypeEnum.PictureSelection ||
    element.type === TSurveyElementTypeEnum.MultipleChoiceSingle ||
    element.type === TSurveyElementTypeEnum.MultipleChoiceMulti ||
    element.type === TSurveyElementTypeEnum.Ranking;

  return (
    <div className="flex flex-col gap-4">
      <UpdateElementId
        element={element}
        elementIdx={elementIdx}
        localSurvey={localSurvey}
        updateElement={updateElement}
      />

      {showOptionIds && <OptionIds type="element" element={element} />}
    </div>
  );
};
