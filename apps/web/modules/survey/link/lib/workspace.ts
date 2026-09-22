import "server-only";
import { cache as reactCache } from "react";
import { createCacheKey } from "@forma/cache";
import { prisma } from "@forma/database";
import { Prisma, Workspace } from "@forma/database/prisma";
import { logger } from "@forma/logger";
import { ZId } from "@forma/types/common";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { TOrganizationWhitelabel } from "@forma/types/organizations";
import { cache } from "@/lib/cache";
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
 * How long a link survey may serve a workspace's branding after it changed.
 *
 * Matched to `getWorkspaceState`, the other public surface served out of a workspace row, and short
 * on purpose: nothing invalidates this key, so the bound is the whole guarantee. An admin who saves
 * new styling, a new logo or different head scripts sees the public link follow within a minute,
 * and the two workspace writers stay free of any cache knowledge.
 */
const WORKSPACE_CONTEXT_CACHE_TTL_MS = 60 * 1000;

/**
 * Fetches all workspace-related data needed for link surveys in a single optimized query.
 * Combines workspace and organization data using Prisma relationships to minimize database round trips.
 *
 * Cached in Redis across requests, not just deduped within one render. Everything here is
 * workspace-scoped — branding, styling, logo, head scripts, whitelabel — so one entry is shared by
 * every respondent of every survey in the workspace, which is what makes the hit rate high even for
 * a survey nobody is looking at. `reactCache` still wraps it so a single render pays one Redis GET
 * rather than one per caller (the page, its metadata, and the inactive-survey branch).
 *
 * A missing workspace or organization throws out of the cached function, so nothing negative is
 * stored and the next request reads the database again.
 *
 * @param workspaceId - The workspace identifier
 * @returns Object containing workspace styling data, organization ID, and whitelabel settings
 * @throws ResourceNotFoundError if workspace or organization not found
 * @throws DatabaseError if database query fails
 */
export const getWorkspaceContextForLinkSurvey = reactCache(
  async (workspaceId: string): Promise<TWorkspaceContextForLinkSurvey> => {
    validateInputs([workspaceId, ZId]);

    return cache.withCache(
      () => fetchWorkspaceContextForLinkSurvey(workspaceId),
      createCacheKey.workspace.config(workspaceId),
      WORKSPACE_CONTEXT_CACHE_TTL_MS
    );
  }
);

const fetchWorkspaceContextForLinkSurvey = async (
  workspaceId: string
): Promise<TWorkspaceContextForLinkSurvey> => {
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
};

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
