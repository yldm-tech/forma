"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactDOM from "react-dom";
import { SurveyContainerProps } from "@forma/types/forma-surveys";
import { IS_DEVELOPMENT_BUILD } from "@/lib/env-client";
import { executeRecaptcha, loadRecaptchaScript } from "@/modules/ui/components/survey/recaptcha";

const createContainerId = () => `forma-survey-container`;

const surveyScriptUrl = (appUrl?: string) => `${appUrl ?? ""}/js/surveys.umd.cjs`;

// Module-level flag to prevent concurrent script loads across component instances
let isLoadingScript = false;

declare global {
  interface Window {
    formaSurveys: {
      renderSurveyInline: (props: SurveyContainerProps) => void;
      renderSurveyModal: (props: SurveyContainerProps) => void;
      renderSurvey: (props: SurveyContainerProps) => void;
      onFilePick: (files: { name: string; type: string; base64: string }[]) => void;
      setNonce: (nonce: string | undefined) => void;
    };
  }
}

export const SurveyInline = (props: Omit<SurveyContainerProps, "containerId">) => {
  const containerId = useMemo(() => createContainerId(), []);
  const getRecaptchaToken = useCallback(
    () => executeRecaptcha(props.recaptchaSiteKey),
    [props.recaptchaSiteKey]
  );

  const renderInline = useCallback(
    () => window.formaSurveys.renderSurvey({ ...props, containerId, getRecaptchaToken, mode: "inline" }),
    [containerId, props, getRecaptchaToken]
  );
  const [isScriptLoaded, setIsScriptLoaded] = useState(false);
  const hasLoadedRef = useRef(false);

  // Runs during render, including the server one, so the hint reaches the document's own <head>.
  // The renderer is a 1 MB bundle whose URL appeared nowhere in the HTML: it was fetched from an
  // effect after hydration, so the browser's preload scanner could not see it and the respondent's
  // critical path was hydrate-then-fetch-then-paint, strictly serial.
  ReactDOM.preload(surveyScriptUrl(props.appUrl), { as: "script", fetchPriority: "high" });

  const loadSurveyScript: () => Promise<void> = async () =>
    new Promise((resolve, reject) => {
      // Set loading flag immediately to prevent concurrent loads
      isLoadingScript = true;

      // A real `src` rather than fetch-then-assign-textContent. The old shape read the response as
      // text and injected it inline, which threw away the preload hint, the HTTP cache entry and
      // V8's code cache — a returning respondent recompiled 1 MB of JavaScript every time. Nothing
      // was bought for it: the response is same-origin and already `public, max-age=3600`.
      const scriptElement = document.createElement("script");
      scriptElement.src = surveyScriptUrl(props.appUrl);
      scriptElement.async = true;
      if (IS_DEVELOPMENT_BUILD) {
        scriptElement.src += `?t=${String(Date.now())}`;
      }

      scriptElement.onload = () => {
        setIsScriptLoaded(true);
        hasLoadedRef.current = true;
        isLoadingScript = false;
        resolve();
      };
      scriptElement.onerror = () => {
        // Leaving the element behind would make the guard below treat a failed load as done.
        scriptElement.remove();
        isLoadingScript = false;
        reject(new Error("Failed to load the surveys package"));
      };

      document.head.appendChild(scriptElement);
    });

  useEffect(() => {
    // Prevent duplicate loads across multiple renders or component instances
    if (hasLoadedRef.current || isLoadingScript) {
      return;
    }

    const loadScript = async () => {
      if (!window.formaSurveys) {
        try {
          if (props.isSpamProtectionEnabled && props.recaptchaSiteKey) {
            await loadRecaptchaScript(props.recaptchaSiteKey);
          }
          await loadSurveyScript();
        } catch (error) {
          console.error("Failed to load the surveys package: ", error);
        }
      } else {
        renderInline();
      }
    };

    loadScript();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-time script load guarded by hasLoadedRef; depending on loadSurveyScript/renderInline would re-trigger the load
  }, [props]);

  useEffect(() => {
    if (isScriptLoaded) {
      renderInline();
    }
  }, [isScriptLoaded, renderInline]);

  return <div id={containerId} className="h-full w-full" />;
};
