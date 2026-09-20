import { z } from "zod";

/**
 * Ceiling for a single page of a v1 management collection, mirroring `ZGetFilter` in the v2 API (`modules/api/v2/types/api-filter.ts`). Without it a caller could ask for — or, with no `limit` at all, silently get — every row a key can read in one response.
 */
export const MAX_ITEMS_PER_PAGE = 250;

const ZLimit = z.coerce.number().int().min(1).max(MAX_ITEMS_PER_PAGE);
const ZOffset = z.coerce.number().int().min(0);

export type TPagination = { limit: number; offset: number };

export type TParsePaginationResult =
  { ok: true; data: TPagination } | { ok: false; details: Record<string, string> };

type TParsePaginationOptions = {
  /** Page size used when the caller sends no `limit`. Never unbounded. */
  defaultLimit: number;
  /** Query parameter carrying the offset; v1 spells it `skip` on some routes and `offset` on others. */
  offsetParamName?: string;
};

/**
 * Parses and clamps the pagination of a v1 management collection route. An absent or empty parameter takes the default; anything present but out of range is a client error rather than a silently coerced value, so `?limit=0` and `?limit=abc` no longer both mean "no rows".
 */
export const parsePaginationParams = (
  searchParams: URLSearchParams,
  { defaultLimit, offsetParamName = "skip" }: TParsePaginationOptions
): TParsePaginationResult => {
  const details: Record<string, string> = {};

  const rawLimit = searchParams.get("limit");
  let limit = defaultLimit;
  if (rawLimit !== null && rawLimit !== "") {
    const parsedLimit = ZLimit.safeParse(rawLimit);
    if (parsedLimit.success) {
      limit = parsedLimit.data;
    } else {
      details.limit = `limit must be an integer between 1 and ${MAX_ITEMS_PER_PAGE}`;
    }
  }

  const rawOffset = searchParams.get(offsetParamName);
  let offset = 0;
  if (rawOffset !== null && rawOffset !== "") {
    const parsedOffset = ZOffset.safeParse(rawOffset);
    if (parsedOffset.success) {
      offset = parsedOffset.data;
    } else {
      details[offsetParamName] = `${offsetParamName} must be an integer of 0 or greater`;
    }
  }

  if (Object.keys(details).length > 0) {
    return { ok: false, details };
  }

  return { ok: true, data: { limit, offset } };
};
