/**
 * Whether the survey may move focus to `node` without pulling it out of the page around the survey.
 *
 * True when nothing has focus (the body) or focus is already inside the same survey. A survey that mounts a card while the person is typing elsewhere on the host page, such as the editor beside its live preview or a customer's own form beside an inline survey, must leave that caret alone; moving focus there swallowed their keystrokes.
 */
export const canMoveFocusInto = (node: HTMLElement): boolean => {
  const { activeElement, body, documentElement } = node.ownerDocument;
  if (!activeElement || activeElement === body || activeElement === documentElement) return true;
  const surveyRoot = node.closest("#fbjs") ?? node;
  return surveyRoot.contains(activeElement);
};
