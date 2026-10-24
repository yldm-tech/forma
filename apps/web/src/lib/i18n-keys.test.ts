import { expect, test } from "vite-plus/test";
import enUS from "../../locales/en-US.json";
import jaJP from "../../locales/ja-JP.json";
import zhHansCN from "../../locales/zh-Hans-CN.json";
import zhHantTW from "../../locales/zh-Hant-TW.json";

// Every literal key the screens pass to t() must exist in every catalogue, or a screen shows a raw key (or English) in that language.
const sources = import.meta.glob<string>(
  ["../**/*.{ts,tsx}", "!../**/*.test.{ts,tsx}", "!../routeTree.gen.ts"],
  {
    query: "?raw",
    import: "default",
    eager: true,
  }
);

const KEY = /\bt\(\s*"([a-z][\w-]*(?:\.[\w-]+)+)"/g;

// Resolves a key the way i18next does: segment by segment, and, like i18next's deepFind, also through a catalogue key that itself contains dots (forma2 wrote "display_criteria.everyone" beside "display_criteria").
const lookup = (catalogue: unknown, key: string): unknown => {
  const resolve = (node: unknown, parts: string[]): unknown => {
    if (parts.length === 0) return node;
    if (!node || typeof node !== "object") return undefined;
    const record = node as Record<string, unknown>;
    for (let end = 1; end <= parts.length; end++) {
      const segment = parts.slice(0, end).join(".");
      if (!Object.hasOwn(record, segment)) continue;
      const found = resolve(record[segment], parts.slice(end));
      if (found !== undefined) return found;
    }
    return undefined;
  };
  return resolve(catalogue, key.split("."));
};

test("every translation key used by the SPA exists in all four catalogues", () => {
  const keys = new Set<string>();
  for (const source of Object.values(sources)) {
    for (const match of source.matchAll(KEY)) if (match[1]) keys.add(match[1]);
  }
  expect(keys.size).toBeGreaterThan(100);
  const catalogues = { "en-US": enUS, "ja-JP": jaJP, "zh-Hans-CN": zhHansCN, "zh-Hant-TW": zhHantTW };
  const missing = Object.entries(catalogues).flatMap(([locale, catalogue]) =>
    [...keys].filter((key) => typeof lookup(catalogue, key) !== "string").map((key) => `${locale}: ${key}`)
  );
  expect(missing).toEqual([]);
});

type Catalogue = { [key: string]: string | Catalogue };

const flatten = (catalogue: Catalogue, prefix = ""): [string, string][] =>
  Object.entries(catalogue).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return typeof value === "string" ? [[path, value] as [string, string]] : flatten(value, path);
  });

const unspacedCatalogues = { "ja-JP": jaJP, "zh-Hans-CN": zhHansCN, "zh-Hant-TW": zhHantTW };

// CJK ideographs, kana, and CJK / full-width punctuation: text written without spaces between words.
const isCjk = (char: string): boolean => {
  const code = char.codePointAt(0) ?? 0;
  return (
    (code >= 0x3000 && code <= 0x30ff) ||
    (code >= 0x31f0 && code <= 0x31ff) ||
    (code >= 0x3400 && code <= 0x4dbf) ||
    (code >= 0x4e00 && code <= 0x9fff) ||
    (code >= 0xf900 && code <= 0xfaff) ||
    (code >= 0xff00 && code <= 0xffef) ||
    (code >= 0x20000 && code <= 0x2ffff)
  );
};

// Scans character by character for a run of spaces with a CJK character on each side ("这 将 删除"). Spaces next to Latin words, digits and {placeholders} stay.
const hasCjkSpacing = (value: string): boolean => {
  const chars = Array.from(value);
  for (let i = 1; i < chars.length; i++) {
    if (chars[i] !== " " || !isCjk(chars[i - 1] ?? "")) continue;
    let end = i;
    while (chars[end] === " ") end++;
    if (end < chars.length && isCjk(chars[end] ?? "")) return true;
    i = end;
  }
  return false;
};

