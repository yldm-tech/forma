import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import objectHash from "object-hash";
import { afterEach, beforeEach, describe, expect, test, vi } from "vite-plus/test";
import {
  type ScanTarget,
  WEB_TARGET,
  checkExtraTranslations,
  compareWithKnownUnused,
  extractReferencedKeys,
  hasIssues,
  validateTarget,
} from "./scan-translations";

/** Writes files under a fresh directory and returns its path. */
function fixture(files: Record<string, string>): string {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "forma-scan-web-"));
  for (const [name, content] of Object.entries(files)) {
    const file = path.join(root, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, content);
  }
  return root;
}

function lockfile(values: Record<string, string>): string {
  const lines = Object.entries(values).map(([key, value]) => `    ${key}: ${objectHash.MD5(value)}`);
  return ["version: 1", "checksums:", "  55dc377b5e931bfc64e4add83e35d9ea:", ...lines, ""].join("\n");
}

const enUS = {
  auth: { login: "Log in", stale: "Stale" },
  emails: { invite_subject: "You are invited" },
  templates: { nps_name: "Net Promoter Score" },
  common: { unused: "Nobody reads me", recorded: "Recorded as unused" },
};

describe("web scan target", () => {
  let root = "";

  beforeEach(() => {
    vi.spyOn(console, "log").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (root) fs.rmSync(root, { recursive: true, force: true });
    root = "";
  });

  function target(overrides: Partial<ScanTarget> = {}): ScanTarget {
    return {
      name: "fixture",
      sourceDirs: [path.join(root, "web", "src")],
      referenceSources: [
        { dir: path.join(root, "server"), pattern: "**/*.go" },
        { dir: path.join(root, "server", "mailer", "templates"), pattern: "**/*.html" },
        { dir: path.join(root, "server", "catalog"), pattern: "templates.json" },
      ],
      localesDir: path.join(root, "web", "locales"),
      defaultLocale: "en-US",
      knownUnusedKeys: ["common.recorded"],
      ...overrides,
    };
  }

  test("counts keys the Go mailer and the template catalogue read as used, and passes a consistent catalogue", async () => {
    const values = {
      "auth/login": "Log in",
      "auth/stale": "Stale",
      "emails/invite_subject": "You are invited",
      "templates/nps_name": "Net Promoter Score",
      "common/unused": "Nobody reads me",
      "common/recorded": "Recorded as unused",
    };
    root = fixture({
      "web/src/login.tsx": `export const Login = () => <p>{t("auth.login")}</p>;\nexport const s = t("auth.stale");\nexport const u = t("common.unused");`,
      "server/mailer/render.go": `func (Invite) subjectKey() string { return "emails.invite_subject" }\nvar host = "forma.ylam.ai"`,
      "server/mailer/templates/invite.html": `{{template "heading" (.T "emails.invite_subject")}}`,
      "server/catalog/templates.json": `[{"name": "{{templates.nps_name}}"}]`,
      "web/locales/en-US.json": JSON.stringify(enUS),
      "web/locales/ja-JP.json": JSON.stringify(enUS),
      "web/i18n.lock": lockfile(values),
    });

    const results = await validateTarget(target());

    expect([...results.missingKeys]).toEqual([]);
    expect([...results.unusedKeys]).toEqual([]);
    expect(results.staleKnownUnusedKeys).toEqual([]);
    expect(results.usedKeys).toContain("emails.invite_subject");
    expect(results.usedKeys).toContain("templates.nps_name");
    // A dotted string that is no catalogue key is not a missing key.
    expect(results.usedKeys).not.toContain("forma.ylam.ai");
    expect(hasIssues(results)).toBe(false);
  });

  test("reports unused, missing, stale, extra and untranslated keys", async () => {
    const jaJP = { ...enUS, auth: { login: "ログイン" }, legacy: { gone: "古い" } };
    root = fixture({
      "web/src/login.tsx": `export const a = t("auth.login");\nexport const b = t("auth.missing");\nexport const c = t("common.recorded");`,
      "server/mailer/render.go": `const key = "emails.invite_subject"`,
      "web/locales/en-US.json": JSON.stringify(enUS),
      "web/locales/ja-JP.json": JSON.stringify(jaJP),
      // auth/login changed since the last translation run.
      "web/i18n.lock": lockfile({
        "auth/login": "Sign in",
        "auth/stale": "Stale",
        "emails/invite_subject": "You are invited",
        "templates/nps_name": "Net Promoter Score",
        "common/unused": "Nobody reads me",
        "common/recorded": "Recorded as unused",
      }),
    });

    const results = await validateTarget(target());

    expect([...results.missingKeys]).toEqual(["auth.missing"]);
    expect([...results.unusedKeys].sort()).toEqual(["auth.stale", "common.unused", "templates.nps_name"]);
    expect(results.staleKnownUnusedKeys).toEqual(["common.recorded"]);
    expect(results.incompleteTranslations.get("ja-JP")).toEqual(["auth.stale"]);
    expect(results.extraTranslations.get("ja-JP")).toEqual(["legacy.gone"]);
    expect(results.lockfileValidation?.outOfSync).toEqual(["auth/login"]);
    expect(hasIssues(results)).toBe(true);
  });

  test("fails without a lockfile", async () => {
    root = fixture({
      "web/src/a.ts": `export const a = t("auth.login");`,
      "web/locales/en-US.json": JSON.stringify({ auth: { login: "Log in" } }),
    });

    const results = await validateTarget(target({ referenceSources: [], knownUnusedKeys: [] }));

    expect(results.lockfileValidation).toBeNull();
    expect(hasIssues(results)).toBe(true);
  });
});

