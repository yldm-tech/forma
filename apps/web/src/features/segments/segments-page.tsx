import type { TBaseFilters } from "@forma/types/segment";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { FilterIcon, PlusIcon, Trash2, UsersIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DataTable } from "../../components/ui/data-table";
import {
  Dialog,
  DialogBody,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../components/ui/dialog";
import { IdBadge } from "../../components/ui/id-badge";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { ModalWithTabs } from "../../components/ui/modal-with-tabs";
import type { AttributeKey } from "../../lib/attribute-keys";
import { errorMessage } from "../../lib/errors";
import { meQuery } from "../../lib/queries/auth";
import {
  type Segment,
  attributeKeysQuery,
  segmentContactCountsQuery,
  segmentResultSharesQuery,
  segmentsQuery,
  useCreateSegment,
  useDeleteSegment,
  useUpdateSegment,
} from "../../lib/queries/contacts";
import { appendFilter, canSaveSegment, segmentActivitySummary } from "../../lib/segment-filters";
import { formatDateForDisplay, formatDateTimeForDisplay, timeSince } from "../../lib/time";
import { toast } from "../../lib/toast";
import { ContactsPageLayout } from "../contacts/contacts-layout";
import { AddFilterModal } from "./components/add-filter-modal";
import { SegmentEditor } from "./components/segment-editor";
import { SegmentPreviewCount } from "./components/segment-preview-count";
import { formSegment, formSetSegment, toContactAttributeKey, toEditorSegment } from "./lib/editor-adapter";

const column = createColumnHelper<Segment>();

/** forma2's segments page: the workspace's public segments, each opening its activity and settings, and a way to create one. A survey's own targeting segment is private and not listed. */
export const SegmentsPage = ({ workspaceId, isReadOnly }: { workspaceId: string; isReadOnly: boolean }) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const segments = useQuery(segmentsQuery(workspaceId));
  const keys = useQuery(attributeKeysQuery(workspaceId));
  // Counting is slow on a large workspace, so the list shows without waiting for it and the column fills in.
  const counts = useQuery(segmentContactCountsQuery(workspaceId));
  const countOf = useMemo(
    () => new Map((counts.data ?? []).map((count) => [count.segmentId, count])),
    [counts.data]
  );
  const [editing, setEditing] = useState<Segment | null>(null);
  const all = segments.data ?? [];
  const listed = all.filter((segment) => !segment.isPrivate);
  const columns = [
    column.accessor("title", {
      header: t("common.title"),
      cell: (info) => (
        <div className="flex items-center gap-4">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-slate-100">
            <UsersIcon className="size-5 text-slate-600" />
          </div>
          <div className="flex flex-col items-start">
            {/* The title opens the segment, so the keyboard can reach it; the row click stays as a mouse convenience. */}
            <button
              type="button"
              className="rounded-sm text-left font-medium text-slate-900 hover:underline focus-visible:ring-2 focus-visible:ring-slate-700 focus-visible:ring-offset-2 focus-visible:outline-hidden"
              onClick={() => setEditing(info.row.original)}>
              {info.getValue()}
            </button>
            {info.row.original.description && (
              <div className="text-xs text-slate-500">{info.row.original.description}</div>
            )}
          </div>
        </div>
      ),
    }),
    column.display({
      id: "contactCount",
      header: t("common.contacts"),
      cell: (info) => {
        const count = countOf.get(info.row.original.id);
        return (
          <SegmentContactCount
            count={count?.count}
            approximate={count?.approximate ?? false}
            locale={me.locale}
          />
        );
      },
    }),
    column.accessor("updatedAt", {
      header: t("common.updated_at"),
      cell: (info) => <span className="text-sm text-slate-900">{timeSince(info.getValue(), me.locale)}</span>,
    }),
    column.accessor("createdAt", {
      header: t("common.created_at"),
      cell: (info) => (
        <span className="text-sm text-slate-900">
          {formatDateForDisplay(info.getValue(), me.locale, {
            year: "numeric",
            month: "long",
            day: "numeric",
          })}
        </span>
      ),
    }),
  ];

  return (
    <ContactsPageLayout
      pageTitle={t("common.contacts")}
      activeId="segments"
      workspaceId={workspaceId}
      isReadOnly={isReadOnly}
      cta={
        keys.data && (
          <CreateSegmentButton workspaceId={workspaceId} attributeKeys={keys.data} segments={listed} />
        )
      }>
      <div className="rounded-lg border border-slate-200">
        <DataTable
          columns={columns}
          data={listed}
          getRowId={(segment) => segment.id}
          isLoading={segments.isPending}
          emptyMessage={
            segments.isError
              ? errorMessage(t, segments.error)
              : t("workspace.segments.create_your_first_segment")
          }
          onRowClick={setEditing}
          aria-label={t("common.segments")}
        />
      </div>
      {editing && (
        <EditSegmentModal
          key={editing.id}
          workspaceId={workspaceId}
          segment={editing}
          allSegments={all}
          attributeKeys={keys.data ?? []}
          isReadOnly={isReadOnly}
          close={() => setEditing(null)}
        />
      )}
    </ContactsPageLayout>
  );
};

