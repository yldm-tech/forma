import { logger } from "@forma/logger";
import { AUDIT_LOG_ENABLED, AUDIT_LOG_GET_USER_IP } from "@/lib/constants";
import { ActionClientCtx, AuditLoggingCtx } from "@/lib/utils/action-client/types/context";
import { getClientIpFromHeaders } from "@/lib/utils/client-ip";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { deepDiff, redactPII } from "@/lib/utils/logger-helpers";
import { logAuditEvent } from "@/modules/audit-logs/lib/service";
import {
  TActor,
  TAuditAction,
  TAuditLogEvent,
  TAuditStatus,
  TAuditTarget,
  UNKNOWN_DATA,
} from "@/modules/audit-logs/types/audit-log";
import { getIsAuditLogsEnabled } from "@/modules/license-check/lib/utils";

export type TAuditEventInput = {
  action: TAuditAction;
  targetType: TAuditTarget;
  userId: string;
  userType: TActor;
  targetId: string;
  organizationId: string;
  status: TAuditStatus;
  oldObject?: Record<string, unknown> | null;
  newObject?: Record<string, unknown> | null;
  eventId?: string;
  apiUrl?: string;
};

type TBuildAuditEventInput = TAuditEventInput & {
  ipAddress: string;
};

/** The `AuditLoggingCtx` fields that carry a resource id, i.e. the ones a `target.id` can be read from. */
type TAuditLoggingCtxIdField = {
  [K in keyof AuditLoggingCtx]-?: NonNullable<AuditLoggingCtx[K]> extends string ? K : never;
}[keyof AuditLoggingCtx];

/**
 * The `auditLoggingCtx` field `withAuditLogging` reads `target.id` out of, per target type. `null` marks a target that never reaches the wrapper because it is only ever emitted from an API route or a worker calling `queueAuditEvent*` with an explicit `targetId`.
 *
 * A `Record` over the whole union rather than a `switch` on purpose: TypeScript fails the build when a member is added to `ZAuditTarget` and left out here, where a `switch` fell through to its `default` and wrote `target.id: "unknown"` — a record naming neither the team nor the user it was about. That is how "team", "twoFactorAuth" and "contactAttributeKey" went unattributable for as long as they did.
 */
const AUDIT_TARGET_ID_CTX_FIELD: Record<TAuditTarget, TAuditLoggingCtxIdField | null> = {
  actionClass: "actionClassId",
  apiKey: "apiKeyId",
  contact: "contactId",
  contactAttributeKey: "contactAttributeKeyId",
  cubeQuery: null,
  file: null,
  integration: "integrationId",
  invite: "inviteId",
  language: "languageId",
  membership: "membershipId",
  organization: "organizationId",
  quota: "quotaId",
  response: "responseId",
  segment: "segmentId",
  survey: "surveyId",
  tag: "tagId",
  team: "teamId",
  // The subject of a 2FA change is the user whose second factor it is, so the wrapper reports the user id.
  twoFactorAuth: "userId",
  user: "userId",
  webhook: "webhookId",
  workflow: null,
  workspace: "workspaceId",
  workspaceTeam: null,
};

/** Resolves the audited resource's id from the action context, falling back to `UNKNOWN_DATA` when the handler did not record one. */
export const resolveAuditTargetId = (targetType: TAuditTarget, auditLoggingCtx: AuditLoggingCtx): string => {
  const field = AUDIT_TARGET_ID_CTX_FIELD[targetType];
  return (field ? auditLoggingCtx[field] : undefined) ?? UNKNOWN_DATA;
};

/**
 * Builds an audit event and logs it.
 * Redacts sensitive data from the old and new objects before logging.
 */
export const buildAndLogAuditEvent = async ({
  action,
  targetType,
  userId,
  userType,
  targetId,
  organizationId,
  ipAddress,
  status,
  oldObject,
  newObject,
  eventId,
  apiUrl,
}: TBuildAuditEventInput) => {
  if (!AUDIT_LOG_ENABLED && !(await getIsAuditLogsEnabled())) {
    return;
  }

  try {
    let changes;

    if (oldObject && newObject) {
      changes = deepDiff(oldObject, newObject);
      changes = redactPII(changes);
    } else if (newObject) {
      changes = redactPII(newObject);
    } else if (oldObject) {
      changes = redactPII(oldObject);
    }

    const auditEvent: TAuditLogEvent = {
      actor: { id: userId, type: userType },
      action,
      target: { id: targetId, type: targetType },
      timestamp: new Date().toISOString(),
      organizationId,
      status,
      ipAddress: AUDIT_LOG_GET_USER_IP ? ipAddress : UNKNOWN_DATA,
      apiUrl,
      ...(changes ? { changes } : {}),
      ...(status === "failure" && eventId ? { eventId } : {}),
    };

    await logAuditEvent(auditEvent);
  } catch (logError) {
    logger.error(logError, "Failed to create audit log event");
  }
};

/**
 * Logs an audit event.
 * The audit logging runs in the background to avoid blocking the main request.
 */
