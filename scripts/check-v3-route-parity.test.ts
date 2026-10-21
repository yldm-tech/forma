import { describe, expect, test } from "vitest";
// @ts-expect-error -- plain .mjs script, no type declarations
import {
  collectRouteOperations,
  collectSpecOperations,
  compareOperations,
  methodsInSource,
  toPathTemplate,
} from "./check-v3-route-parity.mjs";

describe("toPathTemplate", () => {
  test("turns a dynamic segment into an OpenAPI path parameter", () => {
    expect(toPathTemplate(["surveys", "[surveyId]", "archive"])).toBe("/api/v3/surveys/{surveyId}/archive");
  });

  test("drops a route group, which never appears in the URL", () => {
    expect(toPathTemplate(["(internal)", "tags"])).toBe("/api/v3/tags");
  });

  test("keeps the base path for a route module sitting directly under api/v3", () => {
    expect(toPathTemplate([])).toBe("/api/v3");
  });

  test("refuses a catch-all rather than mapping it to a path no spec can express", () => {
    expect(() => toPathTemplate(["files", "[...path]"])).toThrow(/catch-all/);
  });
});

describe("methodsInSource", () => {
  test("reads both spellings Next.js accepts, and deduplicates", () => {
    const source = [
      "export const GET = withV3Api(handler);",
      "export async function POST(request: Request) {}",
      "export function DELETE() {}",
      "export const GET2 = notAMethod;",
    ].join("\n");
    expect(methodsInSource(source).sort()).toEqual(["DELETE", "GET", "POST"]);
  });

  test("ignores a method named anywhere but at the start of an export", () => {
    const source = ['// export const DELETE was removed\nconst note = "export const PATCH";'].join("\n");
    expect(methodsInSource(source)).toEqual([]);
  });
});

describe("collectRouteOperations", () => {
  const tree: Record<string, string[]> = {
    "/v3": ["surveys"],
    "/v3/surveys": ["[surveyId]", "route.ts"],
    "/v3/surveys/[surveyId]": ["route.ts", "service.ts"],
  };
  const sources: Record<string, string> = {
    "/v3/surveys/route.ts": "export const GET = a;\nexport const POST = b;",
    "/v3/surveys/[surveyId]/route.ts": "export const PATCH = c;",
    "/v3/surveys/[surveyId]/service.ts": "export const DELETE = notARouteHandler;",
  };
  const io = {
    list: (dir: string) => tree[dir],
    read: (file: string) => sources[file],
    isDirectory: (path: string) => path in tree,
  };

  test("maps every route module to its operations and the file that declares them", () => {
    const operations = collectRouteOperations("/v3", io);
    expect([...operations.keys()].sort()).toEqual([
      "GET /api/v3/surveys",
      "PATCH /api/v3/surveys/{surveyId}",
      "POST /api/v3/surveys",
    ]);
    expect(operations.get("PATCH /api/v3/surveys/{surveyId}")).toBe("/v3/surveys/[surveyId]/route.ts");
  });

  test("ignores a non-route module beside a route, however it exports", () => {
    expect([...collectRouteOperations("/v3", io).keys()]).not.toContain("DELETE /api/v3/surveys/{surveyId}");
  });
});

describe("collectSpecOperations", () => {
  test("reads one operation per documented method and ignores the rest of the path item", () => {
    const document = {
      paths: {
        "/api/v3/tags": { get: {}, parameters: [], summary: "Tags" },
        "/api/v3/tags/{tagId}": { patch: {}, delete: {} },
      },
    };
    expect([...collectSpecOperations(document)].sort()).toEqual([
      "DELETE /api/v3/tags/{tagId}",
      "GET /api/v3/tags",
      "PATCH /api/v3/tags/{tagId}",
    ]);
  });

  test("treats a bundle with no paths as documenting nothing rather than throwing", () => {
    expect([...collectSpecOperations({})]).toEqual([]);
  });
});

describe("compareOperations", () => {
  const routes = new Map([
    ["GET /api/v3/tags", "tags/route.ts"],
    ["POST /api/v3/surveys/templates", "surveys/templates/route.ts"],
  ]);
  const spec = new Set(["GET /api/v3/tags", "GET /api/v3/responses"]);
  const empty = new Map();

  test("reports a route with no documented operation", () => {
    const { undocumented } = compareOperations(routes, spec, {
      allowUndocumented: empty,
      allowUnimplemented: empty,
    });
    expect(undocumented).toEqual(["POST /api/v3/surveys/templates"]);
  });

  test("reports a documented operation with no route", () => {
    const { unimplemented } = compareOperations(routes, spec, {
      allowUndocumented: empty,
      allowUnimplemented: empty,
    });
    expect(unimplemented).toEqual(["GET /api/v3/responses"]);
  });

  test("a recorded exception suppresses its own finding and nothing else", () => {
    const { undocumented, unimplemented, stale } = compareOperations(routes, spec, {
      allowUndocumented: new Map([["POST /api/v3/surveys/templates", "recorded"]]),
      allowUnimplemented: new Map([["GET /api/v3/responses", "ENG-2959"]]),
    });
    expect(undocumented).toEqual([]);
    expect(unimplemented).toEqual([]);
    expect(stale).toEqual([]);
  });

  test("an exception that has come true is itself a failure, named with the list it is in", () => {
    const { stale } = compareOperations(routes, spec, {
      allowUndocumented: new Map([["GET /api/v3/tags", "documented since this was written"]]),
      allowUnimplemented: new Map([["GET /api/v3/tags", "routed since this was written"]]),
    });
    expect(stale).toEqual([
      { operation: "GET /api/v3/tags", list: "ROUTE_WITHOUT_DOCUMENTATION" },
      { operation: "GET /api/v3/tags", list: "DOCUMENTED_WITHOUT_ROUTE" },
    ]);
  });
});
