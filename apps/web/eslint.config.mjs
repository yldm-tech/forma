import { readFileSync } from "node:fs";
import next from "@forma/config-eslint/next";

/*
 * Environment access goes through the validated env module (ENG-1685).
 *
 * `lib/env.ts` parses and type-checks every variable the app consumes at boot (next.config.mjs
 * imports it, so an invalid value fails the build/start instead of the first request that needs
 * it). Reading `process.env` anywhere else opts out of that check and gives contributors a second
 * convention to copy. Client components use `lib/env-client.ts` instead — see the note there.
 *
 * The selectors below cover the idiomatic spellings; they are a convention guardrail, not a
 * security boundary. Anything that hides `process` behind a binding still slips through
 * (`globalThis.process.env.X`, `const p = process; p.env.X`, `const { env } = process; env.X`),
 * because a lint selector cannot follow a value across assignments. Nobody reaches for those by
 * accident, and someone determined to bypass the rule can just write an eslint-disable comment.
 */
/*
 * ESLint-baseline ratchet (ENG-2264).
 *
 * `@forma/config-eslint/next` downgrades all 85 error-severity rules of `js.configs.recommended` and `tseslint.configs.recommended` to warnings, because apps/web was never linted against them and turning them on in one step would block every PR. This block promotes back to `error` every one of those rules that is already at zero here, so new code cannot reintroduce a defect class the app has already paid off. The severity-only entry keeps whatever options the baseline configured — none of the 85 ships any, but that is the rule the form relies on.
 *
 * The arithmetic, measured with `eslint . -f json` in apps/web: 18 rules still have findings and stay at `warn` (RATCHET_PENDING below, with their counts); 20 more are already switched off here rather than merely downgraded, so promoting them would change nothing — 19 are off for `.ts`/`.tsx` via `typescript-eslint/eslint-recommended` because `tsc --noEmit` reports them instead (`no-const-assign`, `no-undef`, `getter-return`, `constructor-super` and the rest of that set), and `eslint-config-prettier` deliberately switches off `no-unexpected-multiline`. The remaining 47 are the list below.
 *
 * To promote one: fix its findings, delete its line from RATCHET_PENDING, add it here, and confirm `pnpm --filter @forma/web lint` reports no errors. The counts are a ratchet baseline, not an assertion — they drift as code lands, and a stale count is a diff a reviewer can see, which is the point of keeping them in code rather than on the ticket.
 */
const RATCHET_PENDING = {
  "@typescript-eslint/no-explicit-any": 1367,
  "prefer-const": 104,
  "@typescript-eslint/no-unused-vars": 43,
  "@typescript-eslint/no-unsafe-function-type": 16,
  "@typescript-eslint/ban-ts-comment": 15,
  "@typescript-eslint/no-unused-expressions": 10,
  "no-useless-catch": 8,
  "@typescript-eslint/no-empty-object-type": 7,
  "no-case-declarations": 6,
  "@typescript-eslint/no-require-imports": 5,
  "no-prototype-builtins": 5,
  "@typescript-eslint/no-wrapper-object-types": 5,
  "no-extra-boolean-cast": 4,
  "no-useless-escape": 3,
  "no-var": 3,
  "@typescript-eslint/no-non-null-asserted-optional-chain": 3,
  "@typescript-eslint/prefer-as-const": 2,
  "no-control-regex": 1,
};

// At zero in apps/web and enforced. Every entry is a defect class rather than a style preference, so a new violation is a bug report rather than a formatting quibble.
const PROMOTED_TO_ERROR = [
  "@typescript-eslint/no-array-constructor",
  "@typescript-eslint/no-duplicate-enum-values",
  "@typescript-eslint/no-extra-non-null-assertion",
  "@typescript-eslint/no-misused-new",
  "@typescript-eslint/no-namespace",
  "@typescript-eslint/no-this-alias",
  "@typescript-eslint/no-unnecessary-type-constraint",
  "@typescript-eslint/no-unsafe-declaration-merging",
  "@typescript-eslint/prefer-namespace-keyword",
  "@typescript-eslint/triple-slash-reference",
  "for-direction",
  "no-async-promise-executor",
  "no-compare-neg-zero",
  "no-cond-assign",
  "no-constant-binary-expression",
  "no-constant-condition",
  "no-debugger",
  "no-delete-var",
  "no-dupe-else-if",
  "no-duplicate-case",
  "no-empty",
  "no-empty-character-class",
  "no-empty-pattern",
  "no-empty-static-block",
  "no-ex-assign",
  "no-fallthrough",
  "no-global-assign",
  "no-invalid-regexp",
  "no-irregular-whitespace",
  "no-loss-of-precision",
  "no-misleading-character-class",
  "no-nonoctal-decimal-escape",
  "no-octal",
  "no-regex-spaces",
  "no-self-assign",
  "no-shadow-restricted-names",
  "no-sparse-arrays",
  "no-unsafe-finally",
  "no-unsafe-optional-chaining",
  "no-unused-labels",
  "no-unused-private-class-members",
  "no-useless-backreference",
  "prefer-rest-params",
  "prefer-spread",
  "require-yield",
  "use-isnan",
  "valid-typeof",
];

