import { useTranslation } from "react-i18next";
import { TSurveyElement, TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import { TSurveyVariable } from "@forma/types/surveys/types";
import { getLocalizedValue } from "../../../lib/survey/i18n";
import { IdBadge } from "../../../components/ui/id-badge";
import { Label } from "../../../components/ui/label";

interface OptionIdsElementProps {
  type: "element";
  element: TSurveyElement;
}

interface OptionIdsVariablesProps {
  type: "variables";
  variables: TSurveyVariable[];
}

type OptionIdsProps = OptionIdsElementProps | OptionIdsVariablesProps;

export const OptionIds = (props: OptionIdsProps) => {
  const { t } = useTranslation();
  const selectedLanguageCode = "default";

  const renderChoiceIds = (element: TSurveyElement, selectedLanguageCode: string) => {
    switch (element.type) {
      case TSurveyElementTypeEnum.MultipleChoiceSingle:
      case TSurveyElementTypeEnum.MultipleChoiceMulti:
      case TSurveyElementTypeEnum.Ranking:
        return (
          <div className="flex flex-col gap-2">
            {element.choices.map((choice) => (
              <div key={choice.id}>
                <IdBadge id={choice.id} label={getLocalizedValue(choice.label, selectedLanguageCode)} />
              </div>
            ))}
          </div>
        );

      case TSurveyElementTypeEnum.PictureSelection:
        return (
          <div className="flex flex-col gap-3">
            {element.choices.map((choice) => {
              const imageUrl = choice.imageUrl;
              if (!imageUrl) return null;
              return (
                <div key={choice.id} className="flex items-center gap-3">
                  <div className="relative h-24 w-40 overflow-hidden rounded-lg bg-gray-100">
                    <img
                      src={imageUrl}
                      alt={`Choice ${choice.id}`}
                      style={{ objectFit: "cover" }}
                      className="absolute inset-0 h-full w-full rounded-lg transition-opacity duration-200"
                    />
                  </div>
                  <IdBadge id={choice.id} />
                </div>
              );
            })}
          </div>
        );

      default:
        return <></>;
    }
  };

  const renderVariableIds = (variables: TSurveyVariable[]) => {
    return (
      <div className="flex flex-col gap-2">
        {variables.map((variable) => (
          <div key={variable.id}>
            <IdBadge id={variable.id} label={variable.name} />
          </div>
        ))}
      </div>
    );
  };

  if (props.type === "variables") {
    return (
      <div className="space-y-3">
        <Label className="text-sm font-medium text-gray-700">{t("common.variable_ids")}</Label>
        <div className="w-full">{renderVariableIds(props.variables)}</div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <Label className="text-sm font-medium text-gray-700">{t("common.option_ids")}</Label>
      <div className="w-full">{renderChoiceIds(props.element, selectedLanguageCode)}</div>
    </div>
  );
};
