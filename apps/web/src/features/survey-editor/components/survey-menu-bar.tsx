import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { ArrowLeftIcon, SettingsIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { getLanguageLabel } from "@forma/i18n-utils/utils";
import type { TSegment } from "@forma/types/segment";
import {
  type TSurvey,
  type TSurveyEditorTabs,
  type TSurveyStatus,
  ZSurveyEndScreenCard,
  ZSurveyRedirectUrlCard,
} from "@forma/types/surveys/types";
import type { TWorkspace as Workspace } from "@forma/types/workspace";
import { Alert, AlertButton, AlertTitle } from "../../../components/ui/alert";
import { AlertDialog } from "../../../components/ui/alert-dialog";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/input";
import { ApiError } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { instanceQuery } from "../../../lib/queries/instance";
import { isDeepEqual } from "../../../lib/survey/object";
import { toast } from "../../../lib/toast";
import { analysisKeys } from "../../survey-analysis/api";
import { useCreateSegment } from "../../segments/api";
import { type SurveySaveInput, saveSurvey, surveyEditorKeys } from "../api";
import { ZEditorSurvey } from "../lib/editor-schema";
import { parseSurveyModifiedElsewhereError } from "../lib/save-conflict";
import {
  type SurveyIssue,
  describeFirstIssue,
  getInvalidCards,
  issuesFromDetails,
} from "../lib/survey-issues";
import { hasUnsavedSurveyChanges } from "../lib/unsaved-changes";
import { scrollElementCardIntoView } from "../lib/utils";
import { isMissingRequiredTrigger, isSurveyValid } from "../lib/validation";
import { AutoSaveIndicator } from "./auto-save-indicator";

interface SurveyMenuBarProps {
  localSurvey: TSurvey;
  survey: TSurvey;
  setLocalSurvey: (survey: TSurvey) => void;
  activeId: TSurveyEditorTabs;
  setActiveId: React.Dispatch<React.SetStateAction<TSurveyEditorTabs>>;
  setInvalidElements: React.Dispatch<React.SetStateAction<string[] | null>>;
  setHasTriggerError: React.Dispatch<React.SetStateAction<boolean>>;
  workspace: Workspace;
  responseCount: number;
  finishedResponseCount: number;
  selectedLanguageCode: string;
  isCxMode: boolean;
  locale: string;
  setIsCautionDialogOpen: (open: boolean) => void;
}

export const SurveyMenuBar = ({
  localSurvey,
  survey,
  setLocalSurvey,
  activeId,
  setActiveId,
  setInvalidElements,
  setHasTriggerError,
  workspace,
  responseCount,
  finishedResponseCount,
  selectedLanguageCode,
  isCxMode,
  locale,
  setIsCautionDialogOpen,
}: Readonly<SurveyMenuBarProps>) => {
  const workspaceBasePath = `/workspaces/${workspace.id}`;
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data: instance } = useQuery(instanceQuery);
  const createSegment = useCreateSegment(localSurvey.workspaceId);
  const [audiencePrompt, setAudiencePrompt] = useState(true);
  const [isLinkSurvey, setIsLinkSurvey] = useState(true);
  const [isConfirmDialogOpen, setConfirmDialogOpen] = useState(false);
  const [isSurveyPublishing, setIsSurveyPublishing] = useState(false);
  const [isSurveySaving, setIsSurveySaving] = useState(false);
  const [lastAutoSaved, setLastAutoSaved] = useState<Date | null>(null);
  const isSuccessfullySavedRef = useRef(false);
  const isAutoSavingRef = useRef(false);
  const isSurveyPublishingRef = useRef(false);

  // Refs for interval-based auto-save (to access current values without re-creating interval)
  const localSurveyRef = useRef(localSurvey);
  const surveyRef = useRef(survey);
  const isSurveySavingRef = useRef(isSurveySaving);
  // A snapshot of what the last successful save returned. The `survey` prop is the other persisted state; the two are not interchangeable, so the dirty checks below compare against both. Cloned rather than aliased, so a later in-place edit of the editor's own survey cannot drag the snapshot along with it and hide the change.
  const lastSavedSurveyRef = useRef<TSurvey | null>(null);
  // Set once a save is refused because the survey moved on, and holds the stored version the server named. Two things hang off it: autosave stops while it is set, so a conflict cannot be resolved silently in the loser's favour, and the next explicit save sends it as the precondition, which is what lets the editor deliberately overwrite. Cleared by the save that succeeds.
  const saveConflictVersionRef = useRef<Date | null>(null);

  useEffect(() => {
    if (audiencePrompt && activeId === "settings") {
      setAudiencePrompt(false);
    }
  }, [activeId, audiencePrompt]);

  useEffect(() => {
    setIsLinkSurvey(localSurvey.type === "link");
  }, [localSurvey.type]);

  // Keep refs updated for interval-based auto-save
  useEffect(() => {
    localSurveyRef.current = localSurvey;
  }, [localSurvey]);

  useEffect(() => {
    surveyRef.current = survey;
  }, [survey]);

  useEffect(() => {
    isSurveySavingRef.current = isSurveySaving;
  }, [isSurveySaving]);

  // Reset the successfully saved flag when the survey prop updates (the saved version has replaced it)
  useEffect(() => {
    if (isSuccessfullySavedRef.current) {
      isSuccessfullySavedRef.current = false;
    }
  }, [survey]);

  useEffect(() => {
    const warningText = t("workspace.surveys.edit.unsaved_changes_warning");
    const handleWindowClose = (e: BeforeUnloadEvent) => {
      // Skip warning if we just successfully saved
      if (isSuccessfullySavedRef.current) {
        return;
      }

      if (hasUnsavedSurveyChanges(localSurvey, [survey, lastSavedSurveyRef.current])) {
        e.preventDefault();
        return (e.returnValue = warningText);
      }
    };

    window.addEventListener("beforeunload", handleWindowClose);
    return () => {
      window.removeEventListener("beforeunload", handleWindowClose);
    };
  }, [localSurvey, survey, t]);

  /**
   * The version a draft save is made conditional on: the one the server last handed this editor, or the stored version a conflict named. It deliberately does not read `localSurvey` — autosave writes its result back into the refs and not into state, so `localSurvey.updatedAt` goes stale the moment the first autosave lands and would make every later save look like somebody else's edit.
   */
  const getExpectedSurveyVersion = (): Date =>
    saveConflictVersionRef.current ?? lastSavedSurveyRef.current?.updatedAt ?? surveyRef.current.updatedAt;

  /** Records the stored version when a save was refused because the survey moved on, so the next explicit save can overwrite it. */
  const recordSaveConflict = (error: unknown): boolean => {
    const conflictVersion =
      error instanceof ApiError ? parseSurveyModifiedElsewhereError(error.message) : null;
    if (!conflictVersion) return false;
    saveConflictVersionRef.current = conflictVersion;
    return true;
  };

  /** The saved survey becomes the editor's baseline, as forma2's router.refresh() re-read it. */
  const adoptSavedSurvey = (saved: TSurvey) => {
    saveConflictVersionRef.current = null;
    lastSavedSurveyRef.current = structuredClone(saved);
    // Set before the baseline changes, so the beforeunload prompt stays quiet while it does.
    isSuccessfullySavedRef.current = true;
    queryClient.setQueryData(surveyEditorKeys.survey(saved.id), saved);
    // The analysis views read the survey and summarise against its elements; they read both again after a save.
    queryClient.removeQueries({ queryKey: analysisKeys.all(saved.id) });
  };

  const clearSurveyLocalStorage = () => {
    if (typeof localStorage !== "undefined") {
      localStorage.removeItem(`${localSurvey.id}-columnOrder`);
      localStorage.removeItem(`${localSurvey.id}-columnVisibility`);
    }
  };

  /**
   * A missing trigger used to disable Save / Save & Close / Publish outright, which left the user with a greyed-out button and no reason for it (ENG-2581). The buttons now stay clickable and the click reports the problem: the trigger-required toast, plus the Survey Trigger card marked invalid on the Settings tab, where it can be fixed. The rule itself is unchanged and still enforced server-side.
   */
  const blockOnMissingTrigger = (targetStatus: TSurveyStatus): boolean => {
    if (!isMissingRequiredTrigger(localSurvey, targetStatus)) return false;

    toast.error(t("workspace.surveys.edit.please_set_a_survey_trigger"));
    setHasTriggerError(true);
    setActiveId("settings");
    return true;
  };

  const isPublishScheduled = localSurvey.status === "draft" && localSurvey.publishOn !== null;
  const draftSaveLabel = isPublishScheduled ? t("common.save_without_scheduling") : t("common.save_as_draft");
  let draftPrimaryLabel = t("workspace.surveys.edit.publish");
  if (isPublishScheduled) {
    draftPrimaryLabel = t("workspace.surveys.edit.schedule_survey");
  } else if (isCxMode) {
    draftPrimaryLabel = t("workspace.surveys.edit.save_and_close");
  }

  const getDraftSurveyToPersist = (draftSurvey: TSurvey, segment: TSegment | null): TSurvey => ({
    ...draftSurvey,
    closeOn: draftSurvey.publishOn ? null : draftSurvey.closeOn,
    publishOn: null,
    segment,
    status: "draft",
  });

  const goBack = () => router.history.back();

  const handleBack = () => {
    if (hasUnsavedSurveyChanges(localSurvey, [survey, lastSavedSurveyRef.current])) {
      setConfirmDialogOpen(true);
    } else {
      goBack();
    }
  };

  // An app survey still on the placeholder segment the editor gives it gets a private segment of its own before it is saved. With no targeting there is nothing to create: forma2's createSegmentAction refused an empty filter tree for every segment, private ones included, and its editor then saved the survey without a segment, so the save itself gave the app survey its empty private segment. The server's save does the same here (survey saveSegment), so the placeholder is sent as no segment.
  const handleSegmentUpdate = async (): Promise<TSegment | null> => {
    if (localSurvey.segment?.id !== "temp") return localSurvey.segment;
    if (localSurvey.type !== "app" || localSurvey.segment.filters.length === 0) return null;
    return createSegment.mutateAsync({
      filters: localSurvey.segment.filters,
      isPrivate: true,
      surveyId: localSurvey.id,
      title: localSurvey.id,
    });
  };

  /** Marks the cards the issues concern, scrolls to the first and reports it, as forma2 did for both its own zod check and a refused save. */
  const reportIssues = (issues: readonly SurveyIssue[]) => {
    const { invalidIds, firstInvalidScrollId } = getInvalidCards(localSurvey, issues);
    if (invalidIds.length > 0) {
      setInvalidElements((prev) => {
        const merged = [...(prev ?? [])];
        for (const id of invalidIds) {
          if (!merged.includes(id)) merged.push(id);
        }
        return merged;
      });
    }
    if (firstInvalidScrollId) {
      scrollElementCardIntoView(firstInvalidScrollId, "start");
    }
    const [firstIssue] = issues;
    if (!firstIssue) return;
    const report = describeFirstIssue(
      firstIssue,
      (blockNumber) => t("workspace.surveys.edit.block_name_required_for", { blockNumber }),
      (code) => getLanguageLabel(code, locale) ?? code
    );
    toast.error(report.message);
    if (report.showLanguageTab) setActiveId("language");
  };

  const validateSurveyWithZod = (): boolean => {
    const result = ZEditorSurvey.safeParse(localSurvey);
    if (result.success) return true;
    reportIssues(result.error.issues);
    return false;
  };

  /** Reports a refused save: the server's document issues on their cards, a conflict as forma2's error_saving_changes, an expected error in the server's words and anything else as the fallback. */
  const reportSaveFailure = (error: unknown, fallback: string) => {
    if (recordSaveConflict(error)) {
      toast.error(t("workspace.surveys.edit.error_saving_changes"));
      return;
    }
    if (error instanceof ApiError) {
      const issues = issuesFromDetails(error.details);
      if (issues.length > 0) {
        reportIssues(issues);
        return;
      }
      if (error.status < 500) {
        toast.error(errorMessage(t, error));
        return;
      }
    }
    console.error(error);
    toast.error(fallback);
  };

  // Interval-based auto-save for draft surveys (every 10 seconds)
  useEffect(() => {
    // Only set up interval for draft surveys
    if (localSurvey.status !== "draft" || localSurvey.publishOn !== null) return;

    const intervalId = setInterval(async () => {
      // Skip if tab is not visible (no computation, no API calls for background tabs)
      if (document.hidden) return;

      // Skip if already saving, publishing, or auto-saving
      if (isAutoSavingRef.current || isSurveySavingRef.current || isSurveyPublishingRef.current) return;

      // Paused while a concurrent edit is outstanding: resolving it means overwriting somebody else's work, which is the user's call to make from the Save button, not a decision to take every ten seconds without telling them.
      if (saveConflictVersionRef.current) return;

      // Check for changes using refs (avoids re-creating interval on every change), and skip if there are none
      if (!hasUnsavedSurveyChanges(localSurveyRef.current, [surveyRef.current, lastSavedSurveyRef.current]))
        return;

      isAutoSavingRef.current = true;

      try {
        const currentSurvey = localSurveyRef.current;
        const savedData = await saveSurvey(
          {
            ...currentSurvey,
            segment: currentSurvey.segment?.id === "temp" ? null : currentSurvey.segment,
            updatedAt: getExpectedSurveyVersion(),
          },
          true
        );

        // If the segment changed on the server (e.g., private segment was deleted when switching from app to link type), update localSurvey to prevent stale segment references when publishing
        if (!isDeepEqual(localSurveyRef.current.segment, savedData.segment)) {
          setLocalSurvey({ ...localSurveyRef.current, segment: savedData.segment });
        }

        // Update surveyRef (not localSurvey state) to prevent re-renders during auto-save. This keeps the UI stable while still tracking that changes have been saved. The comparison uses refs, so this prevents unnecessary re-saves.
        surveyRef.current = { ...savedData };
        lastSavedSurveyRef.current = structuredClone(savedData);
        isSuccessfullySavedRef.current = true;
        // The cached survey is the baseline the editor reopens on within gcTime; left behind, it would bring back the version before this autosave, and the next save would conflict with, or overwrite, what was autosaved.
        queryClient.setQueryData(surveyEditorKeys.survey(savedData.id), savedData);
        setLastAutoSaved(new Date());
      } catch (error) {
        // Autosave stays quiet about a transient failure — it retries in ten seconds. A concurrent edit is not transient: it stops the loop, so it is reported once, here.
        if (recordSaveConflict(error)) {
          toast.error(t("workspace.surveys.edit.error_saving_changes"));
        }
      } finally {
        isAutoSavingRef.current = false;
      }
    }, 10000);

    return () => clearInterval(intervalId);
    // The interval reads the survey through refs; re-creating it on every edit would restart a ten-second timer before it ever fires.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [localSurvey.publishOn, localSurvey.status, setLocalSurvey]);

  const handleSurveySaveDraft = async (): Promise<boolean> => {
    setIsSurveySaving(true);

    try {
      const segment = await handleSegmentUpdate();
      clearSurveyLocalStorage();
      const updatedSurvey = await saveSurvey(
        { ...getDraftSurveyToPersist(localSurvey, segment), updatedAt: getExpectedSurveyVersion() },
        true
      );
      setIsSurveySaving(false);
      setLocalSurvey(updatedSurvey);
      adoptSavedSurvey(updatedSurvey);
      toast.success(t("workspace.surveys.edit.changes_saved"));
      return true;
    } catch (error) {
      setIsSurveySaving(false);
      reportSaveFailure(error, t("workspace.surveys.edit.error_saving_changes"));
      return false;
    }
  };

  /** The survey as a full save sends it: forma2 dropped the editor's draft markers, emptied the legacy questions and stripped unknown keys from the endings. */
  const getSurveyToPersist = (overrides: Partial<SurveySaveInput>): SurveySaveInput => ({
    ...localSurvey,
    blocks: localSurvey.blocks.map((block) => ({
      ...block,
      elements: block.elements.map(({ isDraft: _isDraft, ...element }) => element),
    })),
    questions: [],
    endings: localSurvey.endings.map((ending) =>
      ending.type === "redirectToUrl"
        ? ZSurveyRedirectUrlCard.parse(ending)
        : ZSurveyEndScreenCard.parse(ending)
    ),
    ...overrides,
  });

  const handleSurveySave = async (): Promise<boolean> => {
    // Ahead of the spinner: a click that cannot go through should report why, not appear to work.
    if (blockOnMissingTrigger(localSurvey.status)) return false;

    setIsSurveySaving(true);

    if (!validateSurveyWithZod()) {
      setIsSurveySaving(false);
      return false;
    }

    if (!isSurveyValid(localSurvey, selectedLanguageCode, t, finishedResponseCount)) {
      setIsSurveySaving(false);
      return false;
    }

    try {
      const segment = await handleSegmentUpdate();
      clearSurveyLocalStorage();
      const updatedSurvey = await saveSurvey(getSurveyToPersist({ segment }), false);
      setIsSurveySaving(false);
      setLocalSurvey(updatedSurvey);
      adoptSavedSurvey(updatedSurvey);
      toast.success(t("workspace.surveys.edit.changes_saved"));
      return true;
    } catch (error) {
      setIsSurveySaving(false);
      reportSaveFailure(error, t("workspace.surveys.edit.error_saving_changes"));
      return false;
    }
  };

  const handleSaveAndGoBack = async () => {
    const isSurveySaved =
      localSurvey.status === "draft" ? await handleSurveySaveDraft() : await handleSurveySave();
    if (isSurveySaved) {
      // Navigate explicitly rather than history.back(): the editor is often reached without an in-app history entry behind it (new tab, pasted/bookmarked URL, hard reload), and back() silently no-ops there. "Save & Close" renders only for a published survey, whose editor is reached from its summary, so it returns there; a draft gets here only through the unsaved-changes dialog, and the list never links a draft to a summary, since a draft has no responses to summarise, so a draft returns to the list.
      await router.navigate({
        href:
          localSurvey.status === "draft"
            ? `${workspaceBasePath}/surveys`
            : `${workspaceBasePath}/surveys/${localSurvey.id}/summary`,
      });
    }
  };

  /** Publishing ("inProgress") and scheduling ("paused") are full saves that change the status and leave for the survey's summary, which announces the result: ?success=true celebrates a publish and opens a link survey's share modal, ?scheduled=true confirms a schedule. */
  const publishAs = async (status: "inProgress" | "paused") => {
    // Scheduling lands on "paused", which is live enough to need a trigger.
    if (blockOnMissingTrigger(status)) return;

    isSurveyPublishingRef.current = true;
    setIsSurveyPublishing(true);
    const stop = () => {
      isSurveyPublishingRef.current = false;
      setIsSurveyPublishing(false);
    };

    if (
      !validateSurveyWithZod() ||
      !isSurveyValid(localSurvey, selectedLanguageCode, t, finishedResponseCount)
    ) {
      stop();
      return;
    }

    try {
      const segment = await handleSegmentUpdate();
      clearSurveyLocalStorage();
      const published = await saveSurvey(getSurveyToPersist({ status, segment }), false);
      saveConflictVersionRef.current = null;
      stop();
      // Set so the beforeunload prompt stays quiet during navigation
      isSuccessfullySavedRef.current = true;
      queryClient.setQueryData(surveyEditorKeys.survey(published.id), published);
      queryClient.removeQueries({ queryKey: analysisKeys.all(published.id) });
      await router.navigate({
        href: `${workspaceBasePath}/surveys/${published.id}/summary?${status === "inProgress" ? "success" : "scheduled"}=true`,
      });
    } catch (error) {
      stop();
      reportSaveFailure(error, t("workspace.surveys.edit.error_publishing_survey"));
    }
  };

  const handleSurveyPublish = () => publishAs("inProgress");

  const handleSurveySchedule = () => publishAs("paused");

  return (
    <div className="border-b border-slate-200 bg-white px-5 py-2.5 sm:flex sm:items-center sm:justify-between">
      <div className="flex h-full items-center gap-x-2 whitespace-nowrap">
        {!isCxMode && (
          <Button
            size="sm"
            variant="secondary"
            className="h-full"
            onClick={() => {
              handleBack();
            }}>
            <ArrowLeftIcon />
            {t("common.back")}
          </Button>
        )}
        <p className="hidden pl-4 font-semibold md:block">{workspace.name} / </p>
        <Input
          defaultValue={localSurvey.name}
          onChange={(e) => {
            const updatedSurvey = { ...localSurvey, name: e.target.value };
            setLocalSurvey(updatedSurvey);
          }}
          className="h-8 w-72 border-white py-0 hover:border-slate-200"
        />
      </div>

      <div className="mt-3 flex items-center gap-2 sm:mt-0 sm:ml-4">
        <AutoSaveIndicator isDraft={localSurvey.status === "draft"} lastSaved={lastAutoSaved} />
        {instance?.storageConfigured === false && (
          <div>
            <Alert variant="warning" size="small" role="status">
              <AlertTitle>{t("common.storage_not_configured")}</AlertTitle>
              <AlertButton asChild>
                <a
                  href="https://forma.yldm.ai/docs/self-hosting/configuration/file-uploads"
                  target="_blank"
                  rel="noopener noreferrer">
                  {t("common.learn_more")}
                </a>
              </AlertButton>
            </Alert>
          </div>
        )}
        {responseCount > 0 && (
          <div>
            <Alert variant="warning" size="small" role="status">
              <AlertTitle>{t("workspace.surveys.edit.caution_text")}</AlertTitle>
              <AlertButton onClick={() => setIsCautionDialogOpen(true)}>{t("common.learn_more")}</AlertButton>
            </Alert>
          </div>
        )}
        {!isCxMode && (
          <Button
            data-save-button
            disabled={isSurveySaving}
            variant="secondary"
            size="sm"
            loading={isSurveySaving}
            onClick={() => (localSurvey.status === "draft" ? handleSurveySaveDraft() : handleSurveySave())}
            type="submit">
            {localSurvey.status === "draft" ? draftSaveLabel : t("common.save")}
          </Button>
        )}
        {localSurvey.status !== "draft" && (
          <Button
            disabled={isSurveySaving}
            className="mr-3"
            size="sm"
            loading={isSurveySaving}
            onClick={() => handleSaveAndGoBack()}>
            {t("workspace.surveys.edit.save_and_close")}
          </Button>
        )}
        {localSurvey.status === "draft" && audiencePrompt && !isLinkSurvey && (
          <Button
            size="sm"
            onClick={() => {
              setAudiencePrompt(false);
              setActiveId("settings");
            }}>
            {t("workspace.surveys.edit.continue_to_settings")}
            <SettingsIcon />
          </Button>
        )}
        {/* Always display Publish button for link surveys for better CR */}
        {localSurvey.status === "draft" && (!audiencePrompt || isLinkSurvey) && (
          <Button
            size="sm"
            disabled={isSurveySaving}
            loading={isSurveyPublishing}
            onClick={isPublishScheduled ? handleSurveySchedule : handleSurveyPublish}>
            {draftPrimaryLabel}
          </Button>
        )}
      </div>
      <AlertDialog
        headerText={t("workspace.surveys.edit.confirm_survey_changes")}
        open={isConfirmDialogOpen}
        setOpen={setConfirmDialogOpen}
        mainText={t("workspace.surveys.edit.unsaved_changes_warning")}
        confirmBtnLabel={localSurvey.status === "draft" ? draftSaveLabel : t("common.save")}
        declineBtnLabel={t("common.discard")}
        declineBtnVariant="destructive"
        onDecline={() => {
          setConfirmDialogOpen(false);
          goBack();
        }}
        onConfirm={handleSaveAndGoBack}
      />
    </div>
  );
};
