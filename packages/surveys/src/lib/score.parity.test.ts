import { readFileSync } from "node:fs";
import { describe, expect, test } from "vite-plus/test";
import {
  type TLinkedEmbeddedField,
  coerceToEmbeddedDataType,
  getComputedEmbeddedFields,
} from "@forma/types/embedded-data-resolver";
import { type TJsWorkspaceStateSurvey } from "@forma/types/js";
import type { TResponseData, TResponseDataValue, TResponseVariables } from "@forma/types/responses";
import type { TSurveyBlock } from "@forma/types/surveys/blocks";
import type { TSurveyElement } from "@forma/types/surveys/elements";
import { applyBlockScore, getElementScore, getScoreVariable } from "@forma/types/surveys/scoring";
import type { TSurveyVariable } from "@forma/types/surveys/types";
import { evaluateLogic, performActions } from "./logic";

// The server recomputes the total score of a survey without calculate logic (apps/server/internal/response/score.go) and stores its own value over the one this runtime sends. The table both sides are held to is the server's testdata, read here as it is, so a change to either side that the other does not follow fails one of the two suites.

interface TElementCase {
  name: string;
  element: TSurveyElement;
  answer: TResponseDataValue | null;
  score: number | null;
}

interface TSurveyCase {
  name: string;
  blocks: TSurveyBlock[];
  variables: TSurveyVariable[];
  embeddedFields: TLinkedEmbeddedField[];
  data: TResponseData;
  ttc: Record<string, number>;
  startAt?: string;
  path: string[];
  score: number;
}

const parity = JSON.parse(
  readFileSync(
    new URL("../../../../apps/server/internal/response/testdata/score_parity.json", import.meta.url),
    "utf8"
  )
) as { elements: TElementCase[]; surveys: TSurveyCase[] };

/**
 * The walk survey.tsx makes through a response, over its final data: the variables seeded from the computed fields (the effect that seeds `currentVariables`), the first block or the one holding `startAt` (the `blockId` initializer), and at every submitted block `applyBlockScore`, then the block's logic in order with the first jump of the first rule that holds, then the fallback, then the next block (`evaluateLogicAndGetNextBlockId`). A target that is not a block ends the response, and the walk.
 */
const walk = (parityCase: TSurveyCase): { path: string[]; variables: TResponseVariables } => {
  const survey = {
    id: "parity-survey",
    blocks: parityCase.blocks,
    variables: parityCase.variables,
    embeddedFields: parityCase.embeddedFields,
    hiddenFields: { enabled: false, fieldIds: [] },
    endings: [],
    languages: [],
  } as unknown as TJsWorkspaceStateSurvey;
  let variables = getComputedEmbeddedFields(survey).reduce<TResponseVariables>((acc, { field, link }) => {
    const seed = coerceToEmbeddedDataType(field.defaultValue, field.dataType);
    if (typeof seed === "string" || typeof seed === "number") acc[link.storageKey] = seed;
    return acc;
  }, {});
  const blocks = parityCase.blocks;
  let index = parityCase.startAt
    ? blocks.findIndex((block) => block.elements.some((element) => element.id === parityCase.startAt))
    : 0;
  const path: string[] = [];
  while (index >= 0 && index < blocks.length) {
    const block = blocks[index];
    path.push(block.id);
    variables = applyBlockScore(parityCase.variables, block.elements, parityCase.data, variables);
    let jumpTarget: string | undefined;
    for (const logic of block.logic ?? []) {
      if (!evaluateLogic(survey, parityCase.data, variables, logic.conditions, "default", {})) continue;
      const result = performActions(survey, logic.actions, parityCase.data, variables);
      variables = { ...variables, ...result.calculations };
      jumpTarget = jumpTarget || result.jumpTarget;
    }
    const nextId = jumpTarget || block.logicFallback || blocks[index + 1]?.id;
    index = blocks.findIndex((candidate) => candidate.id === nextId);
  }
  return { path, variables };
};

describe("total score parity with the server's recomputation", () => {
  test.each(parity.elements.map((parityCase) => [parityCase.name, parityCase] as const))(
    "element: %s",
    (_name, parityCase) => {
      const score = getElementScore(parityCase.element, parityCase.answer as TResponseDataValue);
      expect(score ?? null).toBe(parityCase.score);
    }
  );

  test.each(parity.surveys.map((parityCase) => [parityCase.name, parityCase] as const))(
    "survey: %s",
    (_name, parityCase) => {
      const { path, variables } = walk(parityCase);
      expect(path).toEqual(parityCase.path);
      const scoreVariable = getScoreVariable(parityCase.variables);
      // JSON is what the runtime sends, so -0 and 0 are the same total.
      expect(JSON.parse(JSON.stringify(variables[scoreVariable?.id ?? ""]))).toBe(parityCase.score);
    }
  );
});
