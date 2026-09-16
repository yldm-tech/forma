# Repo scripts

Shell and Node entry points that belong to the repository rather than to any one workspace: the dev-environment bootstrap, the catalog check, the lint-staged ESLint shim.

This directory is a workspace purely so its tests run. `turbo run test` only invokes `test` in workspaces, and the repo root is not one, so `setup-dev-env.test.ts` sat here passing by assumption for as long as it existed. Declaring `vitest` here rather than in the root `package.json` is deliberate: a direct dependency at the root shifts what `apps/web` resolves under `nodeLinker: hoisted`, which broke its JSX transform when it was tried.
