import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { logger } from "@forma/logger";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { TSurvey } from "@forma/types/surveys/types";
import { selectSurvey } from "@/lib/survey/service";
import { transformPrismaSurvey } from "@/lib/survey/utils";
import { validateInputs } from "@/lib/utils/validate";

export const getSurveys = reactCache(async (workspaceId: string): Promise<TSurvey[]> => {
  validateInputs([workspaceId, ZId]);

  try {
    const surveysPrisma = await prisma.survey.findMany({
      where: {
        workspaceId,
        status: {
          not: "completed",
        },
        // Archived surveys must not be selectable as integration targets.
        archivedAt: null,
      },
      select: selectSurvey,
      orderBy: {
        updatedAt: "desc",
      },
    });

    return surveysPrisma.map((surveyPrisma) => transformPrismaSurvey<TSurvey>(surveyPrisma));
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      logger.error({ error }, "getSurveys: Could not fetch surveys");
      throw new DatabaseError(error.message);
    }
    throw error;
  }
});
