// Runs the release workflow's final step, the one that moves `stable` and `latest`, under bash in a real git repository, with stand-ins for gh (which emulates GitHub's git data API on that repository) and docker on PATH, and checks what it does. The invariants are the ones release.yml states: `latest` never names an image whose deploy/docker/ files the `stable` branch does not carry, so a refused `stable` update must leave `latest` where it was; `stable` carries those files under docker/ as well, where forma2's installer and guides download them; and `stable` only ever fast-forwards.
import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
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

// Each stand-in appends one line per call to $CALLS: the tool, its arguments and the GH_TOKEN it ran with. The gh stand-in answers the git data API calls the step makes from the repository at $STUB_REPO, as GitHub would: trees and commits are written with git plumbing, and a ref update that is not a fast-forward is refused unless forced.
const GH = `#!/usr/bin/env bash
echo "gh token=$GH_TOKEN $*" >> "$CALLS"
cd "$STUB_REPO"
refuse_ref() {
  [ "$STUB_STABLE_REFUSED" != true ] || { echo "refusing to allow a GitHub App to create or update workflow without workflows permission" >&2; exit 1; }
}
field() {
  while [ $# -gt 1 ]; do
    case "$2" in "$FIELD"=*) echo "\${2#*=}"; return ;; esac
    shift
  done
}
case "$*" in
  "api repos/$GH_REPO/git/ref/heads/main --jq .object.sha") echo "$STUB_MAIN_SHA" ;;
  "api repos/$GH_REPO/git/ref/heads/stable"*) git rev-parse -q --verify refs/heads/stable || { echo "HTTP 404: Not Found" >&2; exit 1; } ;;
  "api -X POST repos/$GH_REPO/git/trees "*)
    request=$(cat)
    export GIT_INDEX_FILE="$STUB_REPO/.git/stub-index"
    rm -f "$GIT_INDEX_FILE"
    git read-tree "$(jq -r .base_tree <<<"$request")"
    jq -r '.tree[] | "\\(.mode),\\(.sha),\\(.path)"' <<<"$request" | while read -r entry; do git update-index --add --cacheinfo "$entry"; done
    git write-tree
    rm -f "$GIT_INDEX_FILE" ;;
  "api -X POST repos/$GH_REPO/git/commits "*)
    parents=()
    for arg in "$@"; do
      case "$arg" in parents*=*) parents+=(-p "\${arg#*=}") ;; esac
    done
    git commit-tree "$(FIELD=tree field "$@")" "\${parents[@]}" -m "$(FIELD=message field "$@")" ;;
  "api -X PATCH repos/$GH_REPO/git/refs/heads/stable "*)
    refuse_ref
    sha=$(FIELD=sha field "$@")
    git merge-base --is-ancestor refs/heads/stable "$sha" || { echo "HTTP 422: Update is not a fast forward" >&2; exit 1; }
    git update-ref refs/heads/stable "$sha" ;;
  "api -X POST repos/$GH_REPO/git/refs "*)
    refuse_ref
    git update-ref refs/heads/stable "$(FIELD=sha field "$@")" "" ;;
  "release view"*) exit 1 ;;
esac
`;
const DOCKER = `#!/usr/bin/env bash
echo "docker $*" >> "$CALLS"
`;

let dir: string;
let repo: string;
let gitEnv: Record<string, string>;

const git = (...args: string[]) =>
  execFileSync("git", args, { cwd: repo, env: gitEnv, encoding: "utf8" }).trim();

const writeFile = (path: string, content: string, mode = 0o644) => {
  mkdirSync(dirname(join(repo, path)), { recursive: true });
  writeFileSync(join(repo, path), content);
  chmodSync(join(repo, path), mode);
};

const commit = (message: string) => {
  git("add", "-A");
  git("commit", "-q", "-m", message);
  return git("rev-parse", "HEAD");
};

