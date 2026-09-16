// Flags an HTTP endpoint that reads or writes data belonging to a licensed feature without checking whether this installation has that feature.
// Run from the repo root as `pnpm entitlement:check`, which `pnpm lint` calls alongside `tenant:check` and `catalog:check`.
//
// The web app answers three separate authorization questions, with no single place that composes them: does this installation have the feature (`modules/ee/license-check`), may this subject act on this resource (AuthZed), and what role does this member hold (`getAccessFlags`). Because nothing states when the first one applies at an HTTP boundary, sibling endpoints disagree — `v2/management/contact-attribute-keys` calls `checkContactsEnabledApiV2`, while `v2/management/responses` returns the same contacts' attributes without asking.
//
// This does not decide which of them is right; both readings are defensible, and adding a guard where one is missing changes what an existing API consumer receives. It makes the disagreement visible: an endpoint either guards, or says in one line why it does not, and a reviewer sees which.
//
// Scope is deliberately narrow. Only features whose data lives in dedicated models are covered, because that is the part a static check can see. Entitlement that gates a *capability* rather than a table — bigger uploads, removing branding — is invisible here and stays a matter of review.
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB_ROOT = join(HERE, "..");
const REPO_ROOT = join(WEB_ROOT, "..", "..");

/** Where HTTP endpoints live. A `route.ts` under either tree is an endpoint. */
const ENDPOINT_ROOTS = [join(WEB_ROOT, "app/api"), join(WEB_ROOT, "modules/api")];

/**
 * Models whose rows exist only because a licensed feature does, paired with every spelling of the guard that answers "is it licensed here".
 *
 * `getIs<Feature>Enabled` returns a boolean; `check<Feature>Enabled*` wraps it into an API error. Both count — the question is whether the endpoint asks, not how it phrases it.
 */
const GATED_FEATURES = [
  {
    feature: "contacts",
    models: ["contact", "contactAttributeKey", "contactAttributeValue", "segment"],
    guards: ["getIsContactsEnabled", "checkContactsEnabled"],
  },
  {
    feature: "quotas",
    models: ["surveyQuota", "surveyQuotaLink"],
    guards: ["getIsQuotasEnabled", "checkQuotasEnabled"],
  },
  {
    feature: "workflows",
    models: ["workflow", "workflowRun", "workflowVersion"],
    guards: ["getIsWorkflowsEnabled", "checkWorkflowsEnabled"],
  },
] as const;

const EXEMPTION = /\/\/\s*ee-entitlement-exempt:\s*\S/;

export interface EntitlementFinding {
  endpoint: string;
  feature: string;
  models: string[];
}

/** Whether `text` touches any of `models` through the Prisma client. */
export const touchesModels = (text: string, models: readonly string[]): string[] =>
  models.filter((m) => new RegExp(`prisma\\.${m}\\.`).test(text));

export const isGuarded = (text: string, guards: readonly string[]): boolean =>
  guards.some((g) => text.includes(g));

export const isExempt = (routeSource: string): boolean => EXEMPTION.test(routeSource);

/**
 * Classifies one endpoint from its own source plus the code colocated with it.
 *
 * Colocated rather than route-only because the query almost always sits in a sibling `lib/`, and the guard almost always sits in `route.ts` — reading either alone gets the answer wrong in opposite directions.
 */
export const classifyEndpoint = (routeSource: string, colocatedSource: string): EntitlementFinding[] => {
  if (isExempt(routeSource)) return [];
  const all = `${routeSource}\n${colocatedSource}`;
  return GATED_FEATURES.flatMap(({ feature, models, guards }) => {
    const touched = touchesModels(all, models);
    if (touched.length === 0 || isGuarded(all, guards)) return [];
    return [{ endpoint: "", feature, models: touched }];
  });
};

/** Files colocated with an endpoint: its own directory and anything under it that is not itself another endpoint. */
const colocatedFiles = (endpointDir: string, out: string[] = []): string[] => {
  for (const entry of readdirSync(endpointDir, { withFileTypes: true })) {
    const full = join(endpointDir, entry.name);
    if (entry.isDirectory()) {
      // A nested directory holding its own route.ts is a separate endpoint and is judged on its own.
      const nested = readdirSync(full).includes("route.ts");
      if (!nested) colocatedFiles(full, out);
    } else if (entry.name.endsWith(".ts") && !entry.name.includes(".test.")) {
      out.push(full);
    }
  }
  return out;
};

const findEndpoints = (dir: string, out: string[] = []): string[] => {
  if (!statSync(dir, { throwIfNoEntry: false })?.isDirectory()) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) findEndpoints(full, out);
    else if (entry.name === "route.ts") out.push(full);
  }
  return out;
};

const main = (): number => {
  const findings: EntitlementFinding[] = [];

  for (const root of ENDPOINT_ROOTS) {
    for (const route of findEndpoints(root)) {
      const routeSource = readFileSync(route, "utf8");
      const colocated = colocatedFiles(dirname(route))
        .filter((f) => f !== route)
        .map((f) => readFileSync(f, "utf8"))
        .join("\n");

      for (const finding of classifyEndpoint(routeSource, colocated)) {
        findings.push({ ...finding, endpoint: relative(REPO_ROOT, route) });
      }
    }
  }

  if (findings.length === 0) {
    console.log(`✓ entitlement check passed (${GATED_FEATURES.length} licensed features)`);
    return 0;
  }

  console.error(
    `✗ entitlement check: ${findings.length} endpoint${findings.length === 1 ? " touches" : "s touch"} licensed data without checking the licence\n`
  );
  for (const f of findings) {
    console.error(`  ${f.endpoint}`);
    console.error(`    reads or writes ${f.models.join(", ")} — "${f.feature}" is a licensed feature`);
  }
  console.error(`
Call the feature's guard in the route, or record why this endpoint does not need one:

    // ee-entitlement-exempt: returns the contact attached to a response, not the contacts feature
`);
  return 1;
};

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
