import { CommandQueue, CommandType } from "@/lib/common/command-queue";
import {
  type TFormaEventName,
  type TFormaEventPayloads,
  offFormaEvent,
  onFormaEvent,
} from "@/lib/common/events";
import * as Setup from "@/lib/common/setup";
import { getIsDebug } from "@/lib/common/utils";
import * as Action from "@/lib/survey/action";
import { EmbeddedDataStore, type TEmbeddedDataInput } from "@/lib/survey/embedded-data";
import { checkPageUrl } from "@/lib/survey/no-code-action";
import * as Attribute from "@/lib/user/attribute";
import * as User from "@/lib/user/user";
import { type TConfigInput, type TLegacyConfigInput } from "@/types/config";
import { type TTrackProperties } from "@/types/survey";

const queue = CommandQueue.getInstance();

const setup = async (setupConfig: TConfigInput): Promise<void> => {
  // If the initConfig has a userId or attributes, we need to use the legacy init

  if (
    // @ts-expect-error -- userId and attributes were in the older type
    setupConfig.userId ||
    // @ts-expect-error -- attributes were in the older type
    setupConfig.attributes ||
    // @ts-expect-error -- apiHost was in the older type
    setupConfig.apiHost
  ) {
    const isDebug = getIsDebug();
    if (isDebug) {
      console.warn("🧱 Forma - Warning: Using legacy init");
    }
    await queue.add(Setup.setup, CommandType.Setup, false, {
      ...setupConfig,
      // @ts-expect-error -- apiHost was in the older type
      ...(setupConfig.apiHost && { appUrl: setupConfig.apiHost as string }),
    });
  } else {
    await queue.add(Setup.setup, CommandType.Setup, false, setupConfig);
  }

  // wait for setup to complete
  await queue.wait();

  // Schedule checkPageUrl to run in the next event loop iteration. This ensures that any user actions (like setUserId) called synchronously after setup() will be queued BEFORE the page view actions are processed.
  // Through the queue, exactly like `registerRouteChange`, rather than called directly: the default setup check is what stops a failed setup (revoked workspaceId, forbidden fetch) from reaching `Config.get()` on a null config and throwing an uncaught rejection into the host page. An SDK that could not set itself up degrades to a console warning; it does not crash the page it is embedded in.
  setTimeout(() => {
    void queue.add(checkPageUrl, CommandType.GeneralAction);
  }, 0);
};

const setUserId = async (userId: string): Promise<void> => {
  await queue.add(User.setUserId, CommandType.UserAction, true, userId);
};

const setEmail = async (email: string): Promise<void> => {
  await queue.add(Attribute.setAttributes, CommandType.UserAction, true, { email });
};

const setAttribute = async (key: string, value: string): Promise<void> => {
  await queue.add(Attribute.setAttributes, CommandType.UserAction, true, { [key]: value });
};

const setAttributes = async (attributes: Record<string, string>): Promise<void> => {
  await queue.add(Attribute.setAttributes, CommandType.UserAction, true, attributes);
};

const setLanguage = async (language: string): Promise<void> => {
  await queue.add(Attribute.setAttributes, CommandType.UserAction, true, { language });
};

const logout = async (): Promise<void> => {
  await queue.add(User.logout, CommandType.GeneralAction);
};

/**
 * @param code - The code of the action to track
 * @param properties - Optional properties to set, like the hidden fields (deprecated, hidden fields will be removed in a future version)
 */
const track = async (code: string, properties?: TTrackProperties): Promise<void> => {
  await queue.add(Action.trackCodeAction, CommandType.GeneralAction, true, code, properties);
};

const registerRouteChange = async (): Promise<void> => {
  await queue.add(checkPageUrl, CommandType.GeneralAction);
};

