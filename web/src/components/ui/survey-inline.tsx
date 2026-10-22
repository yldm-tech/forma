import { useEffect, useState } from "react";
import type { SurveyContainerProps } from "@forma/types/forma-surveys";

// The survey renderer is packages/surveys, built into /js/surveys.umd.js and served by the Go server (server/internal/webui). The `.umd.js` name rather than the embed snippets' `.umd.cjs`, because a CDN caches the former.
const SURVEY_SCRIPT_URL = "/js/surveys.umd.js";
const CONTAINER_ID = "forma-survey-container";

declare global {
  interface Window {
    formaSurveys?: {
      renderSurvey: (props: SurveyContainerProps) => void;
    };
  }
}

let scriptPromise: Promise<void> | undefined;

/** Loads the renderer once per page; in development with a cache-buster, so a rebuilt bundle is picked up without a hard refresh. */
const loadSurveyScript = (): Promise<void> => {
  if (window.formaSurveys) return Promise.resolve();
  scriptPromise ??= new Promise<void>((resolve, reject) => {
    const element = document.createElement("script");
    element.async = true;
    element.src = import.meta.env.DEV ? `${SURVEY_SCRIPT_URL}?t=${String(Date.now())}` : SURVEY_SCRIPT_URL;
    element.addEventListener("load", () => resolve(), { once: true });
    element.addEventListener(
      "error",
      () => {
        scriptPromise = undefined;
        element.remove();
        reject(new Error("Failed to load the surveys package"));
      },
      { once: true }
    );
    document.head.appendChild(element);
  });
  return scriptPromise;
};

/** forma2's SurveyInline: renders a survey with the packages/surveys runtime into a container of its own. */
export const SurveyInline = (props: Omit<SurveyContainerProps, "containerId">) => {
  const [isScriptLoaded, setIsScriptLoaded] = useState(() => Boolean(window.formaSurveys));

  useEffect(() => {
    if (isScriptLoaded) return;
    let active = true;
    loadSurveyScript().then(
      () => {
        if (active) setIsScriptLoaded(true);
      },
      (error: unknown) => console.error("Failed to load the surveys package: ", error)
    );
    return () => {
      active = false;
    };
  }, [isScriptLoaded]);

  useEffect(() => {
    if (isScriptLoaded)
      window.formaSurveys?.renderSurvey({ ...props, containerId: CONTAINER_ID, mode: "inline" });
  }, [isScriptLoaded, props]);

  return <div id={CONTAINER_ID} className="h-full w-full" />;
};
