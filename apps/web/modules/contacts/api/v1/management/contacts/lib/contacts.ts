import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";
import { TContact } from "@/modules/contacts/types/contact";

/**
 * Hard ceiling on one page. The query was unbounded, so a workspace with a large contact list pulled
 * every row of every workspace the API key can reach into memory and serialized them into a single
 * response. Callers that need more page through with `skip`.
 */
export const CONTACTS_PAGE_LIMIT_MAX = 500;

export const getContacts = reactCache(
  async (workspaceIds: string[], limit?: number, skip?: number): Promise<TContact[]> => {
    validateInputs([workspaceIds, ZId.array()]);

    try {
      const contacts = await prisma.contact.findMany({
        where: { workspaceId: { in: workspaceIds } },
        // Paging is only meaningful over a stable order, and `id` breaks ties on identical
        // timestamps so a row is neither skipped nor returned twice across pages.
        orderBy: [{ createdAt: "desc" }, { id: "desc" }],
        take: Math.min(limit ?? CONTACTS_PAGE_LIMIT_MAX, CONTACTS_PAGE_LIMIT_MAX),
        ...(skip ? { skip } : {}),
      });

      return contacts;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);
