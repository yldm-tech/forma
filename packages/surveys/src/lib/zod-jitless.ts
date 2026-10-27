import { config } from "zod";

/** The attribute the server puts on the runtime's script tag in Forma's own survey page (apps/server/internal/linksurvey), the one document where the runtime may change page-wide settings. */
export const FORMA_SURVEY_PAGE_ATTRIBUTE = "data-forma-survey-page";

// zod compiles object schemas with `new Function` when the page lets it, and asks once by trying, as the first object schema is built. Forma's survey page has a Content-Security-Policy without 'unsafe-eval', so that try is refused and logged as a policy violation. jitless skips the try; parsing takes the path it took under the policy anyway, and a survey's documents are small.
// The setting is page-wide: zod keeps it on globalThis.__zod_globalConfig, which every zod 4 copy on the page shares. On a customer's site (the SDK appends this script to the customer's document) it would also turn off JIT for the customer's own schemas, so it is set only where the server marked the script tag. The admin app sets it in its own entry points (apps/web/src/lib/zod-jitless.ts) before it loads the runtime. index.ts imports this first, so it runs before any module builds a schema, while document.currentScript is still this script.
if (globalThis.document?.currentScript?.hasAttribute(FORMA_SURVEY_PAGE_ATTRIBUTE)) {
  config({ jitless: true });
}
