import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { err, ok } from "@forma/types/error-handlers";

export const getContactAttributeKeys = reactCache(async (workspaceId: string) => {
  try {
    const contactAttributeKeys = await prisma.contactAttributeKey.findMany({
      where: { workspaceId },
      select: {
        key: true,
      },
    });

    const keys = contactAttributeKeys.map((key) => key.key);
    return ok(keys);
  } catch (error) {
    return err({
      type: "internal_server_error",
      details: [
        {
          field: "contact attribute keys",
          issue: error instanceof Error ? error.message : "Unknown error occurred",
        },
      ],
    });
  }
});
