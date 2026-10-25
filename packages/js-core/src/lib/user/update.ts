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

/**
 * The attribute keys the server reports it did not write, read from its `messages` and `errors`. A type mismatch is reported in `messages` by servers that predate it moving to `errors`, so both are read. `invalid_language_ignored` names the rejected value rather than the key, and always concerns `language`.
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
    if (text.startsWith("Ignored invalid language code ")) {
      skipped.add("language");
    }
  }

  return skipped;
};

export const sendUpdates = async ({
  updates,
}: {
  updates: TUpdates;
}): Promise<Result<{ skippedAttributeKeys: Set<string> }, ApiErrorResponse>> => {
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

    // Log informational messages (debug only)
    messages?.forEach((message) => {
      logger.debug(`User update message: ${message}`);
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

    return ok({ skippedAttributeKeys: getSkippedAttributeKeys(messages, errors) });
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
