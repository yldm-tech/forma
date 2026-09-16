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
    // `modules/` is not covered yet — 67 files there still import from `app/`, mostly route components under `(app)/workspaces` and helpers under `api/v3`. That is a design problem rather than a misplacement, so it needs its own change before a rule like this can be turned on for that directory.
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
];

export default config;
