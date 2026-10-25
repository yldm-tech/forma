import { Config } from "@/lib/common/config";
import { JS_LOCAL_STORAGE_KEY } from "@/lib/common/constants";
import { addCleanupEventListeners, addEventListeners } from "@/lib/common/event-listeners";
import { FORMA_EVENTS, type TSetupFailureCode, emitFormaEvent } from "@/lib/common/events";
import { Logger } from "@/lib/common/logger";
import { getIsSetup, setIsSetup } from "@/lib/common/status";
import { TimeoutStack } from "@/lib/common/timeout-stack";
import {
  filterSurveys,
  getIsDebug,
  isNowExpired,
  normalizeAppUrl,
  toSurveyKeys,
  wrapThrows,
} from "@/lib/common/utils";
import { addLiveRegionContainer, closeSurvey, prefetchSurveysScript } from "@/lib/survey/widget";
import { DEFAULT_USER_STATE_NO_USER_ID } from "@/lib/user/state";
import { sendUpdatesToBackend, withUserHash } from "@/lib/user/update";
import { fetchWorkspaceState } from "@/lib/workspace/state";
import {
  type TConfig,
  type TConfigInput,
  type TLegacyConfig,
  type TUserState,
  type TWorkspaceState,
} from "@/types/config";
import {
  type MissingFieldError,
  type MissingPersonError,
  type NetworkError,
  type Result,
  err,
  okVoid,
} from "@/types/error";

const migrateLocalStorage = (): { changed: boolean; newState?: TLegacyConfig } => {
  const existingConfig = localStorage.getItem(JS_LOCAL_STORAGE_KEY);

  if (existingConfig) {
    let parsedConfig = JSON.parse(existingConfig) as TLegacyConfig;
    let changed = false;

    // Migrate intermediate format: environmentId → workspaceId, environment → workspace
    if (parsedConfig.environmentId ?? parsedConfig.environment) {
      const { environmentId, environment, ...rest } = parsedConfig;
      const workspace = environment
        ? (() => {
            const envData = environment.data as unknown as Record<string, unknown>;
            const migratedData = { ...envData };

            if (migratedData.project) {
              migratedData.settings = migratedData.project;
              delete migratedData.project;
            }

            if (migratedData.workspace) {
              migratedData.settings = migratedData.workspace;
              delete migratedData.workspace;
            }
            return { ...environment, data: migratedData };
          })()
        : undefined;

      parsedConfig = {
        ...rest,
        workspaceId: environmentId ?? (rest as unknown as TConfig).workspaceId,
        ...(workspace && { workspace }),
      } as TLegacyConfig;
      changed = true;
    }

    if (changed) {
      return { changed: true, newState: parsedConfig };
    }
  }

  return { changed: false };
};

/**
 * Rebuild a legacy `user` into a complete `TUserState`.
 *
 * A legacy blob is unchecked JSON: `data` can be absent, or present with missing fields or wrong
 * types. Spreading it over the default is not enough on its own — a spread only fills keys that are
 * absent, so a stored `displays: null` would win over the default and reach `filterSurveys`, which
 * calls `.filter` on it. The three fields it destructures as arrays are therefore type-checked.
 *
 * A repaired state is marked already-expired rather than trusted, because a half-empty one is not
 * merely incomplete, it is wrong: an identified user completed to `segments: []` is filtered down
 * to no surveys at all. Expiring it makes setup re-sync instead of rendering nothing.
 */
const completeLegacyUserState = (legacyUser: TLegacyConfig["user"]): TUserState => {
  const legacyData = legacyUser?.data;

  if (!legacyData) {
    return DEFAULT_USER_STATE_NO_USER_ID;
  }

  const defaults = DEFAULT_USER_STATE_NO_USER_ID.data;
  const wasRepaired =
    !Array.isArray(legacyData.segments) ||
    !Array.isArray(legacyData.displays) ||
    !Array.isArray(legacyData.responses);

  return {
    // `new Date(0)` reads as expired wherever `expiresAt` is checked, which is what forces the sync.
    expiresAt: wasRepaired ? new Date(0) : (legacyUser.expiresAt ?? null),
    data: {
      ...defaults,
      ...legacyData,
      segments: Array.isArray(legacyData.segments) ? legacyData.segments : defaults.segments,
      // Legacy states name surveys by id; the state names them by survey key now.
      displays: toSurveyKeys(legacyData.userId ?? null, legacyData.displays),
      responses: toSurveyKeys(legacyData.userId ?? null, legacyData.responses),
    },
  };
};

