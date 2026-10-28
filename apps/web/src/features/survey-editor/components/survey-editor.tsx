import { useQueryClient } from "@tanstack/react-query";
import {
  type Dispatch,
  type SetStateAction,
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type { TActionClass } from "@forma/types/action-classes";
import type { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type { TSurveyQuota } from "@forma/types/quota";
import type { TSegment } from "@forma/types/segment";
import type { TSurvey, TSurveyEditorTabs, TSurveyStyling } from "@forma/types/surveys/types";
import type { TUserLocale } from "@forma/types/user";
import type { TWorkspace } from "@forma/types/workspace";
import { PreviewSurvey } from "../../../components/ui/preview-survey/index";
import { workspaceKeys } from "../../../lib/queries/workspaces";
import { extractLanguageCodes, getEnabledLanguages } from "../../../lib/survey/i18n";
import { useDocumentVisibility } from "../../../lib/use-document-visibility";
import { shouldShowFollowUpsTab } from "../follow-ups/lib/deprecation";
import type { TSurveySchedulingConfig } from "../lib/scheduling-config";
import type { TFollowUpEmailToUser } from "../types/survey-follow-up";
import { EditPublicSurveyAlertDialog } from "./edit-public-survey-alert-dialog";
import { ElementsView } from "./elements-view";
import { LoadingSkeleton } from "./loading-skeleton";
import { SurveyEditorTabs } from "./survey-editor-tabs";
import { SurveyMenuBar } from "./survey-menu-bar";

// The route always opens on the Elements tab, so the other four views are code-split: their subtrees stay out of the editor's first-load JS and arrive with the first switch to that tab. Declared at module scope, so a render never mints a new component type and remounts the open tab.
const StylingView = lazy(() => import("./styling-view").then((m) => ({ default: m.StylingView })));
const SettingsView = lazy(() => import("./settings-view").then((m) => ({ default: m.SettingsView })));
const LanguageView = lazy(() =>
  import("../multi-language/components/language-view").then((m) => ({ default: m.LanguageView }))
);
const FollowUpsView = lazy(() =>
  import("../follow-ups/components/follow-ups-view").then((m) => ({ default: m.FollowUpsView }))
);

/** The skeleton for a tab whose code is still loading, shown only after 200 ms so a quick load keeps the tab switch looking instant. */
const DeferredViewFallback = () => {
  const [pastDelay, setPastDelay] = useState(false);
  useEffect(() => {
    const timer = setTimeout(() => setPastDelay(true), 200);
    return () => clearTimeout(timer);
  }, []);
  return pastDelay ? <LoadingSkeleton /> : null;
};

interface SurveyEditorProps {
  survey: TSurvey;
  workspace: TWorkspace;
  actionClasses: TActionClass[];
  contactAttributeKeys: TContactAttributeKey[];
  segments: TSegment[];
  responseCount: number;
  finishedResponseCount: number;
  /** forma2's isMember && hasReadAccess for the workspace. */
  isReadOnly: boolean;
  colors: string[];
  isSpamProtectionAllowed: boolean;
  /** The service the instance's spam protection uses (AuthOptions.spamProtectionProvider). */
  spamProtectionProvider?: "turnstile" | "recaptcha";
  isCxMode: boolean;
  surveySchedulingConfig: TSurveySchedulingConfig;
  locale: TUserLocale;
  userEmail: string;
  teamMemberDetails: TFollowUpEmailToUser[];
  quotas: TSurveyQuota[];
  /** The user is an owner or a manager of the organization, who may save the survey as a template of it. */
  canSaveAsTemplate: boolean;
}

export const SurveyEditor = ({
  survey,
  workspace,
  actionClasses,
  contactAttributeKeys,
  segments,
  responseCount,
  finishedResponseCount,
  isReadOnly,
  colors,
  isSpamProtectionAllowed,
  spamProtectionProvider,
  isCxMode,
  surveySchedulingConfig,
  locale,
  userEmail,
  teamMemberDetails,
  quotas,
  canSaveAsTemplate,
}: Readonly<SurveyEditorProps>) => {
  const queryClient = useQueryClient();
  const isFollowUpsTabVisible = shouldShowFollowUpsTab(survey.followUps.length);

  const [activeView, setActiveView] = useState<TSurveyEditorTabs>("elements");
  const [activeElementId, setActiveElementId] = useState<string | null>(null);
  // `localSurvey` must stay a structural clone of `survey`: the menu bar compares the two with `isDeepEqual` to gate the draft auto-save, the back-navigation dialog and the beforeunload prompt, and that comparison short-circuits on differing key counts.
  const [localSurvey, setLocalSurvey] = useState<TSurvey>(() => structuredClone(survey));
  const [invalidElements, setInvalidElements] = useState<string[] | null>(null);
  const [hasIncompleteTranslations, setHasIncompleteTranslations] = useState(false);
  // Set when a save or publish is blocked by a missing trigger, so the Survey Trigger card can say so (ENG-2581). The card itself stops showing the error once the survey has a trigger.
  const [hasTriggerError, setHasTriggerError] = useState(false);

  const [selectedLanguageCode, setSelectedLanguageCode] = useState<string>("default");

  // `isFollowUpsTabVisible` tracks the saved `survey`, which a save replaces. Deleting the last follow-up therefore hides the tab while `activeView` — client state — still points at it, leaving an empty main pane with no tab selected. Fall back to the elements view so the deletion flow cannot dead-end.
  useEffect(() => {
    if (!isFollowUpsTabVisible && activeView === "followUps") {
      setActiveView("elements");
    }
  }, [isFollowUpsTabVisible, activeView]);
  const surveyEditorRef = useRef(null);

  const [styling, setStyling] = useState<TSurveyStyling | null>(localSurvey.styling ?? null);
  const [localStylingChanges, setLocalStylingChanges] = useState<TSurveyStyling | null>(null);

  // Workspace settings and languages can change in another tab while this one is open; forma2 re-read them whenever the editor became visible again.
  const fetchLatestWorkspaceData = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: workspaceKeys.detail(workspace.id), exact: true });
  }, [queryClient, workspace.id]);

  const [isCautionDialogOpen, setIsCautionDialogOpen] = useState(false);

  useDocumentVisibility(fetchLatestWorkspaceData);

  // when the survey type changes, we need to reset the active element id to the first element
  useEffect(() => {
    const firstBlock = localSurvey.blocks[0];
    if (firstBlock) {
      setActiveElementId(firstBlock.elements[0]?.id ?? null);
    }
    // Resets the active element only when the survey type changes, not on every block edit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [localSurvey.type]);

  useEffect(() => {
    const enabledLanguageCodes = extractLanguageCodes(getEnabledLanguages(localSurvey.languages));
    if (!enabledLanguageCodes.includes(selectedLanguageCode)) {
      setSelectedLanguageCode("default");
    }
  }, [localSurvey.languages, selectedLanguageCode]);

  const setLocalSurveyState: Dispatch<SetStateAction<TSurvey>> = setLocalSurvey;

  return (
    <div className="flex h-full w-full flex-col">
      <SurveyMenuBar
        setLocalSurvey={setLocalSurveyState}
        localSurvey={localSurvey}
        survey={survey}
        activeId={activeView}
        setActiveId={setActiveView}
        setInvalidElements={setInvalidElements}
        setHasTriggerError={setHasTriggerError}
        workspace={workspace}
        responseCount={responseCount}
        finishedResponseCount={finishedResponseCount}
        selectedLanguageCode={selectedLanguageCode}
        locale={locale}
        setIsCautionDialogOpen={setIsCautionDialogOpen}
        schedulingTimeZone={surveySchedulingConfig.timeZone}
        canSaveAsTemplate={canSaveAsTemplate}
      />
      <div className="relative z-0 flex flex-1 overflow-hidden">
        <main
          className="relative z-0 w-full overflow-y-auto bg-slate-50 focus:outline-hidden md:w-2/3"
          ref={surveyEditorRef}>
          <SurveyEditorTabs
            activeId={activeView}
            setActiveId={setActiveView}
            isCxMode={isCxMode}
            isStylingTabVisible={!!workspace.styling.allowStyleOverwrite}
            isFollowUpsTabVisible={isFollowUpsTabVisible}
            hasLanguageErrors={hasIncompleteTranslations}
          />

          {activeView === "elements" && (
            <ElementsView
              localSurvey={localSurvey}
              setLocalSurvey={setLocalSurveyState}
              activeElementId={activeElementId}
              setActiveElementId={setActiveElementId}
              workspace={workspace}
              invalidElements={invalidElements}
              setInvalidElements={setInvalidElements}
              selectedLanguageCode={selectedLanguageCode || "default"}
              isCxMode={isCxMode}
              locale={locale}
              responseCount={responseCount}
              quotas={quotas}
            />
          )}

          <Suspense fallback={<DeferredViewFallback />}>
            {activeView === "styling" && workspace.styling.allowStyleOverwrite && (
              <StylingView
                colors={colors}
                localSurvey={localSurvey}
                setLocalSurvey={setLocalSurveyState}
                workspace={workspace}
                styling={styling ?? null}
                setStyling={setStyling}
                localStylingChanges={localStylingChanges}
                setLocalStylingChanges={setLocalStylingChanges}
                isCxMode={isCxMode}
              />
            )}

            {activeView === "language" && (
              <LanguageView
                localSurvey={localSurvey}
                setLocalSurvey={setLocalSurveyState}
                workspaceLanguages={workspace.languages}
                locale={locale}
                setHasIncompleteTranslations={setHasIncompleteTranslations}
              />
            )}

            {activeView === "settings" && (
              <SettingsView
                localSurvey={localSurvey}
                setLocalSurvey={setLocalSurveyState}
                actionClasses={actionClasses}
                contactAttributeKeys={contactAttributeKeys}
                segments={segments}
                responseCount={responseCount}
                finishedResponseCount={finishedResponseCount}
                isReadOnly={isReadOnly}
                isSpamProtectionAllowed={isSpamProtectionAllowed}
                spamProtectionProvider={spamProtectionProvider}
                quotas={quotas}
                surveySchedulingConfig={surveySchedulingConfig}
                locale={locale}
                appSetupCompleted={workspace.appSetupCompleted}
                hasTriggerError={hasTriggerError}
              />
            )}

            {activeView === "followUps" && isFollowUpsTabVisible && (
              <FollowUpsView
                localSurvey={localSurvey}
                setLocalSurvey={setLocalSurveyState}
                selectedLanguageCode={selectedLanguageCode}
                userEmail={userEmail}
                teamMemberDetails={teamMemberDetails}
                locale={locale}
              />
            )}
          </Suspense>
        </main>

        <aside className="group hidden w-1/3 shrink-0 items-center justify-center overflow-hidden border-l border-slate-200 bg-slate-100 shadow-inner md:flex md:flex-col">
          <PreviewSurvey
            survey={localSurvey}
            elementId={activeElementId}
            workspace={workspace}
            previewType={localSurvey.type === "app" ? "modal" : "fullwidth"}
            languageCode={selectedLanguageCode}
            setLanguageCode={setSelectedLanguageCode}
            locale={locale}
            isSpamProtectionAllowed={isSpamProtectionAllowed}
            spamProtectionProvider={spamProtectionProvider}
          />
        </aside>
      </div>
      <EditPublicSurveyAlertDialog open={isCautionDialogOpen} setOpen={setIsCautionDialogOpen} />
    </div>
  );
};
