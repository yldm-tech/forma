# Repo scripts

Node entry points that belong to the repository rather than to any one workspace: the catalog check, the bundle budget, the check that every committed test file is run by something, and a test that runs the release workflow's `stable` and `latest` step (`release-workflow.test.ts`).

This directory is a workspace so that its own tests run: `vp run -r test` only runs `test` in workspaces, and `check-tests-run.mjs` would otherwise report these tests as orphans.
