import type { Locale } from "../site";
import { en } from "./en";
import { ja } from "./ja";
import { type SiteContent, siteContentSchema } from "./schema";
import { zh } from "./zh";

export type { SiteContent } from "./schema";

export const DICTIONARIES: Record<Locale, SiteContent> = { en, zh, ja };

/** Validates every locale's dictionary and throws one error that lists every problem in every locale. */
export const validateDictionaries = (dictionaries: Record<string, unknown> = DICTIONARIES): void => {
  const problems = Object.entries(dictionaries).flatMap(([locale, dictionary]) => {
    const result = siteContentSchema.safeParse(dictionary);
    return result.success
      ? []
      : result.error.issues.map(
          (issue) => `${locale}: ${issue.path.join(".") || "(root)"}: ${issue.message}`
        );
  });
  if (problems.length > 0) {
    throw new Error(`Invalid website content:\n${problems.join("\n")}`);
  }
};
