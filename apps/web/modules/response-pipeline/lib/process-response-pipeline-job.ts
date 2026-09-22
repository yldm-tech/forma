import "server-only";
import { createHash } from "node:crypto";
import { prisma } from "@forma/database";
import { PipelineTriggers, Prisma, type Webhook } from "@forma/database/prisma";
import { type JobHandler, type TResponsePipelineJobData, UnrecoverableError } from "@forma/jobs";
import { logger } from "@forma/logger";
import { type TLinkedEmbeddedField } from "@forma/types/embedded-data-resolver";
import { type TUserLocale, ZUserLocale } from "@forma/types/user";
import { DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS, POSTHOG_KEY } from "@/lib/constants";
import { generateStandardWebhookSignature } from "@/lib/crypto";
import { selectSurveyEmbeddedDataLinks, withInlinedEmbeddedFields } from "@/lib/embedded-data/survey-fields";
import { getIntegrations } from "@/lib/integration/service";
import { isDatabasePoolExhaustionError } from "@/lib/jobs/pool-exhaustion";
import { getResponseCountBySurveyId } from "@/lib/response/service";
import { sendTelemetryEvents } from "@/lib/telemetry/usage-update";
import { createPinnedDispatcher, validateAndResolveWebhookUrl } from "@/lib/utils/validate-webhook-url";
import { queueAuditEventWithoutRequest } from "@/modules/audit-logs/lib/handler";
import { type TAuditStatus, UNKNOWN_DATA } from "@/modules/audit-logs/types/audit-log";
import { recordResponseCreatedMeterEvent } from "@/modules/billing/lib/metering";
import { sendResponseFinishedEmail } from "@/modules/email";
import { captureSurveyResponsePostHogEvent } from "@/modules/response-pipeline/lib/posthog";
import { resolveStorageUrlsInObject } from "@/modules/storage/utils";
import { sendFollowUpsForResponse } from "@/modules/survey/follow-ups/lib/follow-ups";
import { FollowUpSendError } from "@/modules/survey/follow-ups/types/follow-up";
import { getFinishedResponseCountBySurveyId } from "@/modules/survey/lib/response";
import { dispatchWorkflowRunViaJobs } from "@/modules/workflows/lib/runner/dispatch";
import { enqueueResponseCompletedWorkflowRuns } from "@/modules/workflows/lib/runner/enqueue-response-completed-runs";
import { handleIntegrations } from "./handle-integrations";

const WEBHOOK_TIMEOUT_MS = 5_000;
const DEFAULT_NOTIFICATION_LOCALE: TUserLocale = "en-US";

const pipelineOrganizationSelect = {
  id: true,
  displayTimeZone: true,
  billing: {
    select: {
      stripeCustomerId: true,
    },
  },
} satisfies Prisma.OrganizationSelect;

const pipelineSurveySelect = {
  id: true,
  workspaceId: true,
  name: true,
  type: true,
  status: true,
  createdAt: true,
  updatedAt: true,
  blocks: true,
  hiddenFields: true,
  variables: true,
  // ENG-1837: the definitions the notification email, follow-ups and integrations below resolve
  // through. Inlined by `getSurveyForPipeline` so the raw relation never reaches the handlers.
  embeddedDataLinks: selectSurveyEmbeddedDataLinks,
  followUps: true,
  autoComplete: true,
  languages: {
    select: {
      default: true,
      enabled: true,
      language: {
        select: {
          id: true,
          code: true,
          alias: true,
          createdAt: true,
          updatedAt: true,
          workspaceId: true,
        },
      },
    },
  },
} satisfies Prisma.SurveySelect;

type TPipelineOrganization = Prisma.OrganizationGetPayload<{ select: typeof pipelineOrganizationSelect }>;
type TPipelineSurveyRow = Prisma.SurveyGetPayload<{ select: typeof pipelineSurveySelect }>;
type TPipelineSurvey = Omit<TPipelineSurveyRow, "embeddedDataLinks"> & {
  embeddedFields?: TLinkedEmbeddedField[];
};

const getOrganizationForPipeline = async (workspaceId: string): Promise<TPipelineOrganization | null> =>
  prisma.organization.findFirst({
    where: {
      workspaces: {
        some: {
          id: workspaceId,
        },
      },
    },
    select: pipelineOrganizationSelect,
  });

