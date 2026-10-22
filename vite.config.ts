import { defineConfig } from "vite-plus";

// Workspace-wide static checks. `vp check` (format + lint + type-check) reads these blocks from the root even when run inside a package; per-package configs only carry build, test and dev settings.
export default defineConfig({
  lint: {
    plugins: ["eslint", "typescript", "unicorn", "oxc", "react", "jsx-a11y", "import", "vitest"],
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    categories: {
      correctness: "error",
    },
    rules: {
      "eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_" },
      ],
      "typescript/no-explicit-any": "error",
      "react/rules-of-hooks": "error",
      "react/exhaustive-deps": "warn",
      "vite-plus/prefer-vite-plus-imports": "error",
      // React Compiler diagnostics stay advisory, as they were under eslint-plugin-react-hooks 7.
      "react/purity": "warn",
      "react/set-state-in-effect": "warn",
      "react/immutability": "warn",
      // Not part of the baseline these packages were held to under ESLint.
      "vitest/require-mock-type-parameters": "off",
      "vitest/no-conditional-expect": "off",
      "typescript/no-useless-default-assignment": "off",
      "typescript/no-misused-spread": "off",
      "typescript/require-array-sort-compare": "off",
      "unicorn/no-useless-spread": "off",
      "unicorn/no-useless-length-check": "off",
      "unicorn/no-useless-fallback-in-spread": "off",
      "jsx-a11y/prefer-tag-over-role": "off",
      "jsx-a11y/control-has-associated-label": "off",
      // eslint-plugin-jsx-a11y's recommended options: only these handlers count, and load/error listeners on media are expected.
      "jsx-a11y/no-noninteractive-element-interactions": [
        "error",
        {
          handlers: [
            "onClick",
            "onError",
            "onLoad",
            "onMouseDown",
            "onMouseUp",
            "onKeyPress",
            "onKeyDown",
            "onKeyUp",
          ],
          alert: ["onKeyUp", "onKeyDown", "onKeyPress"],
          body: ["onError", "onLoad"],
          dialog: ["onKeyUp", "onKeyDown", "onKeyPress"],
          iframe: ["onError", "onLoad"],
          img: ["onError", "onLoad"],
        },
      ],
    },
    overrides: [
      {
        // Assertions such as `expect(obj.method).toHaveBeenCalled()` reference methods without calling them.
        files: ["**/*.test.ts", "**/*.test.tsx"],
        rules: { "typescript/unbound-method": "off" },
      },
      {
        // The survey runtime was linted for hook rules only. Type-aware and TypeScript rules are enabled when it is next worked on, not as a side effect of the toolchain switch.
        files: ["packages/surveys/**"],
        rules: {
          "typescript/no-explicit-any": "off",
          "typescript/unbound-method": "off",
          "typescript/no-floating-promises": "off",
          "typescript/no-base-to-string": "off",
          "typescript/no-misused-spread": "off",
          "typescript/restrict-template-expressions": "off",
          "typescript/no-redundant-type-constituents": "off",
          "typescript/no-useless-default-assignment": "off",
          "eslint/no-unused-vars": "off",
        },
      },
      {
        // forma2's survey editor moves focus to the field the author has just created or opened (a new choice, an added card, a popover's search box), in answer to their own action and never on page load, which is what the rule guards against.
        files: [
          "web/src/features/survey-editor/**",
          "web/src/features/segments/**",
          "web/src/components/ui/date-picker/**",
          "web/src/components/ui/editor/**",
          "web/src/components/ui/input-combo-box.tsx",
        ],
        rules: { "jsx-a11y/no-autofocus": "off" },
      },
      {
        // The link survey page hands the respondent runtime its autoFocus option, as forma2 did: the survey is the whole page, and the option is off whenever the page is framed by another site.
        files: ["web/src/survey-page/components/survey-client-wrapper.tsx"],
        rules: { "jsx-a11y/no-autofocus": "off" },
      },
      {
        // These tests were never linted.
        files: ["packages/survey-ui/src/**/*.test.ts"],
        rules: { "typescript/no-explicit-any": "off" },
      },
      {
        // Known defects carried over from forma2, each to be fixed in its own change: the cleanup removes a fresh closure so the beforeunload listener is never removed; `String(x) ?? fallback` can never fall back; `placeholder` is defined twice in the colour map.
        files: [
          "packages/js-core/src/lib/common/event-listeners.ts",
          "packages/surveys/src/lib/recall.ts",
          "packages/surveys/tailwind.config.cjs",
        ],
        rules: {
          "unicorn/no-invalid-remove-event-listener": "off",
          "eslint/no-constant-binary-expression": "off",
          "eslint/no-dupe-keys": "off",
        },
      },
    ],
    ignorePatterns: [
      "**/dist/**",
      // Storybook stories were never linted or type-checked; Storybook itself compiles them.
      "packages/survey-ui/src/**/*.stories.tsx",
      "packages/survey-ui/src/**/story-helpers.tsx",
      "**/coverage/**",
      "**/storybook-static/**",
      "docs/forma.js",
      "server/**",
      "web/public/js/**",
      "web/src/routeTree.gen.ts",
    ],
    options: {
      typeAware: true,
      typeCheck: true,
    },
  },
  fmt: {
    bracketSpacing: true,
    bracketSameLine: true,
    singleQuote: false,
    jsxSingleQuote: false,
    trailingComma: "es5",
    semi: true,
    printWidth: 110,
    arrowParens: "always",
    importOrder: [
      "(.*)/__mocks__/(.*)",
      "<THIRD_PARTY_MODULES>",
      "^@forma/(.*)$",
      "^~/(.*)$",
      "^@/(.*)$",
      "^[./]",
    ],
    importOrderSeparation: false,
    importOrderSortSpecifiers: true,
    jsonRecursiveSort: true,
    sortPackageJson: false,
    sortTailwindcss: {},
    ignorePatterns: [
      "pnpm-lock.yaml",
      "server/**",
      "web/public/js/**",
      "web/src/routeTree.gen.ts",
      "docs/api-v3-reference/openapi.yml",
      "docs/api-v3-reference/.redocly.lint-ignore.yaml",
      // Oxfmt treats Markdown inside MDX JSX children as one text block and would collapse Mintlify lists and steps.
      "*.mdx",
      // The template's intentionally empty list items would be folded into the label above them.
      ".github/pull_request_template.md",
    ],
  },
  staged: {
    "*.{js,mjs,cjs,ts,mts,cts,tsx,json,md,yml,yaml,css}": "vp check --fix",
    "server/**/*.go": "gofmt -w",
  },
});