test("the CJK spacing check finds spaces between CJK characters only", () => {
  expect(hasCjkSpacing("这 将 删除")).toBe(true);
  expect(hasCjkSpacing("糟糕 ，偵測到小螢幕！")).toBe(true);
  expect(hasCjkSpacing("リソース の 読み込み")).toBe(true);
  expect(hasCjkSpacing("第一次使用 Forma？")).toBe(false);
  expect(hasCjkSpacing("将删除 {count} 个回复")).toBe(false);
  expect(hasCjkSpacing("{value, plural, other { 件の回答}}")).toBe(false);
});

// Machine translation split zh-Hans-CN into words with spaces ("邀请 未 找到"); a later lingo.dev run must not bring that back.
test("zh and ja catalogues have no spaces between CJK characters", () => {
  const spaced = Object.entries(unspacedCatalogues).flatMap(([locale, catalogue]) =>
    flatten(catalogue)
      .filter(([, value]) => hasCjkSpacing(value))
      .map(([key, value]) => `${locale}: ${key} = ${value}`)
  );
  expect(spaced).toEqual([]);
});

// zh-Hans-CN writes 账户 (the 帐户 variant is the older and Taiwan form).
test("zh-Hans-CN uses 账户, not 帐户", () => {
  expect(flatten(zhHansCN).filter(([, value]) => value.includes("帐户"))).toEqual([]);
});

// Keys whose value is a brand, product, technical term or example that stays the same in every language.
const SAME_IN_EVERY_LOCALE = new Set([
  "auth.oauth.forma_website",
  "common.html",
  "common.id",
  "common.powered_by_forma",
  "common.saas",
  "common.url",
  "common.webhook",
  "common.webhooks",
  "templates.collect_feedback_question_6_choice_1",
  "templates.collect_feedback_question_6_choice_4",
  "templates.collect_feedback_question_7_placeholder",
  "templates.csat_survey_name",
  "templates.new_integration_survey_question_1_choice_1",
  "templates.new_integration_survey_question_1_choice_2",
  "templates.new_integration_survey_question_1_choice_3",
  "templates.new_integration_survey_question_1_choice_4",
  "workspace.actions.url",
  "workspace.app-connection.install_method_android",
  "workspace.app-connection.install_method_flutter",
  "workspace.app-connection.install_method_gtm",
  "workspace.app-connection.install_method_html",
  "workspace.app-connection.install_method_nextjs",
  "workspace.app-connection.install_method_react",
  "workspace.app-connection.install_method_react_native",
  "workspace.app-connection.install_method_swift",
  "workspace.app-connection.install_method_vue",
  "workspace.app-connection.install_method_wordpress",
  "workspace.settings.billing.plan_hobby",
  "workspace.settings.billing.plan_pro",
  "workspace.settings.billing.plan_scale",
  "workspace.settings.general.ai_enabled",
  "workspace.surveys.ai_create.characters",
  "workspace.surveys.edit.validation.kb",
  "workspace.surveys.edit.validation.mb",
  "workspace.surveys.featured_templates.csat_title",
  "workspace.surveys.responses.os",
  "workspace.surveys.share.anonymous_links.custom_single_use_id_placeholder",
  "workspace.surveys.share.pretty_url.slug_placeholder",
  "workspace.workflows.email_to_placeholder",
  "workspace.xm-templates.ces",
  "workspace.xm-templates.csat",
  "workspace.xm-templates.enps",
  "workspace.xm-templates.nps",
]);

// A value left in English in a zh or ja catalogue shows English on that screen (the editor's block categories and the whole OAuth consent page did).
test("zh and ja values differ from en-US unless the key is a brand or technical term", () => {
  const english = new Map(flatten(enUS));
  const untranslated = Object.entries(unspacedCatalogues).flatMap(([locale, catalogue]) =>
    flatten(catalogue)
      .filter(
        ([key, value]) =>
          value === english.get(key) && /[A-Za-z]{2}/.test(value) && !SAME_IN_EVERY_LOCALE.has(key)
      )
      .map(([key, value]) => `${locale}: ${key} = ${value}`)
  );
  expect(untranslated).toEqual([]);
});
