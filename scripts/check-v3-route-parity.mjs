#!/usr/bin/env node
// Fails when an implemented /api/v3 route handler has no operation in the committed OpenAPI bundle.
// Built to run as a repo-level gate alongside `catalog:check`, `tenant:check` and `tests:run-check`.
//
// Documenting a v3 operation is what enrols it in everything downstream: the published reference page,
// and — because docs/api-v3-reference/contract-tests drives the committed bundle — the Schemathesis
// contract suite that asserts its status codes, content type and response schema on every PR. Nothing
// checked the other direction, so a route could ship with no entry at all and no gate would notice.
// `POST /api/v3/surveys/templates` did exactly that: a survey-creating, audit-logged endpoint with no
// documentation and no contract case.
//
// The check is structural — App Router folder names and `export const <METHOD>` — rather than a
// TypeScript parse, for the reason check-tests-run.mjs gives for its own approach: the stronger question
// costs far more than the case it catches. A handler assigned through an alias, or re-exported from
// another module, would read as absent here; every route under api/v3 today declares its methods inline.
//
// Two directions, reported together so one run lists everything:
//
//   1. A route operation with no documented operation. Failure unless ROUTE_WITHOUT_DOCUMENTATION
//      records it — the one existing instance, so the gate can go live now and hold the line for the
//      next route instead of waiting on a docs change.
//   2. A documented operation with no route. Failure unless DOCUMENTED_WITHOUT_ROUTE lists it with a
//      reason and a ticket. The /api/v3/responses set lives there: ENG-2868 agreed that contract before
//      implementation deliberately, and ENG-2959 tracks the routes. That list is also what
//      .github/workflows/api-v3-contract-tests.yml excludes by regex, so the two want to stay in step.
//
// Both lists are checked for staleness too: an entry that has become true is an error, so neither can
// quietly outlive the reason it was written.
import yaml from "js-yaml";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = resolve(fileURLToPath(new URL("..", import.meta.url)));
const ROUTES_ROOT = join(REPO_ROOT, "apps", "web", "app", "api", "v3");
const BUNDLE_PATH = join(REPO_ROOT, "docs", "api-v3-reference", "openapi.yml");
const BASE_PATH = "/api/v3";

const HTTP_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE"];

/**
 * Documented operations that deliberately have no route yet. Each entry carries the ticket that will
 * remove it — an entry with no ticket is a gap being hidden rather than recorded.
 */
export const DOCUMENTED_WITHOUT_ROUTE = new Map([
  ["GET /api/v3/responses", "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built"],
  ["POST /api/v3/responses", "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built"],
  ["GET /api/v3/responses/count", "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built"],
  [
    "POST /api/v3/responses/validate",
    "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built",
  ],
  [
    "POST /api/v3/responses/batch-delete",
    "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built",
  ],
  [
    "GET /api/v3/responses/{responseId}",
    "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built",
  ],
  [
    "PATCH /api/v3/responses/{responseId}",
    "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built",
  ],
  [
    "DELETE /api/v3/responses/{responseId}",
    "ENG-2959 — contract agreed up front by ENG-2868, routes not yet built",
  ],
]);

/**
 * Routes that exist with no documented operation — the state this gate is here to stop, recorded rather
 * than hidden so the gate can go live today and catch the next one. This list may only shrink: remove an
 * entry by documenting the operation, never by adding one. `check-catalog.mjs`'s ALLOW_UNCATALOGED and
 * `modules-app-imports-baseline.json` are the same shape for the same reason.
 */
export const ROUTE_WITHOUT_DOCUMENTATION = new Map([
  [
    "POST /api/v3/surveys/templates",
    "undocumented since it shipped, found by this gate — needs a paths file and `x-excluded: true`, no ticket yet",
  ],
]);

/**
 * Turn the App Router directory segments below `app/api/v3` into an OpenAPI path template.
 *
 * `[surveyId]` becomes `{surveyId}`; a route group `(internal)` contributes no segment, because it does
 * not appear in the URL. A catch-all throws rather than guessing: no OpenAPI path template can express
 * one, so a silent mapping would report a real route as documented.
 */
export const toPathTemplate = (segments) => {
  const parts = [];
  for (const segment of segments) {
    if (segment.startsWith("(") && segment.endsWith(")")) continue;
    if (segment.startsWith("@")) continue;
    const dynamic = /^\[{1,2}(\.{3})?([^\]]+)\]{1,2}$/.exec(segment);
    if (!dynamic) {
      parts.push(segment);
      continue;
    }
    if (dynamic[1]) {
      throw new Error(
        `catch-all segment "${segment}" has no OpenAPI path template. Document the concrete paths it serves and teach this script how to map it.`
      );
    }
    parts.push(`{${dynamic[2]}}`);
  }
  return parts.length === 0 ? BASE_PATH : `${BASE_PATH}/${parts.join("/")}`;
};

/**
 * The HTTP methods a route module exports. Both spellings Next.js accepts for a route handler, matched
 * at the start of a line so a method named inside a comment or a string is not counted.
 */
