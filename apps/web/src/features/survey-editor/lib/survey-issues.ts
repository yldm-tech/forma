import type { TSurvey } from "@forma/types/surveys/types";

/**
 * One rule a survey breaks: a zod issue from the editor's own check, or one of the issues the server returns with a 400 validation error (`details.issues`, the same path/message/params shape).
 */
export interface SurveyIssue {
  path: readonly PropertyKey[];
  message: string;
  params?: Record<string, unknown>;
}

/** The cards an issue list marks invalid, and the DOM id of the first one to scroll to. */
export interface InvalidCards {
  invalidIds: string[];
  firstInvalidScrollId: string | null;
}

/**
 * forma2's mapping from validation issues to the editor's cards: an element issue flags the element, a block-name issue the block, a logic issue the rule (or, without a rule index, every rule of the block), a welcome card issue "start" and an ending issue the ending. A logic issue scrolls to the block, whose card carries the red bar.
 */
export const getInvalidCards = (survey: TSurvey, issues: readonly SurveyIssue[]): InvalidCards => {
  const invalidIds: string[] = [];
  let firstInvalidScrollId: string | null = null;
  const flag = (id: string | undefined) => {
    if (id && !invalidIds.includes(id)) invalidIds.push(id);
  };

  for (const { path } of issues) {
    const [section, index, field, subIndex] = path;
    if (section === "blocks") {
      const block = typeof index === "number" ? survey.blocks[index] : undefined;
      if (field === "elements" && typeof subIndex === "number") {
        const element = block?.elements[subIndex];
        flag(element?.id);
        firstInvalidScrollId ??= element?.id ?? null;
      } else if (field === "name") {
        flag(block?.id);
        firstInvalidScrollId ??= block?.id ?? null;
      } else if (field === "logic" && typeof subIndex === "number") {
        flag(block?.logic?.[subIndex]?.id);
        firstInvalidScrollId ??= block?.id ?? null;
      } else if (field === "logic") {
        for (const logicItem of block?.logic ?? []) flag(logicItem.id);
        firstInvalidScrollId ??= block?.id ?? null;
      }
    } else if (section === "welcomeCard") {
      flag("start");
      firstInvalidScrollId ??= "start";
    } else if (section === "endings") {
      const endingId = typeof index === "number" ? survey.endings[index]?.id : undefined;
      flag(endingId);
      firstInvalidScrollId ??= endingId ?? null;
    }
  }

  return { invalidIds, firstInvalidScrollId };
};

/** What the editor reports for the first issue: a message, and whether it belongs to the Language tab. */
export interface IssueReport {
  message: string;
  showLanguageTab: boolean;
}

/**
 * forma2's toast for the first issue. A block-name issue names the block, since the schema cannot; a translation issue lists the languages it concerns by name, replacing the codes after the "-fLang-" marker, and sends the author to the Language tab.
 */
export const describeFirstIssue = (
  issue: SurveyIssue,
  blockNameRequired: (blockNumber: number) => string,
  languageLabel: (code: string) => string
): IssueReport => {
  const [section, index, field] = issue.path;
  if (section === "blocks" && field === "name" && typeof index === "number") {
    return { message: blockNameRequired(index + 1), showLanguageTab: false };
  }
  const codes = issue.params?.invalidLanguageCodes;
  if (Array.isArray(codes) && codes.length > 0) {
    const labels = codes.map((code) => (typeof code === "string" ? languageLabel(code) : String(code)));
    return {
      message: `${issue.message.split("-fLang-")[0] ?? ""} ${labels.join(", ")}`,
      showLanguageTab: true,
    };
  }
  return { message: issue.message, showLanguageTab: false };
};

/** The issues of a server validation error (`details.issues`), or none for any other error body. */
export const issuesFromDetails = (details: Record<string, unknown> | undefined): SurveyIssue[] => {
  const issues = details?.issues;
  if (!Array.isArray(issues)) return [];
  return issues.filter(
    (issue): issue is SurveyIssue =>
      typeof issue === "object" &&
      issue !== null &&
      Array.isArray((issue as SurveyIssue).path) &&
      typeof (issue as SurveyIssue).message === "string"
  );
};
