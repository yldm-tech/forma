import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import { useCallback, useEffect, useRef, useState } from "react";
import { executeRecaptcha, loadRecaptchaScript } from "../lib/recaptcha";

/** The container the runtime renders into; the loading overlay watches it. */
export const SURVEY_CONTAINER_ID = "forma-survey-container";

/**
 * The runtime bundle. `.umd.js`, not the `.umd.cjs` the embed snippets use: CDNs will not cache a `.cjs`, and both names are built.
 */
export const surveyScriptUrl = (appUrl?: string) => `${appUrl ?? ""}/js/surveys.umd.js`;

interface SurveysRuntime {
  renderSurvey: (props: SurveyContainerProps) => void;
}

const runtime = (): SurveysRuntime | undefined =>
  (globalThis as unknown as { formaSurveys?: SurveysRuntime }).formaSurveys;

// Shared by every instance on the page, so the bundle is awaited once.
let isLoadingScript = false;

/**
 * Waits for the runtime bundle. The server writes its script tag into the document (apps/server/internal/linksurvey), so the browser fetches and runs it alongside this bundle; only a page served without one (the Vite dev server) adds it here.
 */
const awaitSurveyScript = (scriptUrl: string): Promise<void> =>
  new Promise((resolve, reject) => {
    if (runtime()) {
      resolve();
      return;
    }
    const rendered = document.querySelector<HTMLScriptElement>(`script[src="${scriptUrl}"]`);
    const element = rendered ?? document.createElement("script");
    element.addEventListener("load", () => resolve(), { once: true });
    element.addEventListener("error", () => reject(new Error("Failed to load the surveys package")), {
      once: true,
    });
    // The script may have finished between the check above and the listeners being attached; its load event will not fire again.
    if (runtime()) {
      resolve();
      return;
    }
    if (!rendered) {
      element.async = true;
      element.src = scriptUrl;
      document.head.appendChild(element);
    }
  });

/** forma2's SurveyInline: loads reCAPTCHA when the survey uses it, waits for the runtime and renders the survey inline. */
export const SurveyInline = (props: Omit<SurveyContainerProps, "containerId">) => {
  const getRecaptchaToken = useCallback(
    () => executeRecaptcha(props.recaptchaSiteKey),
    [props.recaptchaSiteKey]
  );
  const renderInline = useCallback(
    () =>
      runtime()?.renderSurvey({
        ...props,
        containerId: SURVEY_CONTAINER_ID,
        getRecaptchaToken,
        mode: "inline",
      }),
    [props, getRecaptchaToken]
  );
  const [isScriptLoaded, setIsScriptLoaded] = useState(false);
  const hasLoadedRef = useRef(false);
  const scriptUrl = surveyScriptUrl(props.appUrl);

  useEffect(() => {
    if (hasLoadedRef.current || isLoadingScript) return;
    if (runtime()) {
      hasLoadedRef.current = true;
      setIsScriptLoaded(true);
      return;
    }
    isLoadingScript = true;
    const load = async () => {
      try {
        if (props.isSpamProtectionEnabled && props.recaptchaSiteKey) {
          await loadRecaptchaScript(props.recaptchaSiteKey);
        }
        await awaitSurveyScript(scriptUrl);
        hasLoadedRef.current = true;
        setIsScriptLoaded(true);
      } catch (error) {
        console.error("Failed to load the surveys package: ", error);
      } finally {
        isLoadingScript = false;
      }
    };
    void load();
    // oxlint-disable-next-line react/exhaustive-deps -- one load per page, guarded by hasLoadedRef; the props only matter to the render below
  }, []);

  useEffect(() => {
    if (isScriptLoaded) renderInline();
  }, [isScriptLoaded, renderInline]);

  return <div id={SURVEY_CONTAINER_ID} className="h-full w-full" />;
};
