// `selectSurvey` and `transformPrismaSurvey` live in `lib/survey`: they describe how a survey is read, and a second copy here had already drifted — this one was missing `archivedAt`.
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { TOrganizationBilling } from "@forma/types/organizations";
import { TSurvey } from "@forma/types/surveys/types";
import { selectSurvey } from "@/lib/survey/service";
import { getOrganizationBillingWithReadThroughSync } from "@/modules/billing/lib/organization-billing";
import { transformPrismaSurvey } from "@/modules/survey/lib/utils";

export const getOrganizationBilling = reactCache(
  async (organizationId: string): Promise<TOrganizationBilling> => {
    const billing = await getOrganizationBillingWithReadThroughSync(organizationId);
    if (!billing) throw new ResourceNotFoundError("Organization", organizationId);
    return billing;
  }
);

export const getSurvey = reactCache(async (surveyId: string): Promise<TSurvey> => {
  try {
    const survey = await prisma.survey.findUnique({
      where: { id: surveyId },
      select: selectSurvey,
    });

    if (!survey) {
      throw new ResourceNotFoundError("Survey", surveyId);
    }

    return transformPrismaSurvey<TSurvey>(survey);
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
});