/**
 * Attach Embedded Data to future responses without tying it to a trigger (ENG-1844). Merges into the
 * in-memory bag, last write wins per key; `{ key: null }` removes a key and `undefined` values are
 * skipped. Values land only on the survey's declared *ingested* fields — anything else is dropped
 * and logged by the renderer, never fatal.
 *
 * Synchronous and network-free on purpose (like `setNonce`, unlike the queued methods): calling it
 * on every SPA route change is free, and routing it through the command queue would silently drop
 * calls made before `setup()` completes — the exact failure the `forma_setup_successful`
 * readiness event exists to prevent (ENG-1846).
 */
const setEmbeddedData = (data: TEmbeddedDataInput): void => {
  EmbeddedDataStore.getInstance().setEmbeddedData(data);
};

/**
 * Remove one Embedded Data key, or clear the whole bag when called with no argument — logout, or a
 * hard context switch. Synchronous, no network. A key that evaluated to `undefined` is a no-op, not
 * a full clear: the arity is forwarded, so only a literal zero-argument call wipes everything.
 */
const clearEmbeddedData = (...args: [] | [key: string]): void => {
  EmbeddedDataStore.getInstance().clearEmbeddedData(...args);
};

/**
 * Subscribe to a Forma event (ENG-1814).
 *
 * The host application is notified about what the SDK actually did — a survey reached the screen
 * (`forma_survey_shown`), was answered (`forma_response_submitted`, with the persisted
 * `responseId` and a `finished` flag), was dismissed or completed (`forma_survey_closed`), an
 * action was tracked, or setup finished — so it can drive frequency capping and analytics off
 * reality rather than off the `track()` calls it made. The same events, under the same names, go
 * out as `window.dataLayer` pushes for Google Tag Manager.
 *
 * Subscriptions are independent of setup(): registering before or after setup() both work, and a
 * handler stays registered across logout() until it is removed.
 *
 * @param event - Full event name, e.g. "forma_survey_shown"
 * @param handler - Called with that event's payload (survey id, and where it applies the response id)
 * @returns A function that removes this subscription. `off()` with the same arguments does the same.
 */
const on = <E extends TFormaEventName>(
  event: E,
  handler: (payload: TFormaEventPayloads[E]) => void
): (() => void) => onFormaEvent(event, handler);

/**
 * Remove a subscription registered with on().
 *
 * @param event - The event name the handler was registered for
 * @param handler - The same function reference that was passed to on()
 */
const off = <E extends TFormaEventName>(
  event: E,
  handler: (payload: TFormaEventPayloads[E]) => void
): void => {
  offFormaEvent(event, handler);
};

/**
 * Set the CSP nonce for inline styles
 * @param nonce - The CSP nonce value (without 'nonce-' prefix), or undefined to clear
 */
const setNonce = (nonce: string | undefined): void => {
  // Store nonce on window for access when surveys package loads
  globalThis.window.__formaNonce = nonce;

  // Set nonce in surveys package if it's already loaded

  globalThis.window.formaSurveys?.setNonce?.(nonce);
};

const forma = {
  /** @deprecated Use setup() instead. This method will be removed in a future version */
  init: (initConfig: TLegacyConfigInput) => setup(initConfig as unknown as TConfigInput),
  setup,
  setEmail,
  setAttribute,
  setAttributes,
  setLanguage,
  setUserId,
  track,
  logout,
  registerRouteChange,
  setNonce,
  setEmbeddedData,
  clearEmbeddedData,
  on,
  off,
};

// Explicitly assign to globalThis so the wrapper SDK (@forma/js) can
// find us even when the UMD environment detection is fooled by a leaked
// `exports` or `module` global on the page (e.g. from another UMD bundle,
// a tag manager, or a browser extension).  This runs inside the UMD factory,
// so it executes regardless of which branch the wrapper picks.
(globalThis as unknown as Record<string, unknown>).forma = forma;

type TForma = typeof forma;
export type { TForma };
export type { TFormaEventName, TFormaEventPayloads } from "@/lib/common/events";
export default forma;
