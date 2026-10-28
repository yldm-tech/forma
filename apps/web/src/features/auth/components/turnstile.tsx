import { useEffect, useRef } from "react";

// Cloudflare Turnstile, rendered explicitly through Cloudflare's own script, which is how the widget is meant to be embedded; the token it produces is verified by the server on sign-up.
const SCRIPT_URL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

interface TurnstileApi {
  render: (
    container: HTMLElement,
    options: {
      sitekey: string;
      action?: string;
      size?: "normal" | "flexible" | "compact";
      callback: (token: string) => void;
      "error-callback": () => void;
      "expired-callback": () => void;
    }
  ) => string;
  reset: (widgetId: string) => void;
  remove: (widgetId: string) => void;
}

declare global {
  interface Window {
    turnstile?: TurnstileApi;
  }
}

let scriptLoad: Promise<TurnstileApi> | undefined;

const loadTurnstile = (): Promise<TurnstileApi> => {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  scriptLoad ??= new Promise<TurnstileApi>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = SCRIPT_URL;
    script.async = true;
    script.onload = () =>
      window.turnstile ? resolve(window.turnstile) : reject(new Error("Turnstile did not load"));
    script.onerror = () => {
      scriptLoad = undefined;
      reject(new Error("Turnstile did not load"));
    };
    document.head.appendChild(script);
  });
  return scriptLoad;
};

/**
 * The CAPTCHA widget. `resetKey` changing clears a spent token, as forma2 reset the widget after a refused sign-up. A widget that fails to load or errors reports through onError.
 */
export function Turnstile({
  siteKey,
  resetKey,
  onToken,
  onError,
}: {
  siteKey: string;
  resetKey: number;
  onToken: (token: string | undefined) => void;
  onError: () => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const widget = useRef<{ api: TurnstileApi; id: string }>(undefined);
  const callbacks = useRef({ onToken, onError });
  useEffect(() => {
    callbacks.current = { onToken, onError };
  });

  useEffect(() => {
    let cancelled = false;
    loadTurnstile().then(
      (api) => {
        if (cancelled || !container.current) return;
        const id = api.render(container.current, {
          sitekey: siteKey,
          // The server takes a sign-up token only for this action (turnstile.ActionSignUp), so a token the survey widget minted is not one.
          action: "sign_up",
          size: "flexible",
          callback: (token) => callbacks.current.onToken(token),
          "error-callback": () => {
            callbacks.current.onToken(undefined);
            callbacks.current.onError();
          },
          "expired-callback": () => callbacks.current.onToken(undefined),
        });
        widget.current = { api, id };
      },
      () => {
        if (!cancelled) callbacks.current.onError();
      }
    );
    return () => {
      cancelled = true;
      if (widget.current) widget.current.api.remove(widget.current.id);
      widget.current = undefined;
    };
  }, [siteKey]);

  useEffect(() => {
    if (resetKey > 0 && widget.current) widget.current.api.reset(widget.current.id);
  }, [resetKey]);

  // The widget is a fixed 300px iframe, wider than the card on a small phone; the clipping wrapper keeps it from widening the page.
  return (
    <div className="mt-4 w-full overflow-hidden">
      <div ref={container} />
    </div>
  );
}
