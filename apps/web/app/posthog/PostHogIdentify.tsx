"use client";

import { useEffect, useRef } from "react";
import { IS_DEVELOPMENT_BUILD } from "@/lib/env-client";
import { initPostHogClient } from "@/lib/posthog/client";

interface PostHogIdentifyProps {
  posthogKey: string;
  userId: string;
  email: string;
  name: string | null;
}

export const PostHogIdentify = ({ posthogKey, userId, email, name }: PostHogIdentifyProps) => {
  const lastIdentifiedUserId = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    // The one call that pulls `posthog-js` into the browser, and it only runs where this component
    // renders — which the app layout gates on POSTHOG_KEY. No key, no download.
    void initPostHogClient(posthogKey, {
      api_host: "/ingest",
      ui_host: "https://eu.i.posthog.com",
      defaults: "2026-01-30",
      capture_exceptions: true,
      debug: IS_DEVELOPMENT_BUILD,
      session_recording: {
        blockSelector: "iframe[src*='cdn-plain']",
      },
    }).then((posthog) => {
      if (cancelled || !posthog) return;

      if (lastIdentifiedUserId.current && lastIdentifiedUserId.current !== userId) {
        posthog.reset();
      }

      posthog.identify(userId, { email, name });
      lastIdentifiedUserId.current = userId;
    });

    return () => {
      cancelled = true;
    };
  }, [posthogKey, userId, email, name]);

  return null;
};
