import type { TSurveyBlock } from "@forma/types/surveys/blocks";
import type { TSurveyElement } from "@forma/types/surveys/elements";

/** What an element form changes through updateElement: the element's own fields, or block-level ones (logic, fallback, button labels) that the editor writes to the element's block. */
export type TElementUpdate = Partial<TSurveyElement> &
  Partial<Pick<TSurveyBlock, "logic" | "logicFallback" | "buttonLabel" | "backButtonLabel">>;

/** Computes an update from the element as it is in the editor's latest state rather than in the render that scheduled it. A field commits its text 100 ms after the last keystroke, so an update built from a render-time copy of a list (choices, rows, columns) can land after another edit of that list and undo it. It must be pure: React may call it more than once. */
export type TElementUpdater = (current: TSurveyElement) => TElementUpdate;

/** The editor's element setter: a plain update, or an updater over the element's latest state. */
export type TUpdateElement = (elementIdx: number, update: TElementUpdate | TElementUpdater) => void;
