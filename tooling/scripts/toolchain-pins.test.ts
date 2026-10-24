// The Dockerfiles build with the same Node and pnpm as CI, which reads both from the repository: setup-vp takes Node from .node-version and pnpm from package.json's packageManager. A Dockerfile names its Node image itself (a tag pinned by digest), so this checks that tag against .node-version, and it checks that no Dockerfile writes a pnpm version of its own: corepack installs packageManager's.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { basename } from "node:path";
import { describe, expect, test } from "vite-plus/test";

const ROOT = new URL("../../", import.meta.url);
const read = (path: string) => readFileSync(new URL(path, ROOT), "utf8");

const nodeVersion = read(".node-version").trim();
const dockerfiles = execFileSync("git", ["ls-files", "-z"], { cwd: ROOT, encoding: "utf8" })
  .split("\0")
  .filter((path) => basename(path) === "Dockerfile");

/** The node images a Dockerfile names, as an ARG default or straight in a FROM line. */
const nodeImages = (lines: string[]) =>
  lines.flatMap((line) => line.match(/^(?:ARG \w+=|FROM (?:--\S+ )*)(node:\S+)/)?.[1] ?? []);

describe("Dockerfile toolchain pins", () => {
  test("covers the images that build with Node", () => {
    for (const path of ["deploy/docker/Dockerfile", "apps/website/Dockerfile"]) {
      expect(dockerfiles).toContain(path);
      expect(nodeImages(read(path).split("\n"))).not.toEqual([]);
    }
  });

  describe.each(dockerfiles)("%s", (path) => {
    const lines = read(path).split("\n");

    test("builds on the Node release .node-version names, pinned by digest", () => {
      const pinned = new RegExp(`^node:${nodeVersion.replaceAll(".", "\\.")}-[^@\\s]+@sha256:[0-9a-f]{64}$`);
      for (const image of nodeImages(lines)) expect(image).toMatch(pinned);
    });

    test("writes no pnpm version of its own", () => {
      expect(lines.filter((line) => /pnpm@\d/.test(line))).toEqual([]);
    });

    // An image built on Node builds the workspace, with pnpm.
    test.skipIf(nodeImages(lines).length === 0)(
      "installs packageManager's pnpm once package.json is copied",
      () => {
        const copy = lines.findIndex((line) => /^COPY (?:\S+ )*package\.json\b/.test(line));
        const install = lines.findIndex((line) => line.trim() === "RUN corepack install");
        expect(copy).toBeGreaterThan(-1);
        expect(install).toBeGreaterThan(copy);
      }
    );
  });
});
