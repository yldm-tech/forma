import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";

// Guards the narrow contact join shared by the three `responseSelection` copies (r11). Selecting the
// whole related `attributeKey` row pulled all ten `ContactAttributeKey` columns — including the
// nullable `description` text — for every attribute of every contact on every response read, on the
// responses table, the single-response modal, response create/update, and the v1 management and
// v1/v2 client response APIs. Only `userId` is ever read back off that relation
// (`getResponseContact`, and `buildClientResponse` does not read it at all), so the join is filtered
// to that one key and to the one column the lookup uses.
//
// This is a source-text check rather than a shape assertion because the point is that all three
// copies stay in step: two of them live under `app/api`, are `server-only`, and importing them here
// would drag their whole module graph into this spec. Narrowing one copy and leaving the others is
// the regression this exists to catch. (A fourth, unrelated copy lives in the survey summary lib and
// is deliberately out of scope here.)
const here = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(here, "..", "..");

const RESPONSE_SELECTION_FILES = [
  "lib/response/service.ts",
  "app/api/v1/management/responses/lib/response.ts",
  "app/api/v1/client/[workspaceId]/responses/lib/response.ts",
];

describe("responseSelection contact join", () => {
  test.each(RESPONSE_SELECTION_FILES)("%s filters the contact attributes to userId", (relativePath) => {
    const source = fs.readFileSync(path.join(webRoot, relativePath), "utf8");

    expect(source).toContain("export const responseSelection");
    expect(source).toContain('where: { attributeKey: { key: "userId" } }');
    expect(source).toContain("select: { attributeKey: { select: { key: true } }, value: true }");
    // The unfiltered form, which drags every ContactAttributeKey column along with every attribute.
    expect(source).not.toContain("select: { attributeKey: true, value: true }");
  });

  test("covers every responseSelection declaration under apps/web", () => {
    const declarations = listFilesWithResponseSelection(webRoot);

    // A new copy has to be added to the list above, or it silently keeps the wide join.
    expect(declarations.sort()).toEqual([...RESPONSE_SELECTION_FILES].sort());
  });
});

const IGNORED_DIRECTORIES = new Set(["node_modules", ".next", "dist", "coverage", "playwright-report"]);

function listFilesWithResponseSelection(root: string): string[] {
  const found: string[] = [];

  const walk = (directory: string): void => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      if (entry.name.startsWith(".") || IGNORED_DIRECTORIES.has(entry.name)) continue;
      const full = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!entry.name.endsWith(".ts") || entry.name.endsWith(".test.ts")) continue;
      if (fs.readFileSync(full, "utf8").includes("export const responseSelection")) {
        found.push(path.relative(root, full));
      }
    }
  };

  walk(root);
  return found;
}