const getSurveyForPipeline = async (surveyId: string): Promise<TPipelineSurvey | null> => {
  const survey = await prisma.survey.findUnique({
    where: {
      id: surveyId,
    },
    select: pipelineSurveySelect,
  });

  return survey ? withInlinedEmbeddedFields(survey) : null;
};

const getPipelineLogContext = (
  data: TResponsePipelineJobData,
  context: Parameters<JobHandler<TResponsePipelineJobData>>[1]
) => ({
  attempt: context.attempt,
  workspaceId: data.workspaceId,
  event: data.event,
  jobId: context.jobId,
  jobName: context.jobName,
  maxAttempts: context.maxAttempts,
  queueName: context.queueName,
  responseId: data.response.id,
  surveyId: data.surveyId,
});

const toError = (error: unknown, fallbackMessage: string): Error =>
  error instanceof Error ? error : new Error(fallbackMessage);

const toUserLocale = (locale: string): TUserLocale => {
  const parsedLocale = ZUserLocale.safeParse(locale);
  return parsedLocale.success ? parsedLocale.data : DEFAULT_NOTIFICATION_LOCALE;
};

const createWebhookMessageId = ({
  event,
  jobId,
  webhookId,
}: {
  event: TResponsePipelineJobData["event"];
  jobId: string;
  webhookId: string;
}): string => createHash("sha256").update(`${jobId}:${webhookId}:${event}`).digest("hex");

type WebhookFetchOptions = RequestInit & {
  dispatcher?: ReturnType<typeof createPinnedDispatcher>;
};

const fetchWithTimeout = async (
  url: string,
  options: WebhookFetchOptions,
  timeoutMs: number = WEBHOOK_TIMEOUT_MS
): Promise<Response> => {
  const abortController = new AbortController();
  const signal = options.signal
    ? AbortSignal.any([options.signal, abortController.signal])
    : abortController.signal;
  const timeoutId = setTimeout(() => {
    abortController.abort(new Error("Timeout"));
  }, timeoutMs);

  try {
    return await fetch(url, {
      ...options,
      signal,
    } as RequestInit);
  } finally {
    clearTimeout(timeoutId);
  }
};

const getWebhooksForPipeline = async (
  workspaceId: string,
  event: PipelineTriggers,
  surveyId: string
): Promise<Webhook[]> => {
  return await prisma.webhook.findMany({
    where: {
      workspaceId,
      triggers: { has: event },
      OR: [{ surveyIds: { has: surveyId } }, { surveyIds: { isEmpty: true } }],
    },
  });
};

const createWebhookDeliveryTask = async ({
  webhook,
  data,
  survey,
  logContext,
}: {
  webhook: Webhook;
  data: TResponsePipelineJobData;
  survey: TPipelineSurvey;
  logContext: ReturnType<typeof getPipelineLogContext>;
}): Promise<void> => {
  try {
    const body = JSON.stringify({
      webhookId: webhook.id,
      event: data.event,
      data: {
        ...data.response,
        data: resolveStorageUrlsInObject(data.response.data),
        survey: {
          title: survey?.name,
          type: survey?.type,
          status: survey?.status,
          createdAt: survey?.createdAt,
          updatedAt: survey?.updatedAt,
        },
      },
    });

    const webhookMessageId = createWebhookMessageId({
      event: data.event,
      jobId: logContext.jobId,
      webhookId: webhook.id,
    });
    const webhookTimestamp = Math.floor(Date.now() / 1000);
    const requestHeaders: Record<string, string> = {
      "content-type": "application/json",
      "webhook-id": webhookMessageId,
      "webhook-timestamp": webhookTimestamp.toString(),
    };

    if (webhook.secret) {
      requestHeaders["webhook-signature"] = generateStandardWebhookSignature(
        webhookMessageId,
        webhookTimestamp,
        body,
        webhook.secret
      );
    }

    const address = await validateAndResolveWebhookUrl(webhook.url);
    // Pin TCP connect to the validated IP — closes DNS-rebinding TOCTOU between
    // validation and fetch. Skip pinning when address is null (DANGEROUSLY flag +
    // blocked name resolved via /etc/hosts).
    const dispatcher = address ? createPinnedDispatcher(address) : undefined;
    // `redirect: "manual"` blocks 30x-based SSRF to private/internal hosts.
    // Gated on the same env var as URL validation for self-hosters who opted in.
    const redirectMode: RequestRedirect = DANGEROUSLY_ALLOW_WEBHOOK_INTERNAL_URLS ? "follow" : "manual";

    try {
      const response = await fetchWithTimeout(webhook.url, {
        method: "POST",
        headers: requestHeaders,
        body,
        redirect: redirectMode,
        dispatcher,
      });

      // With `redirect: "manual"`, undici returns the actual 30x (not opaqueredirect).
      // Treat as delivery failure so redirect-based SSRF cannot silently succeed.
      if (response.status >= 300 && response.status < 400) {
        throw new Error(`Webhook delivery blocked: redirect status ${response.status}`);
      }

      if (!response.ok) {
        throw new Error(`Webhook delivery failed with status ${response.status}`);
      }
    } finally {
      try {
        await dispatcher?.destroy();
      } catch (cleanupError) {
        logger.warn(
          {
            ...logContext,
            err: cleanupError,
            webhookId: webhook.id,
            webhookUrl: webhook.url,
          },
          "Response pipeline webhook dispatcher cleanup failed"
        );
      }
    }
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
        webhookId: webhook.id,
        webhookUrl: webhook.url,
      },
      "Response pipeline webhook delivery failed"
    );
    throw error;
  }
};

