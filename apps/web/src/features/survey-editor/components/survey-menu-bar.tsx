import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { ArrowLeftIcon, SettingsIcon, TriangleAlertIcon } from "lucide-react";
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
import { Button } from "../../../components/ui/button";
import { ConfirmationModal } from "../../../components/ui/confirmation-modal";
import { Input } from "../../../components/ui/input";
import { TooltipRenderer } from "../../../components/ui/tooltip";
import { ApiError } from "../../../lib/api";
import { cn } from "../../../lib/cn";
import { errorMessage } from "../../../lib/errors";
import { instanceQuery } from "../../../lib/queries/instance";
import { isDeepEqual } from "../../../lib/survey/object";
import { FALLBACK_MISSING_TOAST_ID, checkForEmptyFallBackValue } from "../../../lib/survey/recall";
import { toast } from "../../../lib/toast";
import { analysisKeys } from "../../survey-analysis/api";
import { useCreateSegment } from "../../segments/api";
import { type SurveySaveInput, saveSurvey, surveyEditorKeys } from "../api";
import { ZEditorSurvey } from "../lib/editor-schema";
import { type PublishWarning, getPublishWarnings } from "../lib/publish-warnings";
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
import { type AutoSaveStatus, AutoSaveIndicator } from "./auto-save-indicator";

