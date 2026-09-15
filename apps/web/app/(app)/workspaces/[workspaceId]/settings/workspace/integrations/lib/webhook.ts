import { z } from "zod";
import { prisma } from "@forma/database";
import { Prisma, Webhook } from "@forma/database/prisma";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";

export const getWebhookCountBySource = async (
  workspaceId: string,
  source?: Webhook["source"]
): Promise<number> => {
  validateInputs([workspaceId, ZId], [source, z.string().optional()]);

  try {
    const count = await prisma.webhook.count({
      where: {
        workspaceId,
        source,
      },
    });
    return count;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};