export const setup = async (
  configInput: TConfigInput | (TConfigInput & { userId: string; attributes: Record<string, string> })
): Promise<Result<void, MissingFieldError | NetworkError | MissingPersonError>> => {
  const isDebug = getIsDebug();
  const logger = Logger.getInstance();

  if (isDebug) {
    logger.configure({ logLevel: "debug" });
  }

  let config = Config.getInstance();

  const { changed, newState } = migrateLocalStorage();

  if (changed) {
    config.resetConfig();
    config = Config.getInstance();

    // Persist the migrated state for every legacy config, identified or not. `resetConfig()` has
    // just wiped storage, so skipping the write leaves no config at all and the fresh-setup path
    // below would silently downgrade an identified user to anonymous.
    if (newState) {
      config.update({ ...newState, user: completeLegacyUserState(newState.user) });
    }
  }

  if (getIsSetup()) {
    logger.debug("Already set up, skipping setup.");
    return okVoid();
  }

  logger.debug("Start setup");

  // Resolve effective ID: prefer workspaceId, fall back to environmentId
  const effectiveId = configInput.workspaceId ?? configInput.environmentId;

  if (!effectiveId) {
    const message =
      "setup() needs a workspaceId. Copy it from the workspace's app connection settings in Forma.";
    logger.error(message);
    emitSetupFailed(null, "missing_field", message);
    return err({
      code: "missing_field",
      field: "workspaceId",
    });
  }

  if (configInput.environmentId && !configInput.workspaceId) {
    logger.debug(
      "environmentId is deprecated and will be removed in a future version. Please use workspaceId instead."
    );
  }

  // Normalized once here and used for everything below (requests, the stored config, the error-state match), so `https://forma.example.com/` and `https://forma.example.com` are the same integration.
  const appUrl = configInput.appUrl ? normalizeAppUrl(configInput.appUrl) : "";

  if (!appUrl) {
    const message =
      "setup() needs an appUrl: the address of your Forma instance, e.g. https://forma.yldm.ai.";
    logger.error(message);
    emitSetupFailed(effectiveId, "missing_field", message);
    return err({
      code: "missing_field",
      field: "appUrl",
    });
  }

  let existingConfig: TConfig | undefined;
  try {
    existingConfig = config.get();
    logger.debug("Found existing configuration.");
  } catch {
    logger.debug("No existing configuration found.");
  }

  // An earlier first setup failed and put forma in an error state, which holds further attempts off for a while so a broken integration does not hit the server on every page load.
  if (existingConfig?.status.value === "error") {
    // The cool-down belongs to the workspaceId and appUrl that failed. A developer who fixes a typo in either is running a different setup and must not wait out the old one's cool-down. An error state written by an older SDK records neither, so it never matches and is dropped too.
    const isSameSetup = existingConfig.workspaceId === effectiveId && existingConfig.appUrl === appUrl;

    if (!isSameSetup) {
      logger.debug(
        "Forma is in an error state for a different workspaceId or appUrl. Resetting config and continuing."
      );
      config.resetConfig();
      existingConfig = undefined;
    } else if (isDebug) {
      logger.debug("Forma is in error state, but debug mode is active. Resetting config and continuing.");
      config.resetConfig();
      existingConfig = undefined;
    } else {
      console.error("🧱 Forma - Forma was set to an error state.");

      const expiresAt = existingConfig.status.expiresAt;

      if (expiresAt && !isNowExpired(new Date(expiresAt))) {
        const message = `Setup for workspace ${effectiveId} failed recently; skipping initialization until ${new Date(expiresAt).toISOString()}. Add ?formaDebug=true to the page URL to retry now.`;
        console.error(`🧱 Forma - Error state is not expired, skipping initialization. ${message}`);
        emitSetupFailed(effectiveId, "error_state", message);
        return okVoid();
      }
      console.error("🧱 Forma - Error state is expired. Continuing with initialization.");
    }
  }

  if (
    existingConfig?.workspace &&
    existingConfig.workspaceId === effectiveId &&
    existingConfig.appUrl === appUrl
  ) {
    logger.debug("Configuration fits setup parameters.");
    let isWorkspaceStateExpired = false;
    let isUserStateExpired = false;

    const workspaceExpiresAt = new Date(existingConfig.workspace.expiresAt);

    if (isNowExpired(workspaceExpiresAt)) {
      logger.debug("Workspace state expired. Syncing.");
      isWorkspaceStateExpired = true;
    }

    if (existingConfig.user.expiresAt && isNowExpired(new Date(existingConfig.user.expiresAt))) {
      logger.debug("User state expired. Syncing.");
      isUserStateExpired = true;
    }

    try {
      // fetch the workspace state (if expired)
      let workspace: TWorkspaceState = existingConfig.workspace;
      let userState: TUserState = existingConfig.user;

      if (isWorkspaceStateExpired || isDebug) {
        if (isDebug) {
          logger.debug("Debug mode is active, refetching workspace state");
        }

        const workspaceResponse = await fetchWorkspaceState({
          appUrl,
          workspaceId: effectiveId,
        });

        if (workspaceResponse.ok) {
          workspace = workspaceResponse.data;
          logger.debug(`Fetched ${workspace.data.surveys.length.toString()} surveys from the backend`);
        } else {
          const message = `Error fetching workspace state: ${workspaceResponse.error.code} - ${workspaceResponse.error.responseMessage ?? ""}`;
          logger.error(message);
          emitSetupFailed(effectiveId, failureCodeFor(workspaceResponse.error), message);
          return err({
            code: "network_error",
            message: "Error fetching workspace state",
            status: 500,
            url: new URL(`${appUrl}/api/v1/client/${effectiveId}/environment`),
            responseMessage: workspaceResponse.error.message,
          });
        }
      }

      if (isUserStateExpired || isDebug) {
        // If the existing person state (expired) has a userId, we need to fetch the person state
        // If the existing person state (expired) has no userId, we need to set the person state to the default

        if (isDebug) {
          logger.debug("Debug mode is active, refetching user state");
        }

        if (userState.data.userId) {
          const { userHash } = userState.data;
          const updatesResponse = await sendUpdatesToBackend({
            appUrl,
            workspaceId: effectiveId,
            updates: {
              userId: userState.data.userId,
              ...(userHash && { userHash }),
            },
          });

          if (updatesResponse.ok) {
            userState = withUserHash(updatesResponse.data.state, userHash);
          } else if (isIdentifyRefused(updatesResponse.error)) {
            // The workspace refuses this stored identity: it requires verified identities and the stored userHash is missing or was made with a secret since regenerated. Failing setup here would drop the page's own setUserId, the one call that can bring a valid hash, on every load until storage is cleared. Setup goes on with the user logged out, and that setUserId identifies again.
            logger.error(
              `The stored user ${userState.data.userId} was refused (${updatesResponse.error.responseMessage ?? updatesResponse.error.code}); continuing as an anonymous user until setUserId() identifies again.`
            );
            userState = DEFAULT_USER_STATE_NO_USER_ID;
          } else {
            const message = `Error updating user state: ${updatesResponse.error.code} - ${updatesResponse.error.responseMessage ?? ""}`;
            logger.error(message);
            emitSetupFailed(effectiveId, failureCodeFor(updatesResponse.error), message);
            return err({
              code: "network_error",
              message: "Error updating user state",
              status: 500,
              url: new URL(`${appUrl}/api/v1/client/${effectiveId}/update/contacts/${userState.data.userId}`),
              responseMessage: "Unknown error",
            });
          }
        } else if (!isDebug) {
          userState = DEFAULT_USER_STATE_NO_USER_ID;
        }
      }

      // filter the workspace state wrt the person state
      const filteredSurveys = filterSurveys(workspace, userState);

      // update the appConfig with the new filtered surveys and person state
      config.update({
        ...existingConfig,
        workspace,
        user: userState,
        filteredSurveys,
      });

      const surveyIds = filteredSurveys.map((s) => s.id);
      logger.debug(
        `${surveyIds.length.toString()} surveys could be shown to current user on trigger: ${surveyIds.join(", ")}`
      );
    } catch {
      logger.debug("Error during sync. Please try again.");
    }
  } else {
    logger.debug("No valid configuration found. Resetting config and creating new one.");
    config.resetConfig();
    logger.debug("Syncing.");

    // During setup, if we don't have a valid config, we need to fetch the workspace state
    // but not the person state, we can set it to the default value.
    // The person state will be fetched when the `setUserId` method is called.

    try {
      const workspaceResponse = await fetchWorkspaceState({
        appUrl,
        workspaceId: effectiveId,
      });

      if (!workspaceResponse.ok) {
        // eslint-disable-next-line @typescript-eslint/only-throw-error -- the catch below feeds this structured ApiErrorResponse (code/responseMessage) to handleErrorOnFirstSetup
        throw workspaceResponse.error;
      }

      let userState: TUserState = DEFAULT_USER_STATE_NO_USER_ID;

      // A migrated legacy config reaches this branch whenever it carried no workspace state to
      // reuse. It can still carry an identified user, so fall back to that id when setup was not
      // given one — otherwise the migration would turn an identified user into an anonymous one.
      // Scoped to the config the migration just wrote: another workspace or app URL is another
      // integration, and its user must not be carried across.
      const migratedUser =
        changed && existingConfig?.workspaceId === effectiveId && existingConfig.appUrl === appUrl
          ? existingConfig.user
          : null;
      const setupUserId = "userId" in configInput && configInput.userId ? configInput.userId : null;
      const userId = setupUserId ?? migratedUser?.data.userId ?? null;

      if (userId) {
        const updatesResponse = await sendUpdatesToBackend({
          appUrl,
          workspaceId: effectiveId,
          updates: {
            userId,
            // Attributes only ever come from the setup call, never from the migrated state.
            ...(setupUserId && "attributes" in configInput ? { attributes: configInput.attributes } : {}),
          },
        });

        if (updatesResponse.ok) {
          userState = updatesResponse.data.state;
        } else {
          logger.error(
            `Error updating user state: ${updatesResponse.error.code} - ${updatesResponse.error.responseMessage ?? ""}`
          );

          // The migration is one-shot: the config just written no longer looks legacy, so `changed`
          // is false on every later load and this id is gone for good if it is not kept now. Keep
          // the migrated state, expired, so the next load retries the sync instead of persisting
          // the default and stranding the user as anonymous.
          if (!setupUserId && migratedUser) {
            userState = { ...migratedUser, expiresAt: new Date(0) };
          }
        }
      }

      const workspace = workspaceResponse.data;
      logger.debug(`Fetched ${workspace.data.surveys.length.toString()} surveys from the backend`);
      const filteredSurveys = filterSurveys(workspace, userState);

      config.update({
        appUrl,
        workspaceId: effectiveId,
        user: userState,
        workspace,
        filteredSurveys,
      });

      const surveyIds = filteredSurveys.map((s) => s.id);
      logger.debug(
        `${surveyIds.length.toString()} surveys could be shown to current user on trigger: ${surveyIds.join(", ")}`
      );
    } catch (e) {
      await handleErrorOnFirstSetup(e as TFirstSetupError, { workspaceId: effectiveId, appUrl });
    }
  }

  logger.debug("Adding event listeners");
  addEventListeners();
  addCleanupEventListeners();

  // Mount the status live region now so assistive tech has registered it long before the
  // first survey announces its opening into it.
  addLiveRegionContainer();

  // Prefetch surveys script so it's warm in the cache when a survey triggers
  prefetchSurveysScript(appUrl);

  setIsSetup(true);
  logger.debug("Set up complete");

  // The readiness signal (ENG-1846): a consent-gated setup means `window.forma` may not exist
  // at page load, so a GTM tag firing `setEmbeddedData` on page load silently drops its value — the
  // host triggers on this event instead. Emitted here, at the single point every *fresh* setup
  // converges on, and nowhere else: the "already set up" and missing-config early returns above
  // return `okVoid()` without reaching this line, so a repeated `setup()` call cannot double-fire
  // the host's tags.
  // `effectiveId`, not `config.get().workspaceId`: the input is what this setup just ran with, and
  // it is already resolved through the legacy `environmentId` shim above.
  emitFormaEvent(FORMA_EVENTS.setupSuccessful, { workspaceId: effectiveId });

  return okVoid();
};

