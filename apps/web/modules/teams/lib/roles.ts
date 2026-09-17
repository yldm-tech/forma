import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { logger } from "@forma/logger";
import { ZId, ZString } from "@forma/types/common";
import { DatabaseError, UnknownError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";
import { TTeamRole } from "@/modules/teams/team-list/types/team";
import { TTeamPermission } from "@/modules/teams/workspace-teams/types/team";

export const getWorkspacePermissionByUserId = reactCache(
  async (userId: string, workspaceId: string): Promise<TTeamPermission | null> => {
    validateInputs([userId, ZString], [workspaceId, ZString]);

    try {
      const workspaceMemberships = await prisma.workspaceTeam.findMany({
        where: {
          workspaceId,
          team: {
            teamUsers: {
              some: {
                userId,
              },
            },
          },
        },
      });

      if (!workspaceMemberships) return null;
      let highestPermission: TTeamPermission | null = null;

      for (const membership of workspaceMemberships) {
        if (membership.permission === "manage") {
          highestPermission = "manage";
        } else if (membership.permission === "readWrite" && highestPermission !== "manage") {
          highestPermission = "readWrite";
        } else if (
          membership.permission === "read" &&
          highestPermission !== "manage" &&
          highestPermission !== "readWrite"
        ) {
          highestPermission = "read";
        }
      }

      return highestPermission;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        logger.error(error, "Error fetching workspace permission by user id");
        throw new DatabaseError(error.message);
      }

      throw new UnknownError("Error while fetching membership");
    }
  }
);

export const getTeamRoleByTeamIdUserId = reactCache(
  async (teamId: string, userId: string): Promise<TTeamRole | null> => {
    validateInputs([teamId, ZId], [userId, ZId]);
    try {
      const teamUser = await prisma.teamUser.findUnique({
        where: {
          teamId_userId: {
            teamId,
            userId,
          },
        },
      });

      if (!teamUser) {
        return null;
      }

      return teamUser.role;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);

export const getTeamsWhereUserIsAdmin = reactCache(
  async (userId: string, organizationId: string): Promise<string[]> => {
    validateInputs([userId, ZId], [organizationId, ZId]);
    try {
      const adminTeams = await prisma.teamUser.findMany({
        where: {
          userId,
          role: "admin",
          team: {
            organizationId,
          },
        },
        select: {
          teamId: true,
        },
      });

      return adminTeams.map((at) => at.teamId);
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);