// The files under `modules/` that still import from `app/`. The list is committed at `scripts/modules-app-imports-baseline.json` and read here so the count lives in exactly one place — `scripts/check-modules-app-imports.mjs` is what keeps it shrinking (it fails on a new violation and on an entry that no longer violates), and `scripts/check-modules-app-imports.test.ts` is what runs that check in CI. Paths are repo-relative there and config-relative here.
const MODULES_APP_IMPORT_BACKLOG = JSON.parse(
  readFileSync(new URL("../../scripts/modules-app-imports-baseline.json", import.meta.url), "utf8")
).files.map((file) => file.replace("apps/web/", ""));

const PROCESS_ENV_MESSAGE =
  "Read environment variables through the validated env module: `@/lib/env` (or the derived constants in `@/lib/constants`) on the server, `@/lib/env-client` in client components. Direct `process.env` access skips schema validation, so a missing or mistyped variable fails at use-time instead of at boot. Bootstrap, config, script and test files are exempt — see apps/web/eslint.config.mjs.";

// Injected by Next.js itself rather than by a deployment, so there is nothing for the schema to
// validate and no way for them to go missing at runtime. `NODE_ENV` is deliberately NOT here: it
// is part of the schema, and server code should read it via `@/lib/constants`.
const FRAMEWORK_INJECTED_ENV_VARS = "^(NEXT_RUNTIME|NEXT_PHASE)$";

const PROCESS_ENV_ACCESS = '[object.object.name="process"][object.property.name="env"]';

const noDirectProcessEnv = [
  // `process.env.SOME_VAR`
  {
    selector: `MemberExpression${PROCESS_ENV_ACCESS}[computed=false]:not([property.name=/${FRAMEWORK_INJECTED_ENV_VARS}/])`,
    message: PROCESS_ENV_MESSAGE,
  },
  // `process.env["SOME_VAR"]` and `process.env[someKey]`
  {
    selector: `MemberExpression${PROCESS_ENV_ACCESS}[computed=true]`,
    message: PROCESS_ENV_MESSAGE,
  },
  // Bare `process.env` — spreading it, destructuring it, or aliasing it would otherwise slip past
  // the two selectors above. The `:not()` skips the inner node of a `process.env.X` access so
  // those are reported once, by the matching selector above.
  {
    selector:
      'MemberExpression[object.name="process"][property.name="env"]:not(MemberExpression > MemberExpression)',
    message: PROCESS_ENV_MESSAGE,
  },
  // Any computed access on `process` — `process["env"]`, process[`env`], `process[key]`. The
  // selectors above all key off `env` being an identifier, so a string or template key would
  // otherwise bypass the rule entirely. Computed access on `process` has no legitimate use in
  // application code, so flagging all of it costs nothing and leaves no spelling uncovered.
  {
    selector: 'MemberExpression[object.name="process"][computed=true]',
    message: PROCESS_ENV_MESSAGE,
  },
];

// The shape a survey is read in, and the transform that turns that row into a TSurvey, are one definition each. A second copy of `selectSurvey` had grown in `modules/survey/lib/` and drifted — it was missing `archivedAt`, so a survey read through it was typed as carrying a field the query never selected. Both now live in `lib/survey/`, and this keeps a third from appearing anywhere else.
const noDuplicateSurveyReadShape = ["selectSurvey", "transformPrismaSurvey"].map((name) => ({
  selector: `ExportNamedDeclaration > VariableDeclaration > VariableDeclarator[id.name="${name}"]`,
  message: `\`${name}\` is defined once, in lib/survey/. Import it instead of declaring another copy — the last duplicate drifted from the original and silently changed what a survey read returns.`,
}));

// Files that legitimately read process.env: the env modules themselves, everything that runs
// before (or outside) the Next.js runtime the module is built for, and tests, which set up the
// environment they exercise.
const PROCESS_ENV_EXEMPT_FILES = [
  "lib/env.ts",
  "next.config.mjs",
  "instrumentation.ts",
  "instrumentation-*.ts",
  "sentry.*.config.ts",
  "scripts/**",
  "integration/**",
  "*.config.{ts,mts,mjs}",
  "**/*.test.{ts,tsx}",
  "**/__mocks__/**",
];