export const tearDown = (): void => {
  const logger = Logger.getInstance();
  const appConfig = Config.getInstance();

  const { workspace } = appConfig.get();
  const filteredSurveys = filterSurveys(workspace, DEFAULT_USER_STATE_NO_USER_ID);

  logger.debug("Setting user state to default");

  // clear the user state and set it to the default value
  appConfig.update({
    ...appConfig.get(),
    user: DEFAULT_USER_STATE_NO_USER_ID,
    filteredSurveys,
  });

  // Identity is gone, so a survey still waiting out its `delay` must not arrive after it. Without this the timeout fires against the new (or anonymous) user: it renders a survey that user never triggered, burns their recontact/display caps, and attributes the display and response to their contact id. Nothing pending was scheduled for the identity we are switching to, so cancelling all of it cannot drop a timeout that should still run.
  TimeoutStack.getInstance().clear();

  closeSurvey();
};

interface TFirstSetupError {
  code: string;
  status?: number;
  message?: string;
  responseMessage?: string;
}

const ERROR_STATE_DURATION_MS = 10 * 60000;

/**
 * A 4xx other than 429 means the server understood the request and rejected this workspaceId or appUrl: a configuration mistake that retrying will not fix, but that the developer will fix and reload. `forbidden` is mapped by the API client without always carrying its status.
 */
