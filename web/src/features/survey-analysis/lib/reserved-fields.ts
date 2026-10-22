import type { TFunction } from "i18next";
import {
  AirplayIcon,
  ArrowUpFromDotIcon,
  ClockIcon,
  FileTextIcon,
  FlagIcon,
  GlobeIcon,
  LanguagesIcon,
  type LucideIcon,
  MegaphoneIcon,
  MonitorIcon,
  MousePointerClickIcon,
  ShieldIcon,
  SmartphoneIcon,
  TimerIcon,
} from "lucide-react";
import {
  RESERVED_FIELD_CATALOG,
  type TReservedFieldCatalogEntry,
  dropShadowedReservedEntries,
  getSurveyEmbeddedFields,
  listShadowingNames,
} from "@forma/types/embedded-data-resolver";
import { formatFieldNameToTitleCase } from "@forma/types/safe-identifier";
import type { TSurvey } from "@forma/types/surveys/types";
import { getElementsFromBlocks } from "../../survey-editor/lib/client-utils";

/**
 * The human-readable label for a reserved field (ENG-2540).
 *
 * **Every field either surface displays today has its own key, and so does the one filter-only entry.** All twenty-one `display !== "none"` catalog entries are listed below, plus `durationSeconds`, which no column shows but the response filter offers (ENG-1848). So `Page Path`, `UTM Source` and `Duration (seconds)` are translated in every locale rather than rendered as English derived from the catalog name — which is what the repo's i18n rule asks for, and what shipping them derived would have quietly broken.
 *
 * `formatFieldNameToTitleCase` — the same helper the recall and logic pickers use — stays as the `default` arm, deliberately, as a **last resort rather than the rule**: ENG-1858's next batch of catalog entries still surfaces on both surfaces with no edit here, reading in English until someone adds its key. So a catalog addition is still free, just not yet localized; that is the trade the ticket's "adding a catalog entry surfaces it in both places with no further change" criterion buys, and it is the reason this is not a lookup keyed off a required per-entry field.
 *
 * A switch of literal `t("…")` calls rather than a name → key lookup map on purpose: the translation-key check (src/lib/i18n-keys.test.ts) finds keys only in literal `t()` arguments, and a map would hide every `workspace.surveys.responses.*` key below from it.
 */
export const getReservedFieldLabel = (name: string, t: TFunction): string => {
  switch (name) {
    // The seven `primary` fields, all of which predate the catalog and keep their shipped copy.
    case "action":
      return t("common.action");
    case "browser":
      return t("workspace.surveys.responses.browser");
    case "country":
      return t("workspace.surveys.responses.country");
    // The catalog spells this `deviceType`; both surfaces have always shown `Device`.
    case "deviceType":
      return t("workspace.surveys.responses.device");
    case "os":
      return t("workspace.surveys.responses.os");
    case "source":
      return t("workspace.surveys.responses.source");
    case "url":
      return t("common.url");
    // The fourteen `secondary` fields. `ipAddress` is the only one with copy older than ENG-1841.
    case "ipAddress":
      return t("workspace.surveys.responses.ip_address");
    case "locale":
      return t("workspace.surveys.responses.locale");
    case "pagePath":
      return t("workspace.surveys.responses.page_path");
    case "pageReferrer":
      return t("workspace.surveys.responses.page_referrer");
    case "screenHeight":
      return t("workspace.surveys.responses.screen_height");
    case "screenWidth":
      return t("workspace.surveys.responses.screen_width");
    case "timezone":
      return t("workspace.surveys.responses.timezone");
    case "utmCampaign":
      return t("workspace.surveys.responses.utm_campaign");
    case "utmContent":
      return t("workspace.surveys.responses.utm_content");
    case "utmMedium":
      return t("workspace.surveys.responses.utm_medium");
    case "utmSource":
      return t("workspace.surveys.responses.utm_source");
    case "utmTerm":
      return t("workspace.surveys.responses.utm_term");
    case "viewportHeight":
      return t("workspace.surveys.responses.viewport_height");
    case "viewportWidth":
      return t("workspace.surveys.responses.viewport_width");
    // The one `display: "none"` entry the response filter offers (ENG-1848): never a column, so this key is only ever read by the filter picker (ENG-2894).
    case "durationSeconds":
      return t("workspace.surveys.responses.duration_seconds");
    default:
      return formatFieldNameToTitleCase(name);
  }
};
/**
 * Column and row icons, by catalog entry name. Sparse on purpose: an entry with no icon renders without one rather than borrowing a misleading neighbour's, and the UTM family deliberately shares one so a row of five reads as one group.
 */
export const RESERVED_FIELD_ICONS: Record<string, LucideIcon> = {
  action: MousePointerClickIcon,
  browser: GlobeIcon,
  country: FlagIcon,
  deviceType: SmartphoneIcon,
  // Not displayed by the table (display: "none") but offered by the response filter (ENG-1848).
  durationSeconds: TimerIcon,
  ipAddress: ShieldIcon,
  locale: LanguagesIcon,
  os: AirplayIcon,
  pagePath: FileTextIcon,
  pageReferrer: ArrowUpFromDotIcon,
  screenHeight: MonitorIcon,
  screenWidth: MonitorIcon,
  source: ArrowUpFromDotIcon,
  timezone: ClockIcon,
  url: GlobeIcon,
  utmCampaign: MegaphoneIcon,
  utmContent: MegaphoneIcon,
  utmMedium: MegaphoneIcon,
  utmSource: MegaphoneIcon,
  utmTerm: MegaphoneIcon,
  viewportHeight: MonitorIcon,
  viewportWidth: MonitorIcon,
};

/** The gates every reserved-entry consumer shares (forma2's gateReservedEntries): shadowed entries drop, since a declared field owns its name, and `ipAddress` needs its capture toggle. */
const gateReservedEntries = (
  survey: TSurvey,
  isEligible: (entry: TReservedFieldCatalogEntry) => boolean
): TReservedFieldCatalogEntry[] => {
  const elementIds = getElementsFromBlocks(survey.blocks).map((element) => element.id);
  const shadowingNames = listShadowingNames(getSurveyEmbeddedFields(survey), elementIds);

  return dropShadowedReservedEntries(RESERVED_FIELD_CATALOG, shadowingNames).filter((entry) => {
    if (!isEligible(entry)) return false;
    if (entry.name === "ipAddress" && !survey.isCaptureIpEnabled) return false;
    return true;
  });
};

/**
 * The reserved fields one survey's response filter may offer (forma2's getReservedFilterEntries): the ones the response table shows (`display !== "none"`) plus `durationSeconds`. Shadowed entries drop, and on an anonymized survey so do the `privacy: "drop"` entries it no longer captures, as does `ipAddress` unless the survey captures it: the picker follows what the survey collects.
 */
export const getReservedFilterEntries = (survey: TSurvey): TReservedFieldCatalogEntry[] =>
  gateReservedEntries(
    survey,
    (entry) =>
      (entry.display !== "none" || entry.name === "durationSeconds") &&
      !(survey.isAnonymizeResponsesEnabled && entry.privacy === "drop")
  );
