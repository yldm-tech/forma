import { ApiClient } from "@/lib/common/api";
import { Config } from "@/lib/common/config";
import { Logger } from "@/lib/common/logger";
import { filterSurveys, getIsDebug } from "@/lib/common/utils";
import { prefetchSurveysScript } from "@/lib/survey/widget";
import { type TUpdates, type TUserState } from "@/types/config";
import { type ApiErrorResponse, type Result, type ResultError, err, ok } from "@/types/error";

export const sendUpdatesToBackend = async ({
  appUrl,
  workspaceId,
  updates,
}: {
  appUrl: string;
  workspaceId: string;
  updates: TUpdates;
}): Promise<
  Result<
    {
      state: TUserState;
      messages?: string[];
      errors?: string[];
    },
    ApiErrorResponse
  >
> => {
  const url = `${appUrl}/api/v1/client/${workspaceId}/user`;

  try {
    const api = new ApiClient({
      appUrl,
      workspaceId,
      isDebug: getIsDebug(),
    });

    const response = await api.createOrUpdateUser({
      userId: updates.userId,
      attributes: updates.attributes,
    });

    if (!response.ok) {
      return err({
        code: response.error.code,
        status: response.error.status,
        message: `Error updating user with userId ${updates.userId}`,
        url: new URL(url),
        responseMessage: response.error.message,
      });
    }

    return ok(response.data);
  } catch (e) {
    const errorTyped = e as { message?: string };

    const error = err({
      code: "network_error",
      message: errorTyped.message ?? "Error fetching the person state",
      status: 500,
      url: new URL(url),
      responseMessage: errorTyped.message ?? "Unknown error",
    });

    return error as ResultError<ApiErrorResponse>;
  }
};

// The server reports skipped attributes only as English text (apps/server/internal/contact/identify.go messageTemplates), so the keys are read back out of the templates that name them.
const SKIPPED_KEY_PATTERNS: { pattern: RegExp; isList: boolean }[] = [
  // attribute_type_validation_error: "{error} (attribute '{key}' has dataType: {dataType})"
  { pattern: /\(attribute '(.+?)' has dataType: /g, isList: false },
  // invalid_attribute_keys: "Skipped creating attribute(s) with invalid key(s): {keys}. Keys must ..."
  { pattern: /with invalid key\(s\): (.+?)\. Keys must/g, isList: true },
  // reserved_attribute_keys: "Reserved attribute key(s): {keys}. These keys are reserved ..."
  { pattern: /Reserved attribute key\(s\): (.+?)\. These keys/g, isList: true },
];

// Messages that name the attribute they concern only by meaning, not by key.
const SKIPPED_KEY_PREFIXES: { prefix: string; key: string }[] = [
  // invalid_language_ignored names the rejected value, and always concerns `language`.
  { prefix: "Ignored invalid language code ", key: "language" },
  // email_already_exists / userid_already_exists: the value belongs to another contact and was not written.
  { prefix: "The email already exists for this workspace", key: "email" },
  { prefix: "The userId already exists for this workspace", key: "userId" },
];

// attribute_limit_exceeded: "Could not create {count} new attribute(s) as it would exceed the maximum limit of {limit} attribute classes. ..." The server creates none of the new keys and names none of them, so the SDK cannot tell which of the keys it sent were new.
const ATTRIBUTE_LIMIT_PATTERN = /^Could not create \d+ new attribute/;

export const isAttributeLimitExceeded = (text: string): boolean => ATTRIBUTE_LIMIT_PATTERN.test(text);

// attribute_type_validation_error, the one write refusal servers before the type-mismatch move to `errors` still send in `messages`.
const isTypeMismatch = (text: string): boolean => /\(attribute '.+?' has dataType: /.test(text);

/**
 * The attribute keys the server reports it did not write, read from its `messages` and `errors`. A type mismatch is reported in `messages` by servers that predate it moving to `errors`, so both are read.
 */
export const getSkippedAttributeKeys = (messages: string[] = [], errors: string[] = []): Set<string> => {
  const skipped = new Set<string>();

  for (const text of [...messages, ...errors]) {
    for (const { pattern, isList } of SKIPPED_KEY_PATTERNS) {
      for (const match of text.matchAll(pattern)) {
        const named = isList ? match[1].split(", ") : [match[1]];
        named.forEach((key) => skipped.add(key));
      }
    }
    for (const { prefix, key } of SKIPPED_KEY_PREFIXES) {
      if (text.startsWith(prefix)) {
        skipped.add(key);
      }
    }
  }

  return skipped;
};

export const sendUpdates = async ({
  updates,
}: {
  updates: TUpdates;
}): Promise<
  Result<{ skippedAttributeKeys: Set<string>; attributeLimitExceeded: boolean }, ApiErrorResponse>
> => {
  const config = Config.getInstance();
  const logger = Logger.getInstance();

  const { appUrl, workspaceId } = config.get();
  // update endpoint call
  const url = `${appUrl}/api/v1/client/${workspaceId}/user`;

  try {
    const updatesResponse = await sendUpdatesToBackend({ appUrl, workspaceId, updates });

    if (!updatesResponse.ok) {
      return err(updatesResponse.error);
    }

    const userState = updatesResponse.data.state;
    const filteredSurveys = filterSurveys(config.get().workspace, userState);

    // messages => informational debug messages (e.g., "email already exists")
    // errors => error messages that should always be visible (e.g., invalid attribute keys)
    const { messages, errors } = updatesResponse.data;

    // Log errors (always visible)
    errors?.forEach((error) => {
      logger.error(error);
    });

    // Log informational messages (debug only). A message that reports a value the server refused to store (a type mismatch from a server that still sends it in `messages`, or new keys refused by the attribute limit) is logged as an error instead: the caller asked for that value to be stored, and a debug-only line would leave the failure silent outside debug mode.
    messages?.forEach((message) => {
      if (isTypeMismatch(message) || isAttributeLimitExceeded(message)) {
        logger.error(message);
      } else {
        logger.debug(`User update message: ${message}`);
      }
    });

    config.update({
      ...config.get(),
      user: {
        ...userState,
      },
      filteredSurveys,
    });

    // Identification can turn an empty survey set non-empty, which is the case setup skipped the
    // prefetch for. Cheap and idempotent when it already ran.
    prefetchSurveysScript(appUrl);

    return ok({
      skippedAttributeKeys: getSkippedAttributeKeys(messages, errors),
      attributeLimitExceeded: [...(messages ?? []), ...(errors ?? [])].some(isAttributeLimitExceeded),
    });
  } catch (e) {
    console.error("error in sending updates: ", e);

    if ("code" in (e as ApiErrorResponse)) {
      const errorTyped = e as ApiErrorResponse;
      return err({
        code: errorTyped.code,
        message: errorTyped.message,
        status: errorTyped.status,
        url: new URL(url),
        responseMessage: errorTyped.responseMessage,
      });
    }

    return err({
      code: "network_error",
      message: "Error sending updates",
      status: 500,
      url: new URL(url),
      responseMessage: "Unknown error",
    });
  }
};
