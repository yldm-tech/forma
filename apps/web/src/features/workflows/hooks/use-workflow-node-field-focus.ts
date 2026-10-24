import { useAtomValue, useSetAtom } from "jotai";
import { useEffect, useRef } from "react";
import { clearWorkflowNodeFieldFocusAtom, workflowNodeFieldFocusRequestAtom } from "../state/editor";

interface UseWorkflowNodeFieldFocusArgs {
  /** Only requests aimed at this node are consumed. */
  nodeId: string;
  /** The element to focus for the requested field; nothing is focused for null. */
  resolveElement: (field: string) => HTMLElement | null | undefined;
  /** Runs before the focus: the forms reveal the field errors the jump is meant to explain. */
  onRequest?: (field: string) => void;
}

/** forma2's useWorkflowNodeFieldFocus: consumes a pending jump for one node, focusing the field the problems dialog pointed at and scrolling it into view. The focus runs a frame late, once the inspector has laid out, and the request is cleared inside that frame, since clearing re-runs the effect and would cancel the frame first. */
export const useWorkflowNodeFieldFocus = ({
  nodeId,
  resolveElement,
  onRequest,
}: UseWorkflowNodeFieldFocusArgs): void => {
  const focusRequest = useAtomValue(workflowNodeFieldFocusRequestAtom);
  const clearFocusRequest = useSetAtom(clearWorkflowNodeFieldFocusAtom);

  const callbacksRef = useRef({ resolveElement, onRequest });
  // Refreshed after every render, before the focus effect below reads it.
  useEffect(() => {
    callbacksRef.current = { resolveElement, onRequest };
  });

  useEffect(() => {
    if (focusRequest?.nodeId !== nodeId) return;
    const { field } = focusRequest;
    callbacksRef.current.onRequest?.(field);
    const frame = requestAnimationFrame(() => {
      const target = callbacksRef.current.resolveElement(field);
      target?.focus();
      target?.scrollIntoView({ block: "nearest" });
      clearFocusRequest();
    });
    return () => cancelAnimationFrame(frame);
  }, [focusRequest, nodeId, clearFocusRequest]);
};
