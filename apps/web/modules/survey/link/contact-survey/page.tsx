import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { createCacheKey } from "@forma/cache";
import { cache } from "@/lib/cache";
import { findMatchingLocale } from "@/lib/utils/locale";
import { getTranslate } from "@/lingodotdev/server";
import { verifyContactSurveyToken } from "@/modules/contacts/lib/contact-survey-link";
import { getResponseCountBySurveyId } from "@/modules/survey/lib/response";
import { getSurvey } from "@/modules/survey/lib/survey";
import { SurveyInactive } from "@/modules/survey/link/components/survey-inactive";
import { renderSurvey } from "@/modules/survey/link/components/survey-renderer";
import { getExistingContactResponse } from "@/modules/survey/link/lib/data";
import { checkAndValidateSingleUseId } from "@/modules/survey/link/lib/helper";
import {
  getBasicSurveyMetadata,
  getMetadataBrandColor,
  getSurveyOpenGraphMetadata,
} from "@/modules/survey/link/lib/metadata-utils";
import type { TLinkSurveySearchParams } from "@/modules/survey/link/lib/types";
import { getWorkspaceContextForLinkSurvey } from "@/modules/survey/link/lib/workspace";
import { getWorkspaceById } from "@/modules/survey/link/lib/workspace";

/** Welcome-card response counts are social proof, so a minute of staleness is acceptable. */
const RESPONSE_COUNT_CACHE_TTL_MS = 60 * 1000;

interface ContactSurveyPageProps {
  params: Promise<{
    jwt: string;
  }>;
  searchParams: Promise<TLinkSurveySearchParams>;
}

export const generateMetadata = async (props: ContactSurveyPageProps): Promise<Metadata> => {
  const { jwt } = await props.params;
  try {
    // Verify and decode the JWT token
    const result = verifyContactSurveyToken(jwt);
    if (!result.ok) {
      return {
        title: "Survey",
        description: "Please complete this survey.",
      };
    }
    const { surveyId } = result.data;
    const { title, description, survey, ogImage } = await getBasicSurveyMetadata(surveyId);

    if (!survey) {
      return { title, description };
    }

    // Fetch organization whitelabel data for custom favicon
    const workspaceContext = await getWorkspaceContextForLinkSurvey(survey.workspaceId);
    const customFaviconUrl = workspaceContext.organizationWhitelabel?.faviconUrl;

    const brandColor = getMetadataBrandColor(workspaceContext.workspace.styling, survey.styling);
    const baseMetadata = getSurveyOpenGraphMetadata(survey.id, title, brandColor);

    // Override with the custom image URL
    if (baseMetadata.openGraph) {
      baseMetadata.openGraph.images = ogImage ?? baseMetadata.openGraph.images;
      baseMetadata.openGraph.description = description;
    }

    if (baseMetadata.twitter) {
      baseMetadata.twitter.images = ogImage ?? baseMetadata.twitter.images;
      baseMetadata.twitter.description = description;
    }

    return {
      title,
      description,
      ...baseMetadata,
      ...(customFaviconUrl && { icons: customFaviconUrl }),
    };
  } catch {
    // If the token is invalid, we'll return generic metadata
    return {
      title: "Survey",
      description: "Please complete this survey.",
    };
  }
};

export const ContactSurveyPage = async (props: ContactSurveyPageProps) => {
  const searchParams = await props.searchParams;
  const params = await props.params;

  const t = await getTranslate();
  const { jwt } = params;
  const { preview, suId, suToken } = searchParams;

  const result = verifyContactSurveyToken(jwt);
  if (!result.ok) {
    if (
      result.error.type === "bad_request" &&
      result.error.details?.some((detail) => detail.issue === "token_expired")
    ) {
      return <SurveyInactive surveyClosedMessage={{ heading: t("c.link_expired") }} status="link expired" />;
    }
    // When token is invalid, we don't have survey data to get workspace branding settings
    // So we show SurveyInactive without workspace data (shows branding by default for backward compatibility)
    return <SurveyInactive status="link invalid" />;
  }

  const { surveyId, contactId } = result.data;

  const existingResponse = await getExistingContactResponse(surveyId, contactId)();
  if (existingResponse) {
    const survey = await getSurvey(surveyId);
    if (survey) {
      const workspace = await getWorkspaceById(survey.workspaceId);
      return <SurveyInactive status="response submitted" workspace={workspace || undefined} />;
    }
    return <SurveyInactive status="response submitted" />;
  }

  const isPreview = preview === "true";
  const survey = await getSurvey(surveyId);

  if (!survey) {
    notFound();
  }

  const isSingleUseSurvey = survey?.singleUse?.enabled;
  const isSingleUseSurveyEncrypted = survey?.singleUse?.isEncrypted;

  let singleUseId: string | undefined = undefined;

  if (isSingleUseSurvey) {
    const validatedSingleUseId = checkAndValidateSingleUseId(
      suId,
      isSingleUseSurveyEncrypted,
      survey.id,
      suToken
    );
    if (!validatedSingleUseId) {
      const workspaceContext = await getWorkspaceContextForLinkSurvey(survey.workspaceId);
      return <SurveyInactive status="link invalid" workspace={workspaceContext.workspace} />;
    }

    singleUseId = validatedSingleUseId;
  }

  // The count is only ever rendered by the welcome card, which is only mounted when the card itself
  // is enabled - fetching it on showResponseCount alone pays for a number nothing displays.
  const needsResponseCount = survey.welcomeCard.enabled && survey.welcomeCard.showResponseCount;

  // Parallel fetch of environment context, locale and the conditional welcome-card response count
  const [workspaceContext, locale, singleUseResponse, responseCount] = await Promise.all([
    getWorkspaceContextForLinkSurvey(survey.workspaceId),
    findMatchingLocale(),
    // Fetch existing response for this contact
    getExistingContactResponse(survey.id, contactId)(),
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

  return renderSurvey({
    survey,
    searchParams,
    contactId,
    isPreview,
    singleUseId,
    singleUseResponse,
    workspaceContext,
    locale,
    responseCount,
  });
};
