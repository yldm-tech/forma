// Enforces the multi-tenancy rule AGENTS.md states but nothing checked: "All data must be scoped by Organization or Environment."
// Run from the repo root as `pnpm tenant:check`, which `pnpm lint` calls alongside `catalog:check` and `api:v3:lint`.
//
// The rule was being kept by hand — an audit of all 3613 Prisma call sites found no unscoped query — but by discipline alone, across 397 files with no repository layer to hold the constraint. Discipline does not survive a rushed PR, and the failure mode here is one tenant reading another's data, which no test in this repo would catch.
//
// A violation is a read or bulk write against a model carrying a tenant column, where the enclosing function never mentions that column. Function scope rather than call scope is deliberate: the common shape in this codebase builds the `where` in a helper (`getWebhooksQuery(workspaceIds, params)`) and spreads it, so a narrower check would report the whole API v2 surface as unsafe.
//
// Scoping through a parent's id (`where: { segmentId }`) satisfies the check, because the segment is itself workspace-scoped. That is a real pattern here and a real limit: the guarantee moves to whoever resolved that id. Catching that needs taint tracking, which is a different tool.
//
// The models come from the Prisma schema, so a new one is covered when it is added rather than when someone remembers this file.
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const PACKAGE_ROOT = join(HERE, "..", "..");
const REPO_ROOT = join(PACKAGE_ROOT, "..", "..");
const SCHEMA_DIR = join(PACKAGE_ROOT, "schema");
const SEARCH_ROOTS = ["apps/web", "packages"];

// Reads and bulk writes: a query that can return or touch rows the caller never named. `findUnique` is absent on purpose — it takes a unique key, so the question there is whether the caller was entitled to that id, which this check cannot answer.
const RISKY_OPERATIONS = [
  "findMany",
  "findFirst",
  "findFirstOrThrow",
  "updateMany",
  "deleteMany",
  "count",
  "aggregate",
  "groupBy",
] as const;

const TENANT_COLUMNS = ["organizationId", "workspaceId", "environmentId"] as const;

// A helper receiving ids to filter by is scoped; the column name then only appears in whatever builds the `where`.
const TENANT_PARAMETER = /\b(workspaceIds?|organizationIds?|environmentIds?)\b/;

const EXEMPTION = /\/\/\s*tenant-scope-exempt:\s*\S/;

const IGNORED_SEGMENTS = ["node_modules", "/dist/", "/.next/", "/generated/", "/playwright/"];

export interface TenantScopingViolation {
  line: number;
  model: string;
  operation: string;
  column: string;
}

/** Maps each model carrying a tenant column to that column, under the name Prisma exposes on the client. */
export const parseTenantModels = (schema: string): Map<string, string> => {
  const models = new Map<string, string>();
  for (const match of schema.matchAll(/^model\s+(\w+)\s*\{([\s\S]*?)^\}/gm)) {
    const [, name, body] = match;
    const column = TENANT_COLUMNS.find((c) => new RegExp(`^\\s+${c}\\s`, "m").test(body));
    if (column) models.set(name[0].toLowerCase() + name.slice(1), column);
  }
  return models;
};

/** The source between the nearest top-level declaration at or before `position` and the next one. */
export const enclosingFunction = (source: string, position: number): string => {
  const starts = [...source.matchAll(/^(export\s+)?(const|async\s+function|function)\s+\w+/gm)].map(
    (m) => m.index
  );
  const before = starts.filter((s) => s <= position);
  const after = starts.filter((s) => s > position);
  // Index arithmetic rather than `.at(-1)`: this package's tsconfig lib predates it.
  return source.slice(before.length > 0 ? before[before.length - 1] : 0, after[0] ?? source.length);
};

export const findViolationsInSource = (
  source: string,
  tenantModels: Map<string, string>
): TenantScopingViolation[] => {
  const pattern = new RegExp(`prisma\\.([a-zA-Z]+)\\.(${RISKY_OPERATIONS.join("|")})\\s*\\(`, "g");
  const lines = source.split("\n");
  const violations: TenantScopingViolation[] = [];

  for (const match of source.matchAll(pattern)) {
    const column = tenantModels.get(match[1]);
    if (!column) continue;

    const scope = enclosingFunction(source, match.index);
    if (scope.includes(column) || TENANT_PARAMETER.test(scope)) continue;

    const line = source.slice(0, match.index).split("\n").length;
    // The exemption sits on the call's own line or the one above, so it reads as a note on this query rather than on the block.
    if ([lines[line - 1], lines[line - 2]].some((l) => l && EXEMPTION.test(l))) continue;

    violations.push({ line, model: match[1], operation: match[2], column });
  }
  return violations;
};

const listSourceFiles = (dir: string, out: string[] = []): string[] => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (IGNORED_SEGMENTS.some((s) => full.includes(s))) continue;
    if (entry.isDirectory()) listSourceFiles(full, out);
    else if (/\.tsx?$/.test(entry.name) && !entry.name.includes(".test.")) out.push(full);
  }
  return out;
};

const readSchema = (): string =>
  readdirSync(SCHEMA_DIR)
    .filter((f) => f.endsWith(".prisma"))
    .map((f) => readFileSync(join(SCHEMA_DIR, f), "utf8"))
    .join("\n");

const main = (): number => {
  const tenantModels = parseTenantModels(readSchema());
  if (tenantModels.size === 0) {
    console.error(
      "✗ tenant scoping check: no tenant-scoped models found — is packages/database/schema readable?"
    );
    return 1;
  }

  const violations = SEARCH_ROOTS.flatMap((root) =>
    listSourceFiles(join(REPO_ROOT, root))
      .map((file) => ({ file, source: readFileSync(file, "utf8") }))
      .filter(({ source }) => source.includes("prisma."))
      .flatMap(({ file, source }) =>
        findViolationsInSource(source, tenantModels).map((v) => ({
          ...v,
          file: relative(REPO_ROOT, file),
        }))
      )
  );

  if (violations.length === 0) {
    console.log(`✓ tenant scoping check passed (${tenantModels.size} tenant-scoped models)`);
    return 0;
  }

  console.error(
    `✗ tenant scoping check: ${violations.length} quer${violations.length === 1 ? "y" : "ies"} with no tenant constraint\n`
  );
  for (const v of violations) {
    console.error(`  ${v.file}:${v.line}`);
    console.error(
      `    prisma.${v.model}.${v.operation}() — the enclosing function never constrains ${v.column}`
    );
  }
  console.error(`
Scope the query by ${TENANT_COLUMNS.join(", ")} or by an id that is itself tenant-scoped.

If the query is legitimately global — telemetry, authentication, a background sweep — say so on the line above it:

    // tenant-scope-exempt: aggregate telemetry, deliberately across all tenants
    prisma.integration.findMany({ select: { type: true }, distinct: ["type"] });
`);
  return 1;
};

// Importing this module (the test does) must not run the check or exit the process.
if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  process.exit(main());
}
