import { headers } from "next/headers";
import { TUserLocale } from "@forma/types/user";
import { AVAILABLE_LOCALES, DEFAULT_LOCALE } from "@/lib/constants";

/** Chinese regions conventionally written in Traditional Han. Every other Chinese region is Simplified. */
const TRADITIONAL_CHINESE_REGIONS = new Set(["tw", "hk", "mo"]);

interface LocaleParts {
  language: string;
  script?: string;
  region?: string;
}

/**
 * Splits a BCP 47 tag into the subtags matching cares about, lowercased.
 *
 * A 4-letter subtag is an ISO 15924 script (`Hans`), a 2-or-3-letter one after the language is a region (`TW`). When a Chinese tag names a region but no script — which is what a browser actually sends, `zh-TW` rather than `zh-Hant-TW` — the script is inferred from the region, because that is the only thing that distinguishes the two Chinese catalogues this app ships.
 */
const parseLocaleTag = (tag: string): LocaleParts => {
  const [language, ...rest] = tag.toLowerCase().split("-");
  let script: string | undefined;
  let region: string | undefined;

  for (const subtag of rest) {
    if (!script && subtag.length === 4 && /^[a-z]+$/.test(subtag)) {
      script = subtag;
    } else if (!region && /^([a-z]{2}|\d{3})$/.test(subtag)) {
      region = subtag;
    }
  }

  if (!script && language === "zh" && region) {
    script = TRADITIONAL_CHINESE_REGIONS.has(region) ? "hant" : "hans";
  }

  return { language, script, region };
};

interface WeightedTag {
  parts: LocaleParts;
  quality: number;
}

/**
 * Parses an `Accept-Language` header into its tags, best first.
 *
 * Every entry after the first carries a leading space and a `;q=` weight, both of which the previous implementation compared literally — so `fr-FR,ja-JP;q=0.9` never saw `ja-JP` at all and fell through to the default. Entries with `q=0` are an explicit refusal and are dropped. `Array.prototype.sort` is stable, so equal weights keep header order, which is what makes the first-listed language win a tie.
 */
const parseAcceptLanguage = (header: string): WeightedTag[] =>
  header
    .split(",")
    .map((entry) => {
      const [tag, ...parameters] = entry.split(";");
      const quality = parameters
        .map((parameter) => /^\s*q\s*=\s*([\d.]+)\s*$/i.exec(parameter))
        .find((match) => match !== null)?.[1];

      return { tag: tag.trim(), quality: quality === undefined ? 1 : Number.parseFloat(quality) };
    })
    .filter(({ tag, quality }) => tag.length > 0 && tag !== "*" && Number.isFinite(quality) && quality > 0)
    .map(({ tag, quality }) => ({ parts: parseLocaleTag(tag), quality }))
    .sort((a, b) => b.quality - a.quality);

/**
 * How well an available locale serves a requested one. Higher is better, 0 is no match.
 *
 * Script outranks region so that `zh-HK` reaches the Traditional catalogue (`zh-Hant-TW`) rather than the Simplified one that happens to sit earlier in the list — the old base-language scan collapsed both to "zh" and always returned the first, sending every Traditional-Chinese browser to Simplified.
 */
const scoreMatch = (available: LocaleParts, requested: LocaleParts): number => {
  if (available.language !== requested.language) return 0;
  if (available.script && requested.script && available.script !== requested.script) return 0;
  if (available.region === requested.region && available.script === requested.script) return 4;
  if (available.script && available.script === requested.script) return 3;
  if (available.region && available.region === requested.region) return 2;
  return 1;
};

export const findMatchingLocale = async (): Promise<TUserLocale> => {
  const headersList = await headers();
  const acceptLanguage = headersList.get("accept-language");
  if (!acceptLanguage) {
    return DEFAULT_LOCALE;
  }

  const availableParts = AVAILABLE_LOCALES.map(parseLocaleTag);

  // Requested tags are tried best-weighted first, and the first one with any match wins: a user who
  // asks for French before Japanese should get Japanese, not a closer match to a language they ranked lower.
  for (const { parts: requested } of parseAcceptLanguage(acceptLanguage)) {
    let bestIndex = -1;
    let bestScore = 0;

    availableParts.forEach((available, index) => {
      const score = scoreMatch(available, requested);
      if (score > bestScore) {
        bestScore = score;
        bestIndex = index;
      }
    });

    if (bestIndex !== -1) return AVAILABLE_LOCALES[bestIndex];
  }

  return DEFAULT_LOCALE;
};