/** `git ls-tree -r` of a directory at a commit, with the directory prefix stripped: mode, type, blob and name. */
const files = (rev: string, path: string) =>
  git("ls-tree", "-r", rev, `${path}/`)
    .split("\n")
    .filter(Boolean)
    .map((line) => line.replace(`\t${path}/`, "\t"));

const parents = (rev: string) => git("rev-list", "--parents", "-n", "1", rev).split(" ").slice(1);

const stableSha = () =>
  spawnSync("git", ["rev-parse", "-q", "--verify", "refs/heads/stable"], {
    cwd: repo,
    env: gitEnv,
    encoding: "utf8",
  }).stdout.trim();

// The release commit's parent, and the release commit: a repository with the files the step reads, deploy/docker/ among them.
let base: string;
let release: string;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "release-workflow-"));
  repo = join(dir, "repo");
  mkdirSync(repo);
  const bin = join(dir, "bin");
  mkdirSync(bin);
  for (const [tool, body] of [
    ["gh", GH],
    ["docker", DOCKER],
  ]) {
    writeFileSync(join(bin, tool), body);
    chmodSync(join(bin, tool), 0o755);
  }
  gitEnv = {
    PATH: `${bin}:${process.env.PATH}`,
    HOME: dir,
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_AUTHOR_NAME: "Release",
    GIT_AUTHOR_EMAIL: "release@example.com",
    GIT_COMMITTER_NAME: "Release",
    GIT_COMMITTER_EMAIL: "release@example.com",
  };
  git("init", "-q", "-b", "main");
  writeFile("README.md", "forma\n");
  writeFile(".github/workflows/release.yml", "name: Release\n");
  base = commit("base");
  writeFile("deploy/docker/Dockerfile", "FROM scratch\n");
  writeFile("deploy/docker/docker-compose.yml", "services: {}\n");
  writeFile("deploy/docker/forma.sh", "#!/usr/bin/env bash\n", 0o755);
  writeFile("deploy/docker/authzed-postgres-bootstrap.sh", "#!/usr/bin/env bash\n", 0o755);
  writeFile("deploy/docker/rustfs-init.sh", "#!/bin/sh\n", 0o755);
  release = commit("release");
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const run = (stub: Record<string, string> = {}) => {
  const calls = join(dir, "calls");
  const summary = join(dir, "summary");
  writeFileSync(calls, "");
  writeFileSync(summary, "");
  const script = join(dir, "step.sh");
  writeFileSync(script, stepScript(readFileSync(WORKFLOW, "utf8"), STEP));
  // `shell: bash` on a GitHub runner is `bash --noprofile --norc -eo pipefail {0}`.
  const result = spawnSync("bash", ["--noprofile", "--norc", "-eo", "pipefail", script], {
    cwd: repo,
    encoding: "utf8",
    env: {
      ...gitEnv,
      CALLS: calls,
      GITHUB_STEP_SUMMARY: summary,
      STUB_REPO: repo,
      VERSION: "0.2.7",
      IMAGE: "ghcr.io/yldm-tech/forma-server",
      DOCS_IMAGE: "ghcr.io/yldm-tech/forma-docs-site",
      RELEASE_SHA: release,
      GH_REPO: REPO,
      GH_TOKEN: "job-token",
      STABLE_TOKEN: "stable-token",
      MOVE_LATEST: "true",
      STUB_MAIN_SHA: release,
      ...stub,
    },
  });
  return {
    status: result.status,
    stderr: result.stderr,
    calls: readFileSync(calls, "utf8").trim().split("\n").filter(Boolean),
    summary: readFileSync(summary, "utf8"),
  };
};

const movesLatest = (call: string) =>
  call.startsWith("docker buildx imagetools create") && call.includes(":latest");
const movesStable = (call: string) => call.startsWith("gh ") && call.includes(`repos/${REPO}/git/refs`);
const writesGitData = (call: string) =>
  call.startsWith("gh ") && call.includes("-X") && call.includes(`repos/${REPO}/git/`);

