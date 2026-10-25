import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { RotateCwIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { IdBadge } from "../../../components/ui/id-badge";
import { Sheet, SheetSection } from "../../../components/ui/sheet";
import { formatDateTimeForDisplay, timeSince } from "../../../lib/time";
import { toast } from "../../../lib/toast";
import {
  type Webhook,
  type WebhookDelivery,
  type WebhookHealth,
  useResendWebhookDelivery,
  webhookDeliveriesQuery,
  webhookKeys,
} from "../api";
import {
  attemptFailureText,
  deliveryStatusBadge,
  triggerLabel,
  webhookErrorMessage,
  webhookHealthBadge,
} from "../webhooks";

const useLocale = () => {
  const { i18n } = useTranslation();
  return i18n.resolvedLanguage ?? i18n.language ?? "en-US";
};

/** A webhook's delivery health in the table: its badge, with what it rests on as the tooltip. */
export const WebhookHealthBadge = ({ health }: { health: WebhookHealth | undefined }) => {
  const { t } = useTranslation();
  const locale = useLocale();
  const badge = webhookHealthBadge(t, health);
  const detail = health ? healthDetail(t, health, locale) : undefined;
  return <Badge type={badge.type} size="tiny" text={badge.label} title={detail} />;
};

/** One line on a webhook's health: when it started failing and why, or when it last delivered. */
const healthDetail = (t: TFunction, health: WebhookHealth, locale: string): string => {
  if (health.status === "healthy") {
    return t("workspace.integrations.webhooks.deliveries.last_delivered", {
      time: timeSince(health.lastAttemptAt, locale),
    });
  }
  const since = health.failingSince ?? health.lastAttemptAt;
  const reason = attemptFailureText(t, health);
  const failing = t("workspace.integrations.webhooks.deliveries.first_failure", {
    time: timeSince(since, locale),
  });
  return reason ? `${failing}: ${reason}` : failing;
};

/** A webhook's delivery health with the details it rests on, for its overview and the delivery log. */
export const WebhookHealthSummary = ({ health }: { health: WebhookHealth | undefined }) => {
  const { t } = useTranslation();
  const locale = useLocale();
  const badge = webhookHealthBadge(t, health);
  const reason = health ? attemptFailureText(t, health) : null;
  return (
    <dl className="space-y-2 text-sm">
      <div>
        <dt className="sr-only">{t("common.status")}</dt>
        <dd>
          <Badge type={badge.type} text={badge.label} />
        </dd>
      </div>
      {health ? (
        <>
          <div>
            <dt className="text-xs text-slate-500">
              {t("workspace.integrations.webhooks.deliveries.last_attempt")}
            </dt>
            <dd className="text-slate-900">{timeSince(health.lastAttemptAt, locale)}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-500">
              {t("workspace.integrations.webhooks.deliveries.last_success")}
            </dt>
            <dd className="text-slate-900">
              {health.lastSuccessAt
                ? timeSince(health.lastSuccessAt, locale)
                : t("workspace.integrations.webhooks.deliveries.never")}
            </dd>
          </div>
          {health.status !== "healthy" && health.failingSince && (
            <div>
              <dt className="text-xs text-slate-500">
                {t("workspace.integrations.webhooks.deliveries.failing_since_label")}
              </dt>
              <dd className="text-slate-900">{formatDateTimeForDisplay(health.failingSince, locale)}</dd>
            </div>
          )}
          {health.failedCount > 0 && (
            <div>
              <dt className="text-xs text-slate-500">
                {t("workspace.integrations.webhooks.deliveries.lost_label")}
              </dt>
              <dd className="text-slate-900">{health.failedCount}</dd>
            </div>
          )}
          {health.nextAttemptAt && (
            <div>
              <dt className="text-xs text-slate-500">
                {t("workspace.integrations.webhooks.deliveries.next_attempt")}
              </dt>
              <dd className="text-slate-900">{timeSince(health.nextAttemptAt, locale)}</dd>
            </div>
          )}
          {reason && (
            <div>
              <dt className="text-xs text-slate-500">
                {t("workspace.integrations.webhooks.deliveries.last_error")}
              </dt>
              <dd className="rounded-md bg-red-50 px-2 py-1 break-words text-red-700">{reason}</dd>
            </div>
          )}
        </>
      ) : (
        <p className="text-xs text-slate-500">{t("workspace.integrations.webhooks.deliveries.empty")}</p>
      )}
    </dl>
  );
};

const DeliveryItem = ({
  delivery,
  workspaceId,
  canResend,
  resending,
  onResend,
}: {
  delivery: WebhookDelivery;
  workspaceId: string;
  canResend: boolean;
  resending: boolean;
  onResend: () => void;
}) => {
  const { t } = useTranslation();
  const locale = useLocale();
  const badge = deliveryStatusBadge(t, delivery.status);
  const reason = attemptFailureText(t, delivery);
  return (
    <li className="rounded-lg border border-slate-200 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Badge type={badge.type} text={badge.label} />
          <span className="text-sm font-medium text-slate-900">{triggerLabel(t, delivery.event)}</span>
          <span
            className="text-xs text-slate-500"
            title={formatDateTimeForDisplay(delivery.createdAt, locale)}>
            {timeSince(delivery.createdAt, locale)}
          </span>
        </div>
        {canResend && (
          <Button type="button" size="sm" variant="secondary" loading={resending} onClick={onResend}>
            <RotateCwIcon className="size-4" />
            {t("workspace.integrations.webhooks.deliveries.resend")}
          </Button>
        )}
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:grid-cols-4">
        <div className="col-span-2">
          <dt className="text-slate-500">{t("common.survey")}</dt>
          <dd className="truncate text-slate-900">
            {delivery.surveyName === null ? (
              "-"
            ) : (
              <Link
                to="/workspaces/$workspaceId/surveys/$surveyId/responses"
                params={{ workspaceId, surveyId: delivery.surveyId }}
                className="underline-offset-4 hover:underline">
                {delivery.surveyName}
              </Link>
            )}
          </dd>
        </div>
        <div className="col-span-2">
          <dt className="text-slate-500">{t("common.response")}</dt>
          <dd className="mt-0.5">
            <IdBadge id={delivery.responseId} />
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.attempts_label")}</dt>
          <dd className="text-slate-900">{delivery.attempts}</dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.last_attempt")}</dt>
          <dd className="text-slate-900" title={formatDateTimeForDisplay(delivery.lastAttemptAt, locale)}>
            {timeSince(delivery.lastAttemptAt, locale)}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.response_code")}</dt>
          <dd className="text-slate-900">{delivery.statusCode ?? "-"}</dd>
        </div>
        <div>
          <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.duration")}</dt>
          <dd className="text-slate-900">
            {t("workspace.integrations.webhooks.deliveries.duration_ms", { ms: delivery.latencyMs })}
          </dd>
        </div>
        {delivery.nextAttemptAt && (
          <div className="col-span-2">
            <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.next_attempt")}</dt>
            <dd className="text-slate-900" title={formatDateTimeForDisplay(delivery.nextAttemptAt, locale)}>
              {timeSince(delivery.nextAttemptAt, locale)}
            </dd>
          </div>
        )}
        {reason && (
          <div className="col-span-2 sm:col-span-4">
            <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.last_error")}</dt>
            <dd className="mt-0.5 rounded-md bg-red-50 px-2 py-1 break-words text-red-700">{reason}</dd>
          </div>
        )}
        <div className="col-span-2 sm:col-span-4">
          <dt className="text-slate-500">{t("workspace.integrations.webhooks.deliveries.message_id")}</dt>
          <dd className="font-mono break-all text-slate-700">{delivery.messageId}</dd>
        </div>
      </dl>
    </li>
  );
};

/** A webhook's delivery log: its health, then its last 100 events, newest first, each with the outcome of its latest attempt and, for a member who may write to the workspace, a Resend that sends it again now. */
export const WebhookDeliveriesSheet = ({
  workspaceId,
  webhook,
  isReadOnly,
  open,
  onOpenChange,
}: {
  workspaceId: string;
  webhook: Webhook;
  isReadOnly: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const deliveries = useQuery({ ...webhookDeliveriesQuery(workspaceId, webhook.id), enabled: open });
  const resend = useResendWebhookDelivery(workspaceId, webhook.id);
  // The health above the log comes from the webhooks list, so a refresh reloads both.
  const refresh = () =>
    Promise.all([
      deliveries.refetch(),
      queryClient.invalidateQueries({ queryKey: webhookKeys.all(workspaceId), exact: true }),
    ]);

  const onResend = async (delivery: WebhookDelivery) => {
    try {
      const after = await resend.mutateAsync(delivery.id);
      if (after.status === "succeeded") {
        toast.success(t("workspace.integrations.webhooks.deliveries.resend_succeeded"));
      } else {
        toast.error(
          t("workspace.integrations.webhooks.deliveries.resend_failed", {
            reason: attemptFailureText(t, after) ?? "",
          })
        );
      }
    } catch (error) {
      toast.error(webhookErrorMessage(t, error));
    }
  };

  let list: ReactNode;
  if (deliveries.isPending) {
    list = (
      <div className="space-y-3" aria-hidden="true">
        <div className="h-24 w-full animate-pulse rounded-lg bg-slate-200" />
        <div className="h-24 w-full animate-pulse rounded-lg bg-slate-200" />
      </div>
    );
  } else if (deliveries.isError) {
    list = <p className="text-sm text-slate-600">{webhookErrorMessage(t, deliveries.error)}</p>;
  } else if (deliveries.data.length === 0) {
    list = <p className="text-sm text-slate-500">{t("workspace.integrations.webhooks.deliveries.empty")}</p>;
  } else {
    list = (
      <ul className="space-y-3">
        {deliveries.data.map((delivery) => (
          <DeliveryItem
            key={delivery.id}
            delivery={delivery}
            workspaceId={workspaceId}
            canResend={!isReadOnly}
            resending={resend.isPending && resend.variables === delivery.id}
            onResend={() => void onResend(delivery)}
          />
        ))}
      </ul>
    );
  }

  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title={t("workspace.integrations.webhooks.deliveries.recent_deliveries")}
      description={webhook.name || webhook.url}>
      <SheetSection className="bg-slate-50">
        <WebhookHealthSummary health={webhook.health} />
      </SheetSection>
      <div className="flex items-start justify-between gap-4">
        <p className="text-xs text-slate-500">
          {t("workspace.integrations.webhooks.deliveries.retry_policy")}
        </p>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          loading={deliveries.isFetching && !deliveries.isPending}
          onClick={() => void refresh()}>
          {t("common.refresh")}
        </Button>
      </div>
      {list}
    </Sheet>
  );
};
