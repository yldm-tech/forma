import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { TSurvey, TSurveyRedirectUrlCard } from "@forma/types/surveys/types";
import { headlineToRecall, recallToHeadline } from "../../../lib/survey/recall";
import { RecallWrapper } from "./element-form-input/components/recall-wrapper";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";

interface RedirectUrlFormProps {
  localSurvey: TSurvey;
  endingCard: TSurveyRedirectUrlCard;
  updateSurvey: (input: Partial<TSurveyRedirectUrlCard>) => void;
}

export const RedirectUrlForm = ({ localSurvey, endingCard, updateSurvey }: RedirectUrlFormProps) => {
  const selectedLanguageCode = "default";
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);

  return (
    <form className="mt-3 space-y-3">
      <div className="space-y-2">
        <Label>{t("common.url")}</Label>
        <RecallWrapper
          value={endingCard.url ?? ""}
          elementId={endingCard.id}
          onChange={(val, recallItems, fallbacks) => {
            const updatedValue = {
              ...endingCard,
              url: recallItems && fallbacks ? headlineToRecall(val, recallItems, fallbacks) : val,
            };

            updateSurvey(updatedValue);
          }}
          onAddFallback={() => {
            inputRef.current?.focus();
          }}
          isRecallAllowed
          localSurvey={localSurvey}
          usedLanguageCode={"default"}
          render={({ value, onChange, highlightedJSX, children }) => {
            return (
              <div className="group relative">
                {/* The highlight container is absolutely positioned behind the input */}
                <div
                  className={`no-scrollbar absolute top-0 z-0 mt-0.5 flex h-10 w-full overflow-scroll px-3 py-2 text-center text-sm whitespace-nowrap text-transparent`}
                  dir="auto"
                  key={highlightedJSX.length}>
                  {highlightedJSX}
                </div>
                <Input
                  ref={inputRef}
                  id="redirectUrl"
                  name="redirectUrl"
                  className="relative text-black caret-black"
                  placeholder="https://forma.yldm.ai"
                  value={
                    recallToHeadline(
                      {
                        [selectedLanguageCode]: value,
                      },
                      localSurvey,
                      false,
                      "default"
                    )[selectedLanguageCode]
                  }
                  onChange={(e) => onChange(e.target.value)}
                />
                {children}
              </div>
            );
          }}
        />
      </div>
      <div className="space-y-2">
        <Label>{t("common.label")}</Label>
        <Input
          id="redirectUrlLabel"
          name="redirectUrlLabel"
          className="bg-white"
          placeholder="Forma App"
          value={endingCard.label ?? ""}
          onChange={(e) => updateSurvey({ label: e.target.value })}
        />
      </div>
    </form>
  );
};
