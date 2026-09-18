"use client";

import { useEffect } from "react";
import { whenPostHogReady } from "@/lib/posthog/client";

interface PostHogGroupIdentifyProps {
  organizationId: string;
  organizationName: string;
  workspaceId: string;
  workspaceName: string;
}

export const PostHogGroupIdentify = ({
  organizationId,
  organizationName,
  workspaceId,
  workspaceName,
}: PostHogGroupIdentifyProps) => {
  useEffect(
    () =>
      // PostHogIdentify initialises the SDK from a sibling effect in the app layout and effect order
      // is not guaranteed, so this waits rather than assuming it is loaded. It gives up after a few
      // seconds, which is what happens on an install with no POSTHOG_KEY.
      whenPostHogReady((posthog) => {
        posthog.group("organization", organizationId, { name: organizationName });
        posthog.group("workspace", workspaceId, { name: workspaceName });
      }),
    [organizationId, organizationName, workspaceId, workspaceName]
  );

  return null;
};
