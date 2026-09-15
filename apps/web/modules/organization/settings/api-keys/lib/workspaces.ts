import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { DatabaseError } from "@forma/types/errors";
import { TOrganizationWorkspace } from "@/modules/organization/settings/api-keys/types/api-keys";

export const getWorkspacesByOrganizationId = reactCache(
  async (organizationId: string): Promise<TOrganizationWorkspace[]> => {
    try {
      const workspaces = await prisma.workspace.findMany({
        where: {
          organizationId,
        },
        select: {
          id: true,
          name: true,
        },
      });

      return workspaces;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);
