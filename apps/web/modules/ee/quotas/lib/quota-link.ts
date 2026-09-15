import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";

export const getQuotaLinkCountByQuotaId = reactCache(async (quotaId: string): Promise<number> => {
  try {
    validateInputs([quotaId, ZId]);

    const quotaLinkCount = await prisma.responseQuotaLink.count({
      where: {
        quotaId,
        status: "screenedIn",
      },
    });

    return quotaLinkCount;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
});
