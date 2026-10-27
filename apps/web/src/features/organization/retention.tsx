import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "../../components/ui/alert";
import { Button } from "../../components/ui/button";
import { ConfirmationModal } from "../../components/ui/confirmation-modal";
import { Label } from "../../components/ui/label";
import { SettingsCard } from "../../components/ui/page";
import { Select } from "../../components/ui/select";
import { errorMessage } from "../../lib/errors";
import {
  type Retention,
  type RetentionWindows,
  retentionPreviewQuery,
  retentionQuery,
  useUpdateRetention,
} from "../../lib/queries/organizations";
import { toast } from "../../lib/toast";

/** The windows an owner chooses from, in days. The server accepts any from 1 to 3650; a window set otherwise (through SQL) is offered as it is. */
export const RETENTION_PERIODS = [30, 90, 180, 365, 730] as const;

/** The select's value for "keep forever". */
const FOREVER = "forever";

const formatDate = (language: string, iso: string): string =>
  new Intl.DateTimeFormat(language, { dateStyle: "long", timeStyle: "short" }).format(new Date(iso));

/** Whether saving next would delete more than the current windows do: a window set where there was none, or shortened. On an instance whose sweep deletes, the owner confirms that first. */
export const deletesMore = (current: RetentionWindows, next: RetentionWindows): boolean =>
  (["workflowRunRetentionDays", "displayRetentionDays"] as const).some((key) => {
    const to = next[key];
    const from = current[key];
    return to !== null && (from === null || to < from);
  });

/**
 * The organization's Data retention card on its general settings page, for owners: how long finished workflow runs and survey impressions without a response are kept, whether this instance's daily sweep only counts (RETENTION_SWEEP_DRY_RUN, the default) or deletes, and what its last run counted or deleted under these windows. Responses and contacts are not affected. On Forma Cloud the owner is not told about the environment variable, which only a self-hosting operator can set.
 */
export const RetentionCard = ({
  organizationId,
  isFormaCloud,
}: {
  organizationId: string;
  isFormaCloud: boolean;
}) => {
  const { t } = useTranslation();
  const retention = useQuery(retentionQuery(organizationId));
  return (
    <SettingsCard
      width="full"
      title={t("organizations.retention.card_title")}
      description={t("organizations.retention.card_description")}>
      <div className="max-w-xl space-y-4 pb-4">
        {retention.isError ? (
          <p className="text-sm text-red-600">{errorMessage(t, retention.error)}</p>
        ) : retention.data ? (
          <RetentionForm
            key={`${retention.data.workflowRunRetentionDays}-${retention.data.displayRetentionDays}`}
            organizationId={organizationId}
            isFormaCloud={isFormaCloud}
            retention={retention.data}
          />
        ) : null}
      </div>
    </SettingsCard>
  );
};

const RetentionForm = ({
  organizationId,
  isFormaCloud,
  retention,
}: {
  organizationId: string;
  isFormaCloud: boolean;
  retention: Retention;
}) => {
  const { t, i18n } = useTranslation();
  const update = useUpdateRetention(organizationId);
  const [windows, setWindows] = useState<RetentionWindows>({
    workflowRunRetentionDays: retention.workflowRunRetentionDays,
    displayRetentionDays: retention.displayRetentionDays,
  });
  const [confirming, setConfirming] = useState(false);
  const changed =
    windows.workflowRunRetentionDays !== retention.workflowRunRetentionDays ||
    windows.displayRetentionDays !== retention.displayRetentionDays;
  const nextSweep = formatDate(i18n.language, retention.nextSweepAt);

  const save = async () => {
    setConfirming(false);
    try {
      await update.mutateAsync(windows);
      toast.success(t("organizations.retention.saved"));
    } catch (error) {
      toast.error(errorMessage(t, error));
    }
  };
  const submit = () => {
    if (!retention.dryRun && deletesMore(retention, windows)) setConfirming(true);
    else void save();
  };

  return (
    <>
      {retention.dryRun ? (
        <Alert variant="info" role="status">
          <AlertTitle>{t("organizations.retention.dry_run_title")}</AlertTitle>
          <AlertDescription>
            {isFormaCloud
              ? t("organizations.retention.dry_run_description_cloud")
              : t("organizations.retention.dry_run_description")}
          </AlertDescription>
        </Alert>
      ) : (
        <Alert variant="warning" role="status">
          <AlertTitle>{t("organizations.retention.deleting_title")}</AlertTitle>
          <AlertDescription>{t("organizations.retention.deleting_description")}</AlertDescription>
        </Alert>
      )}
      <WindowSelect
        id="retention-workflow-runs"
        label={t("organizations.retention.workflow_runs_label")}
        description={t("organizations.retention.workflow_runs_description")}
        value={windows.workflowRunRetentionDays}
        onChange={(days) => setWindows((w) => ({ ...w, workflowRunRetentionDays: days }))}
      />
      <WindowSelect
        id="retention-displays"
        label={t("organizations.retention.displays_label")}
        description={t("organizations.retention.displays_description_identified_kept")}
        value={windows.displayRetentionDays}
        onChange={(days) => setWindows((w) => ({ ...w, displayRetentionDays: days }))}
      />
      <p className="text-xs text-slate-500">{t("organizations.retention.not_affected")}</p>
      <Button size="sm" loading={update.isPending} disabled={!changed} onClick={submit}>
        {t("common.save_changes")}
      </Button>
      <LastSweep retention={retention} nextSweep={nextSweep} />
      <ConfirmationModal
        open={confirming}
        setOpen={setConfirming}
        title={t("organizations.retention.confirm_title")}
        description={t("organizations.retention.confirm_description", { date: nextSweep })}
        body={confirming ? <DeletionPreview organizationId={organizationId} windows={windows} /> : null}
        buttonText={t("organizations.retention.confirm_button")}
        onConfirm={() => void save()}
      />
    </>
  );
};

