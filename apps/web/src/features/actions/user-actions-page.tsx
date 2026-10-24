import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createColumnHelper } from "@tanstack/react-table";
import { Code2Icon, Loader2, MousePointerClickIcon, TrashIcon } from "lucide-react";
import { type FormEvent, type ReactNode, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "../../components/ui/button";
import { DataTable } from "../../components/ui/data-table";
import { DeleteDialog } from "../../components/ui/delete-dialog";
import { Dialog, DialogBody, DialogDescription, DialogHeader, DialogTitle } from "../../components/ui/dialog";
import { ModalWithTabs } from "../../components/ui/modal-with-tabs";
import { PageContentWrapper, PageHeader, SettingsCard } from "../../components/ui/page";
import {
  type ActionClass,
  type ActionFormValues,
  NEW_ACTION_VALUES,
  actionFormErrors,
  buildActionInput,
  formValuesOf,
} from "../../lib/action-classes";
import { errorMessage } from "../../lib/errors";
import {
  actionClassSurveysQuery,
  actionClassesQuery,
  useCreateActionClass,
  useDeleteActionClass,
  useUpdateActionClass,
} from "../../lib/queries/action-classes";
import { meQuery } from "../../lib/queries/auth";
import { formatDateTimeForDisplay, timeSince } from "../../lib/time";
import { toast } from "../../lib/toast";
import { ACTION_DOCS_HREF, ActionFields, TabToggle } from "./action-form";

const ACTION_TYPE_ICONS: Record<string, ReactNode> = {
  code: <Code2Icon className="size-4" />,
  noCode: <MousePointerClickIcon className="size-4" />,
};

const column = createColumnHelper<ActionClass>();

/** forma2's user actions page: the workspace's action classes, oldest first, each opening its activity and settings, and a way to track a new one. */
export const UserActionsPage = ({
  workspaceId,
  isReadOnly,
}: {
  workspaceId: string;
  isReadOnly: boolean;
}) => {
  const { t } = useTranslation();
  const { data: me } = useSuspenseQuery(meQuery);
  const actionClasses = useQuery(actionClassesQuery(workspaceId));
  const [isAddOpen, setIsAddOpen] = useState(false);
  const [active, setActive] = useState<ActionClass>();
  const rows = actionClasses.data ?? [];
  const columns = [
    column.accessor("name", {
      header: t("workspace.actions.user_actions"),
      meta: { headerClassName: "w-[65%]" },
      cell: (info) => (
        <div className="flex items-center gap-4" title={info.getValue()}>
          <div className="size-5 shrink-0 text-slate-500">{ACTION_TYPE_ICONS[info.row.original.type]}</div>
          <div className="text-left">
            <div className="font-medium wrap-break-word text-slate-900">{info.getValue()}</div>
            <div className="text-xs wrap-break-word text-slate-400">{info.row.original.description}</div>
          </div>
        </div>
      ),
    }),
    column.accessor("createdAt", {
      header: t("common.created"),
      meta: {
        headerClassName: "w-[35%]",
        align: "center",
        cellClassName: "whitespace-nowrap text-slate-500",
      },
      cell: (info) => timeSince(info.getValue(), me.locale),
    }),
  ];

  return (
    <PageContentWrapper>
      <PageHeader pageTitle={t("common.user_actions")} />
      <SettingsCard
        title={t("common.actions")}
        description={t("common.actions_description")}
        bodyVariant="flush"
        width="full"
        cta={
          <Button size="sm" onClick={() => setIsAddOpen(true)}>
            {t("common.add_action")}
          </Button>
        }>
        <DataTable
          columns={columns}
          data={rows}
          getRowId={(actionClass) => actionClass.id}
          isLoading={actionClasses.isPending}
          emptyMessage={
            actionClasses.isError ? errorMessage(t, actionClasses.error) : t("common.no_actions_found")
          }
          onRowClick={setActive}
          aria-label={t("common.actions")}
        />
      </SettingsCard>
      {isAddOpen && (
        <AddActionModal
          workspaceId={workspaceId}
          actionClasses={rows}
          isReadOnly={isReadOnly}
          setOpen={setIsAddOpen}
        />
      )}
      {active && (
        <ActionDetailModal
          key={active.id}
          workspaceId={workspaceId}
          actionClass={active}
          actionClasses={rows}
          isReadOnly={isReadOnly}
          locale={me.locale}
          setOpen={(open) => !open && setActive(undefined)}
        />
      )}
    </PageContentWrapper>
  );
};

/** The names and keys a form's action must not reuse: every other action's. */
const takenNamesAndKeys = (actionClasses: ActionClass[], exceptId?: string) => {
  const others = actionClasses.filter((actionClass) => actionClass.id !== exceptId);
  return {
    names: others.map((actionClass) => actionClass.name),
    keys: others.filter((a) => a.type === "code" && a.key !== null).map((a) => a.key as string),
  };
};

/** forma2's AddActionModal and CreateNewActionTab. */
const AddActionModal = ({
  workspaceId,
  actionClasses,
  isReadOnly,
  setOpen,
}: {
  workspaceId: string;
  actionClasses: ActionClass[];
  isReadOnly: boolean;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const create = useCreateActionClass(workspaceId);
  const [values, setValues] = useState<ActionFormValues>(NEW_ACTION_VALUES);
  const [submitted, setSubmitted] = useState(false);
  const taken = useMemo(() => takenNamesAndKeys(actionClasses), [actionClasses]);
  const errors = actionFormErrors(values, taken.names, taken.keys, t);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) return;
    if (isReadOnly) {
      toast.error(t("common.you_are_not_authorized_to_perform_this_action"));
      return;
    }
    try {
      await create.mutateAsync(buildActionInput(values));
      toast.success(t("workspace.actions.action_created_successfully"));
      setOpen(false);
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <Dialog open onOpenChange={setOpen} disableCloseOnOutsideClick>
      <DialogHeader>
        <MousePointerClickIcon />
        <DialogTitle>{t("workspace.actions.track_new_user_action")}</DialogTitle>
        <DialogDescription>
          {t("workspace.actions.track_user_action_to_display_surveys_or_create_user_segment")}
        </DialogDescription>
      </DialogHeader>
      <DialogBody>
        <form onSubmit={submit} aria-label={t("workspace.actions.create_action")} noValidate>
          <div className="w-full space-y-4">
            <div className="w-3/5 space-y-1">
              <p className="text-sm font-semibold text-slate-800">{t("workspace.actions.action_type")}</p>
              <TabToggle
                label={t("workspace.actions.action_type")}
                value={values.type}
                onChange={(type) => setValues({ ...values, type })}
                options={[
                  { value: "noCode", label: t("common.no_code") },
                  { value: "code", label: t("common.code") },
                ]}
              />
            </div>
            <ActionFields
              values={values}
              onChange={setValues}
              errors={submitted ? errors : {}}
              isReadOnly={isReadOnly}
              isKeyReadOnly={isReadOnly}
              idPrefix="create-action"
            />
          </div>
          <div className="sticky bottom-0 flex justify-end gap-x-2 bg-white pt-4">
            <Button type="button" variant="secondary" onClick={() => setOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" loading={create.isPending}>
              {t("workspace.actions.create_action")}
            </Button>
          </div>
        </form>
      </DialogBody>
    </Dialog>
  );
};

/** forma2's ActionDetailModal: what the action triggers, and its settings. */
const ActionDetailModal = ({
  workspaceId,
  actionClass,
  actionClasses,
  isReadOnly,
  locale,
  setOpen,
}: {
  workspaceId: string;
  actionClass: ActionClass;
  actionClasses: ActionClass[];
  isReadOnly: boolean;
  locale: string;
  setOpen: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const description =
    actionClass.description ||
    `${actionClass.type === "noCode" ? t("common.no_code") : t("common.code")} ${t("common.action").toLowerCase()}`;
  return (
    <ModalWithTabs
      open
      setOpen={setOpen}
      icon={ACTION_TYPE_ICONS[actionClass.type]}
      label={actionClass.name}
      description={description}
      tabs={[
        {
          title: t("common.activity"),
          children: <ActionActivityTab actionClass={actionClass} locale={locale} />,
        },
        {
          title: t("common.settings"),
          children: (
            <ActionSettingsTab
              workspaceId={workspaceId}
              actionClass={actionClass}
              actionClasses={actionClasses}
              isReadOnly={isReadOnly}
              close={() => setOpen(false)}
            />
          ),
        },
      ]}
    />
  );
};

const ActionActivityTab = ({ actionClass, locale }: { actionClass: ActionClass; locale: string }) => {
  const { t } = useTranslation();
  const surveys = useQuery(actionClassSurveysQuery(actionClass.id));
  if (surveys.isPending) {
    return (
      <Loader2 className="mx-auto size-6 animate-spin text-slate-500" aria-label={t("common.loading")} />
    );
  }
  if (surveys.isError) {
    return (
      <p role="alert" className="text-sm text-slate-600">
        {errorMessage(t, surveys.error)}
      </p>
    );
  }
  const list = (names: string[]) =>
    names.length === 0 ? (
      <p className="text-sm text-slate-900">-</p>
    ) : (
      names.map((name) => (
        <p key={name} className="text-sm text-slate-900">
          {name}
        </p>
      ))
    );
  return (
    <div className="grid grid-cols-3 pb-2">
      <div className="col-span-2 space-y-4 pr-6">
        <div>
          <p className="text-sm font-medium text-slate-500">{t("common.active_surveys")}</p>
          {list(surveys.data.activeSurveys)}
        </div>
        <div>
          <p className="text-sm font-medium text-slate-500">{t("common.inactive_surveys")}</p>
          {list(surveys.data.inactiveSurveys)}
        </div>
      </div>
      <div className="col-span-1 space-y-3 rounded-lg border border-slate-100 bg-slate-50 p-2">
        <div>
          <p className="text-xs text-slate-500">{t("common.created_at")}</p>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(actionClass.createdAt, locale)}</p>
        </div>
        <div>
          <p className="text-xs text-slate-500">{t("common.updated_at")}</p>
          <p className="text-xs text-slate-700">{formatDateTimeForDisplay(actionClass.updatedAt, locale)}</p>
        </div>
        <div>
          <p className="text-xs text-slate-500">{t("common.type")}</p>
          <div className="mt-1 flex items-center">
            <div className="mr-1.5 size-4 text-slate-600">{ACTION_TYPE_ICONS[actionClass.type]}</div>
            <p className="text-sm text-slate-700 capitalize">{actionClass.type}</p>
          </div>
        </div>
      </div>
    </div>
  );
};

/** forma2's ActionSettingsTab: a code action's key cannot change; anything that is neither code nor no-code was created automatically and only its name and description can. */
const ActionSettingsTab = ({
  workspaceId,
  actionClass,
  actionClasses,
  isReadOnly,
  close,
}: {
  workspaceId: string;
  actionClass: ActionClass;
  actionClasses: ActionClass[];
  isReadOnly: boolean;
  close: () => void;
}) => {
  const { t } = useTranslation();
  const initial = useMemo(() => formValuesOf(actionClass), [actionClass]);
  const [values, setValues] = useState(initial);
  const [submitted, setSubmitted] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const update = useUpdateActionClass(workspaceId);
  const remove = useDeleteActionClass(workspaceId);
  const taken = useMemo(
    () => takenNamesAndKeys(actionClasses, actionClass.id),
    [actionClasses, actionClass.id]
  );
  const errors = actionFormErrors(values, taken.names, taken.keys, t);
  const isDirty = JSON.stringify(values) !== JSON.stringify(initial);
  const isKnownType = actionClass.type === "code" || actionClass.type === "noCode";

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (Object.keys(errors).length > 0) return;
    if (isReadOnly) {
      toast.error(t("common.you_are_not_authorized_to_perform_this_action"));
      return;
    }
    try {
      await update.mutateAsync({ id: actionClass.id, input: buildActionInput(values) });
      toast.success(t("workspace.actions.action_updated_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  const deleteAction = async () => {
    try {
      await remove.mutateAsync(actionClass.id);
      toast.success(t("workspace.actions.action_deleted_successfully"));
      close();
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };

  return (
    <div>
      <form onSubmit={submit} noValidate>
        <div className="max-h-[400px] w-full space-y-4 overflow-y-auto">
          {isKnownType ? (
            <ActionFields
              values={values}
              onChange={setValues}
              errors={submitted ? errors : {}}
              isReadOnly={isReadOnly}
              isKeyReadOnly
              idPrefix="action-settings"
            />
          ) : (
            <p className="text-sm text-slate-600">
              {t("workspace.actions.this_action_was_created_automatically_you_cannot_make_changes_to_it")}
            </p>
          )}
        </div>
        <div className="flex justify-between gap-x-2 border-slate-200 pt-4">
          <div className="flex items-center gap-x-2">
            {!isReadOnly && (
              <Button type="button" variant="destructive" onClick={() => setConfirmDelete(true)}>
                <TrashIcon />
                {t("common.delete")}
              </Button>
            )}
            <Button variant="secondary" asChild>
              <a href={ACTION_DOCS_HREF} target="_blank" rel="noreferrer">
                {t("common.read_docs")}
              </a>
            </Button>
          </div>
          {!isReadOnly && isKnownType && (
            <Button type="submit" loading={update.isPending} disabled={!isDirty}>
              {t("common.save_changes")}
            </Button>
          )}
        </div>
      </form>
      <DeleteDialog
        open={confirmDelete}
        setOpen={setConfirmDelete}
        isDeleting={remove.isPending}
        deleteWhat={t("common.action")}
        text={t("workspace.actions.delete_action_text")}
        onDelete={() => void deleteAction()}
      />
    </div>
  );
};
