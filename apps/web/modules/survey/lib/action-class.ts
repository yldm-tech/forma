import { cache as reactCache } from "react";
import { z } from "zod";
import { prisma } from "@forma/database";
import { ActionClass } from "@forma/database/prisma";
import { DatabaseError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";

export const getActionClasses = reactCache(async (workspaceId: string): Promise<ActionClass[]> => {
  validateInputs([workspaceId, z.cuid2()]);

  try {
    return await prisma.actionClass.findMany({
      where: {
        workspaceId,
      },
      orderBy: {
        createdAt: "asc",
      },
    });
  } catch (error) {
    throw new DatabaseError(`Database error when fetching actions for workspace ${workspaceId}`);
  }
});
