import type { GenerationDraftSnapshot } from "./events";

/** One question row as the preview renders it, flattened out of the block structure. */
export interface AiDraftQuestion {
  /** `block:question`, the row's position in the draft, stable for its whole life. */
  key: string;
  /** The block it belongs to, so the preview can show the structure the model wrote. */
  blockKey: string;
  blockName?: string;
  type?: string;
  headline?: string;
  choiceCount?: number;
}

export interface AiDraftState {
  name?: string;
  questions: readonly AiDraftQuestion[];
}

export const EMPTY_AI_DRAFT: AiDraftState = { questions: [] };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const text = (value: unknown): string | undefined =>
  typeof value === "string" && value.length > 0 ? value : undefined;

/**
 * forma2's mergeAiDraftSnapshot: folds a streamed snapshot into the draft the preview renders. Append-only and keyed by position, so a snapshot that arrives with fewer questions or a field blanked out loses to what is already there; and referentially stable, so a question whose fields did not change keeps its object and an unchanged draft is returned as it was, which keeps the memoised rows from re-rendering on every snapshot.
 */
export function mergeAiDraftSnapshot(
  previous: AiDraftState,
  snapshot: GenerationDraftSnapshot
): AiDraftState {
  const incoming = flattenSnapshotQuestions(snapshot);
  const byKey = new Map(previous.questions.map((question) => [question.key, question]));
  const questions: AiDraftQuestion[] = [...previous.questions];
  let changed = false;

  for (const incomingQuestion of incoming) {
    const previousQuestion = byKey.get(incomingQuestion.key);
    if (!previousQuestion) {
      questions.push(incomingQuestion);
      byKey.set(incomingQuestion.key, incomingQuestion);
      changed = true;
      continue;
    }
    const merged: AiDraftQuestion = {
      key: previousQuestion.key,
      blockKey: previousQuestion.blockKey,
      blockName: incomingQuestion.blockName ?? previousQuestion.blockName,
      type: incomingQuestion.type ?? previousQuestion.type,
      headline: incomingQuestion.headline ?? previousQuestion.headline,
      choiceCount: incomingQuestion.choiceCount ?? previousQuestion.choiceCount,
    };
    if (isSameQuestion(previousQuestion, merged)) continue;
    questions[questions.indexOf(previousQuestion)] = merged;
    byKey.set(merged.key, merged);
    changed = true;
  }

  // Ordered by position, not arrival: a question still empty is skipped and lands in a later snapshot, and appending it would render it below the questions after it.
  if (changed) {
    questions.sort((a, b) => comparePosition(a.key, b.key));
  }

  const name = text(snapshot.name) ?? previous.name;
  if (name !== previous.name) changed = true;
  return changed ? { name, questions } : previous;
}

/** `block:question` keys compared numerically: "10:0" sorts after "9:0", which strings do not. */
function comparePosition(a: string, b: string): number {
  const [aBlock = 0, aQuestion = 0] = a.split(":").map(Number);
  const [bBlock = 0, bQuestion = 0] = b.split(":").map(Number);
  return aBlock - bBlock || aQuestion - bQuestion;
}

/** The rows worth rendering: a question exists once the model has written its type or its headline. */
function flattenSnapshotQuestions(snapshot: GenerationDraftSnapshot): AiDraftQuestion[] {
  const blocks = Array.isArray(snapshot.blocks) ? (snapshot.blocks as unknown[]) : [];
  const questions: AiDraftQuestion[] = [];
  blocks.forEach((block, blockIndex) => {
    if (!isRecord(block)) return;
    const blockQuestions = Array.isArray(block.questions) ? (block.questions as unknown[]) : [];
    blockQuestions.forEach((question, questionIndex) => {
      if (!isRecord(question)) return;
      const type = typeof question.type === "string" ? question.type : undefined;
      const headline = typeof question.headline === "string" ? question.headline : undefined;
      if (!type && !headline) return;
      questions.push({
        key: `${blockIndex}:${questionIndex}`,
        blockKey: String(blockIndex),
        blockName: text(block.name),
        type,
        headline,
        choiceCount: Array.isArray(question.choices) ? question.choices.length : undefined,
      });
    });
  });
  return questions;
}

function isSameQuestion(a: AiDraftQuestion, b: AiDraftQuestion): boolean {
  return (
    a.type === b.type &&
    a.headline === b.headline &&
    a.choiceCount === b.choiceCount &&
    a.blockName === b.blockName
  );
}

/** Groups the flat rows back into the blocks the model wrote, in order. */
export function groupAiDraftByBlock(
  questions: readonly AiDraftQuestion[]
): { key: string; name?: string; questions: AiDraftQuestion[] }[] {
  const blocks: { key: string; name?: string; questions: AiDraftQuestion[] }[] = [];
  for (const question of questions) {
    const current = blocks.at(-1);
    if (current?.key === question.blockKey) {
      current.questions.push(question);
      current.name ??= question.blockName;
      continue;
    }
    blocks.push({ key: question.blockKey, name: question.blockName, questions: [question] });
  }
  return blocks;
}
