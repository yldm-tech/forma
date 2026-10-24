# `@forma/config-typescript`

Shared `tsconfig` presets for the Forma monorepo. Every package/app `tsconfig.json` extends exactly one of these:

| Config               | Contents                                                                                                              | Used by                                                                              |
| -------------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `base.json`          | strict defaults shared by everything: `module: ESNext`, `moduleResolution: bundler`, declarations, unused-code checks | (composed by the presets below)                                                      |
| `js-library.json`    | base + `ES2022` target/lib — for bundled (Vite) TS libraries                                                          | i18n-utils, js-core, surveys, types, tooling/vite-plugins, tooling/scripts, apps/e2e |
| `react-library.json` | js-library-style settings + `jsx: react-jsx`                                                                          | survey-ui, apps/web, apps/storybook                                                  |
