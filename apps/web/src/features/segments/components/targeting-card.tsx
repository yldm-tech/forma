import * as Collapsible from "@radix-ui/react-collapsible";
import { AlertCircle, CheckIcon, ChevronDownIcon, ChevronUpIcon, PencilIcon } from "lucide-react";
import React, { useEffect, useMemo, useState } from "react";
import { toast } from "../../../lib/toast";
import { TransLink } from "../../../components/ui/trans-link";
import { Trans, useTranslation } from "react-i18next";
import { TContactAttributeKey } from "@forma/types/contact-attribute-key";
import type { TBaseFilter, TSegment, TSegmentCreateInput, TSegmentUpdateInput } from "@forma/types/segment";
import type { TSurvey } from "@forma/types/surveys/types";
import { cn } from "../../../lib/cn";
import { errorMessage } from "../../../lib/errors";
import { Alert, AlertDescription } from "../../../components/ui/alert";
import { AlertDialog } from "../../../components/ui/alert-dialog";
import { Button } from "../../../components/ui/button";
import { LoadSegmentModal } from "./load-segment-modal";
import { SaveAsNewSegmentModal } from "./save-as-new-segment-modal";
import { SegmentTitle } from "./segment-title";
import { TargetingIndicator } from "./targeting-indicator";
import {
  useCloneSegment,
  useCreateSegment,
  useLoadSegment,
  useResetSurveySegment,
  useUpdateSegment,
} from "../api";
import { AddFilterModal } from "./add-filter-modal";
import { SegmentEditor } from "./segment-editor";
import { appHref } from "../../../lib/base-path";

interface TargetingCardProps {
  localSurvey: TSurvey;
  setLocalSurvey: React.Dispatch<React.SetStateAction<TSurvey>>;
  contactAttributeKeys: TContactAttributeKey[];
  segments: TSegment[];
  initialSegment?: TSegment;
}

