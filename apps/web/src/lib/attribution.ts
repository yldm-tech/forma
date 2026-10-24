import { useEffect } from "react";

// Marketing attribution captured on the sign-in and sign-up pages, forma2's modules/auth/lib/attribution.ts and AttributionTracker. Only the whitelisted keys are read or stored: non-personal values (source, campaign, landing page), so fb_attribution is a functional cookie rather than user tracking. The server reads it into user_signed_up and user_signed_in and clears it (apps/server/internal/auth/attribution.go).

export const ATTRIBUTION_PARAM_KEYS = [
  "page",
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_term",
  "utm_content",
  "initial_source",
  "entry_page",
] as const;

export const ATTRIBUTION_COOKIE_NAME = "fb_attribution";

/** 30 minutes: long enough to survive the identity provider round trip and the moves between sign-in and sign-up, short enough that stale attribution cannot reach a later user on a shared browser. */
export const ATTRIBUTION_COOKIE_MAX_AGE = 60 * 30;

const MAX_ATTRIBUTION_VALUE_LENGTH = 256;

/** Browsers silently drop a cookie over about 4 KB; the encoded value stays well under that. */
export const MAX_ATTRIBUTION_COOKIE_VALUE_LENGTH = 3500;

/** The whitelisted, non-blank attribution parameters, trimmed and capped: what the cookie stores, and what the links between sign-in and sign-up carry on (forma2's buildAttributionQuerySuffix). */
export const pickAttributionParams = (params: URLSearchParams | null | undefined): Record<string, string> => {
  const result: Record<string, string> = {};
  if (!params) return result;
  for (const key of ATTRIBUTION_PARAM_KEYS) {
    const value = params.get(key);
    if (value && value.trim().length > 0) result[key] = value.trim().slice(0, MAX_ATTRIBUTION_VALUE_LENGTH);
  }
  return result;
};

/** The fb_attribution cookie for the page's query, or null when it must not be written: one is already set (first touch wins), the query carries nothing, or the value would be too large to persist. */
export const attributionCookie = (cookie: string, search: string, protocol: string): string | null => {
  const alreadySet = cookie.split(";").some((part) => part.trim().startsWith(`${ATTRIBUTION_COOKIE_NAME}=`));
  if (alreadySet) return null;
  const attribution = pickAttributionParams(new URLSearchParams(search));
  if (Object.keys(attribution).length === 0) return null;
  const value = encodeURIComponent(JSON.stringify(attribution));
  if (value.length > MAX_ATTRIBUTION_COOKIE_VALUE_LENGTH) return null;
  // Secure over HTTPS only: a plain-HTTP development instance would have the browser reject it.
  const secure = protocol === "https:" ? "; Secure" : "";
  return `${ATTRIBUTION_COOKIE_NAME}=${value}; path=/; max-age=${ATTRIBUTION_COOKIE_MAX_AGE}; SameSite=Lax${secure}`;
};

/** forma2's AttributionTracker: on the auth pages, writes the first-touch attribution cookie from the URL the visitor arrived with. */
export const useAttributionTracker = (): void => {
  useEffect(() => {
    const cookie = attributionCookie(
      document.cookie,
      globalThis.location.search,
      globalThis.location.protocol
    );
    // oxlint-disable-next-line unicorn/no-document-cookie -- the cookie must be readable by the server on the sign-up and sign-in requests, as forma2 wrote it
    if (cookie) document.cookie = cookie;
  }, []);
};