export const methodsInSource = (source) => {
  const pattern = new RegExp(
    `^export (?:const|async function|function) (${HTTP_METHODS.join("|")})\\b`,
    "gm"
  );
  return [...new Set([...source.matchAll(pattern)].map((match) => match[1]))];
};

/** Every `<METHOD> <path>` a route module under `dir` implements, keyed to the file that declares it. */
export const collectRouteOperations = (
  dir = ROUTES_ROOT,
  { list = readdirSync, read = readFileSync, isDirectory = (p) => statSync(p).isDirectory() } = {}
) => {
  const operations = new Map();

  const walk = (current, segments) => {
    for (const entry of list(current).sort()) {
      const path = join(current, entry);
      if (isDirectory(path)) {
        walk(path, [...segments, entry]);
        continue;
      }
      if (entry !== "route.ts" && entry !== "route.tsx") continue;
      const template = toPathTemplate(segments);
      for (const method of methodsInSource(read(path, "utf8"))) {
        operations.set(`${method} ${template}`, path);
      }
    }
  };

  walk(dir, []);
  return operations;
};

/** Every `<METHOD> <path>` the bundle documents. */
export const collectSpecOperations = (document) => {
  const operations = new Set();
  for (const [path, item] of Object.entries(document?.paths ?? {})) {
    for (const method of HTTP_METHODS) {
      if (item?.[method.toLowerCase()]) operations.add(`${method} ${path}`);
    }
  }
  return operations;
};

export const compareOperations = (
  routeOperations,
  specOperations,
  { allowUndocumented = ROUTE_WITHOUT_DOCUMENTATION, allowUnimplemented = DOCUMENTED_WITHOUT_ROUTE } = {}
) => ({
  undocumented: [...routeOperations.keys()]
    .filter((operation) => !specOperations.has(operation) && !allowUndocumented.has(operation))
    .sort(),
  unimplemented: [...specOperations]
    .filter((operation) => !routeOperations.has(operation) && !allowUnimplemented.has(operation))
    .sort(),
  stale: [
    ...[...allowUndocumented.keys()]
      .filter((operation) => specOperations.has(operation) || !routeOperations.has(operation))
      .map((operation) => ({ operation, list: "ROUTE_WITHOUT_DOCUMENTATION" })),
    ...[...allowUnimplemented.keys()]
      .filter((operation) => routeOperations.has(operation) || !specOperations.has(operation))
      .map((operation) => ({ operation, list: "DOCUMENTED_WITHOUT_ROUTE" })),
  ].sort((a, b) => a.operation.localeCompare(b.operation)),
});

const main = () => {
  const routeOperations = collectRouteOperations();
  const specOperations = collectSpecOperations(yaml.load(readFileSync(BUNDLE_PATH, "utf8")));
  const { undocumented, unimplemented, stale } = compareOperations(routeOperations, specOperations);

  if (undocumented.length === 0 && unimplemented.length === 0 && stale.length === 0) {
    console.log(
      `✓ /api/v3 route parity check passed (${routeOperations.size} route operations, ${specOperations.size} documented, ${ROUTE_WITHOUT_DOCUMENTATION.size} undocumented and recorded)`
    );
    return 0;
  }

  if (undocumented.length > 0) {
    console.error(`\n✗ /api/v3 route with no documented operation (${undocumented.length}):\n`);
    for (const operation of undocumented) {
      const file = routeOperations.get(operation).slice(REPO_ROOT.length + 1);
      console.error(`  ${operation}  (${file})`);
    }
    console.error(`
  An undocumented operation has no reference page and no contract test — Schemathesis drives the
  committed bundle, so it exercises exactly what is written there.

  Add a file under docs/api-v3-reference/src/paths/, reference it from src/openapi.yml, and run
  \`pnpm api:v3:bundle\`. An operation that is not ready to be exercised against real data is
  documented with \`x-excluded: true\`, which still enrols it at its 401/403 depth.`);
  }

  if (unimplemented.length > 0) {
    console.error(`\n✗ Documented /api/v3 operation with no route (${unimplemented.length}):\n`);
    for (const operation of unimplemented) console.error(`  ${operation}`);
    console.error(`
  A documented operation with no handler answers Next.js's HTML 404 against a spec that promises a
  JSON response, so the contract suite fails on it the moment it is selected.

  Implement the route, remove the operation from the bundle, or add it to DOCUMENTED_WITHOUT_ROUTE in
  this script with the ticket that will take it back out.`);
  }

  if (stale.length > 0) {
    console.error(`\n✗ Recorded exception that no longer applies (${stale.length}):\n`);
    for (const { operation, list } of stale) console.error(`  ${operation}  (${list})`);
    console.error(`
  The operation is now on both sides, or has disappeared from the side the entry was written about.
  Delete the entry from this script — a list of exceptions only means anything while every line in it
  is still true.`);
  }

  console.error("");
  return 1;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
