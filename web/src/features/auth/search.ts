// Search parameter parsing for the auth routes. Every parameter is optional and a repeated one keeps its first value, as forma2's getSearchParamValue did.

export const stringParam = (value: unknown): string | undefined => {
  const first = Array.isArray(value) ? (value as unknown[])[0] : value;
  if (typeof first === "string") return first === "" ? undefined : first;
  // The router parses a numeric-looking value into a number; a token or an email never needs that, so it is turned back into its text.
  if (typeof first === "number" || typeof first === "boolean") return String(first);
  return undefined;
};

/** Search params with a callback URL, which every signed-out screen accepts. */
export interface CallbackSearch {
  callbackUrl?: string;
}

export const callbackSearch = (search: Record<string, unknown>): CallbackSearch => ({
  callbackUrl: stringParam(search.callbackUrl),
});

/** Search params of an emailed link. */
export interface TokenSearch {
  token?: string;
}

export const tokenSearch = (search: Record<string, unknown>): TokenSearch => ({
  token: stringParam(search.token),
});