/** Autosave failures in a row after which the indicator stops retrying quietly and offers Retry. */
const AUTOSAVE_FAILURES_BEFORE_WARNING = 2;

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
  locale: string;
  setIsCautionDialogOpen: (open: boolean) => void;
  /** The zone the editor shows publish and close dates in, sent with every save. */
  schedulingTimeZone: string;
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
  locale,
  setIsCautionDialogOpen,
  schedulingTimeZone,
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
  // A publish or schedule held back for the author to confirm: what the survey still carries over from its template.
  const [pendingPublish, setPendingPublish] = useState<{
    status: "inProgress" | "paused";
    warnings: PublishWarning[];
  } | null>(null);
  const [isSurveySaving, setIsSurveySaving] = useState(false);
  const [autoSaveStatus, setAutoSaveStatus] = useState<AutoSaveStatus>({ state: "idle" });
  const [isLeaving, setIsLeaving] = useState(false);
  const [isNameInvalid, setIsNameInvalid] = useState(false);
  const nameInputRef = useRef<HTMLInputElement>(null);
  const autoSaveFailuresRef = useRef(0);
  // The draft as it stood when the server refused it as invalid; every edit replaces the editor's survey object, so autosave tries again only after one.
  const refusedDraftRef = useRef<TSurvey | null>(null);
  // When the draft was last saved, kept so a single quiet failure does not wipe "Saved at" from the bar.
  const lastSavedAtRef = useRef<Date | null>(null);
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
  // The primary button of a draft says what it does in every editor mode: the onboarding (CX) editor once labelled it "Save & Close" while it published the survey.
  const draftPrimaryLabel = isPublishScheduled
    ? t("workspace.surveys.edit.schedule_survey")
    : t("workspace.surveys.edit.publish");

  const getDraftSurveyToPersist = (draftSurvey: TSurvey, segment: TSegment | null): TSurvey => ({
    ...draftSurvey,
    closeOn: draftSurvey.publishOn ? null : draftSurvey.closeOn,
    publishOn: null,
    segment,
    status: "draft",
  });

  // Where Back, Discard and Save & Close leave for. Navigated to explicitly rather than with history.back(): the editor is often reached without an in-app history entry behind it (new tab, pasted or bookmarked URL, hard reload), where back() does nothing or leaves the app. A draft has no responses to summarise and the list never links a draft to its summary, so a draft returns to the list; any other survey to its summary.
  const exitHref =
    localSurvey.status === "draft"
      ? `${workspaceBasePath}/surveys`
      : `${workspaceBasePath}/surveys/${localSurvey.id}/summary`;

  const goBack = () => router.navigate({ href: exitHref });

  const handleBack = () => {
    if (hasUnsavedSurveyChanges(localSurvey, [survey, lastSavedSurveyRef.current])) {
      setConfirmDialogOpen(true);
    } else {
      void goBack();
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

  /** Marks the cards the issues concern, and the name field when the name is one of them, without moving the page or the focus. */
  const markIssues = (issues: readonly SurveyIssue[]) => {
    const marked = getInvalidCards(localSurveyRef.current, issues);
    if (marked.invalidIds.length > 0) {
      setInvalidElements((prev) => {
        const merged = [...(prev ?? [])];
        for (const id of marked.invalidIds) {
          if (!merged.includes(id)) merged.push(id);
        }
        return merged;
      });
    }
    // The survey name sits in this bar, outside every card: it is marked here.
    const isNameIssue = issues.some((issue) => issue.path.length === 1 && issue.path[0] === "name");
    if (isNameIssue) setIsNameInvalid(true);
    return { ...marked, isNameIssue };
  };

  /** The first issue as the editor words it. */
  const describeIssue = (issue: SurveyIssue) =>
    describeFirstIssue(localSurveyRef.current, issue, {
      surveyNameRequired: () => t("workspace.surveys.survey_name_required"),
      blockNameRequired: (blockNumber) =>
        t("workspace.surveys.edit.block_name_required_for", { blockNumber }),
      questionTextMissing: (blockNumber, questionNumber) =>
        t("workspace.surveys.edit.issue_question_text_missing", { blockNumber, questionNumber }),
      optionLabelMissing: (blockNumber, questionNumber, optionNumber) =>
        t("workspace.surveys.edit.issue_option_label_missing", { blockNumber, questionNumber, optionNumber }),
      logicRuleIncomplete: (blockNumber, ruleNumber) =>
        t("workspace.surveys.edit.issue_logic_rule_incomplete", { blockNumber, ruleNumber }),
      languageLabel: (code) => getLanguageLabel(code, locale) ?? code,
    });

  /** Shows a card on the Questions tab, switching to it first: from any other tab the card is not mounted, so scrolling alone would leave the author where they were. The scroll waits two frames, by which the tab has mounted. */
  const showCard = (scrollId: string, fallbackScrollId: string | null) => {
    setActiveId("elements");
    scrollElementCardIntoView(scrollId, "start", fallbackScrollId);
  };

  /** Marks the cards the issues concern, shows the first and reports it, as forma2 did for both its own zod check and a refused save. A translation issue sends the author to the Language tab instead, where it is fixed. */
  const reportIssues = (issues: readonly SurveyIssue[]) => {
    const { firstInvalidScrollId, fallbackScrollId, isNameIssue } = markIssues(issues);
    const [firstIssue] = issues;
    if (!firstIssue) return;
    if (isNameIssue) nameInputRef.current?.focus();
    const report = describeIssue(firstIssue);
    toast.error(report.message);
    if (report.showLanguageTab) {
      setActiveId("language");
    } else if (firstInvalidScrollId) {
      showCard(firstInvalidScrollId, fallbackScrollId);
    }
  };

  /** A question whose text recalls an answer with no fallback is reported on its card, from whichever tab the save was clicked on: the check is the one isSurveyValid makes, run first so the author is taken to the question instead of only told. The toast shares its id with the one the Questions tab raises for the same question as it mounts, so the author is told once. */
  const showEmptyFallback = (): boolean => {
    const element = checkForEmptyFallBackValue(localSurvey, selectedLanguageCode);
    if (!element) return false;
    const block = localSurvey.blocks.find((candidate) =>
      candidate.elements.some((blockElement) => blockElement.id === element.id)
    );
    toast.error(t("workspace.surveys.edit.fallback_missing"), { id: FALLBACK_MISSING_TOAST_ID });
    showCard(element.id, block?.id ?? null);
    return true;
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

  /** Records a successful draft save for the autosave indicator. */
  const markSaved = () => {
    const at = new Date();
    lastSavedAtRef.current = at;
    autoSaveFailuresRef.current = 0;
    setAutoSaveStatus({ state: "saved", at });
  };

  /** One autosave of the draft as it stands, with no checks: the interval below decides when one is due, and Retry runs one at once. */
  const autoSave = async () => {
    isAutoSavingRef.current = true;
    setAutoSaveStatus({ state: "saving" });

    const currentSurvey = localSurveyRef.current;
    try {
      const savedData = await saveSurvey(
        {
          ...currentSurvey,
          segment: currentSurvey.segment?.id === "temp" ? null : currentSurvey.segment,
          updatedAt: getExpectedSurveyVersion(),
          schedulingTimeZone,
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
      markSaved();
    } catch (error) {
      const issues = error instanceof ApiError ? issuesFromDetails(error.details) : [];
      const [firstIssue] = issues;
      if (recordSaveConflict(error)) {
        // A concurrent edit is not transient: it stops the loop, so it is reported once, here.
        toast.error(t("workspace.surveys.edit.error_saving_changes"));
        setAutoSaveStatus({ state: "idle" });
      } else if (firstIssue) {
        // A draft the server refuses to store (a blank name, say) is refused again until the author fixes it, so retrying cannot help: the fields are marked and the bar says what to fix, without a toast every ten seconds and without taking the focus or the page. The next autosave after the fix goes through.
        autoSaveFailuresRef.current = 0;
        refusedDraftRef.current = currentSurvey;
        markIssues(issues);
        setAutoSaveStatus({ state: "invalid", reason: describeIssue(firstIssue).message });
      } else {
        // A single failure is retried quietly in ten seconds; failures in a row are shown, with Retry, so work is never lost without the author knowing.
        autoSaveFailuresRef.current += 1;
        if (autoSaveFailuresRef.current >= AUTOSAVE_FAILURES_BEFORE_WARNING) {
          setAutoSaveStatus({ state: "failed", failures: autoSaveFailuresRef.current });
        } else {
          const at = lastSavedAtRef.current;
          setAutoSaveStatus(at ? { state: "saved", at } : { state: "idle" });
        }
      }
    } finally {
      isAutoSavingRef.current = false;
    }
  };
  // The interval reads the latest autoSave (and through it the latest t) without being re-created.
  const autoSaveRef = useRef(autoSave);
  useEffect(() => {
    autoSaveRef.current = autoSave;
  });

  // Interval-based auto-save for draft surveys (every 10 seconds)
  useEffect(() => {
    // Only set up interval for draft surveys
    if (localSurvey.status !== "draft" || localSurvey.publishOn !== null) return;

    const intervalId = setInterval(() => {
      // Skip if tab is not visible (no computation, no API calls for background tabs)
      if (document.hidden) return;

      // Skip if already saving, publishing, or auto-saving
      if (isAutoSavingRef.current || isSurveySavingRef.current || isSurveyPublishingRef.current) return;

      // Paused while a concurrent edit is outstanding: resolving it means overwriting somebody else's work, which is the user's call to make from the Save button, not a decision to take every ten seconds without telling them.
      if (saveConflictVersionRef.current) return;

      // A draft the server refused as invalid is not sent again until the author changes something.
      if (localSurveyRef.current === refusedDraftRef.current) return;

      // Check for changes using refs (avoids re-creating interval on every change), and skip if there are none
      if (!hasUnsavedSurveyChanges(localSurveyRef.current, [surveyRef.current, lastSavedSurveyRef.current])) {
        // Undoing the change a refused draft carried leaves nothing unsaved, so the refusal no longer applies.
        setAutoSaveStatus((status) => {
          if (status.state !== "invalid") return status;
          const at = lastSavedAtRef.current;
          return at ? { state: "saved", at } : { state: "idle" };
        });
        return;
      }

      void autoSaveRef.current();
    }, 10000);

    return () => clearInterval(intervalId);
  }, [localSurvey.publishOn, localSurvey.status]);

  const handleSurveySaveDraft = async (): Promise<boolean> => {
    setIsSurveySaving(true);

    try {
      const segment = await handleSegmentUpdate();
      clearSurveyLocalStorage();
      const updatedSurvey = await saveSurvey(
        {
          ...getDraftSurveyToPersist(localSurvey, segment),
          updatedAt: getExpectedSurveyVersion(),
          schedulingTimeZone,
        },
        true
      );
      setIsSurveySaving(false);
      setLocalSurvey(updatedSurvey);
      adoptSavedSurvey(updatedSurvey);
      markSaved();
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
    schedulingTimeZone,
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

    if (showEmptyFallback() || !isSurveyValid(localSurvey, selectedLanguageCode, t, finishedResponseCount)) {
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
    setIsLeaving(true);
    const isSurveySaved =
      localSurvey.status === "draft" ? await handleSurveySaveDraft() : await handleSurveySave();
    if (isSurveySaved) {
      // "Save & Close" renders only for a published survey, whose editor is reached from its summary; a draft gets here only through the unsaved-changes dialog. Both leave for exitHref.
      await router.navigate({ href: exitHref });
    }
    setIsLeaving(false);
  };

  /** Publishing ("inProgress") and scheduling ("paused") are full saves that change the status and leave for the survey's summary, which announces the result: ?success=true celebrates a publish and opens a link survey's share modal, ?scheduled=true confirms a schedule. A valid survey that still carries template text (the default workspace name, a placeholder) is held back once for the author to confirm; `confirmed` is that confirmation. */
  const publishAs = async (status: "inProgress" | "paused", confirmed = false) => {
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
      showEmptyFallback() ||
      !isSurveyValid(localSurvey, selectedLanguageCode, t, finishedResponseCount)
    ) {
      stop();
      return;
    }

    if (!confirmed) {
      const warnings = getPublishWarnings(localSurvey);
      if (warnings.length > 0) {
        stop();
        setPendingPublish({ status, warnings });
        return;
      }
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

  /** A publish warning as the confirmation dialog lists it. */
  const describePublishWarning = ({ kind, text, location }: PublishWarning): string => {
    let where: string;
    if (location.type === "welcomeCard") {
      where = t("workspace.surveys.edit.publish_warning_location_welcome_card");
    } else if (location.type === "ending") {
      where = t("workspace.surveys.edit.publish_warning_location_ending", {
        endingNumber: location.endingNumber,
      });
    } else {
      where = t("workspace.surveys.edit.publish_warning_location_question", {
        blockNumber: location.blockNumber,
        questionNumber: location.questionNumber,
      });
    }
    return kind === "defaultWorkspaceName"
      ? t("workspace.surveys.edit.publish_warning_default_workspace_name", { location: where, name: text })
      : t("workspace.surveys.edit.publish_warning_placeholder", { location: where, placeholder: text });
  };

  const confirmPendingPublish = () => {
    if (!pendingPublish) return;
    setPendingPublish(null);
    void publishAs(pendingPublish.status, true);
  };

  /** Keep editing from the publish warning: back to the first card it names. */
  const editPendingPublish = () => {
    const [first] = pendingPublish?.warnings ?? [];
    setPendingPublish(null);
    if (first) showCard(first.scrollId, first.fallbackScrollId);
  };

  const handleSurveyPublish = () => publishAs("inProgress");

  const handleSurveySchedule = () => publishAs("paused");

  const isDraft = localSurvey.status === "draft";

  return (
    <div className="border-b border-slate-200 bg-white px-5 py-2.5 sm:flex sm:items-center sm:justify-between sm:gap-x-4">
      <div className="flex h-full min-w-0 items-center gap-x-2 sm:flex-1">
        <Button
          size="sm"
          variant="secondary"
          className="h-full shrink-0"
          onClick={() => {
            handleBack();
          }}>
          <ArrowLeftIcon />
          {t("common.back")}
        </Button>
        <p className="hidden max-w-48 shrink-0 truncate pl-4 font-semibold whitespace-nowrap md:block">
          {workspace.name} /{" "}
        </p>
        <Input
          ref={nameInputRef}
          defaultValue={localSurvey.name}
          aria-label={t("workspace.surveys.edit.survey_name")}
          isInvalid={isNameInvalid}
          onChange={(e) => {
            if (isNameInvalid && e.target.value.trim() !== "") setIsNameInvalid(false);
            const updatedSurvey = { ...localSurvey, name: e.target.value };
            setLocalSurvey(updatedSurvey);
          }}
          className={cn(
            "h-8 w-full max-w-72 min-w-0 py-0",
            !isNameInvalid && "border-white hover:border-slate-200"
          )}
        />
      </div>

      <div className="mt-3 flex shrink-0 items-center gap-2 sm:mt-0">
        <AutoSaveIndicator
          isDraft={isDraft}
          status={autoSaveStatus}
          locale={locale}
          onRetry={() => {
            if (!isAutoSavingRef.current) void autoSave();
          }}
        />
        {instance?.storageConfigured === false && (
          <>
            <div className="hidden xl:block">
              <Alert variant="warning" size="small" role="status">
                <AlertTitle>{t("common.storage_not_configured")}</AlertTitle>
              </Alert>
            </div>
            {/* Below xl the warnings shrink to an icon, so Save and Publish keep their place in the bar. */}
            <TooltipRenderer tooltipContent={t("common.storage_not_configured")} triggerClass="xl:hidden">
              <span
                role="img"
                aria-label={t("common.storage_not_configured")}
                className="flex size-8 items-center justify-center rounded-md border border-amber-200 bg-amber-50 text-amber-700">
                <TriangleAlertIcon className="size-4" aria-hidden="true" />
              </span>
            </TooltipRenderer>
          </>
        )}
        {responseCount > 0 && (
          <>
            <div className="hidden xl:block">
              <Alert variant="warning" size="small" role="status">
                <AlertTitle>{t("workspace.surveys.edit.caution_text")}</AlertTitle>
                <AlertButton onClick={() => setIsCautionDialogOpen(true)}>
                  {t("common.learn_more")}
                </AlertButton>
              </Alert>
            </div>
            <TooltipRenderer
              tooltipContent={t("workspace.surveys.edit.caution_text")}
              triggerClass="xl:hidden">
              <Button
                variant="outline"
                size="icon"
                className="size-8 border-amber-200 bg-amber-50 text-amber-700"
                aria-label={t("workspace.surveys.edit.caution_text")}
                onClick={() => setIsCautionDialogOpen(true)}>
                <TriangleAlertIcon aria-hidden="true" />
              </Button>
            </TooltipRenderer>
          </>
        )}
        <Button
          data-save-button
          disabled={isSurveySaving}
          variant="secondary"
          size="sm"
          loading={isSurveySaving}
          onClick={() => (isDraft ? handleSurveySaveDraft() : handleSurveySave())}
          type="submit">
          {isDraft ? draftSaveLabel : t("common.save")}
        </Button>
        {!isDraft && (
          <Button
            disabled={isSurveySaving}
            className="mr-3"
            size="sm"
            loading={isSurveySaving}
            onClick={() => handleSaveAndGoBack()}>
            {t("workspace.surveys.edit.save_and_close")}
          </Button>
        )}
        {isDraft && audiencePrompt && !isLinkSurvey && (
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
        {isDraft && (!audiencePrompt || isLinkSurvey) && (
          <Button
            size="sm"
            disabled={isSurveySaving}
            loading={isSurveyPublishing}
            onClick={isPublishScheduled ? handleSurveySchedule : handleSurveyPublish}>
            {draftPrimaryLabel}
          </Button>
        )}
      </div>
      <ConfirmationModal
        title={t("workspace.surveys.edit.confirm_survey_changes")}
        open={isConfirmDialogOpen}
        // While Save runs, the dialog stays: closing it (Keep editing, Escape, the backdrop) would not stop the save, which then leaves the editor anyway.
        setOpen={(open) => {
          if (!open && isLeaving) return;
          setConfirmDialogOpen(open);
        }}
        closeOnOutsideClick={!isLeaving}
        hideCloseButton={isLeaving}
        description={t("workspace.surveys.edit.unsaved_changes_warning")}
        body={null}
        Icon={TriangleAlertIcon}
        cancelButtonText={t("workspace.surveys.edit.keep_editing")}
        secondaryButton={{
          text: t("common.discard"),
          variant: "destructive",
          disabled: isLeaving,
          onAction: () => {
            setConfirmDialogOpen(false);
            void goBack();
          },
        }}
        buttonText={isDraft ? draftSaveLabel : t("common.save")}
        buttonVariant="default"
        buttonLoading={isLeaving}
        isButtonDisabled={isLeaving}
        onConfirm={() => void handleSaveAndGoBack()}
      />
      <ConfirmationModal
        title={t("workspace.surveys.edit.publish_warning_title")}
        open={pendingPublish !== null}
        setOpen={(open) => {
          if (!open) setPendingPublish(null);
        }}
        description={t("workspace.surveys.edit.publish_warning_description")}
        body={
          <ul className="list-disc space-y-1 pl-5 text-sm text-slate-700">
            {pendingPublish?.warnings.map((warning) => (
              <li key={`${warning.scrollId}-${warning.text}`}>{describePublishWarning(warning)}</li>
            ))}
          </ul>
        }
        Icon={TriangleAlertIcon}
        cancelButtonText={t("workspace.surveys.edit.keep_editing")}
        onCancel={editPendingPublish}
        buttonText={
          pendingPublish?.status === "paused"
            ? t("workspace.surveys.edit.publish_warning_schedule_anyway")
            : t("workspace.surveys.edit.publish_warning_publish_anyway")
        }
        buttonVariant="default"
        onConfirm={confirmPendingPublish}
      />
    </div>
  );
};
