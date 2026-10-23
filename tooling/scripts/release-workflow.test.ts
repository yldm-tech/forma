// Runs the release workflow's final step, the one that moves `stable` and `latest`, under bash with stand-ins for gh, git and docker on PATH, and checks the order of what it does. The invariant is the one release.yml states: `latest` never names an image whose deploy/docker/ files the `stable` branch does not carry, so a refused `stable` update must leave `latest` where it was.
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, test } from "vite-plus/test";

const WORKFLOW = new URL("../../.github/workflows/release.yml", import.meta.url);
const STEP = "Publish the release and move stable and latest";

/** The `run: |` block of the named step, dedented, as the runner hands it to bash. */
const stepScript = (yaml: string, name: string) => {
  const lines = yaml.split("\n");
  const start = lines.findIndex((line) => line.trim() === `- name: ${name}`);
  if (start < 0) throw new Error(`release.yml has no step named "${name}"`);
  const runAt = lines.findIndex((line, i) => i > start && /^\s*run: \|\s*$/.test(line));
  if (runAt < 0) throw new Error(`step "${name}" has no run block`);
  const keyIndent = lines[runAt].search(/\S/);
  const body: string[] = [];
  for (const line of lines.slice(runAt + 1)) {
    if (line.trim() !== "" && line.search(/\S/) <= keyIndent) break;
    body.push(line);
  }
  const indent = Math.min(...body.filter((l) => l.trim() !== "").map((l) => l.search(/\S/)));
  return body.map((l) => l.slice(indent)).join("\n");
};

const REPO = "yldm-tech/forma";
const SHA = "1111111111111111111111111111111111111111";

// Each stand-in appends one line per call to $CALLS: the tool, its arguments and the GH_TOKEN it ran with.
const GH = `#!/usr/bin/env bash
echo "gh token=$GH_TOKEN $*" >> "$CALLS"
case "$*" in
  "api repos/$GH_REPO/git/ref/heads/main --jq .object.sha") echo "$STUB_MAIN_SHA" ;;
  "api repos/$GH_REPO/git/ref/heads/stable"*) [ "$STUB_STABLE_EXISTS" = true ] || { echo "HTTP 404: Not Found" >&2; exit 1; } ;;
  *"repos/$GH_REPO/git/refs"*) [ "$STUB_STABLE_REFUSED" != true ] || { echo "refusing to allow a GitHub App to create or update workflow without workflows permission" >&2; exit 1; } ;;
  "release view"*) exit 1 ;;
esac
`;
const GIT = `#!/usr/bin/env bash
echo "git $*" >> "$CALLS"
case "$*" in
  "push "*stable*) [ "$STUB_STABLE_REFUSED" != true ] || { echo "refusing to allow a GitHub App to create or update workflow without workflows permission" >&2; exit 1; } ;;
esac
`;
const DOCKER = `#!/usr/bin/env bash
echo "docker $*" >> "$CALLS"
`;

let dir: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "release-workflow-"));
  for (const [tool, body] of [
    ["gh", GH],
    ["git", GIT],
    ["docker", DOCKER],
  ]) {
    writeFileSync(join(dir, tool), body);
    chmodSync(join(dir, tool), 0o755);
  }
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const run = (stub: Record<string, string>) => {
  const calls = join(dir, "calls");
  const summary = join(dir, "summary");
  writeFileSync(calls, "");
  writeFileSync(summary, "");
  const script = join(dir, "step.sh");
  writeFileSync(script, stepScript(readFileSync(WORKFLOW, "utf8"), STEP));
  // `shell: bash` on a GitHub runner is `bash --noprofile --norc -eo pipefail {0}`.
  const result = spawnSync("bash", ["--noprofile", "--norc", "-eo", "pipefail", script], {
    encoding: "utf8",
    env: {
      PATH: `${dir}:${process.env.PATH}`,
      CALLS: calls,
      GITHUB_STEP_SUMMARY: summary,
      VERSION: "0.2.7",
      IMAGE: "ghcr.io/yldm-tech/forma",
      DOCS_IMAGE: "ghcr.io/yldm-tech/forma-docs",
      RELEASE_SHA: SHA,
      GH_REPO: REPO,
      GH_TOKEN: "job-token",
      STABLE_TOKEN: "stable-token",
      MOVE_LATEST: "true",
      STUB_MAIN_SHA: SHA,
      STUB_STABLE_EXISTS: "true",
      ...stub,
    },
  });
  return {
    status: result.status,
    calls: readFileSync(calls, "utf8").trim().split("\n").filter(Boolean),
    summary: readFileSync(summary, "utf8"),
  };
};

const movesLatest = (call: string) =>
  call.startsWith("docker buildx imagetools create") && call.includes(":latest");
const movesStable = (call: string) =>
  (call.startsWith("git push") && call.includes("refs/heads/stable")) ||
  (call.startsWith("gh ") && call.includes(`repos/${REPO}/git/refs`));

describe(`release.yml "${STEP}"`, () => {
  test("bash parses the extracted step", () => {
    const script = join(dir, "parse.sh");
    writeFileSync(script, stepScript(readFileSync(WORKFLOW, "utf8"), STEP));
    expect(() => execFileSync("bash", ["-n", script])).not.toThrow();
  });

  test("leaves latest alone and fails when the stable update is refused", () => {
    const { status, calls } = run({ STUB_STABLE_REFUSED: "true" });
    expect(status).not.toBe(0);
    expect(calls.some(movesStable)).toBe(true);
    expect(calls.filter(movesLatest)).toEqual([]);
  });

  test("still records the release when the stable update is refused, since the image and its tag are published", () => {
    const { calls } = run({ STUB_STABLE_REFUSED: "true" });
    expect(calls.some((call) => call.includes(`release create v0.2.7`))).toBe(true);
  });

  test("moves stable with the stable token, then latest", () => {
    const { status, calls, summary } = run({});
    expect(status).toBe(0);
    const stable = calls.findIndex(movesStable);
    const latest = calls.findIndex(movesLatest);
    expect(stable).toBeGreaterThanOrEqual(0);
    expect(latest).toBeGreaterThan(stable);
    expect(calls[stable]).toContain("token=stable-token");
    expect(summary).toContain("the `stable` branch now point here");
  });

  test("creates stable on the first move", () => {
    const { status, calls } = run({ STUB_STABLE_EXISTS: "false" });
    expect(status).toBe(0);
    expect(calls.some(movesStable)).toBe(true);
    expect(calls.some(movesLatest)).toBe(true);
  });

  test("moves neither when main has moved on", () => {
    const { status, calls } = run({ STUB_MAIN_SHA: "2222222222222222222222222222222222222222" });
    expect(status).toBe(0);
    expect(calls.filter(movesStable)).toEqual([]);
    expect(calls.filter(movesLatest)).toEqual([]);
  });

  test("moves neither unless RELEASE_MOVE_LATEST is true", () => {
    const { status, calls } = run({ MOVE_LATEST: "" });
    expect(status).toBe(0);
    expect(calls.filter(movesStable)).toEqual([]);
    expect(calls.filter(movesLatest)).toEqual([]);
  });
});
