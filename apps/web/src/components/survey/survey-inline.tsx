import type { SurveyContainerProps } from "@forma/types/forma-surveys";
import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";

// forma2's SurveyInline: the survey runtime the respondents get (packages/surveys, served by the Go binary at /js/), loaded once and rendered into a container.

/** The bundle the app loads; embeds keep the .cjs name, the build writes both. */
const SURVEYS_SCRIPT = "/js/surveys.umd.js";

interface SurveysRuntime {
  renderSurvey: (props: SurveyContainerProps) => void;
}

const runtime = (): SurveysRuntime | undefined =>
  (globalThis as unknown as { formaSurveys?: SurveysRuntime }).formaSurveys;

let loading: Promise<SurveysRuntime> | null = null;

/** Loads the survey runtime once per page; a failed load may be retried. */
const loadSurveysRuntime = (): Promise<SurveysRuntime> => {
  const loaded = runtime();
  if (loaded) return Promise.resolve(loaded);
  loading ??= new Promise<SurveysRuntime>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = SURVEYS_SCRIPT;
    script.async = true;
    script.addEventListener("load", () => {
      const ready = runtime();
      if (ready) resolve(ready);
      else reject(new Error("The survey runtime did not register itself"));
    });
    script.addEventListener("error", () => reject(new Error("Failed to load the survey runtime")));
    document.head.appendChild(script);
  }).catch((error: unknown) => {
    loading = null;
    throw error;
  });
  return loading;
};

/** Renders a survey inline with the respondent runtime, re-rendering whenever options changes; keep it referentially stable between real changes. */
export const SurveyInline = ({
  options,
}: {
  options: Omit<SurveyContainerProps, "containerId" | "mode">;
}) => {
  const { t } = useTranslation();
  const containerId = `forma-survey-${useId().replaceAll(":", "")}`;
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    loadSurveysRuntime()
      .then((surveys) => {
        if (!cancelled) surveys.renderSurvey({ ...options, containerId, mode: "inline" });
      })
      .catch(() => {
        if (!cancelled) setFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, [options, containerId]);

  if (failed) {
    return (
      <p role="alert" className="p-4 text-sm text-slate-500">
        {t("common.something_went_wrong_please_try_again")}
      </p>
    );
  }
  return <div id={containerId} className="h-full w-full" />;
};
