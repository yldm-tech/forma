/**
 * Translation Key Scanner
 *
 * This script scans the admin UI (apps/web/) and the survey runtime (packages/surveys) for translation keys and validates them against
 * their catalogues: keys used but missing, keys present but unused, keys with spaces, keys a locale lacks or has beyond en-US, and
 * en-US values that changed since the last translation run (the Lingo.dev lockfile, i18n.lock).
 *
 * Usage:
 *   pnpm scan-translations
 *
 * Exit codes:
 *   0: Success, no issues found
 *   1: Validation errors found (missing or unused keys)
 *   2: Invalid or missing API key
 */
import { glob } from "glob";
import * as fs from "node:fs";
import * as path from "node:path";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import objectHash from "object-hash";

// Get __dirname equivalent in ES modules
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const REPO_ROOT = path.join(__dirname, "..", "..", "..");

/** Another consumer of a catalogue, outside the TypeScript sources: every catalogue key it names, quoted or as a `{{key}}` marker, counts as used. */
export interface ReferenceSource {
  dir: string;
  pattern: string;
}

/** One catalogue and the code that reads it. */
export interface ScanTarget {
  name: string;
  /** TypeScript sources, read for `t("key")` and `i18nKey`. */
  sourceDirs: string[];
  referenceSources?: ReferenceSource[];
  localesDir: string;
  defaultLocale: string;
  /** Keys known to be unused, recorded rather than hidden. The list may only shrink: a new unused key fails, and so does an entry that is used again or gone. */
  knownUnusedKeys?: string[];
}

const WEB_DIR = path.join(REPO_ROOT, "apps", "web");

/**
 * The admin UI's catalogue, apps/web/locales, is read by the SPA and by the Go server: the mailer renders the `emails` namespace (apps/server/internal/platform/mailer, from an embedded copy a Go test keeps identical) and the survey template catalogue resolves `{{templates.*}}` markers (apps/server/internal/survey/catalog). forma2 scanned apps/web and packages/email together for the same reason.
 */
export const WEB_TARGET: ScanTarget = {
  name: "Admin UI (web)",
  sourceDirs: [path.join(WEB_DIR, "src")],
  referenceSources: [
    { dir: path.join(REPO_ROOT, "apps", "server", "internal"), pattern: "**/*.go" },
    {
      dir: path.join(REPO_ROOT, "apps", "server", "internal", "platform", "mailer", "templates"),
      pattern: "**/*.html",
    },
    {
      dir: path.join(REPO_ROOT, "apps", "server", "internal", "survey", "catalog"),
      pattern: "templates.json",
    },
  ],
  localesDir: path.join(WEB_DIR, "locales"),
  defaultLocale: "en-US",
  knownUnusedKeys: JSON.parse(
    fs.readFileSync(path.join(__dirname, "..", "web-unused-keys.json"), "utf-8")
  ) as string[],
};

const SURVEYS_PKG_DIR = path.join(REPO_ROOT, "packages", "surveys");

export const SURVEYS_TARGET: ScanTarget = {
  name: "Surveys Package",
  sourceDirs: [SURVEYS_PKG_DIR],
  localesDir: path.join(SURVEYS_PKG_DIR, "locales"),
  defaultLocale: "en-US",
  knownUnusedKeys: JSON.parse(
    fs.readFileSync(path.join(__dirname, "..", "surveys-unused-keys.json"), "utf-8")
  ) as string[],
};

