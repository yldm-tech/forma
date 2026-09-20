import { describe, expect, test } from "vitest";
import { MAX_ITEMS_PER_PAGE, parsePaginationParams } from "./pagination";

const parse = (query: string, defaultLimit = 25, offsetParamName?: string) =>
  parsePaginationParams(new URLSearchParams(query), { defaultLimit, offsetParamName });

describe("parsePaginationParams", () => {
  test("applies the default page size when no limit is sent", () => {
    expect(parse("")).toEqual({ ok: true, data: { limit: 25, offset: 0 } });
  });

  test("passes a limit inside the ceiling through", () => {
    expect(parse("limit=100&skip=40")).toEqual({ ok: true, data: { limit: 100, offset: 40 } });
    expect(parse(`limit=${MAX_ITEMS_PER_PAGE}`)).toEqual({
      ok: true,
      data: { limit: MAX_ITEMS_PER_PAGE, offset: 0 },
    });
  });

  test("rejects a limit above the ceiling instead of running the query", () => {
    const result = parse("limit=100000000");

    expect(result.ok).toBe(false);
    expect(result).toMatchObject({ details: { limit: expect.stringContaining("1 and 250") } });
  });

  test.each(["limit=0", "limit=-1", "limit=abc", "limit=1.5"])("rejects %s", (query) => {
    expect(parse(query).ok).toBe(false);
  });

  test("rejects a negative or non-numeric offset", () => {
    expect(parse("skip=-1").ok).toBe(false);
    expect(parse("skip=abc")).toMatchObject({ ok: false, details: { skip: expect.any(String) } });
  });

  test("reads the offset from the parameter the route documents", () => {
    expect(parse("offset=10", 250, "offset")).toEqual({ ok: true, data: { limit: 250, offset: 10 } });
    // `skip` is not this route's parameter, so it is ignored rather than silently paginating.
    expect(parse("skip=10", 250, "offset")).toEqual({ ok: true, data: { limit: 250, offset: 0 } });
  });

  test("treats an empty parameter as absent", () => {
    // `?limit=` used to mean `Number("") === 0` on the surveys route, which returned zero rows.
    expect(parse("limit=&skip=")).toEqual({ ok: true, data: { limit: 25, offset: 0 } });
  });

  test("reports every invalid parameter at once", () => {
    expect(parse("limit=0&skip=-3")).toEqual({
      ok: false,
      details: { limit: expect.any(String), skip: expect.any(String) },
    });
  });
});