/** A segment's contact count in the list, a dash while it loads or when it could not be counted; an approximate one is marked and says why. */
const SegmentContactCount = ({
  count,
  approximate,
  locale,
}: {
  count: number | undefined;
  approximate: boolean;
  locale: string;
}) => {
  const { t } = useTranslation();
  if (count === undefined) return <span className="text-sm text-slate-500">-</span>;
  const formatted = new Intl.NumberFormat(locale).format(count);
  if (!approximate) return <span className="text-sm text-slate-900 tabular-nums">{formatted}</span>;
  const note = t("workspace.segments.preview_count_approximate");
  return (
    <span className="text-sm text-slate-900 tabular-nums" title={note}>
      <span aria-hidden>≈ </span>
      {formatted}
      <span className="sr-only"> ({note})</span>
    </span>
  );
};

/** The title, description and filter tree shared by the create and settings forms. The tree is edited with the survey editor's segment editor, through a segment whose filters are the form's. */
const SegmentForm = ({
  workspaceId,
  segmentId,
  title,
  setTitle,
  description,
  setDescription,
  filters,
  setFilters,
  attributeKeys,
  segments,
  linkedSurveyIds,
  viewOnly,
}: {
  workspaceId: string;
  segmentId?: string;
  title: string;
  setTitle: (title: string) => void;
  description: string;
  setDescription: (description: string) => void;
  filters: TBaseFilters;
  setFilters: (filters: TBaseFilters) => void;
  attributeKeys: AttributeKey[];
  segments: Segment[];
  linkedSurveyIds: string[];
  viewOnly: boolean;
}) => {
  const { t } = useTranslation();
  const [addFilterOpen, setAddFilterOpen] = useState(false);
  const contactAttributeKeys = useMemo(() => attributeKeys.map(toContactAttributeKey), [attributeKeys]);
  const editorSegments = useMemo(() => segments.map(toEditorSegment), [segments]);
  const segment = formSegment({ segmentId, workspaceId, filters, linkedSurveyIds });
  return (
    <>
      <div className="flex w-full items-center gap-4">
        <div className="flex w-1/2 flex-col gap-2">
          <Label htmlFor="segment-title">{t("common.title")}</Label>
          <Input
            id="segment-title"
            value={title}
            disabled={viewOnly}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={t("workspace.segments.ex_power_users")}
          />
        </div>
        <div className="flex w-1/2 flex-col gap-2">
          <Label htmlFor="segment-description">{t("common.description")}</Label>
          <Input
            id="segment-description"
            value={description}
            disabled={viewOnly}
            onChange={(e) => setDescription(e.target.value)}
            placeholder={t("workspace.segments.ex_fully_activated_recurring_users")}
          />
        </div>
      </div>
      <div className="flex flex-col gap-y-2 pt-4">
        <span className="text-sm font-medium text-slate-900">{t("common.targeting")}</span>
        <div className="flex max-h-96 w-full flex-col gap-4 overflow-auto rounded-lg border border-slate-200 bg-slate-50 p-4">
          {filters.length === 0 && (
            <div className="-mb-2 flex items-center gap-1">
              <FilterIcon className="size-5 text-slate-700" />
              <h3 className="text-sm font-medium text-slate-700">
                {t("workspace.segments.add_your_first_filter_to_get_started")}
              </h3>
            </div>
          )}
          <SegmentEditor
            group={filters}
            segment={segment}
            segments={editorSegments}
            contactAttributeKeys={contactAttributeKeys}
            setSegment={formSetSegment(segment, setFilters)}
            viewOnly={viewOnly}
          />
          <div>
            <Button
              size="sm"
              variant="secondary"
              className="w-fit"
              disabled={viewOnly}
              onClick={() => setAddFilterOpen(true)}>
              {t("common.add_filter")}
            </Button>
          </div>
          <AddFilterModal
            open={addFilterOpen}
            setOpen={setAddFilterOpen}
            contactAttributeKeys={contactAttributeKeys}
            segments={editorSegments}
            onAddFilter={(filter) => setFilters(appendFilter(filters, filter))}
          />
        </div>
        {filters.length > 0 && <SegmentPreviewCount workspaceId={workspaceId} filters={filters} />}
      </div>
    </>
  );
};