// Patterns to match translation keys
const TRANSLATION_PATTERNS = [
  // Pattern: t("key") or t('key')
  /\bt\s*\(\s*["'](?<temp1>[^"']+)["']/g,
  // Pattern: t(`key`)
  /\bt\s*\(\s*`(?<temp1>[^`]+)`/g,
  // Pattern: <Trans i18nKey="key" /> or <Trans i18nKey='key' />
  /i18nKey\s*=\s*["'](?<temp1>[^"']+)["']/g,
  // Pattern: <Trans i18nKey={"key"} /> or <Trans i18nKey={'key'} />
  /i18nKey\s*=\s*\{\s*["'](?<temp1>[^"']+)["']\s*\}/g,
];

// Extracts string literals from dynamic i18nKey={...} expressions (e.g. ternaries)
const I18N_KEY_BLOCK_PATTERN = /i18nKey\s*=\s*\{(?<block>[\s\S]*?)\}/g;
const STRING_LITERAL_PATTERN = /["'](?<key>[^"']+)["']/g;

// Directories and files to exclude from scanning
const EXCLUDE_DIRS = [
  "**/node_modules/**",
  "**/dist/**",
  "**/build/**",
  "**/.next/**",
  "**/coverage/**",
  "**/locales/**",
  "**/*.test.ts",
  "**/*.test.tsx",
  "**/*.spec.ts",
  "**/*.spec.tsx",
];

export interface TranslationKeys {
  [key: string]: string | TranslationKeys;
}

export interface LockfileValidationResults {
  missing: string[];
  outOfSync: string[];
  extra: string[];
}

export interface ScanResults {
  usedKeys: Set<string>;
  translationKeys: Set<string>;
  missingKeys: Set<string>;
  unusedKeys: Set<string>;
  incompleteTranslations: Map<string, string[]>; // locale -> missing keys
  extraTranslations: Map<string, string[]>; // locale -> keys the default locale does not have
  staleKnownUnusedKeys: string[]; // recorded as unused, but used again or gone
  keysWithSpaces: Set<string>; // keys that contain spaces
  lockfileValidation?: LockfileValidationResults | null;
  cjkSpacing?: Map<string, string[]>; // zh/ja locale -> keys whose value has spaces between CJK characters
}

/**
 * Recursively flatten nested translation keys
 */
export function flattenKeys(obj: TranslationKeys, prefix = ""): string[] {
  let keys: string[] = [];

  for (const key in obj) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    const value = obj[key];

    if (value && typeof value === "object" && !Array.isArray(value)) {
      keys = keys.concat(flattenKeys(value, fullKey));
    } else {
      keys.push(fullKey);
    }
  }

  return keys;
}

/**
 * Remove comments from content to avoid detecting translation keys in comments
 * This function carefully preserves // in URLs and strings while removing actual comments
 */
export function stripComments(content: string): string {
  // Remove multi-line comments (/* ... */) first
  let result = content.replaceAll(/\/\*[\s\S]*?\*\//g, "");

  // Remove single-line comments, but be careful not to match:
  // - https:// or http:// in URLs
  // - // inside strings
  // This regex matches // that is NOT preceded by : (to avoid https://)
  // and removes everything after it until end of line
  result = result.replaceAll(/(?<!:)\/\/.*$/gm, "");

  return result;
}

/**
 * Extract translation keys from a file's content
 */
export function extractKeysFromContent(content: string): string[] {
  const keys: string[] = [];

  // Strip comments first to avoid detecting keys in comments
  const contentWithoutComments = stripComments(content);

  for (const pattern of TRANSLATION_PATTERNS) {
    let match: RegExpExecArray | null = null;
    // Reset lastIndex for global regex
    pattern.lastIndex = 0;

    while ((match = pattern.exec(contentWithoutComments)) !== null) {
      const key = match[1];
      // Skip dynamic keys (containing variables like ${}, {{}} etc.)
      if (!key.includes("${") && !key.includes("{{") && !key.includes("}")) {
        keys.push(key);
      }
    }
  }

  // Extract keys from dynamic i18nKey={...} expressions (e.g. ternaries, conditionals)
  I18N_KEY_BLOCK_PATTERN.lastIndex = 0;
  let blockMatch: RegExpExecArray | null = null;
  while ((blockMatch = I18N_KEY_BLOCK_PATTERN.exec(contentWithoutComments)) !== null) {
    const blockContent = blockMatch.groups?.block ?? "";
    STRING_LITERAL_PATTERN.lastIndex = 0;
    let strMatch: RegExpExecArray | null = null;
    while ((strMatch = STRING_LITERAL_PATTERN.exec(blockContent)) !== null) {
      const key = strMatch.groups?.key ?? "";
      if (key.includes(".") && !key.includes("${") && !key.includes(" ")) {
        keys.push(key);
      }
    }
  }

  return keys;
}

// A dotted key quoted or between `{{ }}`: Go string literals, template arguments and catalogue markers.
const REFERENCED_KEY_PATTERN = /["'`{](?<key>[A-Za-z][\w-]*(?:\.[\w-]+)+)["'`}]/g;

/**
 * Catalogue keys a non-TypeScript consumer names. Only keys the catalogue has count, so a dotted string that is no key (a host name, a file name) is never reported as missing.
 */
export function extractReferencedKeys(content: string, catalogueKeys: Set<string>): string[] {
  const keys: string[] = [];
  REFERENCED_KEY_PATTERN.lastIndex = 0;
  let match: RegExpExecArray | null = null;
  while ((match = REFERENCED_KEY_PATTERN.exec(content)) !== null) {
    const key = match.groups?.key ?? "";
    if (catalogueKeys.has(key)) {
      keys.push(key);
    }
  }
  return keys;
}

async function scanReferenceSources(
  sources: ReferenceSource[],
  catalogueKeys: Set<string>,
  usedKeys: Set<string>
): Promise<void> {
  for (const source of sources) {
    const files = await glob(source.pattern, { cwd: source.dir, ignore: EXCLUDE_DIRS, absolute: true });
    console.log(
      `   Found ${files.length.toString()} files naming catalogue keys in ${path.relative(REPO_ROOT, source.dir)} (${source.pattern})`
    );
    for (const file of files) {
      const content = await fs.promises.readFile(file, "utf-8");
      extractReferencedKeys(content, catalogueKeys).forEach((key) => usedKeys.add(key));
    }
  }
}

/**
 * Scan source files for translation keys
 */
async function scanSourceFiles(sourceDirs: string | string[], packageName: string): Promise<Set<string>> {
  console.log(`🔍 Scanning ${packageName} source files for translation keys...`);

  const usedKeys = new Set<string>();
  const dirs = Array.isArray(sourceDirs) ? sourceDirs : [sourceDirs];

  for (const dir of dirs) {
    // Find all TypeScript and TypeScript React files
    const files = await glob("**/*.{ts,tsx}", {
      cwd: dir,
      ignore: EXCLUDE_DIRS,
      absolute: true,
    });

    console.log(`   Found ${files.length.toString()} files to scan in ${path.relative(REPO_ROOT, dir)}`);

    for (const file of files) {
      try {
        const content = await fs.promises.readFile(file, "utf-8");
        const keys = extractKeysFromContent(content);
        keys.forEach((key) => usedKeys.add(key));
      } catch (error) {
        console.error(`❌ Error: Could not read file ${file}:`, error);
      }
    }
  }

  console.log(`   ✅ Found ${usedKeys.size.toString()} unique translation keys\n`);

  return usedKeys;
}

/**
 * Get all locale files in the locales directory
 */
async function getLocaleFiles(localesDir: string): Promise<string[]> {
  try {
    const files = await fs.promises.readdir(localesDir);
    return files.filter((file) => file.endsWith(".json")).map((file) => file.replace(".json", ""));
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : "Unknown error";
    throw new Error(`❌ Failed to read locales directory at ${localesDir}: ${errorMessage}`);
  }
}

/**
 * Load translation keys from a specific locale file
 */
async function loadKeysFromLocale(locale: string, localesDir: string): Promise<Set<string>> {
  const localePath = path.join(localesDir, `${locale}.json`);
  const translationKeys = new Set<string>();

  try {
    const content = await fs.promises.readFile(localePath, "utf-8");
    const translations = JSON.parse(content) as TranslationKeys;
    const keys = flattenKeys(translations);

    keys.forEach((key) => translationKeys.add(key));

    return translationKeys;
  } catch (error) {
    const errorMessage = error instanceof Error ? error.message : "Unknown error";
    throw new Error(`❌ Failed to parse ${localePath}: ${errorMessage}`);
  }
}

/**
 * Load translation keys from all locale files
 */
async function loadAllTranslationKeys(
  localesDir: string,
  defaultLocale: string,
  packageName: string
): Promise<Map<string, Set<string>>> {
  console.log(`📚 Loading ${packageName} translation keys from locale files...`);

  const allLocales = await getLocaleFiles(localesDir);
  const translationsByLocale = new Map<string, Set<string>>();

  // Load all locale files in parallel for better performance
  const localeResults = await Promise.all(
    allLocales.map(async (locale) => {
      const keys = await loadKeysFromLocale(locale, localesDir);
      return { locale, keys };
    })
  );

  // Populate the map and log results
  for (const { locale, keys } of localeResults) {
    translationsByLocale.set(locale, keys);
    console.log(`   • ${locale}.json: ${keys.size.toString()} keys`);
  }
  console.log();

  // Verify default locale exists
  if (!translationsByLocale.has(defaultLocale)) {
    throw new Error(`❌ Default locale ${defaultLocale} not found in ${packageName}`);
  }

  return translationsByLocale;
}

/**
 * Check for incomplete translations across locales
 */
function checkIncompleteTranslations(
  translationsByLocale: Map<string, Set<string>>,
  defaultLocale: string
): Map<string, string[]> {
  const defaultKeys = translationsByLocale.get(defaultLocale);
  if (!defaultKeys) {
    throw new Error(`Default locale ${defaultLocale} not found`);
  }

  const incompleteTranslations = new Map<string, string[]>();

  for (const [locale, keys] of translationsByLocale.entries()) {
    // Skip the default locale
    if (locale === defaultLocale) continue;

    const missingKeys: string[] = [];

    for (const key of defaultKeys) {
      if (!keys.has(key)) {
        missingKeys.push(key);
      }
    }

    if (missingKeys.length > 0) {
      incompleteTranslations.set(locale, missingKeys.sort());
    }
  }

  return incompleteTranslations;
}

/**
 * Keys a locale has that the default locale does not: left behind by a rename or removal, they are never shown and never translated again.
 */
export function checkExtraTranslations(
  translationsByLocale: Map<string, Set<string>>,
  defaultLocale: string
): Map<string, string[]> {
  const defaultKeys = translationsByLocale.get(defaultLocale);
  if (!defaultKeys) {
    throw new Error(`Default locale ${defaultLocale} not found`);
  }
  const extra = new Map<string, string[]>();
  for (const [locale, keys] of translationsByLocale.entries()) {
    if (locale === defaultLocale) continue;
    const extraKeys = [...keys].filter((key) => !defaultKeys.has(key)).sort();
    if (extraKeys.length > 0) {
      extra.set(locale, extraKeys);
    }
  }
  return extra;
}

/**
 * Split unused keys against the recorded ones: the unused keys nobody recorded, and the recorded keys that are no longer unused (used again, or removed from the catalogue).
 */
export function compareWithKnownUnused(
  unusedKeys: Set<string>,
  knownUnusedKeys: string[]
): { unexpected: Set<string>; stale: string[] } {
  const known = new Set(knownUnusedKeys);
  return {
    unexpected: new Set([...unusedKeys].filter((key) => !known.has(key))),
    stale: knownUnusedKeys.filter((key) => !unusedKeys.has(key)).sort(),
  };
}

/** Locales written without spaces between words. */
const UNSPACED_LOCALE = /^(?:zh|ja)(?:-|$)/;

/** Whether a character is a CJK ideograph, kana, or CJK / full-width punctuation. */
export function isCjkCharacter(char: string): boolean {
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
}

/**
 * Whether a value has a run of spaces with a CJK character on each side ("这 将 删除"), the mark word-by-word machine translation left on forma2's zh-Hans-CN catalogue. Spaces next to Latin words, digits and {placeholders} are fine. Scans character by character.
 */
export function hasCjkSpacing(value: string): boolean {
  const chars = Array.from(value);
  for (let i = 1; i < chars.length; i++) {
    if (chars[i] !== " " || !isCjkCharacter(chars[i - 1] ?? "")) continue;
    let end = i;
    while (chars[end] === " ") end++;
    if (end < chars.length && isCjkCharacter(chars[end] ?? "")) return true;
    i = end;
  }
  return false;
}

/** Keys of a catalogue whose value has spaces between CJK characters. */
export function findCjkSpacing(catalogue: TranslationKeys, prefix = ""): string[] {
  return Object.entries(catalogue).flatMap(([key, value]) => {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") return hasCjkSpacing(value) ? [fullKey] : [];
    return findCjkSpacing(value, fullKey);
  });
}

/** For each zh and ja catalogue in the directory, the keys whose value has spaces between CJK characters. */
async function checkCjkSpacing(localesDir: string): Promise<Map<string, string[]>> {
  const result = new Map<string, string[]>();
  for (const locale of await getLocaleFiles(localesDir)) {
    if (!UNSPACED_LOCALE.test(locale)) continue;
    const content = await fs.promises.readFile(path.join(localesDir, `${locale}.json`), "utf-8");
    const keys = findCjkSpacing(JSON.parse(content) as TranslationKeys);
    if (keys.length > 0) result.set(locale, keys);
  }
  return result;
}

/**
 * Detect keys that contain spaces (which should not be used in translation keys)
 */
export function detectKeysWithSpaces(usedKeys: Set<string>, translationKeys: Set<string>): Set<string> {
  const keysWithSpaces = new Set<string>();

  // Check used keys for spaces
  for (const key of usedKeys) {
    if (/\s/.test(key)) {
      keysWithSpaces.add(key);
    }
  }

  // Check translation keys for spaces
  for (const key of translationKeys) {
    if (/\s/.test(key)) {
      keysWithSpaces.add(key);
    }
  }

  return keysWithSpaces;
}

/**
 * Compare used keys with translation keys to find missing and unused keys
 */
function compareKeys(
  usedKeys: Set<string>,
  translationKeys: Set<string>,
  translationsByLocale: Map<string, Set<string>>,
  defaultLocale: string,
  packageName: string,
  knownUnusedKeys: string[]
): ScanResults {
  console.log(`🔄 Comparing ${packageName} keys...`);

  const missingKeys = new Set<string>();
  const unusedKeys = new Set<string>();

  // Find missing keys (used but not in translations)
  for (const key of usedKeys) {
    if (!translationKeys.has(key)) {
      missingKeys.add(key);
    }
  }

  // Find unused keys (in translations but not used)
  for (const key of translationKeys) {
    if (!usedKeys.has(key)) {
      unusedKeys.add(key);
    }
  }

  // Check for incomplete translations across locales
  const incompleteTranslations = checkIncompleteTranslations(translationsByLocale, defaultLocale);
  const extraTranslations = checkExtraTranslations(translationsByLocale, defaultLocale);
  const { unexpected, stale } = compareWithKnownUnused(unusedKeys, knownUnusedKeys);

  // Detect keys with spaces
  const keysWithSpaces = detectKeysWithSpaces(usedKeys, translationKeys);

  console.log();

  return {
    usedKeys,
    translationKeys,
    missingKeys,
    unusedKeys: unexpected,
    incompleteTranslations,
    extraTranslations,
    staleKnownUnusedKeys: stale,
    keysWithSpaces,
  };
}

/** Whether a scan found anything that fails validation. */
export function hasIssues(results: ScanResults): boolean {
  return (
    results.missingKeys.size > 0 ||
    results.unusedKeys.size > 0 ||
    results.incompleteTranslations.size > 0 ||
    results.extraTranslations.size > 0 ||
    results.staleKnownUnusedKeys.length > 0 ||
    results.keysWithSpaces.size > 0 ||
    (results.cjkSpacing?.size ?? 0) > 0 ||
    hasLockfileIssues(results)
  );
}

function hasLockfileIssues(results: ScanResults): boolean {
  return (
    !results.lockfileValidation ||
    results.lockfileValidation.missing.length > 0 ||
    results.lockfileValidation.outOfSync.length > 0 ||
    results.lockfileValidation.extra.length > 0
  );
}

/**
 * Display validation results
 */
function displayResults(results: ScanResults, packageName: string, defaultLocale: string): void {
  console.log("═══════════════════════════════════════════════════════════");
  console.log(`              ${packageName} VALIDATION RESULTS              `);
  console.log("═══════════════════════════════════════════════════════════\n");

  if (!hasIssues(results)) {
    console.log("✅ All translation keys are valid!\n");
    console.log(`   • ${results.usedKeys.size.toString()} keys used in code`);
    console.log(`   • ${results.translationKeys.size.toString()} keys in translations`);
    console.log(`   • 0 missing keys`);
    console.log(`   • 0 unused keys`);
    console.log(`   • 0 keys with spaces`);
    console.log(`   • All locales complete\n`);
    return;
  }

  if (results.missingKeys.size > 0) {
    console.log(`❌ MISSING KEYS (${results.missingKeys.size.toString()}):\n`);
    console.log("   These keys are used in code but not found in translation files:\n");
    const sortedMissingKeys = Array.from(results.missingKeys).sort();
    sortedMissingKeys.forEach((key) => {
      console.log(`   • ${key}`);
    });
  }

  if (results.unusedKeys.size > 0) {
    console.log(`\n⚠️  UNUSED KEYS (${results.unusedKeys.size.toString()}):\n`);
    console.log("   These keys exist in translation files but are not used in code:\n");
    const sortedUnusedKeys = Array.from(results.unusedKeys).sort();
    sortedUnusedKeys.forEach((key) => {
      console.log(`   • ${key}`);
    });
  }

  if (results.incompleteTranslations.size > 0) {
    console.log(`\n⚠️  INCOMPLETE TRANSLATIONS:\n`);
    console.log(`   Some keys from ${defaultLocale} are missing in target languages:\n`);

    for (const [locale, missingKeys] of results.incompleteTranslations.entries()) {
      console.log(`   📝 ${locale} (${missingKeys.length.toString()} missing keys):`);

      // Show first 10 missing keys for each locale to avoid overwhelming output
      const keysToShow = missingKeys.slice(0, 10);
      keysToShow.forEach((key) => {
        console.log(`      • ${key}`);
      });

      if (missingKeys.length > 10) {
        const moreKeys = missingKeys.length - 10;
        console.log(`      ... and ${moreKeys.toString()} more`);
      }
    }
  }

  if (results.extraTranslations.size > 0) {
    console.log(`\n❌ KEYS MISSING FROM ${defaultLocale}:\n`);
    console.log(
      `   These keys exist in a target language but not in ${defaultLocale}, so nothing shows them:\n`
    );
    for (const [locale, extraKeys] of results.extraTranslations.entries()) {
      console.log(`   📝 ${locale} (${extraKeys.length.toString()} extra keys):`);
      extraKeys.slice(0, 10).forEach((key) => {
        console.log(`      • ${key}`);
      });
      if (extraKeys.length > 10) {
        console.log(`      ... and ${(extraKeys.length - 10).toString()} more`);
      }
    }
  }

  if (results.staleKnownUnusedKeys.length > 0) {
    console.log(
      `\n❌ RECORDED UNUSED KEYS THAT ARE USED OR GONE (${results.staleKnownUnusedKeys.length.toString()}):\n`
    );
    console.log(
      "   Delete these entries from the list of recorded unused keys (packages/i18n-utils/web-unused-keys.json or surveys-unused-keys.json):\n"
    );
    results.staleKnownUnusedKeys.forEach((key) => {
      console.log(`   • ${key}`);
    });
  }

  if (results.keysWithSpaces.size > 0) {
    console.log(`\n❌ KEYS WITH SPACES (${results.keysWithSpaces.size.toString()}):\n`);
    console.log("   Translation keys should not contain spaces. These keys have spaces:\n");
    const sortedKeysWithSpaces = Array.from(results.keysWithSpaces).sort();
    sortedKeysWithSpaces.forEach((key) => {
      // Show the key with visible spaces marked
      const visualKey = key.replaceAll(" ", "␣").replaceAll("\t", "⇥").replaceAll("\n", "↵");
      console.log(`   • "${visualKey}"`);
    });
    console.log("\n   Please remove spaces from these keys or use valid key names.\n");
  }

  if (results.cjkSpacing && results.cjkSpacing.size > 0) {
    console.log(`\n❌ SPACES BETWEEN CJK CHARACTERS:\n`);
    console.log("   Chinese and Japanese are written without spaces between words. Remove them from:\n");
    for (const [locale, keys] of results.cjkSpacing.entries()) {
      console.log(`   📝 ${locale} (${keys.length.toString()} values):`);
      keys.slice(0, 10).forEach((key) => {
        console.log(`      • ${key}`);
      });
      if (keys.length > 10) {
        console.log(`      ... and ${(keys.length - 10).toString()} more`);
      }
    }
    console.log();
  }

  if (results.lockfileValidation) {
    const { missing, outOfSync, extra } = results.lockfileValidation;

    if (hasLockfileIssues(results)) {
      console.log(`❌ LOCKFILE OUT OF SYNC:\n`);
      console.log(`   Please run "pnpm i" and then "pnpm i18n" to regenerate i18n.lock.\n`);

      if (missing.length > 0) {
        console.log(`   • Keys missing from lockfile (${missing.length.toString()}):`);
        missing.slice(0, 10).forEach((key) => {
          console.log(`      - ${key}`);
        });
        if (missing.length > 10) {
          console.log(`      ... and ${(missing.length - 10).toString()} more`);
        }
      }

      if (outOfSync.length > 0) {
        console.log(`   • Keys out of sync/stale in lockfile (${outOfSync.length.toString()}):`);
        outOfSync.slice(0, 10).forEach((key) => {
          console.log(`      - ${key}`);
        });
        if (outOfSync.length > 10) {
          console.log(`      ... and ${(outOfSync.length - 10).toString()} more`);
        }
      }

      if (extra.length > 0) {
        console.log(`   • Unused/extra keys in lockfile (${extra.length.toString()}):`);
        extra.slice(0, 10).forEach((key) => {
          console.log(`      - ${key}`);
        });
        if (extra.length > 10) {
          console.log(`      ... and ${(extra.length - 10).toString()} more`);
        }
      }
      console.log();
    }
  } else {
    console.log(`❌ LOCKFILE MISSING:\n`);
    console.log(`   Could not find i18n.lock file. Please run "pnpm i18n" to generate it.\n`);
  }

  console.log("═══════════════════════════════════════════════════════════\n");
}
export function parseLockfile(content: string): Record<string, string> {
  const checksums: Record<string, string> = {};
  const lines = content.split(/\r?\n/);
  const regex = /^\s{4}(?<key>[^:]+):\s*(?<hash>[a-f0-9]{32})\s*$/;
  for (const line of lines) {
    const match = regex.exec(line);
    if (match?.groups) {
      const key = match.groups.key;
      const hash = match.groups.hash;
      checksums[key] = hash;
    }
  }
  return checksums;
}

async function loadDefaultLocaleWithValues(
  localesDir: string,
  defaultLocale: string
): Promise<Record<string, string>> {
  const localePath = path.join(localesDir, `${defaultLocale}.json`);
  const content = await fs.promises.readFile(localePath, "utf-8");
  const translations = JSON.parse(content) as TranslationKeys;

  const flattened: Record<string, string> = {};

  function recurse(obj: TranslationKeys, prefix = ""): void {
    for (const key in obj) {
      const fullKey = prefix ? `${prefix}/${key}` : key;
      const value = obj[key];
      if (value && typeof value === "object" && !Array.isArray(value)) {
        recurse(value, fullKey);
      } else if (typeof value === "string") {
        flattened[fullKey] = value;
      }
    }
  }

  recurse(translations);
  return flattened;
}

export async function validateLockfile(
  localesDir: string,
  defaultLocale: string
): Promise<LockfileValidationResults | null> {
  const lockfilePath = path.join(localesDir, "..", "i18n.lock");

  try {
    await fs.promises.access(lockfilePath);
  } catch {
    return null;
  }

  const lockfileContent = await fs.promises.readFile(lockfilePath, "utf-8");
  const lockChecksums = parseLockfile(lockfileContent);
  const sourceKeysWithValues = await loadDefaultLocaleWithValues(localesDir, defaultLocale);

  const missing: string[] = [];
  const outOfSync: string[] = [];
  const extra: string[] = [];

  for (const [key, val] of Object.entries(sourceKeysWithValues)) {
    const expected = objectHash.MD5(val);
    const actual = lockChecksums[key];
    if (!actual) {
      missing.push(key);
    } else if (actual !== expected) {
      outOfSync.push(key);
    }
  }

  for (const key of Object.keys(lockChecksums)) {
    if (!(key in sourceKeysWithValues)) {
      extra.push(key);
    }
  }

  return { missing, outOfSync, extra };
}

/**
 * Validate one catalogue against the code that reads it, printing what it finds.
 */
export async function validateTarget(target: ScanTarget): Promise<ScanResults> {
  const usedKeys = await scanSourceFiles(target.sourceDirs, target.name);

  const translationsByLocale = await loadAllTranslationKeys(
    target.localesDir,
    target.defaultLocale,
    target.name
  );
  const defaultKeys = translationsByLocale.get(target.defaultLocale);
  if (!defaultKeys) {
    throw new Error(`Default locale ${target.defaultLocale} not found in ${target.name}`);
  }

  if (target.referenceSources) {
    await scanReferenceSources(target.referenceSources, defaultKeys, usedKeys);
    console.log();
  }

  const results = compareKeys(
    usedKeys,
    defaultKeys,
    translationsByLocale,
    target.defaultLocale,
    target.name,
    target.knownUnusedKeys ?? []
  );
  const resultsWithLockfile: ScanResults = {
    ...results,
    lockfileValidation: await validateLockfile(target.localesDir, target.defaultLocale),
    cjkSpacing: await checkCjkSpacing(target.localesDir),
  };

  displayResults(resultsWithLockfile, target.name, target.defaultLocale);
  return resultsWithLockfile;
}

/**
 * Main execution
 */
async function main(): Promise<void> {
  console.log("\n");
  console.log("╔══════════════════════════════════════════════════════════╗");
  console.log("║         Translation Key Validation for Forma        ║");
  console.log("╚══════════════════════════════════════════════════════════╝");
  console.log();

  try {
    const results: ScanResults[] = [];
    for (const target of [WEB_TARGET, SURVEYS_TARGET]) {
      results.push(await validateTarget(target));
    }

    if (results.some(hasIssues)) {
      console.error("═══════════════════════════════════════════════════════════");
      console.error("❌ Translation validation failed!\n");
      console.error("   Please fix the issues above before committing.\n");

      if (results.some((r) => r.missingKeys.size > 0)) {
        console.error("   • Add missing keys to your translation files");
      }
      if (results.some((r) => r.unusedKeys.size > 0)) {
        console.error("   • Remove unused keys from translation files");
      }
      if (results.some((r) => r.staleKnownUnusedKeys.length > 0)) {
        console.error("   • Delete the recorded unused keys that are used again or gone");
      }
      if (results.some((r) => r.keysWithSpaces.size > 0)) {
        console.error("   • Remove spaces from translation keys (use underscores or camelCase instead)");
      }
      if (results.some((r) => r.incompleteTranslations.size > 0)) {
        console.error("   • Complete missing translations in target language files");
      }
      if (results.some((r) => r.extraTranslations.size > 0)) {
        console.error("   • Remove keys the source language no longer has from target language files");
      }
      if (results.some((r) => (r.cjkSpacing?.size ?? 0) > 0)) {
        console.error("   • Remove the spaces between Chinese or Japanese characters in the listed values");
      }
      if (results.some(hasLockfileIssues)) {
        console.error("   • Rebuild and update your lockfiles (run 'pnpm i18n')");
      }
      console.error();
      process.exit(1);
    }

    console.log("═══════════════════════════════════════════════════════════");
    console.log("✅ All translation validations passed!\n");
    process.exit(0);
  } catch (error) {
    console.error("\n❌ Error during validation:\n");
    console.error("   ", error);
    console.error();
    process.exit(1);
  }
}

// Run the script only when this file IS the process entrypoint (`tsx src/scan-translations.ts`, i.e.
// `pnpm scan-translations` / `pnpm i18n` / the translation-check workflow).
//
// This module doubles as a library: scan-translations.test.ts imports the pure helpers above. Calling
// main() unconditionally meant that importing it started a full repo-wide scan (~1900 files under
// apps/web) in the background, which then raced the importing test suite for the event loop and ended
// in a process.exit() that could tear the vitest worker down mid-run. Locally the tests finish before
// the scan gets far, so it looked fine; under CI load it surfaced as an unrelated-looking 5s test
// timeout in the SonarQube job.
const entrypointArg = process.argv.at(1);
const isProcessEntrypoint = entrypointArg !== undefined && path.resolve(entrypointArg) === __filename;

if (isProcessEntrypoint) {
  void main();
}
