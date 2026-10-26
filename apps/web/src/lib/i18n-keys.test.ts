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

// Spaces between CJK characters ("这 将 删除") are rejected by scan-translations (packages/i18n-utils), which CI runs on every catalogue; the checks below cover what it does not.

const isIdeograph = (char: string | undefined): boolean => {
  const code = char?.codePointAt(0) ?? 0;
  return (code >= 0x3400 && code <= 0x9fff) || (code >= 0xf900 && code <= 0xfaff);
};

// Half-width , ? ! ( ) touching a Chinese character, scanned character by character. A ? or , followed by an ASCII letter or digit (a URL query, code) is left alone.
const halfWidthPunctuation = (value: string): boolean => {
  const chars = Array.from(value);
  return chars.some((char, i) => {
    const prev = chars[i - 1];
    const next = chars[i + 1];
    if (char === "(") return isIdeograph(prev) || isIdeograph(next);
    if (char === ")") return isIdeograph(prev) || isIdeograph(next);
    if (char !== "," && char !== "?" && char !== "!") return false;
    if (next !== undefined && /[A-Za-z0-9]/.test(next)) return false;
    return isIdeograph(prev) || isIdeograph(next);
  });
};

test("the punctuation check finds half-width punctuation next to Chinese text only", () => {
  expect(halfWidthPunctuation("你确定要删除这个团队吗?这也会移除")).toBe(true);
  expect(halfWidthPunctuation("例如：score, price")).toBe(false);
  expect(halfWidthPunctuation("部署 Forma,无需修改")).toBe(true);
  expect(halfWidthPunctuation("UTC(默认)")).toBe(true);
  expect(halfWidthPunctuation("动态 (弹出窗口)")).toBe(true);
  expect(halfWidthPunctuation("第一次使用 Forma？")).toBe(false);
  expect(halfWidthPunctuation("将 mavenCentral() 添加到你的仓库中")).toBe(false);
  expect(halfWidthPunctuation("{count, plural, other {已选择{count}项}}")).toBe(false);
});

// zh-Hans-CN writes full-width punctuation in Chinese sentences; the machine translation left "吗?" and "Forma,无需".
test("zh-Hans-CN uses full-width punctuation next to Chinese text", () => {
  expect(flatten(zhHansCN).filter(([, value]) => halfWidthPunctuation(value))).toEqual([]);
});

// zh-Hans-CN addresses Forma users (the admin UI and its emails) as 你 and respondents (the survey pages, contact links, survey content) as 您. Template gallery descriptions are admin text and template questions are survey content, so templates.* is left to review.
const RESPONDENT_CONTENT = new Set([
  "workspace.surveys.edit.follow_ups_modal_action_subject",
  "workspace.workflows.email_subject_placeholder",
]);

test("zh-Hans-CN uses 你 in the admin UI and 您 on respondent pages", () => {
  const values = flatten(zhHansCN);
  const admin = values.filter(
    ([key, value]) =>
      /^(auth|setup|organizations|workspace)\./.test(key) &&
      !RESPONDENT_CONTENT.has(key) &&
      value.includes("您")
  );
  const respondent = values.filter(([key, value]) => /^(s|c)\./.test(key) && /你(?!们)/.test(value));
  expect({ admin, respondent }).toEqual({ admin: [], respondent: [] });
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
  "emails.weekly_summary_email_nps",
  "organizations.two_factor.status_sso",
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
  "workspace.surveys.summary.nps_score_badge",
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

// The responses table renders these as status badges with their own icons; an emoji in the label renders differently on every platform (insights-19).
test("response status labels are plain text in every catalogue", () => {
  const catalogues = { "en-US": enUS, "ja-JP": jaJP, "zh-Hans-CN": zhHansCN, "zh-Hant-TW": zhHantTW };
  const withEmoji = Object.entries(catalogues).flatMap(([locale, catalogue]) =>
    ["workspace.surveys.responses.completed", "workspace.surveys.responses.not_completed"]
      .map((key) => [key, lookup(catalogue, key)] as const)
      .filter(([, value]) => typeof value !== "string" || /\p{Extended_Pictographic}/u.test(value))
      .map(([key, value]) => `${locale}: ${key} = ${String(value)}`)
  );
  expect(withEmoji).toEqual([]);
});
