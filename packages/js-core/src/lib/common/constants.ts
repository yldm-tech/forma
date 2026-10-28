export const JS_LOCAL_STORAGE_KEY = "forma-js";
// Separate from JS_LOCAL_STORAGE_KEY on purpose: that blob is wiped by every resetConfig(), and the anonymous sampling id must survive it.
export const SAMPLING_ID_STORAGE_KEY = "forma-sampling-id";
export const CONTAINER_ID = "forma-modal-container";
// Shipped id contract with the surveys renderer (packages/surveys/src/lib/live-region.ts) — old SDKs
// stay on the wire for years, so this value must never change.
export const LIVE_REGION_ID = "forma-live-region";
export const RECAPTCHA_SCRIPT_ID = "forma-recaptcha-script";
// The same id the surveys runtime looks for (packages/surveys/src/lib/turnstile.ts), so the script loads once whichever side adds it.
export const TURNSTILE_SCRIPT_ID = "forma-turnstile-script";
export const TURNSTILE_SCRIPT_URL = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
