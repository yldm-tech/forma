import { useEffect, useRef } from "react";
import { type TWorkflowSurface, WORKFLOW_CLIENT_EVENTS, trackWorkflowEventWhenReady } from "../lib/analytics";

interface WorkflowSurfaceDetails {
  workflowId?: string;
  workflowStatus?: string;
  isReadOnly?: boolean;
}

/**
 * forma2's useTrackWorkflowSurface: reports workflow_surface_viewed once per screen the user lands on. surface is null while the screen is still resolving, so a loading state is never counted as a visit. The effect is keyed on the screen's identity (the surface, plus the workflow when there is one), so a detail such as the status changing underneath it never reports again; the details travel through a ref for that reason. The capture waits for PostHog to start and is cancelled on cleanup, so an unmount or a strict-mode re-run neither loses nor duplicates the event.
 */
export const useTrackWorkflowSurface = (
  surface: TWorkflowSurface | null,
  { workflowId, workflowStatus, isReadOnly }: WorkflowSurfaceDetails = {}
): void => {
  const detailsRef = useRef({ workflowStatus, isReadOnly });

  useEffect(() => {
    detailsRef.current = { workflowStatus, isReadOnly };
  }, [workflowStatus, isReadOnly]);

  useEffect(() => {
    if (!surface) return;
    return trackWorkflowEventWhenReady(WORKFLOW_CLIENT_EVENTS.surfaceViewed, {
      surface,
      workflow_id: workflowId,
      workflow_status: detailsRef.current.workflowStatus,
      is_read_only: detailsRef.current.isReadOnly,
    });
  }, [surface, workflowId]);
};
