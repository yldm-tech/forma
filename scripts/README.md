# Repo scripts

Node entry points that belong to the repository rather than to any one workspace: the catalog check, the bundle budget and the check that every committed test file is run by something.

This directory is a workspace so that its own tests run: `vp run -r test` only runs `test` in workspaces, and `check-tests-run.mjs` would otherwise report these tests as orphans.
