import { createId } from "@paralleldrive/cuid2";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { TSurveyElementTypeEnum } from "@forma/types/surveys/elements";
import {
  CHOICE_SCORE_LIMIT,
  type TScorableElement,
  getNewScoreVariableName,
  getScoreVariable,
  isScorableElement,
  isScoredElement,
} from "@forma/types/surveys/scoring";
import type { TSurvey } from "@forma/types/surveys/types";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { Label } from "../../../components/ui/label";
import { Switch } from "../../../components/ui/switch";
import { cn } from "../../../lib/cn";
import type { TElementUpdate, TUpdateElement } from "../types/editor";

/** The survey with a total score variable added, when it has none yet. */
export const withScoreVariable = (survey: TSurvey): TSurvey =>
  getScoreVariable(survey.variables)
    ? survey
    : {
        ...survey,
        variables: [
          ...survey.variables,
          {
            id: createId(),
            name: getNewScoreVariableName(survey.variables),
            type: "number",
            value: 0,
            isScore: true,
          },
        ],
      };

/** The update that turns scoring of an element on (every choice without a score gets 0) or off (every score removed). */
export const choiceScoringUpdate = (element: TScorableElement, enabled: boolean): TElementUpdate => {
  if (element.type === TSurveyElementTypeEnum.PictureSelection) {
    return {
      choices: element.choices.map(({ score, ...choice }) =>
        enabled ? { ...choice, score: score ?? 0 } : choice
      ),
    };
  }
  return {
    choices: element.choices.map(({ score, ...choice }) =>
      enabled ? { ...choice, score: score ?? 0 } : choice
    ),
  };
};

/** A score typed into a choice's score field: the number, or null for an empty field or anything that is not a score within the bound, which the field does not save. */
export const parseChoiceScore = (raw: string): number | null => {
  if (raw.trim() === "") return null;
  const score = Number(raw);
  if (!Number.isFinite(score) || Math.abs(score) > CHOICE_SCORE_LIMIT) return null;
  return score;
};

/** The score field of one choice. It saves every valid number as it is typed; an empty or invalid field saves nothing and shows the saved score again when left, so clearing the field to type a new number never unscores the choice (and with it, maybe the element). */
export const ChoiceScoreInput = ({
  score,
  label,
  onChange,
  className,
}: {
  score: number | undefined;
  label: string;
  onChange: (score: number) => void;
  className?: string;
}) => {
  // What the author is typing, null while the field shows the saved score.
  const [text, setText] = useState<string | null>(null);
  return (
    <Input
      type="number"
      inputMode="decimal"
      step="any"
      min={-CHOICE_SCORE_LIMIT}
      max={CHOICE_SCORE_LIMIT}
      aria-label={label}
      title={label}
      placeholder="0"
      className={cn("h-10 w-20 shrink-0", className)}
      value={text ?? (score === undefined ? "" : String(score))}
      onChange={(e) => {
        setText(e.target.value);
        const parsed = parseChoiceScore(e.target.value);
        if (parsed !== null) onChange(parsed);
      }}
      onBlur={() => setText(null)}
    />
  );
};

/** The "Score answers" switch of a single choice, multiple choice or picture selection element, and where its points add up: the survey's total score variable, which turning scoring on creates when the survey has none. */
export const ChoiceScoringToggle = ({
  localSurvey,
  element,
  elementIdx,
  updateElement,
  setLocalSurvey,
}: {
  localSurvey: TSurvey;
  element: TScorableElement;
  elementIdx: number;
  updateElement: TUpdateElement;
  setLocalSurvey?: (survey: TSurvey) => void;
}) => {
  const { t } = useTranslation();
  const id = useId();
  const scored = isScoredElement(element);
  const scoreVariable = getScoreVariable(localSurvey.variables);

  const addScoreVariable = () => setLocalSurvey?.(withScoreVariable(localSurvey));

  const setScoring = (enabled: boolean) => {
    // The variables first: updateElement applies its updater over the survey this sets.
    if (enabled) addScoreVariable();
    updateElement(elementIdx, (current) =>
      isScorableElement(current) ? choiceScoringUpdate(current, enabled) : {}
    );
  };

  return (
    <div className="mt-4">
      <div className="flex items-center gap-x-2">
        <Switch id={id} checked={scored} onCheckedChange={setScoring} />
        <Label htmlFor={id} className="cursor-pointer">
          <div className="ml-2">
            <h3 className="text-sm font-semibold text-slate-700">
              {t("workspace.surveys.edit.scoring_toggle")}
            </h3>
            <p className="text-xs font-normal text-slate-500">
              {t("workspace.surveys.edit.scoring_toggle_description")}
            </p>
          </div>
        </Label>
      </div>
      {scored &&
        (scoreVariable ? (
          <p className="mt-2 text-xs text-slate-500">
            {t("workspace.surveys.edit.scoring_total_variable", { name: scoreVariable.name })}
          </p>
        ) : (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <p className="text-xs text-amber-700">{t("workspace.surveys.edit.scoring_no_total_variable")}</p>
            {setLocalSurvey && (
              <Button size="sm" variant="secondary" type="button" onClick={addScoreVariable}>
                {t("workspace.surveys.edit.scoring_add_total_variable")}
              </Button>
            )}
          </div>
        ))}
    </div>
  );
};