export function TargetingCard({
  localSurvey,
  setLocalSurvey,
  contactAttributeKeys,
  segments,
  initialSegment,
}: TargetingCardProps) {
  const { t } = useTranslation();
  const workspaceBasePath = `/workspaces/${localSurvey.workspaceId}`;
  const cloneSegment = useCloneSegment(localSurvey.workspaceId);
  const createSegment = useCreateSegment(localSurvey.workspaceId);
  const loadSegment = useLoadSegment(localSurvey.workspaceId);
  const resetSurveySegment = useResetSurveySegment(localSurvey.workspaceId);
  const updateSegment = useUpdateSegment(localSurvey.workspaceId);
  const [open, setOpen] = useState(false);
  const [segment, setSegment] = useState<TSegment | null>(localSurvey.segment);

  const [addFilterModalOpen, setAddFilterModalOpen] = useState(false);
  const [saveAsNewSegmentModalOpen, setSaveAsNewSegmentModalOpen] = useState(false);
  const [resetAllFiltersModalOpen, setResetAllFiltersModalOpen] = useState(false);
  const [loadSegmentModalOpen, setLoadSegmentModalOpen] = useState(false);
  const [isSegmentEditorOpen, setIsSegmentEditorOpen] = useState(Boolean(localSurvey.segment?.isPrivate));
  const [segmentEditorViewOnly, setSegmentEditorViewOnly] = useState(true);

  useEffect(() => {
    setLocalSurvey((localSurveyOld) => ({
      ...localSurveyOld,
      segment,
    }));
  }, [setLocalSurvey, segment]);

  const isSegmentUsedInOtherSurveys = useMemo(
    () => (localSurvey.segment ? localSurvey.segment.surveys.length > 1 : false),
    [localSurvey.segment]
  );

  const handleCloneSegment = async () => {
    if (!segment) return;

    try {
      setSegment(await cloneSegment.mutateAsync({ segmentId: segment.id, surveyId: localSurvey.id }));
    } catch (err) {
      toast.error(errorMessage(t, err));
    }
  };

  useEffect(() => {
    if (segment && segment.filters.length > 0) {
      setOpen(true);
    }
  }, [segment, segment?.filters.length]);

  useEffect(() => {
    if (localSurvey.type === "link") {
      setOpen(false);
    }
  }, [localSurvey.type]);

  const handleAddFilterInGroup = (filter: TBaseFilter) => {
    const updatedSegment = structuredClone(segment);
    if (updatedSegment?.filters.length === 0) {
      updatedSegment.filters.push({
        ...filter,
        connector: null,
      });
    } else {
      updatedSegment?.filters.push(filter);
    }

    setSegment(updatedSegment);
  };

  const handleLoadNewSegment = (surveyId: string, segmentId: string): Promise<TSurvey> =>
    loadSegment.mutateAsync({ surveyId, segmentId });

  const handleSaveAsNewSegmentUpdate = (segmentId: string, data: TSegmentUpdateInput): Promise<TSegment> =>
    updateSegment.mutateAsync({ segmentId, data });

  const handleSaveAsNewSegmentCreate = (data: TSegmentCreateInput): Promise<TSegment> =>
    createSegment.mutateAsync(data);

  const handleSaveSegment = async (data: TSegmentUpdateInput) => {
    if (!segment) {
      toast.error(t("workspace.segments.invalid_segment"));
      return;
    }
    try {
      await updateSegment.mutateAsync({ segmentId: segment.id, data });
      toast.success(t("workspace.segments.segment_saved_successfully"));

      setIsSegmentEditorOpen(false);
      setSegmentEditorViewOnly(true);
    } catch (err) {
      toast.error(errorMessage(t, err));
    }
  };

  const handleResetAllFilters = async () => {
    try {
      return await resetSurveySegment.mutateAsync(localSurvey.id);
    } catch {
      toast.error(t("workspace.segments.error_resetting_filters"));
    }
  };

  if (localSurvey.type === "link") {
    return null; // Hide card completely
  }

  if (!segment) {
    throw new Error(t("workspace.segments.invalid_segment"));
  }

  return (
    <Collapsible.Root
      className="w-full overflow-hidden rounded-lg border border-slate-300 bg-white"
      onOpenChange={setOpen}
      open={open}>
      <Collapsible.CollapsibleTrigger
        asChild
        className="h-full w-full cursor-pointer rounded-lg hover:bg-slate-50">
        <div className="inline-flex px-4 py-4">
          <div className="flex items-center pr-5 pl-2">
            <CheckIcon
              className="size-7 rounded-full border border-green-300 bg-green-100 p-1.5 text-green-600"
              strokeWidth={3}
            />
          </div>
          <div>
            <p className="font-semibold text-slate-800">{t("workspace.segments.target_audience")}</p>
            <p className="mt-1 text-sm text-slate-500">{t("workspace.segments.pre_segment_users")}</p>
          </div>
        </div>
      </Collapsible.CollapsibleTrigger>
      <Collapsible.CollapsibleContent className="min-w-full overflow-auto">
        <hr className="text-slate-600" />

        <div className="flex flex-col gap-5 p-6">
          <TargetingIndicator segment={segment} />

          <div className="filter-scrollbar flex flex-col gap-4 overflow-auto rounded-lg border border-slate-300 bg-slate-50 p-4">
            {Boolean(segment) && (
              <LoadSegmentModal
                currentSegment={segment}
                onSegmentLoad={handleLoadNewSegment}
                open={loadSegmentModalOpen}
                segments={segments}
                setIsSegmentEditorOpen={setIsSegmentEditorOpen}
                setOpen={setLoadSegmentModalOpen}
                setSegment={setSegment}
                surveyId={localSurvey.id}
              />
            )}

            {isSegmentEditorOpen ? (
              <div className="flex w-full flex-col gap-2">
                <SegmentTitle
                  description={localSurvey.segment?.description}
                  isPrivate={segment?.isPrivate}
                  title={localSurvey.segment?.title}
                />
                {Boolean(segment?.filters.length) && (
                  <div className="w-full">
                    <SegmentEditor
                      contactAttributeKeys={contactAttributeKeys}
                      group={segment.filters}
                      key={segment.filters.length}
                      segment={segment}
                      segments={segments}
                      setSegment={setSegment}
                    />
                  </div>
                )}

                <div
                  className={cn(
                    "mt-3 flex items-center gap-2",
                    segment?.isPrivate && !segment.filters.length && "mt-0"
                  )}>
                  <Button
                    onClick={() => {
                      setAddFilterModalOpen(true);
                    }}
                    size="sm"
                    variant="secondary">
                    {t("common.add_filter")}
                  </Button>

                  {isSegmentEditorOpen && !segment?.isPrivate ? (
                    <Button
                      onClick={() => {
                        void handleSaveSegment({ filters: segment?.filters ?? [] });
                      }}
                      size="sm"
                      variant="secondary">
                      {t("common.save_changes")}
                    </Button>
                  ) : null}

                  {isSegmentEditorOpen && !segment?.isPrivate ? (
                    <Button
                      className="flex items-center gap-2"
                      onClick={() => {
                        setIsSegmentEditorOpen(false);
                        setSegmentEditorViewOnly(true);

                        if (initialSegment) {
                          setSegment(initialSegment);
                        }
                      }}
                      size="sm"
                      variant="ghost">
                      {t("common.cancel")}
                    </Button>
                  ) : null}
                </div>

                <AddFilterModal
                  contactAttributeKeys={contactAttributeKeys}
                  onAddFilter={(filter) => {
                    handleAddFilterInGroup(filter);
                  }}
                  open={addFilterModalOpen}
                  segments={segments}
                  setOpen={setAddFilterModalOpen}
                />
                {Boolean(segment) && (
                  <SaveAsNewSegmentModal
                    localSurvey={localSurvey}
                    onCreateSegment={handleSaveAsNewSegmentCreate}
                    onUpdateSegment={handleSaveAsNewSegmentUpdate}
                    open={saveAsNewSegmentModalOpen}
                    segment={segment}
                    setIsSegmentEditorOpen={setIsSegmentEditorOpen}
                    setOpen={setSaveAsNewSegmentModalOpen}
                    setSegment={setSegment}
                  />
                )}
              </div>
            ) : (
              <div className="flex flex-col gap-2 rounded-lg">
                <SegmentTitle
                  description={localSurvey.segment?.description}
                  isPrivate={segment?.isPrivate}
                  title={localSurvey.segment?.title}
                />

                {segmentEditorViewOnly && segment ? (
                  <div className="opacity-60">
                    <SegmentEditor
                      contactAttributeKeys={contactAttributeKeys}
                      group={segment.filters}
                      key={segment.filters.length}
                      segment={segment}
                      segments={segments}
                      setSegment={setSegment}
                      viewOnly={segmentEditorViewOnly}
                    />
                  </div>
                ) : null}

                <div className="mt-3 flex items-center gap-3">
                  <Button
                    onClick={() => {
                      setSegmentEditorViewOnly(!segmentEditorViewOnly);
                    }}
                    size="sm"
                    variant="secondary">
                    {segmentEditorViewOnly
                      ? t("workspace.segments.hide_filters")
                      : t("workspace.segments.view_filters")}
                    {segmentEditorViewOnly ? (
                      <ChevronUpIcon className="ml-2 size-3" />
                    ) : (
                      <ChevronDownIcon className="ml-2 size-3" />
                    )}
                  </Button>

                  {isSegmentUsedInOtherSurveys ? (
                    <Button onClick={() => handleCloneSegment()} size="sm" variant="secondary">
                      {t("workspace.segments.clone_and_edit_segment")}
                    </Button>
                  ) : null}
                  {!isSegmentUsedInOtherSurveys && (
                    <Button
                      onClick={() => {
                        setIsSegmentEditorOpen(true);
                        setSegmentEditorViewOnly(false);
                      }}
                      size="sm"
                      variant={isSegmentUsedInOtherSurveys ? "ghost" : "secondary"}>
                      {t("workspace.segments.edit_segment")}
                      <PencilIcon className="ml-2 size-3" />
                    </Button>
                  )}
                </div>
                {isSegmentUsedInOtherSurveys ? (
                  <p className="mt-1 flex items-center text-xs text-slate-500">
                    <AlertCircle className="mr-1 inline size-3" />
                    <Trans
                      i18nKey="workspace.segments.segment_used_in_other_surveys_make_changes_here"
                      components={{
                        segmentsLink: (
                          <TransLink
                            className="ml-1 underline"
                            href={appHref(`${workspaceBasePath}/segments`)}
                            target="_blank"
                            rel="noopener noreferrer"
                          />
                        ),
                      }}
                    />
                  </p>
                ) : null}
              </div>
            )}
          </div>

          <div className="flex gap-3">
            <Button
              onClick={() => {
                setLoadSegmentModalOpen(true);
              }}
              size="sm"
              variant="secondary">
              {t("workspace.segments.load_segment")}
            </Button>

            {!segment?.isPrivate && Boolean(segment?.filters.length) && (
              <Button
                onClick={() => {
                  setResetAllFiltersModalOpen(true);
                }}
                size="sm"
                variant="secondary">
                {t("workspace.segments.reset_all_filters")}
              </Button>
            )}

            {isSegmentEditorOpen && Boolean(segment?.filters.length) ? (
              <Button
                className="flex items-center gap-2"
                onClick={() => {
                  setSaveAsNewSegmentModalOpen(true);
                }}
                size="sm"
                variant="secondary">
                {t("workspace.segments.save_as_new_segment")}
              </Button>
            ) : null}

            <AlertDialog
              confirmBtnLabel={t("workspace.segments.remove_all_filters")}
              declineBtnLabel={t("common.cancel")}
              headerText={t("common.are_you_sure")}
              mainText={t("workspace.segments.this_action_resets_all_filters_in_this_survey")}
              onConfirm={async () => {
                const segment = await handleResetAllFilters();
                if (segment) {
                  toast.success(t("workspace.segments.filters_reset_successfully"));

                  setSegment(segment);
                  setResetAllFiltersModalOpen(false);
                }
              }}
              onDecline={() => {
                setResetAllFiltersModalOpen(false);
              }}
              open={resetAllFiltersModalOpen}
              setOpen={setResetAllFiltersModalOpen}
            />
          </div>
        </div>

        <div>
          <Alert className="flex items-center rounded-none bg-slate-50" role="status">
            <AlertDescription className="ml-2">
              <span className="mr-1 text-slate-600">
                <Trans
                  i18nKey="workspace.segments.user_targeting_only_available_when_identifying_users"
                  components={{
                    docsLink: (
                      <TransLink
                        href="https://forma.yldm.ai/docs/app-surveys/user-identification"
                        target="_blank"
                        rel="noreferrer"
                        className="underline"
                      />
                    ),
                  }}
                />
              </span>
            </AlertDescription>
          </Alert>
        </div>
      </Collapsible.CollapsibleContent>
    </Collapsible.Root>
  );
}
