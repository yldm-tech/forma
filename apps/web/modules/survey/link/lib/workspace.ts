import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma, Workspace } from "@forma/database/prisma";
import { logger } from "@forma/logger";
import { ZId } from "@forma/types/common";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { TOrganizationWhitelabel } from "@forma/types/organizations";
import { validateInputs } from "@/lib/utils/validate";

type TWorkspaceForLinkSurvey = Pick<
  Workspace,
  "id" | "name" | "styling" | "logo" | "linkSurveyBranding" | "customHeadScripts"
>;

export interface TWorkspaceContextForLinkSurvey {
  workspace: TWorkspaceForLinkSurvey;
  organizationId: string;
  organizationWhitelabel: TOrganizationWhitelabel | null;
}

/**
 * Fetches all workspace-related data needed for link surveys in a single optimized query.
 * Combines workspace and organization data using Prisma relationships to minimize database round trips.
 *
 * @param workspaceId - The workspace identifier
 * @returns Object containing workspace styling data, organization ID, and whitelabel settings
 * @throws ResourceNotFoundError if workspace or organization not found
 * @throws DatabaseError if database query fails
 */
export const getWorkspaceContextForLinkSurvey = reactCache(
  async (workspaceId: string): Promise<TWorkspaceContextForLinkSurvey> => {
    validateInputs([workspaceId, ZId]);

    try {
      const workspace = await prisma.workspace.findUnique({
        where: { id: workspaceId },
        select: {
          id: true,
          name: true,
          styling: true,
          logo: true,
          linkSurveyBranding: true,
          customHeadScripts: true,
          organizationId: true,
          organization: {
            select: {
              id: true,
              whitelabel: true,
            },
          },
        },
      });

      if (!workspace) {
        throw new ResourceNotFoundError("Workspace", workspaceId);
      }

      if (!workspace.organization) {
        throw new ResourceNotFoundError("Organization", null);
      }

      return {
        workspace: {
          id: workspace.id,
          name: workspace.name,
          styling: workspace.styling,
          logo: workspace.logo,
          linkSurveyBranding: workspace.linkSurveyBranding,
          customHeadScripts: workspace.customHeadScripts,
        },
        organizationId: workspace.organizationId,
        organizationWhitelabel: workspace.organization.whitelabel ?? null,
      };
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }
      throw error;
    }
  }
);

export const getWorkspaceById = reactCache(
  async (
    workspaceId: string
  ): Promise<Pick<
    Workspace,
    "styling" | "logo" | "linkSurveyBranding" | "name" | "customHeadScripts"
  > | null> => {
    validateInputs([workspaceId, ZId]);

    let workspacePrisma;

    try {
      workspacePrisma = await prisma.workspace.findUnique({
        where: {
          id: workspaceId,
        },
        select: {
          styling: true,
          logo: true,
          linkSurveyBranding: true,
          name: true,
          customHeadScripts: true,
        },
      });

      return workspacePrisma;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        logger.error(error, "Error fetching workspace by id");
        throw new DatabaseError(error.message);
      }
      throw error;
    }
  }
);