/** stable's tree is the release's, plus deploy/docker/ (the Dockerfile aside) again under docker/, the same blobs with the same modes. */
const expectCarriesRelease = (rev: string) => {
  expect(parents(stableSha())[0]).toBe(rev);
  expect(files("stable", "docker")).toEqual(
    files(rev, "deploy/docker").filter((line) => !line.endsWith("\tDockerfile"))
  );
  expect(files("stable", "docker").map((line) => line.split("\t")[1])).toEqual([
    "authzed-postgres-bootstrap.sh",
    "docker-compose.yml",
    "forma.sh",
    "rustfs-init.sh",
  ]);
  expect(git("diff", "--name-only", rev, "stable", "--", ":(exclude)docker")).toBe("");
};

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
    expect(stableSha()).toBe("");
  });

  test("still records the release when the stable update is refused, since the image and its tag are published", () => {
    const { calls } = run({ STUB_STABLE_REFUSED: "true" });
    expect(calls.some((call) => call.includes(`release create v0.2.7`))).toBe(true);
  });

  test("creates stable on the first move, carrying deploy/docker/ under docker/ as well, then moves latest", () => {
    const { status, calls, summary, stderr } = run();
    expect(stderr).toBe("");
    expect(status).toBe(0);
    expectCarriesRelease(release);
    expect(parents("stable")).toEqual([release]);
    const stable = calls.findIndex(movesStable);
    const latest = calls.findIndex(movesLatest);
    expect(stable).toBeGreaterThanOrEqual(0);
    expect(latest).toBeGreaterThan(stable);
    expect(calls.filter(writesGitData).length).toBe(3);
    for (const call of calls.filter(writesGitData)) expect(call).toContain("token=stable-token");
    expect(summary).toContain("the `stable` branch now point here");
  });

  test("fast-forwards stable from the previous release's commit to the next release", () => {
    expect(run().status).toBe(0);
    const previous = stableSha();
    writeFile("deploy/docker/docker-compose.yml", "services:\n  forma: {}\n");
    const next = commit("next release");
    const { status, calls } = run({ RELEASE_SHA: next, STUB_MAIN_SHA: next });
    expect(status).toBe(0);
    expectCarriesRelease(next);
    expect(parents("stable")).toEqual([next, previous]);
    expect(git("show", "stable:docker/docker-compose.yml")).toBe("services:\n  forma: {}");
    expect(calls.some(movesLatest)).toBe(true);
  });

  test("a rerun leaves stable where it is and moves latest again", () => {
    expect(run().status).toBe(0);
    const first = stableSha();
    const { status, calls, summary } = run();
    expect(status).toBe(0);
    expect(stableSha()).toBe(first);
    expect(calls.filter(movesStable)).toEqual([]);
    expect(calls.some(movesLatest)).toBe(true);
    expect(summary).toContain("`stable` already carries this release");
  });

  test("fast-forwards a stable that is on main with the release as the only parent", () => {
    git("update-ref", "refs/heads/stable", base);
    const { status } = run();
    expect(status).toBe(0);
    expectCarriesRelease(release);
    expect(parents("stable")).toEqual([release]);
  });

  test.each([
    [
      "off main",
      () => {
        git("checkout", "-q", "-b", "elsewhere", base);
        writeFile("README.md", "elsewhere\n");
        return commit("elsewhere");
      },
    ],
    [
      "on top of the release but changing more than docker/",
      () => {
        git("checkout", "-q", "-b", "elsewhere", release);
        writeFile("docker/forma.sh", "#!/usr/bin/env bash\n");
        writeFile("README.md", "elsewhere\n");
        return commit("elsewhere");
      },
    ],
  ])("refuses to overwrite a stable something else moved (%s) and leaves latest alone", (_, moved) => {
    const elsewhere = moved();
    git("checkout", "-q", "main");
    git("update-ref", "refs/heads/stable", elsewhere);
    const { status, calls } = run();
    expect(status).not.toBe(0);
    expect(stableSha()).toBe(elsewhere);
    expect(calls.filter(movesStable)).toEqual([]);
    expect(calls.filter(movesLatest)).toEqual([]);
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