const deliverWebhooks = async ({
  data,
  logContext,
  survey,
  webhooks,
}: {
  data: TResponsePipelineJobData;
  logContext: ReturnType<typeof getPipelineLogContext>;
  survey: TPipelineSurvey;
  webhooks: Webhook[];
}): Promise<void> => {
  const results = await Promise.allSettled(
    webhooks.map((webhook) =>
      createWebhookDeliveryTask({
        webhook,
        data,
        survey,
        logContext,
      })
    )
  );

  const failedResults = results.filter((result) => result.status === "rejected");
  if (failedResults.length === 0) {
    return;
  }

  if (logContext.attempt < logContext.maxAttempts) {
    throw toError(failedResults[0].reason, "Response pipeline webhook delivery failed");
  }

  logger.error(
    {
      ...logContext,
      failedWebhookCount: failedResults.length,
    },
    "Response pipeline webhook delivery exhausted retries; continuing with remaining side effects"
  );
};

const loadIntegrationsSafely = async ({
  logContext,
  workspaceId,
}: {
  logContext: ReturnType<typeof getPipelineLogContext>;
  workspaceId: string;
}): Promise<Awaited<ReturnType<typeof getIntegrations>>> => {
  try {
    return await getIntegrations(workspaceId);
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline integration lookup failed"
    );

    return [];
  }
};

/**
 * Response counts are optional side inputs: a failed lookup must never fail the job, so it
 * resolves to `null` and the consumer skips its own work instead.
 */
const loadResponseCountSafely = async ({
  count,
  failureMessage,
  logContext,
}: {
  count: () => Promise<number>;
  failureMessage: string;
  logContext: Record<string, unknown>;
}): Promise<number | null> => {
  try {
    return await count();
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      failureMessage
    );

    return null;
  }
};

const getUsersWithNotifications = async ({
  data,
  logContext,
  workspaceId,
}: {
  data: TResponsePipelineJobData;
  logContext: ReturnType<typeof getPipelineLogContext>;
  workspaceId: string;
}): Promise<Array<{ email: string; locale: TUserLocale }>> => {
  try {
    const users = await prisma.user.findMany({
      where: {
        memberships: {
          some: {
            organization: {
              workspaces: {
                some: {
                  id: workspaceId,
                },
              },
            },
          },
        },
        OR: [
          {
            memberships: {
              some: {
                role: {
                  in: ["owner", "manager"],
                },
                organization: {
                  workspaces: {
                    some: {
                      id: workspaceId,
                    },
                  },
                },
              },
            },
          },
          {
            teamUsers: {
              some: {
                team: {
                  workspaceTeams: {
                    some: {
                      workspace: {
                        id: workspaceId,
                      },
                    },
                  },
                },
              },
            },
          },
        ],
        notificationSettings: {
          path: ["alert", data.surveyId],
          equals: true,
        },
      },
      select: { email: true, locale: true },
    });

    return users.map((user) => ({
      email: user.email,
      locale: toUserLocale(user.locale),
    }));
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline notification recipient lookup failed"
    );

    return [];
  }
};

