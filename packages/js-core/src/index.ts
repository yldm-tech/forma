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
import {
  type TAttributeValue,
  type TConfigInput,
  type TLegacyConfigInput,
  type TSetUserIdOptions,
} from "@/types/config";
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
    await queue.add("setup()", Setup.setup, CommandType.Setup, false, {
      ...setupConfig,
      // @ts-expect-error -- apiHost was in the older type
      ...(setupConfig.apiHost && { appUrl: setupConfig.apiHost as string }),
    });
  } else {
    await queue.add("setup()", Setup.setup, CommandType.Setup, false, setupConfig);
  }

  // wait for setup to complete
  await queue.wait();

  // Schedule checkPageUrl to run in the next event loop iteration. This ensures that any user actions (like setUserId) called synchronously after setup() will be queued BEFORE the page view actions are processed.
  // Through the queue, exactly like `registerRouteChange`, rather than called directly: the default setup check is what stops a failed setup (revoked workspaceId, forbidden fetch) from reaching `Config.get()` on a null config and throwing an uncaught rejection into the host page. An SDK that could not set itself up degrades to a console warning; it does not crash the page it is embedded in.
  setTimeout(() => {
    void queue.add("page view", checkPageUrl, CommandType.GeneralAction);
  }, 0);
};

/**
 * Identify the visitor as the contact with this userId.
 * @param userId - Your user's id, up to 255 characters
 * @param options - `userHash`: the HMAC-SHA256 of userId under the workspace's identity secret, in hex, computed by your server. A workspace that requires verified identities refuses an identify without it, and only a verified identify may set the email.
 */
const setUserId = async (userId: string, options?: TSetUserIdOptions): Promise<void> => {
  await queue.add("setUserId()", User.setUserId, CommandType.UserAction, true, userId, options);
};

/** Set the user's email. The server writes it only for a user identified with a valid userHash. */
const setEmail = async (email: string): Promise<void> => {
  await queue.add("setEmail()", Attribute.setAttributes, CommandType.UserAction, true, { email });
};

/**
 * Set one attribute on the identified user. The value's type decides the attribute's type when the key is new: a string, a number, or a Date (sent as an ISO 8601 string). An existing key only accepts values of its type; a mismatch is rejected by the server and logged as an error.
 */
const setAttribute = async (key: string, value: TAttributeValue): Promise<void> => {
  await queue.add("setAttribute()", Attribute.setAttributes, CommandType.UserAction, true, { [key]: value });
};

/** Set several attributes at once. Same value types and rules as setAttribute(). */
const setAttributes = async (attributes: Record<string, TAttributeValue>): Promise<void> => {
  await queue.add("setAttributes()", Attribute.setAttributes, CommandType.UserAction, true, attributes);
};

const setLanguage = async (language: string): Promise<void> => {
  await queue.add("setLanguage()", Attribute.setAttributes, CommandType.UserAction, true, { language });
};

const logout = async (): Promise<void> => {
  await queue.add("logout()", User.logout, CommandType.GeneralAction);
};

/**
 * @param code - The code of the action to track
 * @param properties - Optional properties to set, like the hidden fields (deprecated, hidden fields will be removed in a future version)
 */
const track = async (code: string, properties?: TTrackProperties): Promise<void> => {
  await queue.add(
    `track("${code}")`,
    Action.trackCodeAction,
    CommandType.GeneralAction,
    true,
    code,
    properties
  );
};

const registerRouteChange = async (): Promise<void> => {
  await queue.add("registerRouteChange()", checkPageUrl, CommandType.GeneralAction);
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
export type { TAttributeValue, TSetUserIdOptions } from "@/types/config";
export type { TFormaEventName, TFormaEventPayloads, TSetupFailureCode } from "@/lib/common/events";
export default forma;