const WindowSelect = ({
  id,
  label,
  description,
  value,
  onChange,
}: {
  id: string;
  label: string;
  description: string;
  value: number | null;
  onChange: (days: number | null) => void;
}) => {
  const { t } = useTranslation();
  const periods: number[] =
    value === null || (RETENTION_PERIODS as readonly number[]).includes(value)
      ? [...RETENTION_PERIODS]
      : [...RETENTION_PERIODS, value].sort((a, b) => a - b);
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <p id={`${id}-description`} className="text-sm text-slate-500">
        {description}
      </p>
      <Select
        id={id}
        aria-describedby={`${id}-description`}
        containerClassName="max-w-xs"
        value={value === null ? FOREVER : String(value)}
        onChange={(e) => onChange(e.target.value === FOREVER ? null : Number(e.target.value))}>
        <option value={FOREVER}>{t("organizations.retention.keep_forever")}</option>
        {periods.map((days) => (
          <option key={days} value={String(days)}>
            {t("organizations.retention.period_days", { count: days })}
          </option>
        ))}
      </Select>
    </div>
  );
};

/** What the next sweep would delete under the windows being confirmed, counted as of now, so an owner sees the impact before deleting for good. */
const DeletionPreview = ({
  organizationId,
  windows,
}: {
  organizationId: string;
  windows: RetentionWindows;
}) => {
  const { t } = useTranslation();
  const preview = useQuery(retentionPreviewQuery(organizationId, windows));
  if (preview.isError) return <p className="text-sm text-red-600">{errorMessage(t, preview.error)}</p>;
  if (!preview.data)
    return <p className="text-sm text-slate-700">{t("organizations.retention.preview_loading")}</p>;
  const { workflowRuns, displays } = preview.data;
  return (
    <section aria-labelledby="retention-preview" className="space-y-1 text-sm text-slate-700">
      <h3 id="retention-preview" className="font-medium text-slate-900">
        {t("organizations.retention.preview_title")}
      </h3>
      <ul className="list-disc space-y-1 pl-5">
        {workflowRuns !== null && (
          <li>{t("organizations.retention.would_delete_runs", { count: workflowRuns })}</li>
        )}
        {displays !== null && (
          <li>{t("organizations.retention.would_delete_displays", { count: displays })}</li>
        )}
      </ul>
    </section>
  );
};

/** What the last sweep counted or deleted under the windows the organization has now, or when the first one with them runs. */
const LastSweep = ({ retention, nextSweep }: { retention: Retention; nextSweep: string }) => {
  const { t, i18n } = useTranslation();
  if (retention.workflowRunRetentionDays === null && retention.displayRetentionDays === null)
    return <p className="text-sm text-slate-700">{t("organizations.retention.nothing_deleted")}</p>;
  const sweep = retention.lastSweep;
  if (!sweep)
    return (
      <p className="text-sm text-slate-700">
        {t("organizations.retention.no_sweep_yet", { date: nextSweep })}
      </p>
    );
  const count = (key: "workflowRuns" | "displays") =>
    sweep.dryRun ? sweep[key].matched : sweep[key].deleted;
  const capped = sweep.workflowRuns.cappedUnits + sweep.displays.cappedUnits > 0;
  return (
    <section aria-labelledby="retention-last-sweep" className="space-y-1 text-sm text-slate-700">
      <h3 id="retention-last-sweep" className="font-medium text-slate-900">
        {t("organizations.retention.last_sweep_title", { date: formatDate(i18n.language, sweep.sweptAt) })}
      </h3>
      <ul className="list-disc space-y-1 pl-5">
        {sweep.workflowRunRetentionDays !== null && (
          <li>
            {sweep.dryRun
              ? t("organizations.retention.would_delete_runs", { count: count("workflowRuns") })
              : t("organizations.retention.deleted_runs", { count: count("workflowRuns") })}
          </li>
        )}
        {sweep.displayRetentionDays !== null && (
          <li>
            {sweep.dryRun
              ? t("organizations.retention.would_delete_displays", { count: count("displays") })
              : t("organizations.retention.deleted_displays", { count: count("displays") })}
          </li>
        )}
      </ul>
      {sweep.stoppedEarly && <p>{t("organizations.retention.stopped_early")}</p>}
      {capped && <p>{t("organizations.retention.capped")}</p>}
      <p className="text-xs text-slate-500">{t("organizations.retention.next_sweep", { date: nextSweep })}</p>
    </section>
  );
};