const handleFollowUpsSafely = async ({
  data,
  logContext,
  survey,
}: {
  data: TResponsePipelineJobData;
  logContext: ReturnType<typeof getPipelineLogContext>;
  survey: TPipelineSurvey;
}): Promise<void> => {
  if (!survey.followUps?.length) {
    return;
  }

  try {
    const followUpsResult = await sendFollowUpsForResponse(data.response.id, data.locale);
    if (!followUpsResult.ok && followUpsResult.error.code !== FollowUpSendError.FOLLOW_UP_NOT_ALLOWED) {
      logger.error(
        {
          ...logContext,
          error: followUpsResult.error,
        },
        "Response pipeline follow-up delivery failed"
      );
    }
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline follow-up delivery failed"
    );
  }
};

const sendNotificationEmailsSafely = async ({
  data,
  logContext,
  responseCount,
  survey,
  usersWithNotifications,
  workspaceId,
}: {
  data: TResponsePipelineJobData;
  logContext: ReturnType<typeof getPipelineLogContext>;
  responseCount: number | null;
  survey: TPipelineSurvey;
  usersWithNotifications: Array<{ email: string; locale: TUserLocale }>;
  workspaceId: string;
}): Promise<void> => {
  if (responseCount === null) {
    if (usersWithNotifications.length > 0) {
      logger.error(
        {
          ...logContext,
          notificationRecipientCount: usersWithNotifications.length,
        },
        "Response pipeline notification emails skipped because the response count could not be loaded"
      );
    }

    return;
  }

  // Bounded rather than one `Promise.all` over the whole list. Each send is a DB lookup, a React
  // email render and its own SMTP connection, so a survey whose workspace has many subscribed
  // members opened that many of each at once - on the worker, for every finished response.
  for (let start = 0; start < usersWithNotifications.length; start += NOTIFICATION_EMAIL_MAX_CONCURRENCY) {
    await Promise.all(
      usersWithNotifications.slice(start, start + NOTIFICATION_EMAIL_MAX_CONCURRENCY).map(async (user) => {
        try {
          await sendResponseFinishedEmail(
            user.email,
            user.locale,
            workspaceId,
            survey,
            data.response,
            responseCount
          );
        } catch (error) {
          logger.error(
            {
              ...logContext,
              err: error,
              userEmail: user.email,
            },
            "Response pipeline notification email failed"
          );
        }
      })
    );
  }
};

/**
 * The completed-response count that closes this survey, or `null` when the response limit
 * cannot apply — no limit configured, or the survey is already closed.
 *
 * The limit is defined in terms of *completed* responses, so only finished responses count
 * towards it. Counting every response would close the survey once the number of starts
 * (partial + finished) hit the limit.
 */
const getAutoCompleteThreshold = (survey: TPipelineSurvey): number | null =>
  survey.autoComplete && survey.status !== "completed" ? survey.autoComplete : null;

const handleSurveyAutoCompleteSafely = async ({
  finishedResponseCount,
  logContext,
  organizationId,
  survey,
}: {
  finishedResponseCount: number | null;
  logContext: ReturnType<typeof getPipelineLogContext>;
  organizationId: string;
  survey: TPipelineSurvey;
}): Promise<void> => {
  const autoCompleteThreshold = getAutoCompleteThreshold(survey);

  // `finishedResponseCount` is only looked up when a threshold applies, so a `null` here means
  // the lookup failed — already logged by loadResponseCountSafely.
  if (autoCompleteThreshold === null || finishedResponseCount === null) {
    return;
  }

  if (finishedResponseCount < autoCompleteThreshold) {
    return;
  }

  let logStatus: TAuditStatus = "success";

  try {
    // Status-guarded terminal write: only complete a survey whose status has not moved since the snapshot
    // this job read at the top. A 0-row result means the owner (or the scheduler) changed it while the
    // side-effects above were running — don't clobber that.
    const completed = await prisma.survey.updateMany({
      where: {
        id: survey.id,
        workspaceId: survey.workspaceId,
        status: survey.status,
      },
      data: {
        status: "completed",
      },
    });

    if (completed.count === 0) {
      logger.info(
        {
          ...logContext,
          snapshotStatus: survey.status,
        },
        "Survey status changed since the pipeline snapshot; skipping auto-complete"
      );

      return;
    }
  } catch (error) {
    logStatus = "failure";
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline survey auto-complete update failed"
    );
  }

  try {
    await queueAuditEventWithoutRequest({
      status: logStatus,
      action: "updated",
      targetType: "survey",
      userId: UNKNOWN_DATA,
      userType: "system",
      targetId: survey.id,
      organizationId,
      ...(logStatus === "success"
        ? {
            newObject: {
              status: "completed",
            },
          }
        : {}),
    });
  } catch (error) {
    logger.error(
      {
        ...logContext,
        auditStatus: logStatus,
        err: error,
      },
      "Response pipeline survey auto-complete audit log failed"
    );
  }
};