/** forma2's CreateSegmentModal: a public segment, saved once it has a title and valid filters. */
const CreateSegmentButton = ({
  workspaceId,
  attributeKeys,
  segments,
}: {
  workspaceId: string;
  attributeKeys: AttributeKey[];
  segments: Segment[];
}) => {
  const { t } = useTranslation();
  const create = useCreateSegment(workspaceId);
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [filters, setFilters] = useState<TBaseFilters>([]);

  const close = () => {
    setTitle("");
    setDescription("");
    setFilters([]);
    setOpen(false);
  };

  const save = async () => {
    if (!title) {
      toast.error(t("workspace.segments.title_is_required"));
      return;
    }
    try {
      await create.mutateAsync({ title, description, isPrivate: false, filters, surveyId: "" });
      toast.success(t("workspace.segments.segment_saved_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        {t("common.create_segment")}
        <PlusIcon />
      </Button>
      {open && (
        <Dialog open onOpenChange={(next) => !next && close()} width="wide" disableCloseOnOutsideClick>
          <DialogHeader>
            <UsersIcon />
            <DialogTitle>{t("common.create_segment")}</DialogTitle>
            <DialogDescription>
              {t("workspace.segments.segments_help_you_target_users_with_same_characteristics_easily")}
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            <SegmentForm
              workspaceId={workspaceId}
              title={title}
              setTitle={setTitle}
              description={description}
              setDescription={setDescription}
              filters={filters}
              setFilters={setFilters}
              attributeKeys={attributeKeys}
              segments={segments}
              linkedSurveyIds={[]}
              viewOnly={false}
            />
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="secondary" onClick={close}>
              {t("common.cancel")}
            </Button>
            <Button
              disabled={!canSaveSegment(title, filters)}
              loading={create.isPending}
              onClick={() => void save()}>
              {t("common.create_segment")}
            </Button>
          </DialogFooter>
        </Dialog>
      )}
    </>
  );
};

/** forma2's EditSegmentModal: the surveys the segment reaches, and its settings. */
const EditSegmentModal = ({
  workspaceId,
  segment,
  allSegments,
  attributeKeys,
  isReadOnly,
  close,
}: {
  workspaceId: string;
  segment: Segment;
  allSegments: Segment[];
  attributeKeys: AttributeKey[];
  isReadOnly: boolean;
  close: () => void;
}) => {
  const { t } = useTranslation();
  const activity = segmentActivitySummary(segment, allSegments);
  return (
    <ModalWithTabs
      open
      setOpen={(open) => !open && close()}
      icon={<UsersIcon />}
      label={segment.title}
      description={segment.description ?? ""}
      tabs={[
        {
          title: t("common.activity"),
          children: <SegmentActivityTab segment={segment} activity={activity} />,
        },
        {
          title: t("common.settings"),
          children: (
            <SegmentSettings
              workspaceId={workspaceId}
              segment={segment}
              segments={allSegments.filter((s) => !s.isPrivate)}
              attributeKeys={attributeKeys}
              activity={activity}
              isReadOnly={isReadOnly}
              close={close}
            />
          ),
        },
      ]}
    />
  );
};

const SegmentActivityTab = ({
  segment,
  activity,
}: {
  segment: Segment;
  activity: { activeSurveys: string[]; inactiveSurveys: string[] };
}) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const list = (names: string[]) =>
    names.length === 0 ? (
      <p className="text-sm text-slate-900">-</p>
    ) : (
      names.map((name) => (
        <div className="py-0.5" key={name}>
          <p className="text-sm text-slate-900">{name}</p>
        </div>
      ))
    );
  return (
    <div className="grid grid-cols-3 pb-2">
      <div className="col-span-2 space-y-4 pr-6">
        <div>
          <p className="text-sm font-medium text-slate-500">{t("common.active_surveys")}</p>
          {list(activity.activeSurveys)}
        </div>
        <div>
          <p className="text-sm font-medium text-slate-500">{t("common.inactive_surveys")}</p>
          {list(activity.inactiveSurveys)}
        </div>
      </div>
      <div className="col-span-1 space-y-3 rounded-lg border border-slate-100 bg-slate-50 p-2">
        <div>
          <p className="text-xs text-slate-500">{t("common.created_at")}</p>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(segment.createdAt, me.locale)}</p>
        </div>
        <div>
          <p className="text-xs text-slate-500">{t("common.updated_at")}</p>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(segment.updatedAt, me.locale)}</p>
        </div>
        <IdBadge id={segment.id} label={t("workspace.segments.segment_id")} />
      </div>
    </div>
  );
};

