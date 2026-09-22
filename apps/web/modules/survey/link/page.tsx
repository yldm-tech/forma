import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { createCacheKey } from "@forma/cache";
import { logger } from "@forma/logger";
import { ZId } from "@forma/types/common";
import { TSurvey } from "@forma/types/surveys/types";
import { cache } from "@/lib/cache";
import { findMatchingLocale } from "@/lib/utils/locale";
import { getResponseCountBySurveyId } from "@/modules/survey/lib/response";
import { SurveyInactive } from "@/modules/survey/link/components/survey-inactive";
import { renderSurvey } from "@/modules/survey/link/components/survey-renderer";
import { getResponseBySingleUseId, getSurveyWithMetadata } from "@/modules/survey/link/lib/data";
import { checkAndValidateSingleUseId } from "@/modules/survey/link/lib/helper";
import type { TLinkSurveySearchParams } from "@/modules/survey/link/lib/types";
import { getWorkspaceContextForLinkSurvey } from "@/modules/survey/link/lib/workspace";
import { getMetadataForLinkSurvey } from "@/modules/survey/link/metadata";

/** Welcome-card response counts are social proof, so a minute of staleness is acceptable. */
const RESPONSE_COUNT_CACHE_TTL_MS = 60 * 1000;

interface LinkSurveyPageProps {
  params: Promise<{
    surveyId: string;
  }>;
  searchParams: Promise<TLinkSurveySearchParams>;
}

export const generateMetadata = async (props: LinkSurveyPageProps): Promise<Metadata> => {
  const params = await props.params;
  const searchParams = await props.searchParams;
  const validId = ZId.safeParse(params.surveyId);
  if (!validId.success) {
    notFound();
  }

  // Extract language code from URL params
  const languageCode = typeof searchParams.lang === "string" ? searchParams.lang : undefined;

  return getMetadataForLinkSurvey(params.surveyId, languageCode);
};

export const LinkSurveyPage = async (props: LinkSurveyPageProps) => {
  const searchParams = await props.searchParams;
  const params = await props.params;
  const validId = ZId.safeParse(params.surveyId);
  if (!validId.success) {
    notFound();
  }

  const isPreview = searchParams.preview === "true";

  /**
   * Optimized data fetching strategy for link surveys
   *
   * PERFORMANCE OPTIMIZATION:
   * We fetch data in carefully staged parallel operations to minimize latency.
   * Each sequential database call adds ~100-300ms for users far from servers.
   *
   * Fetch stages:
   * Stage 1: Survey (required first - provides config for all other fetches)
   * Stage 2: Parallel fetch of environment context, locale, conditional single-use response, and
   *          the conditional welcome-card response count
   *
   * This reduces waterfall from 4-5 levels to 2 levels:
   * - Before: ~400-1500ms added latency for distant users
   * - After: ~200-600ms added latency for distant users
   * - Improvement: 50-60% latency reduction
   *
   * CACHING NOTE:
   * getSurveyWithMetadata is wrapped in React's cache(), so the call from
   * generateMetadata and this page component are automatically deduplicated.
   */

  // Stage 1: Fetch survey first (required for all subsequent logic)
  let survey: TSurvey | null = null;
  try {
    survey = await getSurveyWithMetadata(params.surveyId);
  } catch (error) {
    logger.error(error, "Error fetching survey");
    return notFound();
  }

  if (!survey) {
    return notFound();
  }

  const suId = searchParams.suId;
  const suToken = searchParams.suToken;

  // Validate single-use ID early (no I/O, just validation)
  const isSingleUseSurvey = survey.singleUse?.enabled;
  const isSingleUseSurveyEncrypted = survey.singleUse?.isEncrypted;
  let singleUseId: string | undefined = undefined;

  if (isSingleUseSurvey) {
    const validatedSingleUseId = checkAndValidateSingleUseId(
      suId,
      isSingleUseSurveyEncrypted,
      survey.id,
      suToken
    );
    if (!validatedSingleUseId) {
      // Need to fetch workspace for error page - fetch environmentContext for it
      const environmentContext = await getWorkspaceContextForLinkSurvey(survey.workspaceId);
      return <SurveyInactive status="link invalid" workspace={environmentContext.workspace} />;
    }
    singleUseId = validatedSingleUseId;
  }

  const surveyId = survey.id;
  // The count is only ever rendered by the welcome card, which is only mounted when the card itself
  // is enabled - fetching it on showResponseCount alone pays for a number nothing displays.
  const needsResponseCount = survey.welcomeCard.enabled && survey.welcomeCard.showResponseCount;

  // Stage 2: Parallel fetch of all remaining data
  const [workspaceContext, locale, singleUseResponse, responseCount] = await Promise.all([
    getWorkspaceContextForLinkSurvey(survey.workspaceId),
    findMatchingLocale(),
    // Only fetch single-use response if we have a validated ID
    isSingleUseSurvey && singleUseId
      ? getResponseBySingleUseId(survey.id, singleUseId)()
      : Promise.resolve(undefined),
    // Social proof on a welcome card: an O(rows) COUNT(*) per page view is not worth exactness, so
    // it is served from Redis for up to a minute. Nothing that gates behaviour may read this key.
    needsResponseCount
      ? cache.withCache(
          () => getResponseCountBySurveyId(surveyId),
          createCacheKey.response.countBySurveyId(surveyId),
          RESPONSE_COUNT_CACHE_TTL_MS
        )
      : Promise.resolve(undefined),
  ]);

  // Pass all pre-fetched data to renderer
  return renderSurvey({
    survey,
    searchParams,
    singleUseId,
    singleUseResponse: singleUseResponse ?? undefined,
    allowUrlUserIdLookup: true,
    isPreview,
    workspaceContext,
    locale,
    responseCount,
  });
};
