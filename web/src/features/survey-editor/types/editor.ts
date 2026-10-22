import type { TSurveyBlock } from "@forma/types/surveys/blocks";
import type { TSurveyElement } from "@forma/types/surveys/elements";

/** What an element form changes through updateElement: the element's own fields, or block-level ones (logic, fallback, button labels) that the editor writes to the element's block. */
export type TElementUpdate = Partial<TSurveyElement> &
  Partial<Pick<TSurveyBlock, "logic" | "logicFallback" | "buttonLabel" | "backButtonLabel">>;