const isConfigurationError = (e: { code: string; status?: number }): boolean =>
  e.code === "forbidden" || (e.status !== undefined && e.status >= 400 && e.status < 500 && e.status !== 429);

/** An identify the server refused with 403: the workspace requires verified identities and the userHash sent with it does not verify the userId. */
const isIdentifyRefused = (e: { code: string; status?: number }): boolean =>
  e.code === "forbidden" || e.status === 403;

const failureCodeFor = (e: { code: string; status?: number }): TSetupFailureCode =>
  isConfigurationError(e) ? "invalid_config" : "network_error";

const emitSetupFailed = (workspaceId: string | null, code: TSetupFailureCode, message: string): void => {
  emitFormaEvent(FORMA_EVENTS.setupFailed, { workspaceId, code, message });
};

export const handleErrorOnFirstSetup = (
  e: TFirstSetupError,
  { workspaceId, appUrl }: { workspaceId: string; appUrl: string }
): Promise<never> => {
  const logger = Logger.getInstance();
  const reason = e.responseMessage ?? e.message ?? "Unknown error";

  if (isConfigurationError(e)) {
    // Not persisted: an error state would keep the corrected setup from running for ten minutes after the developer fixes the value and reloads. A wrong id costs one request per page load, which is what the server answered anyway.
    const status = e.status === undefined ? e.code : e.status.toString();
    const message = `Setup failed: ${appUrl} rejected workspaceId "${workspaceId}" (${status}: ${reason}). Check the workspaceId and appUrl passed to forma.setup(). Nothing was cached, so the next page load retries.`;
    logger.error(message);
    emitSetupFailed(workspaceId, "invalid_config", message);
    throw new Error("Could not set up forma");
  }

  const message = `Error during first setup: ${e.code} - ${reason}. Retrying in 10 minutes, or on the next page load if the workspaceId or appUrl change.`;
  logger.error(message);

  // Put forma in an error state (by creating a new config) so a server that is down or unreachable is not hit again on every page load. The state records which setup failed: only that same workspaceId and appUrl wait out the cool-down.
  const initialErrorConfig: Partial<TConfig> = {
    workspaceId,
    appUrl,
    status: {
      value: "error",
      expiresAt: new Date(new Date().getTime() + ERROR_STATE_DURATION_MS),
    },
  };

  wrapThrows(() => {
    localStorage.setItem(JS_LOCAL_STORAGE_KEY, JSON.stringify(initialErrorConfig));
  })();

  emitSetupFailed(workspaceId, "network_error", message);
  throw new Error("Could not set up forma");
};
