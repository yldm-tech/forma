import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { DatabaseError } from "@forma/types/errors";

export const getContactAttributes = reactCache(async (workspaceIds: string[]) => {
  try {
    const contactAttributeKeys = await prisma.contactAttribute.findMany({
      where: {
        attributeKey: {
          workspaceId: { in: workspaceIds },
        },
      },
    });

    return contactAttributeKeys;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
});
