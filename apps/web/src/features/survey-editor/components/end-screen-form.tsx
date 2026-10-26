import { PlusIcon } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TSurvey, TSurveyEndScreenCard, TSurveyRedirectUrlCard } from "@forma/types/surveys/types";
import { TUserLocale } from "@forma/types/user";
import { createI18nString, extractLanguageCodes, getLocalizedValue } from "../../../lib/survey/i18n";
import { headlineToRecall, recallToHeadline } from "../../../lib/survey/recall";
import { ElementFormInput } from "./element-form-input/index";
import { RecallWrapper } from "./element-form-input/components/recall-wrapper";
import { getElementsFromBlocks } from "../lib/client-utils";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { Switch } from "../../../components/ui/switch";

interface EndScreenFormProps {
  localSurvey: TSurvey;
  endingCardIndex: number;
  isInvalid: boolean;
  selectedLanguageCode: string;
  updateSurvey: (
    input: Partial<TSurveyEndScreenCard & { _forceUpdate?: boolean }> | Partial<TSurveyRedirectUrlCard>
  ) => void;
  endingCard: TSurveyEndScreenCard;
  locale: TUserLocale;
}

export const EndScreenForm = ({
  localSurvey,
  endingCardIndex,
  isInvalid,
  selectedLanguageCode,
  updateSurvey,
  endingCard,
  locale,
}: EndScreenFormProps) => {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  const surveyLanguageCodes = extractLanguageCodes(localSurvey.languages);

  const questions = getElementsFromBlocks(localSurvey.blocks);

  const defaultLanguageCode = localSurvey.languages.find((lang) => lang.default)?.language.code ?? "default";
  const usedLanguageCode = selectedLanguageCode === defaultLanguageCode ? "default" : selectedLanguageCode;

  const [showEndingCardCTA, setshowEndingCardCTA] = useState<boolean>(
    endingCard.type === "endScreen" &&
      (!!getLocalizedValue(endingCard.buttonLabel, selectedLanguageCode) || !!endingCard.buttonLink)
  );

  // The checkmark only renders when the card carries no media, so the toggle is meaningless once an
  // image or video is set.
  const hasMedia = Boolean(endingCard.imageUrl ?? endingCard.videoUrl);

  return (
    <form>
      <ElementFormInput
        id="headline"
        label={t("common.headline") + "*"}
        value={endingCard.headline}
        localSurvey={localSurvey}
        elementIdx={questions.length + endingCardIndex}
        isInvalid={isInvalid}
        updateSurvey={updateSurvey}
        locale={locale}
        autoFocus={!endingCard.headline?.default || endingCard.headline.default.trim() === ""}
      />
      <div>
        {endingCard.subheader !== undefined && (
          <div className="inline-flex w-full items-center">
            <div className="w-full">
              <ElementFormInput
                id="subheader"
                value={endingCard.subheader}
                label={t("common.description")}
                localSurvey={localSurvey}
                elementIdx={questions.length + endingCardIndex}
                isInvalid={isInvalid}
                updateSurvey={updateSurvey}
                locale={locale}
                autoFocus={!endingCard.subheader?.default || endingCard.subheader.default.trim() === ""}
              />
            </div>
          </div>
        )}

        {endingCard.subheader === undefined && (
          <Button
            size="sm"
            className="mt-3"
            variant="secondary"
            type="button"
            onClick={() => {
              // Directly update the state, bypassing the guard in updateSurvey
              updateSurvey({
                subheader: createI18nString("", surveyLanguageCodes),
                _forceUpdate: true,
              });
            }}>
            <PlusIcon className="mr-1 size-4" />
            {t("workspace.surveys.edit.add_description")}
          </Button>
        )}
      </div>
      {!hasMedia && (
        <div className="mt-4 flex items-center gap-x-1">
          <Switch
            id="showCheckmarkIcon"
            checked={!endingCard.hideDefaultIcon}
            onCheckedChange={(checked) => {
              updateSurvey({ hideDefaultIcon: checked ? undefined : true });
            }}
          />
          <Label htmlFor="showCheckmarkIcon" className="cursor-pointer">
            <div className="ml-2">
              <h3 className="text-sm font-semibold text-slate-700">
                {t("workspace.surveys.edit.show_checkmark_icon")}
              </h3>
              <p className="text-xs font-normal text-slate-500">
                {t("workspace.surveys.edit.show_checkmark_icon_description")}
              </p>
            </div>
          </Label>
        </div>
      )}
      <div className="mt-4">
        <div className="flex items-center gap-x-1">
          <Switch
            id="showButton"
            checked={showEndingCardCTA}
            onCheckedChange={() => {
              if (showEndingCardCTA) {
                updateSurvey({ buttonLabel: undefined, buttonLink: undefined });
              } else {
                // The button is the author's own: it starts empty rather than as a link to Forma that would reach respondents if the author changed nothing.
                updateSurvey({
                  buttonLabel: createI18nString("", surveyLanguageCodes),
                  buttonLink: "",
                });
              }
              setshowEndingCardCTA(!showEndingCardCTA);
            }}
          />
          <Label htmlFor="showButton" className="cursor-pointer">
            <div className="ml-2">
              <h3 className="text-sm font-semibold text-slate-700">
                {t("workspace.surveys.edit.show_button")}
              </h3>
              <p className="text-xs font-normal text-slate-500">
                {t("workspace.surveys.edit.send_your_respondents_to_a_page_of_your_choice")}
              </p>
            </div>
          </Label>
        </div>
        {showEndingCardCTA && (
          <div className="mt-4 space-y-4 rounded-md border bg-slate-100 p-4 pt-2">
            <div className="space-y-2">
              <ElementFormInput
                id="buttonLabel"
                label={t("workspace.surveys.edit.button_label")}
                placeholder={t("workspace.surveys.edit.create_your_own_survey")}
                className="rounded-md"
                value={endingCard.buttonLabel}
                localSurvey={localSurvey}
                elementIdx={questions.length + endingCardIndex}
                isInvalid={isInvalid}
                updateSurvey={updateSurvey}
                locale={locale}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("workspace.surveys.edit.button_url")}</Label>
              <div className="rounded-md bg-white">
                <RecallWrapper
                  value={endingCard.buttonLink ?? ""}
                  elementId={endingCard.id}
                  onChange={(val, recallItems, fallbacks) => {
                    const updatedValue = {
                      ...endingCard,
                      buttonLink:
                        recallItems && fallbacks ? headlineToRecall(val, recallItems, fallbacks) : val,
                    };

                    updateSurvey(updatedValue);
                  }}
                  onAddFallback={() => {
                    inputRef.current?.focus();
                  }}
                  isRecallAllowed
                  localSurvey={localSurvey}
                  usedLanguageCode={usedLanguageCode}
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
                          id="buttonLink"
                          name="buttonLink"
                          className="relative text-black caret-black"
                          placeholder="https://forma.yldm.ai"
                          value={
                            recallToHeadline(
                              {
                                [usedLanguageCode]: value,
                              },
                              localSurvey,
                              false,
                              usedLanguageCode
                            )[usedLanguageCode]
                          }
                          onChange={(e) => onChange(e.target.value)}
                        />
                        {children}
                      </div>
                    );
                  }}
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </form>
  );
};
