"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactDOM from "react-dom";
import { SurveyContainerProps } from "@forma/types/forma-surveys";
import { IS_DEVELOPMENT_BUILD } from "@/lib/env-client";
import { executeRecaptcha, loadRecaptchaScript } from "@/modules/ui/components/survey/recaptcha";

const createContainerId = () => `forma-survey-container`;

// `.umd.js`, not the `.umd.cjs` the embed snippets use: Cloudflare will not cache a `.cjs`, so the
// render-blocking bundle was a full origin round trip on every survey open. Both names are written
// by the build — see `duplicateSuffixes` in copy-compiled-assets.
const surveyScriptUrl = (appUrl?: string) => `${appUrl ?? ""}/js/surveys.umd.js`;

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

  const scriptUrl = surveyScriptUrl(props.appUrl);

  // Runs during render, the server one included, so the hint reaches the document's own <head>.
  ReactDOM.preload(scriptUrl, { as: "script", fetchPriority: "high" });

  /**
   * Waits for the bundle rather than fetching it.
   *
   * In production the `<script>` is rendered below, so the browser has it from the document and may
   * well have run it before this effect does. Creating the element here instead — which is what this
   * component used to do — gated the 1 MB renderer's *execution* on the whole app tree hydrating
   * first, however early its bytes arrived. The preload fixed the download; it could not fix that.
   */
  const awaitSurveyScript: () => Promise<void> = async () =>
    new Promise((resolve, reject) => {
      isLoadingScript = true;

      let settled = false;
      const settle = (ok: boolean) => {
        if (settled) return;
        settled = true;
        isLoadingScript = false;
        if (!ok) {
          reject(new Error("Failed to load the surveys package"));
          return;
        }
        setIsScriptLoaded(true);
        hasLoadedRef.current = true;
        resolve();
      };

      if (window.formaSurveys) {
        settle(true);
        return;
      }

      // React hoists the rendered script into <head> under exactly this src, so an attribute
      // selector finds it. In development nothing is rendered and this is null, which is the signal
      // to create the element instead. The src is a same-origin path, so it needs no escaping here.
      const rendered = document.querySelector<HTMLScriptElement>(`script[src="${scriptUrl}"]`);

      const element = rendered ?? createDevScript(scriptUrl);
      element.addEventListener("load", () => settle(true), { once: true });
      element.addEventListener("error", () => settle(false), { once: true });

      // Closes the race where the script finished between the check above and the listener being
      // attached: a load event that already fired will never fire again.
      if (window.formaSurveys) {
        settle(true);
        return;
      }

      if (!rendered) document.head.appendChild(element);
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
          await awaitSurveyScript();
        } catch (error) {
          console.error("Failed to load the surveys package: ", error);
        }
      } else {
        renderInline();
      }
    };

    loadScript();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-time script load guarded by hasLoadedRef; depending on awaitSurveyScript/renderInline would re-trigger the load
  }, [props]);

  useEffect(() => {
    if (isScriptLoaded) {
      renderInline();
    }
  }, [isScriptLoaded, renderInline]);

  return (
    <>
      {/* Hoisted by React into <head>, and present in the server-rendered HTML — which is the point:
          the browser can fetch and run it alongside hydration instead of after it. Skipped in
          development, where the element is created in the effect with a cache-buster so a rebuilt
          bundle is picked up without a hard refresh; a timestamp rendered on the server would not
          match the one the client computes. */}
      {!IS_DEVELOPMENT_BUILD && <script async src={scriptUrl} />}
      <div id={containerId} className="h-full w-full" />
    </>
  );
};

const createDevScript = (scriptUrl: string): HTMLScriptElement => {
  const element = document.createElement("script");
  element.async = true;
  element.src = `${scriptUrl}?t=${String(Date.now())}`;
  return element;
};
