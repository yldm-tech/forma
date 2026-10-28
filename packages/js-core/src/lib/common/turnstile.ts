import { TURNSTILE_SCRIPT_ID, TURNSTILE_SCRIPT_URL } from "@/lib/common/constants";
import { Logger } from "./logger";

/**
 * Loads Cloudflare Turnstile's script before a survey with spam protection on is shown, as the reCAPTCHA script is: the surveys runtime renders the widget (packages/surveys/src/lib/turnstile.ts) and finds the script already there. A tag another loader added and that is still loading is waited for; a tag that failed is removed, so a later survey tries again.
 */
export const loadTurnstileScript = (): Promise<void> => {
  const logger = Logger.getInstance();

  return new Promise((resolve, reject) => {
    const turnstileWindow = window as Window & { turnstile?: unknown };
    if (turnstileWindow.turnstile) {
      logger.debug("Turnstile script already loaded");
      resolve();
      return;
    }
    const existing = document.getElementById(TURNSTILE_SCRIPT_ID) as HTMLScriptElement | null;
    const script = existing ?? document.createElement("script");
    const fail = () => {
      script.remove();
      logger.debug("Error loading Turnstile script");
      reject(new Error("Error loading Turnstile script"));
    };
    script.addEventListener(
      "load",
      () => {
        if (!turnstileWindow.turnstile) {
          fail();
          return;
        }
        logger.debug("Turnstile script loaded successfully");
        resolve();
      },
      { once: true }
    );
    script.addEventListener("error", fail, { once: true });
    if (!existing) {
      script.id = TURNSTILE_SCRIPT_ID;
      script.src = TURNSTILE_SCRIPT_URL;
      script.async = true;
      document.head.appendChild(script);
    }
  });
};
