"use client";

import forma from "@formbricks/js";
import { usePathname } from "next/navigation";
import { useEffect, useRef } from "react";

export const CHURN_SURVEY_PENDING_KEY = "churnSurveyPending";

interface FormaProviderProps {
  workspaceId: string;
  appUrl: string;
  userId?: string | null;
  userEmail?: string | null;
  userName?: string | null;
}

/**
 * Initializes the Forma SDK on the client, identifies the logged-in user, and
 * tracks client-side route changes so page-triggered surveys fire on navigation.
 */
export const FormaProvider = ({
  workspaceId,
  appUrl,
  userId,
  userEmail,
  userName,
}: Readonly<FormaProviderProps>) => {
  const pathname = usePathname();
  // Guards against a second effect run (deps changing mid-flight) reading and tracking the same
  // marker again before the first run has cleared it.
  const churnTrackInFlightRef = useRef(false);

  // Set up the SDK and identify the user.
  useEffect(() => {
    if (!workspaceId) return;

    const setupForma = async () => {
      await forma.setup({ workspaceId, appUrl });

      if (userId) {
        await forma.setUserId(userId);
        const attributes: Record<string, string> = {};
        if (userEmail) attributes.email = userEmail;
        const [firstName = "", ...rest] = (userName ?? "").trim().split(/\s+/);
        attributes.firstName = firstName;
        attributes.lastName = rest.join(" ");
        await forma.setAttributes(attributes);

        // Marker value is the userId that requested the churn survey, so a logout/login in the same
        // tab before this runs doesn't attribute the cancellation to whoever is now signed in.
        const churnSurveyPendingFor = globalThis.window?.sessionStorage.getItem(CHURN_SURVEY_PENDING_KEY);
        if (churnSurveyPendingFor === userId && !churnTrackInFlightRef.current) {
          churnTrackInFlightRef.current = true;
          try {
            // Only clear the marker once the code action is actually queued; if track() rejects,
            // leave it in place so the next setup run retries it instead of losing the event silently.
            await forma.track("subscription_cancelled");
            // Compare-and-delete: a newer cancellation may have overwritten the marker while this
            // await was pending, and that one hasn't been consumed yet — don't delete it out from
            // under it.
            if (
              globalThis.window?.sessionStorage.getItem(CHURN_SURVEY_PENDING_KEY) === churnSurveyPendingFor
            ) {
              globalThis.window?.sessionStorage.removeItem(CHURN_SURVEY_PENDING_KEY);
            }
          } finally {
            churnTrackInFlightRef.current = false;
          }
        }
      }
    };

    // Handle rejections so failed SDK calls don't become unhandled promise rejections.
    setupForma().catch((error) => {
      console.error("Forma setup failed:", error);
    });
  }, [workspaceId, appUrl, userId, userEmail, userName]);

  // Track client-side navigations for page-triggered surveys.
  useEffect(() => {
    if (!workspaceId) return;
    forma.registerRouteChange().catch((error) => {
      console.error("Forma route change failed:", error);
    });
  }, [workspaceId, pathname]);

  return null;
};
