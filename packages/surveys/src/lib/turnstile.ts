/**
 * Cloudflare Turnstile, the spam protection of an instance that configured it (the server's config.SpamProtectionProvider). The runtime renders the widget itself, under the survey, so it works the same on the link survey page and in the SDK's survey on a customer's site; the server verifies the token on the first write of the response.
 */

// The script is the one every host shares: the SDK may load it before it renders the survey, and a page loads it once whatever renders the widget.
export const TURNSTILE_SCRIPT_ID = "forma-turnstile-script";
export const TURNSTILE_SCRIPT_URL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

export interface TurnstileRenderOptions {
  sitekey: string;
  action?: string;
  language?: string;
  appearance?: "always" | "execute" | "interaction-only";
  size?: "normal" | "flexible" | "compact";
  theme?: "auto" | "light" | "dark";
  callback?: (token: string) => void;
  "error-callback"?: (code: string) => boolean | void;
  "expired-callback"?: () => void;
  "timeout-callback"?: () => void;
  "before-interactive-callback"?: () => void;
  "after-interactive-callback"?: () => void;
  "unsupported-callback"?: () => void;
}

export interface TurnstileApi {
  render: (container: HTMLElement, options: TurnstileRenderOptions) => string | null | undefined;
  getResponse: (widgetId: string) => string | undefined;
  isExpired: (widgetId: string) => boolean;
  reset: (widgetId: string) => void;
  remove: (widgetId: string) => void;
}

declare global {
  interface Window {
    turnstile?: TurnstileApi;
  }
}

/**
 * Loads Turnstile's script once per page, or waits for the tag another host already added. Rejects when the script cannot load (a blocked host, a Content-Security-Policy without challenges.cloudflare.com, no network); a later call tries again.
 */
export const loadTurnstile = (): Promise<TurnstileApi> =>
  new Promise((resolve, reject) => {
    if (globalThis.window === undefined) {
      reject(new Error("Turnstile needs a browser"));
      return;
    }
    if (window.turnstile) {
      resolve(window.turnstile);
      return;
    }
    const existing = document.getElementById(TURNSTILE_SCRIPT_ID) as HTMLScriptElement | null;
    const script = existing ?? document.createElement("script");
    const fail = () => {
      script.remove();
      reject(new Error("Turnstile did not load"));
    };
    script.addEventListener("load", () => (window.turnstile ? resolve(window.turnstile) : fail()), {
      once: true,
    });
    script.addEventListener("error", fail, { once: true });
    if (!existing) {
      script.id = TURNSTILE_SCRIPT_ID;
      script.src = TURNSTILE_SCRIPT_URL;
      script.async = true;
      document.head.appendChild(script);
    }
  });

// Turnstile's languages (developers.cloudflare.com/turnstile/reference/supported-languages) by primary subtag; Chinese is told apart by script or region.
const TURNSTILE_LANGUAGES = new Set([
  "ar",
  "bg",
  "zh",
  "hr",
  "cs",
  "da",
  "nl",
  "en",
  "fa",
  "fi",
  "fr",
  "de",
  "el",
  "he",
  "hi",
  "hu",
  "id",
  "it",
  "ja",
  "ko",
  "lt",
  "ms",
  "nb",
  "pl",
  "pt",
  "ro",
  "ru",
  "sr",
  "sk",
  "sl",
  "es",
  "sv",
  "tl",
  "th",
  "tr",
  "uk",
  "vi",
]);

/**
 * The language Turnstile shows its challenge in for a survey language tag: the survey's own language where Turnstile has it (zh-cn or zh-tw for Chinese, pt-br for Brazilian Portuguese), else "auto", the browser's.
 */
export const turnstileLanguage = (languageTag: string | null | undefined): string => {
  if (!languageTag) return "auto";
  const tag = languageTag.toLowerCase();
  const [primary] = tag.split("-");
  if (primary === "zh") {
    return /-(hant|tw|hk|mo)(-|$)/.test(tag) ? "zh-tw" : "zh-cn";
  }
  if (tag === "pt-br") return "pt-br";
  return TURNSTILE_LANGUAGES.has(primary) ? primary : "auto";
};

/**
 * One rendered widget and the token it holds. The widget is rendered as soon as the survey shows, so the challenge usually runs while the respondent reads the first card, and it stays hidden unless Cloudflare asks the respondent to interact (appearance "interaction-only"). Turnstile refreshes an expired token on its own; getToken returns the current one, or waits for the next.
 */
export class TurnstileChallenge {
  private api: TurnstileApi | null = null;
  private widgetId: string | null = null;
  private token: string | null = null;
  private failed = false;
  private removed = false;
  private waiting: ((token: string | null) => void)[] = [];

  /** language is Turnstile's code for the survey's language (turnstileLanguage); Turnstile takes it only when it renders, so another language needs another challenge. */
  constructor(
    private readonly siteKey: string,
    private readonly language: string,
    private readonly load: () => Promise<TurnstileApi> = loadTurnstile
  ) {}

  /** Renders the widget into container. A widget that cannot load or render fails every getToken until it renders again. */
  async mount(container: HTMLElement, onInteractive?: (interactive: boolean) => void) {
    this.removed = false;
    try {
      const api = await this.load();
      if (this.removed) return;
      const id = api.render(container, {
        sitekey: this.siteKey,
        action: "submit_response",
        language: this.language,
        appearance: "interaction-only",
        size: "flexible",
        callback: (token) => {
          this.failed = false;
          this.token = token;
          this.settle(token);
        },
        "error-callback": () => {
          this.token = null;
          this.fail();
          // Handled here: the runtime shows its own error, and Turnstile keeps retrying in the background.
          return true;
        },
        "expired-callback": () => {
          this.token = null;
        },
        "timeout-callback": () => {
          this.token = null;
        },
        "unsupported-callback": () => this.fail(),
        "before-interactive-callback": () => onInteractive?.(true),
        "after-interactive-callback": () => onInteractive?.(false),
      });
      if (!id) {
        this.fail();
        return;
      }
      this.api = api;
      this.widgetId = id;
    } catch {
      this.fail();
    }
  }

  /** The token to send with the response, waiting for the challenge when it has not finished; null when the widget failed. */
  getToken(): Promise<string | null> {
    if (this.token && this.api && this.widgetId && !this.api.isExpired(this.widgetId)) {
      return Promise.resolve(this.token);
    }
    if (this.failed) return Promise.resolve(null);
    return new Promise((resolve) => this.waiting.push(resolve));
  }

  remove() {
    this.removed = true;
    if (this.api && this.widgetId) this.api.remove(this.widgetId);
    this.api = null;
    this.widgetId = null;
    this.settle(null);
  }

  private fail() {
    this.failed = true;
    this.settle(null);
  }

  private settle(token: string | null) {
    const waiting = this.waiting;
    this.waiting = [];
    for (const resolve of waiting) resolve(token);
  }
}