export const queueAuditEventBackground = async ({
  action,
  targetType,
  userId,
  userType,
  targetId,
  organizationId,
  oldObject,
  newObject,
  status,
  eventId,
  apiUrl,
}: TAuditEventInput) => {
  setImmediate(async () => {
    const ipAddress = await getClientIpFromHeaders();
    await buildAndLogAuditEvent({
      action,
      targetType,
      userId,
      userType,
      targetId,
      organizationId,
      ipAddress,
      status,
      oldObject,
      newObject,
      eventId,
      apiUrl,
    });
  });
};

/**
 * Logs an audit event.
 * This function will block the main request. Use it only in edge runtime functions, like api routes.
 */
export const queueAuditEvent = async ({
  action,
  targetType,
  userId,
  userType,
  targetId,
  organizationId,
  oldObject,
  newObject,
  status,
  eventId,
  apiUrl,
}: TAuditEventInput) => {
  const ipAddress = await getClientIpFromHeaders();

  await buildAndLogAuditEvent({
    action,
    targetType,
    userId,
    userType,
    targetId,
    organizationId,
    ipAddress,
    status,
    oldObject,
    newObject,
    eventId,
    apiUrl,
  });
};

/**
 * Logs an audit event without reading request headers.
 * Use this from background workers or other contexts without a request lifecycle.
 */
export const queueAuditEventWithoutRequest = async ({
  action,
  targetType,
  userId,
  userType,
  targetId,
  organizationId,
  oldObject,
  newObject,
  status,
  eventId,
  apiUrl,
  ipAddress = UNKNOWN_DATA,
}: TAuditEventInput & { ipAddress?: string }) => {
  await buildAndLogAuditEvent({
    action,
    targetType,
    userId,
    userType,
    targetId,
    organizationId,
    ipAddress,
    status,
    oldObject,
    newObject,
    eventId,
    apiUrl,
  });
};

/**
 * Wraps a handler function with audit logging.
 * Logs audit events for server actions. Specifically for server actions that use next-server-action library middleware and its context.
 * The audit logging runs in the background to avoid blocking the main request.
 *
 * @param action - The type of action to audit.
 * @param targetType - The type of target (e.g., "segment", "survey").
 * @param handler - The handler function to wrap. It can be used with both authenticated and unauthenticated actions.
 **/
export const withAuditLogging = <
  TCtx extends ActionClientCtx = ActionClientCtx,
  TParsedInput = Record<string, unknown>,
  TResult = unknown,
>(
  action: TAuditAction,
  targetType: TAuditTarget,
  handler: (args: { ctx: TCtx; parsedInput: TParsedInput }) => Promise<TResult>
) => {
  return async function wrappedAction(args: { ctx: TCtx; parsedInput: TParsedInput }): Promise<TResult> {
    const { ctx, parsedInput } = args;
    const { auditLoggingCtx } = ctx;
    let result!: TResult;
    let status: TAuditStatus = "success";
    let error: any = undefined;

    try {
      result = await handler(args);
    } catch (err) {
      status = "failure";
      error = err;
    }

    if (!AUDIT_LOG_ENABLED) {
      if (status === "failure") throw error;
      return result;
    }

    if (!auditLoggingCtx) {
      logger.error("No audit logging context found");
      return result;
    }

    // The handler ran fine but reported that the audited thing did not actually happen, so `action`
    // here would be a false record (see AuditLoggingCtx.suppressEvent). Deliberately gated on success:
    // a failure is always audited, so this cannot be used to hide one.
    if (status === "success" && auditLoggingCtx.suppressEvent) {
      return result;
    }

    setImmediate(async () => {
      try {
        const userId: string = ctx?.user?.id ?? UNKNOWN_DATA;
        let organizationId =
          auditLoggingCtx?.organizationId || // NOSONAR // We want to use the organizationId from the auditLoggingCtx if it is present and not empty
          (parsedInput as Record<string, any>)?.organizationId || // NOSONAR // We want to use the organizationId from the parsedInput if it is present and not empty
          UNKNOWN_DATA;

        if (!organizationId) {
          const workspaceId = (parsedInput as Record<string, any>)?.workspaceId;
          if (workspaceId && typeof workspaceId === "string") {
            try {
              organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);
            } catch (err) {
              logger.error(err, "Failed to get organizationId from workspaceId in audit logging");
              organizationId = UNKNOWN_DATA;
            }
          } else {
            organizationId = UNKNOWN_DATA;
          }
        }

        const targetId = resolveAuditTargetId(targetType, auditLoggingCtx);

        await buildAndLogAuditEvent({
          action,
          targetType,
          userId,
          userType: "user",
          targetId,
          organizationId,
          ipAddress: AUDIT_LOG_GET_USER_IP ? auditLoggingCtx.ipAddress : UNKNOWN_DATA,
          status,
          oldObject: auditLoggingCtx.oldObject,
          newObject: auditLoggingCtx.newObject,
          eventId: auditLoggingCtx.eventId,
        });
      } catch (logError) {
        logger.error(logError, "Failed to create audit log event");
      }
    });

    if (status === "failure") throw error;
    return result;
  };
};