/** Upper bound on notification emails in flight at once; each one holds its own SMTP connection. */
const NOTIFICATION_EMAIL_MAX_CONCURRENCY = 5;

const runResponseFinishedSideEffects = async ({
  data,
  displayTimeZone,
  logContext,
  organizationId,
  stripeCustomerId,
  survey,
  workspaceId,
}: {
  data: TResponsePipelineJobData;
  displayTimeZone: string | null;
  logContext: ReturnType<typeof getPipelineLogContext>;
  organizationId: string;
  stripeCustomerId: string | null | undefined;
  survey: TPipelineSurvey;
  workspaceId: string;
}) => {
  const [integrations, usersWithNotifications] = await Promise.all([
    loadIntegrationsSafely({
      logContext,
      workspaceId,
    }),
    getUsersWithNotifications({
      data,
      logContext,
      workspaceId,
    }),
  ]);

  // Neither count is consumed until the notification/auto-complete steps at the end, so start
  // them here to overlap with the integration and follow-up work below. Each is skipped
  // entirely when nothing would consume it, keeping this off the hot path for the common case.
  const autoCompleteThreshold = getAutoCompleteThreshold(survey);

  const responseCountPromise =
    usersWithNotifications.length > 0
      ? loadResponseCountSafely({
          count: () => getResponseCountBySurveyId(data.surveyId),
          failureMessage: "Response pipeline response count lookup failed",
          logContext,
        })
      : Promise.resolve(null);

  const finishedResponseCountPromise =
    autoCompleteThreshold !== null
      ? loadResponseCountSafely({
          count: () => getFinishedResponseCountBySurveyId(survey.id),
          failureMessage:
            "Response pipeline survey auto-complete skipped because the finished response count could not be loaded",
          logContext: { ...logContext, autoCompleteThreshold },
        })
      : Promise.resolve(null);

  // Workflow runner (producer): enqueue runs for matching enabled workflows. Isolated so a runner
  // failure never breaks the response pipeline job, its retries, or the side-effects below.
  //
  // Ordered FIRST on purpose. This is the only step here that rethrows — transient DB pool
  // exhaustion must propagate so BullMQ retries the job — and it is also the only one built to be
  // replayed: `idempotencyKey = responseId`, a unique index behind it, and a deterministic jobId.
  // Everything after it appends without any per-response guard, so running them before a step that
  // asks for a retry meant one pool-exhaustion blip sent the respondent their follow-up twice,
  // notified members twice, and appended a duplicate row to every connected Sheet/Airtable/Notion
  // destination. With this first, a retry replays only idempotent work.
  try {
    await enqueueResponseCompletedWorkflowRuns({
      response: data.response,
      workspaceId,
      organizationId,
      stripeCustomerId,
      dispatch: dispatchWorkflowRunViaJobs,
      logContext,
    });
  } catch (error) {
    if (isDatabasePoolExhaustionError(error)) {
      throw error;
    }
    logger.error({ ...logContext, err: error }, "Response pipeline workflow run enqueue failed");
  }

  if (integrations.length > 0) {
    try {
      await handleIntegrations(integrations, data, survey, displayTimeZone ?? "UTC");
    } catch (error) {
      logger.error(
        {
          ...logContext,
          err: error,
        },
        "Response pipeline integration handling failed"
      );
    }
  }

  await handleFollowUpsSafely({
    data,
    logContext,
    survey,
  });

  await sendNotificationEmailsSafely({
    data,
    logContext,
    responseCount: await responseCountPromise,
    survey,
    usersWithNotifications,
    workspaceId,
  });

  await handleSurveyAutoCompleteSafely({
    finishedResponseCount: await finishedResponseCountPromise,
    logContext,
    organizationId,
    survey,
  });
};

