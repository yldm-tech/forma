import type { TSurvey } from "@forma/types/surveys/types";
import { AlertTriangleIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "../../../../components/ui/alert";
import { Button } from "../../../../components/ui/button";
import { Label } from "../../../../components/ui/label";
import { TabToggle } from "../../../../components/ui/tab-toggle";
import { toast } from "../../../../lib/toast";
import { useUpdateSurveySettings } from "./use-survey-settings";

type ScriptsMode = NonNullable<TSurvey["customHeadScriptsMode"]>;

/** forma2's CustomHtmlTab: HTML for the head of the survey's public page, added to the workspace's or replacing it. */
export const CustomHtmlTab = ({
  survey,
  workspaceCustomScripts,
  isReadOnly,
}: {
  survey: TSurvey;
  workspaceCustomScripts: string | null;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const updateSurvey = useUpdateSurveySettings(survey);
  const [saved, setSaved] = useState({
    scripts: survey.customHeadScripts ?? "",
    mode: (survey.customHeadScriptsMode ?? "add") as ScriptsMode,
  });
  const [scripts, setScripts] = useState(saved.scripts);
  const [scriptsMode, setScriptsMode] = useState<ScriptsMode>(saved.mode);
  const isDirty = scripts !== saved.scripts || scriptsMode !== saved.mode;

  const onSubmit = async () => {
    if (updateSurvey.isPending || isReadOnly) return;
    try {
      await updateSurvey.mutateAsync({
        customHeadScripts: scripts || null,
        customHeadScriptsMode: scriptsMode,
      });
      toast.success(t("workspace.surveys.share.custom_html.saved_successfully"));
      setSaved({ scripts, mode: scriptsMode });
    } catch {
      toast.error(t("common.something_went_wrong_please_try_again"));
    }
  };

  return (
    <div className="px-1">
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void onSubmit();
        }}
        className="space-y-6">
        <div className="space-y-2">
          <Label>{t("workspace.surveys.share.custom_html.script_mode")}</Label>
          <TabToggle
            id="custom-scripts-mode"
            options={[
              {
                value: "add" as ScriptsMode,
                label: t("workspace.surveys.share.custom_html.add_to_workspace"),
              },
              {
                value: "replace" as ScriptsMode,
                label: t("workspace.surveys.share.custom_html.replace_workspace"),
              },
            ]}
            defaultSelected={scriptsMode}
            onChange={setScriptsMode}
            disabled={isReadOnly}
          />
          <p className="text-sm text-slate-500">
            {scriptsMode === "add"
              ? t("workspace.surveys.share.custom_html.add_mode_description")
              : t("workspace.surveys.share.custom_html.replace_mode_description")}
          </p>
        </div>

        {workspaceCustomScripts ? (
          <div className={scriptsMode === "replace" ? "opacity-50" : ""}>
            <Label>{t("workspace.surveys.share.custom_html.workspace_scripts_label")}</Label>
            <div className="mt-2 max-h-32 overflow-auto rounded-md border border-slate-200 bg-slate-50 p-3">
              <pre className="font-mono text-xs whitespace-pre-wrap text-slate-600">
                {workspaceCustomScripts}
              </pre>
            </div>
          </div>
        ) : (
          <div className="rounded-md border border-slate-200 bg-slate-50 p-3">
            <p className="text-sm text-slate-500">
              {t("workspace.surveys.share.custom_html.no_workspace_scripts")}
            </p>
          </div>
        )}

        <div className="space-y-2">
          <Label htmlFor="custom-head-scripts">
            {t("workspace.surveys.share.custom_html.survey_scripts_label")}
          </Label>
          <p className="text-sm text-slate-500">
            {t("workspace.surveys.share.custom_html.survey_scripts_description")}
          </p>
          <textarea
            id="custom-head-scripts"
            rows={8}
            value={scripts}
            onChange={(event) => setScripts(event.target.value)}
            placeholder={t("workspace.surveys.share.custom_html.placeholder")}
            className="focus:border-ink focus:ring-ring flex w-full rounded-md border border-slate-300 bg-white px-3 py-2 font-mono text-xs text-slate-800 placeholder:text-slate-400 focus:ring-2 focus:ring-offset-2 focus:outline-hidden disabled:cursor-not-allowed disabled:opacity-50"
            disabled={isReadOnly}
          />
        </div>

        <Button type="submit" disabled={updateSurvey.isPending || isReadOnly || !isDirty}>
          {updateSurvey.isPending ? t("common.saving") : t("common.save")}
        </Button>
        <Alert variant="warning" className="flex items-start gap-2" role="status">
          <AlertTriangleIcon className="mt-0.5 size-4 shrink-0" />
          <AlertDescription>{t("workspace.surveys.share.custom_html.security_warning")}</AlertDescription>
        </Alert>
      </form>
    </div>
  );
};
