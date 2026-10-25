import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowLeftIcon, SearchIcon } from "lucide-react";
import { Suspense, lazy, useRef, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../../components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../components/ui/dialog";
import { LoadingBlock } from "../../../components/ui/page";
import { CreateWithAICard } from "../ai-create/create-with-ai-card";
import { CUSTOM_TEMPLATE_ID, type SurveyTemplate, surveyTemplatesQuery } from "../api";
import { TemplateList } from "./template-list";
import { useCreateSurveyFromTemplate } from "./use-create-survey";

// The preview pulls in the editor's preview frame and the respondent runtime, so it loads only once a template is previewed.
const TemplatePreview = lazy(() =>
  import("./template-preview").then((m) => ({ default: m.TemplatePreview }))
);

/** Tailwind's lg breakpoint: from here the preview sits beside the gallery; below it, a template is previewed in a dialog. */
const WIDE_SCREEN_QUERY = "(min-width: 1024px)";

const wideScreenQuery = () =>
  typeof window.matchMedia === "function" ? window.matchMedia(WIDE_SCREEN_QUERY) : null;

/** Whether the preview fits beside the gallery, following resizes. Only one preview is ever mounted: the respondent runtime renders into a container with a fixed id. */
const useIsWideScreen = (): boolean =>
  useSyncExternalStore(
    (onChange) => {
      const query = wideScreenQuery();
      query?.addEventListener("change", onChange);
      return () => query?.removeEventListener("change", onChange);
    },
    () => wideScreenQuery()?.matches ?? false,
    () => false
  );

/** The label of the button that creates a survey from the template: "start from scratch" creates a survey, every other card uses its template. */
const createLabelKey = (template: SurveyTemplate) =>
  template.id === CUSTOM_TEMPLATE_ID
    ? "common.create_survey"
    : "workspace.surveys.templates.use_this_template";

/**
 * forma2's TemplateContainerWithPreview: the template gallery with its search, as the templates page (under a menu bar with a back button) or as the survey list of a workspace without any survey yet, which also offers a draft written by AI. The chosen template is previewed as respondents will see it: beside the gallery on a wide screen, in a dialog from the card's Preview button on a narrower one. Both offer to create the survey from it.
 */
export const TemplateGallery = ({
  workspaceId,
  organizationId,
  surveyType,
  defaultLanguage,
  isTemplatePage,
}: {
  workspaceId: string;
  organizationId?: string;
  surveyType: "link" | "app";
  defaultLanguage: string;
  isTemplatePage: boolean;
}) => {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  // Clearing the search from the no-match message removes that message's button, so the focus goes back to the search box.
  const searchInputRef = useRef<HTMLInputElement>(null);
  const clearSearch = () => {
    setSearch("");
    searchInputRef.current?.focus();
  };
  const isWideScreen = useIsWideScreen();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [isPreviewDialogOpen, setIsPreviewDialogOpen] = useState(false);
  // Widening the window hands the preview to the pane, which closes the dialog for good: narrowing it again must not reopen a dialog nobody asked for. Adjusted while rendering, as React recommends for state that follows a prop.
  const [wasWideScreen, setWasWideScreen] = useState(isWideScreen);
  if (wasWideScreen !== isWideScreen) {
    setWasWideScreen(isWideScreen);
    if (isWideScreen) setIsPreviewDialogOpen(false);
  }
  // The same query the list reads, so the chosen template follows the catalog into another dashboard language.
  const catalog = useQuery(surveyTemplatesQuery(workspaceId, i18n.language));
  const activeTemplate = catalog.data?.templates.find((template) => template.id === activeId) ?? null;
  const { create, isPending } = useCreateSurveyFromTemplate();
  const createFrom = (template: SurveyTemplate) =>
    void create({
      workspaceId,
      templateId: template.id,
      source: template.id === CUSTOM_TEMPLATE_ID ? "custom" : "catalog",
      surveyType,
      defaultLanguage,
    });

  return (
    <div className="flex h-full flex-col">
      {isTemplatePage && (
        <div className="border-b border-slate-200 bg-white px-5 py-2.5">
          <Button variant="secondary" size="sm" onClick={() => void navigate({ href: "/" })}>
            <ArrowLeftIcon />
            {t("common.back")}
          </Button>
        </div>
      )}
      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto bg-slate-50">
          <div className="mt-6 mb-3 ml-6 flex flex-col items-center justify-between md:flex-row md:items-end">
            <h1 className="text-2xl font-bold text-slate-800">
              {isTemplatePage
                ? t("workspace.surveys.templates.create_a_new_survey")
                : t("workspace.surveys.all_set_time_to_create_first_survey")}
            </h1>
            <label className="focus-within:ring-ring mx-6 flex h-8 items-center gap-2 rounded-lg border border-slate-700 bg-white px-4 text-slate-800 focus-within:ring-2 focus-within:ring-offset-1">
              <SearchIcon className="size-4" aria-hidden="true" />
              <span className="sr-only">{t("common.search")}</span>
              <input
                ref={searchInputRef}
                type="search"
                className="w-full border-none bg-transparent text-sm focus:outline-hidden"
                placeholder={t("common.search")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </label>
          </div>
          <TemplateList
            workspaceId={workspaceId}
            search={search}
            onClearSearch={clearSearch}
            activeId={activeId}
            onSelect={setActiveId}
            onCreate={createFrom}
            isCreating={isPending}
            hasPreviewPane={isWideScreen}
            onPreview={isWideScreen ? undefined : () => setIsPreviewDialogOpen(true)}
            aiCard={
              isTemplatePage ? undefined : (
                <CreateWithAICard
                  workspaceId={workspaceId}
                  organizationId={organizationId}
                  surveyType={surveyType}
                />
              )
            }
          />
        </div>
        {isWideScreen && (
          <aside
            aria-label={t("workspace.surveys.templates.template_preview")}
            className="flex w-2/5 shrink-0 flex-col overflow-hidden border-l border-slate-200 bg-slate-100">
            {activeTemplate ? (
              <>
                <div className="flex items-start justify-between gap-4 border-b border-slate-200 bg-white px-5 py-3">
                  <div className="min-w-0">
                    <h2 className="truncate text-sm font-semibold text-slate-800">{activeTemplate.name}</h2>
                    <p className="line-clamp-2 text-xs text-slate-500">{activeTemplate.description}</p>
                  </div>
                  <Button
                    size="sm"
                    className="shrink-0"
                    loading={isPending}
                    onClick={() => createFrom(activeTemplate)}>
                    {t(createLabelKey(activeTemplate))}
                  </Button>
                </div>
                <div className="relative min-h-0 flex-1">
                  <Suspense fallback={<PreviewFallback />}>
                    <TemplatePreview
                      workspaceId={workspaceId}
                      templateId={activeTemplate.id}
                      defaultLanguage={defaultLanguage}
                      surveyType={surveyType}
                    />
                  </Suspense>
                </div>
              </>
            ) : (
              <p className="m-auto max-w-xs px-6 text-center text-sm text-slate-500">
                {t("workspace.surveys.templates.select_a_template_to_preview")}
              </p>
            )}
          </aside>
        )}
      </div>
      {!isWideScreen && activeTemplate && (
        <Dialog open={isPreviewDialogOpen} onOpenChange={setIsPreviewDialogOpen} width="wide">
          <DialogHeader>
            <DialogTitle>{activeTemplate.name}</DialogTitle>
            <DialogDescription>{activeTemplate.description}</DialogDescription>
          </DialogHeader>
          <DialogBody
            aria-label={t("workspace.surveys.templates.template_preview")}
            className="relative h-[65dvh] flex-none overflow-hidden">
            <Suspense fallback={<PreviewFallback />}>
              <TemplatePreview
                workspaceId={workspaceId}
                templateId={activeTemplate.id}
                defaultLanguage={defaultLanguage}
                surveyType={surveyType}
              />
            </Suspense>
          </DialogBody>
          <DialogFooter>
            <Button loading={isPending} onClick={() => createFrom(activeTemplate)}>
              {t(createLabelKey(activeTemplate))}
            </Button>
          </DialogFooter>
        </Dialog>
      )}
    </div>
  );
};

const PreviewFallback = () => (
  <div className="flex h-full items-center justify-center">
    <LoadingBlock />
  </div>
);