describe("extractReferencedKeys", () => {
  const catalogue = new Set(["emails.subject", "templates.nps_name", "common.ok"]);

  test("finds quoted keys and template markers the catalogue has", () => {
    const go = `return "emails.subject"\n{{templates.nps_name}}\n'common.ok'\n"github.com/gin-gonic/gin"`;
    expect(extractReferencedKeys(go, catalogue)).toEqual([
      "emails.subject",
      "templates.nps_name",
      "common.ok",
    ]);
  });

  test("ignores dotted strings that are no key", () => {
    expect(extractReferencedKeys(`"forma.ylam.ai" "emails.unknown"`, catalogue)).toEqual([]);
  });
});

describe("checkExtraTranslations", () => {
  test("lists keys a target locale has beyond the default locale", () => {
    const byLocale = new Map([
      ["en-US", new Set(["a", "b"])],
      ["de-DE", new Set(["a", "b", "c"])],
      ["ja-JP", new Set(["a"])],
    ]);
    expect(checkExtraTranslations(byLocale, "en-US")).toEqual(new Map([["de-DE", ["c"]]]));
  });
});

describe("compareWithKnownUnused", () => {
  test("splits unrecorded unused keys from recorded keys that are no longer unused", () => {
    const { unexpected, stale } = compareWithKnownUnused(new Set(["a", "b"]), ["b", "c"]);
    expect([...unexpected]).toEqual(["a"]);
    expect(stale).toEqual(["c"]);
  });
});

describe("WEB_TARGET", () => {
  test("reads the admin UI's catalogue and the Go consumers of it", () => {
    for (const dir of [
      ...WEB_TARGET.sourceDirs,
      WEB_TARGET.localesDir,
      ...(WEB_TARGET.referenceSources ?? []).map((s) => s.dir),
    ]) {
      expect(fs.existsSync(dir) ? dir : `missing: ${dir}`).toBe(dir);
    }
  });

  test("keeps the recorded unused keys sorted and unique, so a diff of the list reads as what changed", () => {
    const known = WEB_TARGET.knownUnusedKeys ?? [];
    expect(known).toEqual([...new Set(known)].sort());
  });
});