const runResponseCreatedSideEffects = async ({
  data,
  logContext,
  organizationId,
  survey,
  stripeCustomerId,
}: {
  data: TResponsePipelineJobData;
  logContext: ReturnType<typeof getPipelineLogContext>;
  organizationId: string;
  survey: TPipelineSurvey;
  stripeCustomerId: string | null | undefined;
}) => {
  try {
    await recordResponseCreatedMeterEvent({
      stripeCustomerId,
      responseId: data.response.id,
      createdAt: data.response.createdAt,
    });
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline meter event failed"
    );
  }

  if (POSTHOG_KEY) {
    try {
      const responseCount = await getResponseCountBySurveyId(data.surveyId);
      captureSurveyResponsePostHogEvent({
        organizationId,
        surveyId: data.surveyId,
        surveyType: survey.type,
        workspaceId: data.workspaceId,
        responseCount,
      });
    } catch (error) {
      logger.error(
        {
          ...logContext,
          err: error,
        },
        "Response pipeline PostHog capture failed"
      );
    }
  }

  try {
    await sendTelemetryEvents();
  } catch (error) {
    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline telemetry dispatch failed"
    );
  }
};

export const processResponsePipelineJob: JobHandler<TResponsePipelineJobData> = async (data, context) => {
  const logContext = getPipelineLogContext(data, context);

  try {
    // `responseUpdated` fans out to nothing but webhook delivery — the two side-effect branches below are
    // keyed on the `responseFinished` and `responseCreated` literals — so with no webhook to render, the
    // organization row and the survey (whose `blocks` JSON is the largest read in this handler) are loaded
    // for nothing. A partial respondent generates one of these per page, so resolve the webhooks first for
    // that event alone and bail before the other two reads. The check is keyed on the explicit literal so a
    // new event kind added to `ZResponsePipelineEvent` falls through to the full path with its side-effects
    // intact, and every other event still loads all three concurrently.
    const webhooksOnlyEvent = data.event === "responseUpdated";
    const webhooksForWebhookOnlyEvent = webhooksOnlyEvent
      ? await getWebhooksForPipeline(data.workspaceId, data.event as PipelineTriggers, data.surveyId)
      : undefined;

    if (webhooksForWebhookOnlyEvent?.length === 0) {
      logger.debug(logContext, "Response pipeline job skipped: no webhooks subscribed to responseUpdated");
      return;
    }

    const [organization, survey, webhooks] = await Promise.all([
      getOrganizationForPipeline(data.workspaceId),
      getSurveyForPipeline(data.surveyId),
      webhooksForWebhookOnlyEvent ??
        getWebhooksForPipeline(data.workspaceId, data.event as PipelineTriggers, data.surveyId),
    ]);

    if (!survey) {
      throw new UnrecoverableError(`Survey ${data.surveyId} not found`);
    }

    if (!organization) {
      throw new UnrecoverableError(`Organization not found for workspace ${data.workspaceId}`);
    }

    if (survey.workspaceId !== data.workspaceId) {
      throw new UnrecoverableError(
        `Survey ${data.surveyId} does not belong to workspace ${data.workspaceId}`
      );
    }

    await deliverWebhooks({
      data,
      logContext,
      survey,
      webhooks,
    });

    if (data.event === "responseFinished") {
      await runResponseFinishedSideEffects({
        data,
        displayTimeZone: organization.displayTimeZone,
        logContext,
        organizationId: organization.id,
        stripeCustomerId: organization.billing?.stripeCustomerId,
        survey,
        workspaceId: data.workspaceId,
      });
    }

    if (data.event === "responseCreated") {
      await runResponseCreatedSideEffects({
        data,
        logContext,
        organizationId: organization.id,
        survey,
        stripeCustomerId: organization.billing?.stripeCustomerId,
      });
    }
  } catch (error) {
    if (isDatabasePoolExhaustionError(error)) {
      logger.warn(
        {
          ...logContext,
          err: error,
        },
        "Response pipeline job hit database pool exhaustion and will be retried"
      );
      throw error;
    }

    logger.error(
      {
        ...logContext,
        err: error,
      },
      "Response pipeline job failed"
    );
    throw error;
  }
};
