import type { TI18nString } from "@forma/types/i18n";
import type { TSurvey } from "@forma/types/surveys/types";
import { getTextContent } from "@forma/types/surveys/validation";

/** The name every new organization's first workspace is given (the server's workspace.DefaultName). A template interpolates the workspace name into its questions when the survey is created, so a survey made before the workspace was renamed asks respondents about "My workspace". */
export const DEFAULT_WORKSPACE_NAME = "My workspace";

/**
 * The template placeholders the author is meant to replace: the English "[ADD GOAL]", "[ADD FEATURE]" and "[PROBLEM AREA]" with their translations in the template catalogue (apps/server/internal/survey/catalog/locales), and a "$[workspaceName]" the server left unresolved (it fills in only the first one of each default headline and subheader).
 */
const PLACEHOLDER =
  /\$\[workspaceName\]|\[(?:ADD [^\]\n]+|PROBLEM AREA|目標を追加|添加目标|新增目標|新增功能|問題區域)\]/g;

export type PublishWarningLocation =
  | { type: "element"; blockNumber: number; questionNumber: number }
  | { type: "welcomeCard" }
  | { type: "ending"; endingNumber: number };

export interface PublishWarning {
  kind: "defaultWorkspaceName" | "placeholder";
  /** The text found: the default workspace name or the placeholder as written. */
  text: string;
  location: PublishWarningLocation;
  /** The DOM id of the card to scroll to. */
  scrollId: string;
  /** The block to scroll to when the card is not rendered (its block is collapsed). */
  fallbackScrollId: string | null;
}

type Card = Omit<PublishWarning, "kind" | "text">;

/** The texts respondents read on a card, in every language the survey carries, as plain text. */
const cardTexts = (...fields: (TI18nString | undefined)[]): string[] =>
  fields.flatMap((field) => Object.values(field ?? {}).map((value) => getTextContent(value)));

/**
 * What a survey about to go live still carries over from its template: the default workspace name or a placeholder in a headline or subheader. One warning per card and text, in the order respondents meet the cards. These are warnings, not errors: the author may publish anyway.
 */
export const getPublishWarnings = (survey: TSurvey): PublishWarning[] => {
  const warnings: PublishWarning[] = [];
  const scan = (card: Card, texts: string[]) => {
    const found = new Map<string, PublishWarning["kind"]>();
    for (const text of texts) {
      if (text.includes(DEFAULT_WORKSPACE_NAME)) found.set(DEFAULT_WORKSPACE_NAME, "defaultWorkspaceName");
      for (const [placeholder] of text.matchAll(PLACEHOLDER)) {
        if (!found.has(placeholder)) found.set(placeholder, "placeholder");
      }
    }
    for (const [text, kind] of found) warnings.push({ kind, text, ...card });
  };

  if (survey.welcomeCard.enabled) {
    scan(
      { location: { type: "welcomeCard" }, scrollId: "start", fallbackScrollId: null },
      cardTexts(survey.welcomeCard.headline, survey.welcomeCard.subheader)
    );
  }
  survey.blocks.forEach((block, blockIndex) => {
    block.elements.forEach((element, elementIndex) => {
      scan(
        {
          location: { type: "element", blockNumber: blockIndex + 1, questionNumber: elementIndex + 1 },
          scrollId: element.id,
          fallbackScrollId: block.id,
        },
        cardTexts(element.headline, element.subheader)
      );
    });
  });
  survey.endings.forEach((ending, endingIndex) => {
    if (ending.type !== "endScreen") return;
    scan(
      {
        location: { type: "ending", endingNumber: endingIndex + 1 },
        scrollId: ending.id,
        fallbackScrollId: null,
      },
      cardTexts(ending.headline, ending.subheader)
    );
  });
  return warnings;
};