/** forma2's SegmentSettings: edit the title, description and filters, or delete the segment when no survey uses it. */
export const SegmentSettings = ({
  workspaceId,
  segment,
  segments,
  attributeKeys,
  activity,
  isReadOnly,
  close,
}: {
  workspaceId: string;
  segment: Segment;
  segments: Segment[];
  attributeKeys: AttributeKey[];
  activity: { activeSurveys: string[]; inactiveSurveys: string[] };
  isReadOnly: boolean;
  close: () => void;
}) => {
  const { t } = useTranslation();
  const update = useUpdateSegment(workspaceId);
  const remove = useDeleteSegment(workspaceId);
  const [title, setTitle] = useState(segment.title);
  const [description, setDescription] = useState(segment.description ?? "");
  const [filters, setFilters] = useState(segment.filters);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const usedBy = [...activity.activeSurveys, ...activity.inactiveSurveys];
  // Results links filtering by the segment do not block deleting it; the dialog names them, since they stop showing the responses they were created for.
  const resultShares = useQuery({
    ...segmentResultSharesQuery(workspaceId, segment.id),
    enabled: confirmDelete && !isReadOnly,
  });
  const linkedShares = resultShares.data ?? [];
  // Deleting waits for the list, so it is never confirmed before the links it breaks are shown.
  const checkingLinks = confirmDelete && !isReadOnly && resultShares.isPending;

  const save = async () => {
    if (!title) {
      toast.error(t("workspace.segments.title_is_required"));
      return;
    }
    try {
      await update.mutateAsync({
        id: segment.id,
        update: { title, description, isPrivate: segment.isPrivate, filters },
      });
      toast.success(t("workspace.segments.segment_updated_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const deleteSegment = async () => {
    try {
      await remove.mutateAsync(segment.id);
      toast.success(t("workspace.segments.segment_deleted_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div>
      <SegmentForm
        workspaceId={workspaceId}
        segmentId={segment.id}
        title={title}
        setTitle={setTitle}
        description={description}
        setDescription={setDescription}
        filters={filters}
        setFilters={setFilters}
        attributeKeys={attributeKeys}
        segments={segments}
        linkedSurveyIds={segment.surveys}
        viewOnly={isReadOnly}
      />
      {!isReadOnly && (
        <div className="flex w-full items-center justify-between pt-4">
          <Button
            type="button"
            variant="destructive"
            loading={remove.isPending}
            onClick={() => setConfirmDelete(true)}>
            {t("common.delete")}
            <Trash2 />
          </Button>
          <Button
            disabled={!canSaveSegment(title, filters)}
            loading={update.isPending}
            onClick={() => void save()}>
            {t("common.save_changes")}
          </Button>
        </div>
      )}
      {confirmDelete && (
        <Dialog open onOpenChange={setConfirmDelete} width="narrow">
          <DialogHeader>
            <DialogTitle>{t("workspace.segments.delete_segment")}</DialogTitle>
            <DialogDescription>{t("workspace.general.this_action_cannot_be_undone")}</DialogDescription>
          </DialogHeader>
          {(usedBy.length > 0 || linkedShares.length > 0 || resultShares.isError) && (
            <DialogBody className="space-y-4">
              {usedBy.length > 0 && (
                <div>
                  <div className="space-y-2">
                    <p>{t("workspace.segments.cannot_delete_segment_used_in_surveys")}</p>
                    <ol className="my-2 ml-4 list-decimal">
                      {usedBy.map((name) => (
                        <li key={name}>{name}</li>
                      ))}
                    </ol>
                  </div>
                  <p className="mt-2">
                    {t(
                      "workspace.segments.please_remove_the_segment_from_these_surveys_in_order_to_delete_it"
                    )}
                  </p>
                </div>
              )}
              {linkedShares.length > 0 && (
                <div className="space-y-2" data-testid="segment-result-shares">
                  <p>{t("workspace.analysis.result_shares.segment_delete_links_warning")}</p>
                  <ul className="my-2 ml-4 list-disc">
                    {linkedShares.map((share) => (
                      <li key={share.id}>
                        {t("workspace.analysis.result_shares.segment_link_in_survey", {
                          name: share.name || t("workspace.analysis.result_shares.untitled_link"),
                          survey: share.surveyName,
                        })}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {resultShares.isError && (
                <p data-testid="segment-result-shares-unchecked">
                  {t("workspace.analysis.result_shares.segment_delete_links_unchecked")}
                </p>
              )}
            </DialogBody>
          )}
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmDelete(false)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={usedBy.length > 0 || checkingLinks}
              loading={remove.isPending}
              onClick={() => void deleteSegment()}>
              {t("common.delete")}
            </Button>
          </DialogFooter>
        </Dialog>
      )}
    </div>
  );
};