const config = [
  // carried over from the legacy .eslintignore / ignorePatterns
  {
    ignores: [".next/**", "public/**", "playwright/**", "vendor/**", "**/package.json", "**/tsconfig.json"],
  },
  ...next,
  {
    // See RATCHET_PENDING above: the recommended baselines arrive downgraded to warnings, and every rule already at zero is put back to error here.
    rules: Object.fromEntries(PROMOTED_TO_ERROR.map((rule) => [rule, "error"])),
  },
  {
    rules: {
      // runtime-only env read in integration/gen-boolean-client.mjs; hashing it in turbo.json is tracked separately (ENG-1682)
      "turbo/no-undeclared-env-vars": ["error", { allowList: ["PATH"] }],
      /*
       * React Compiler-era react-hooks rules (ENG-2366). These were switched off wholesale during
       * the ESLint 9 migration; each now carries the strongest severity its remaining violation
       * count allows, on the same per-rule ratchet as the typescript-eslint baseline (ENG-2264).
       * Counts below are for apps/web and were measured with `--no-inline-config`.
       *
       * Note the app does NOT run the React Compiler (there is no `reactCompiler` in
       * next.config.mjs and no babel plugin), which is what splits these two groups apart.
       */

      // Real bug classes in plain React, so worth enforcing whether or not the compiler is on.
      // At zero and enforced: `purity`, `refs` and `use-memo` come from flat.recommended and are
      // deliberately not listed here — nothing to opt out of.
      "react-hooks/error-boundaries": "error",
      "react-hooks/preserve-manual-memoization": "error",
      // 20 violations across 10 files, and they are genuine defects rather than lint noise:
      // direct mutation of `useState` values (ResponseFilter), assignment to a prop
      // (survey-menu-bar) and mutation of a hook argument (elements-view). Fixing them is
      // behaviour-sensitive work on the survey editor and response filters, so it is ticketed
      // separately (ENG-3071) rather than bundled into the lint change. Promote to "error" once
      // that lands.
      "react-hooks/immutability": "warn",
      // ~98 violations across ~78 files — far too broad to fix in one change, and each one needs
      // a judgement call about whether the effect should derive state instead. The count drifts as
      // new code lands; it is a ratchet baseline, not an assertion. Ratcheted under ENG-3072.
      "react-hooks/set-state-in-effect": "warn",

      // Compiler-conditional advisories: with no compiler in the build these report what *would*
      // be skipped, not a defect. `warn` is also what upstream `flat.recommended` ships.
      // All 28 violations are third-party API shape — react-hook-form's `watch()` and TanStack
      // Table's `useReactTable` — so this rule cannot reach zero while those are in use, and it
      // is deliberately left without a ratchet ticket.
      "react-hooks/incompatible-library": "warn",

      // Kept as a warning (not off): exhaustive-deps is the main guard against stale closures, and the
      // web lint script has no `--max-warnings 0`, so it surfaces violations without blocking.
      "react-hooks/exhaustive-deps": "warn",
      "no-restricted-syntax": ["error", ...noDirectProcessEnv, ...noDuplicateSurveyReadShape],
    },
  },
  {
    files: PROCESS_ENV_EXEMPT_FILES,
    rules: {
      "no-restricted-syntax": "off",
    },
  },
  {
    // Where the two definitions actually live. Deliberately not listing test globs: they are already exempt above, and repeating them here would switch the process.env selectors back on for every spec.
    files: ["lib/survey/**/*.ts"],
    rules: {
      "no-restricted-syntax": ["error", ...noDirectProcessEnv],
    },
  },
  {
    // `lib/` is the bottom layer: routes and feature modules depend on it, and it must not depend back. The dependency used to run both ways because `app/lib/` and `app/middleware/` were libraries that happened to sit inside the App Router tree, so anything wanting them had to import from `app/`. They now live here, and this keeps them from drifting back.
    //
    // Tests are exempt: reaching for a fixture or mock that lives beside the route it was written for is a different thing from production code depending upward, and five specs legitimately do it.
    //
    // The same rule runs for `modules/` in the block below, against a frozen backlog rather than from zero.
    files: ["lib/**/*.ts", "lib/**/*.tsx"],
    ignores: ["lib/**/*.test.ts", "lib/**/*.test.tsx", "lib/**/*.integration.test.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/app/*", "@/app"],
              message:
                "lib/ must not import from app/. Routes and modules depend on lib/, not the other way round — move the shared code into lib/ instead.",
            },
          ],
        },
      ],
    },
  },
  {
    // The other half of the same rule, and the direction AGENTS.md has only ever stated in prose: a feature module must not reach up into a route. A route component several modules need belongs in `modules/`; a module reaching into a route's own `lib/` wants the dependency inverted.
    //
    // This one cannot start from zero — the files in MODULES_APP_IMPORT_BACKLOG already do it, and each needs its own judgement call. So they are exempted by path and the rule gates everything else, which is what turns "do not add new ones" from a sentence into a check. The backlog file is the only place the number lives; the two prose counts that used to state it had already drifted apart (AGENTS.md said 35, the comment above said 67, the real figure is 24).
    //
    // Specs are exempt for the same reason they are above: reaching for a fixture beside the route it was written for is not production code depending upward.
    files: ["modules/**/*.ts", "modules/**/*.tsx"],
    ignores: [
      ...MODULES_APP_IMPORT_BACKLOG,
      "modules/**/*.test.ts",
      "modules/**/*.test.tsx",
      "modules/**/*.integration.test.ts",
      "modules/**/__mocks__/**",
    ],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/app/*", "@/app"],
              message:
                "modules/ must not import from app/. A route component several modules need belongs in modules/; a module reaching into a route's lib/ wants the dependency inverted. The remaining offenders are frozen in scripts/modules-app-imports-baseline.json — that list may only shrink.",
            },
          ],
        },
      ],
    },
  },
];

export default config;
