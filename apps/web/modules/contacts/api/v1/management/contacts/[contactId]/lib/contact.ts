import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";
import { TContact } from "@/modules/contacts/types/contact";

export const getContact = reactCache(async (contactId: string): Promise<TContact | null> => {
  validateInputs([contactId, ZId]);

  try {
    const contact = await prisma.contact.findUnique({
      where: { id: contactId },
    });

    if (!contact) {
      return null;
    }

    return contact;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
});

export const deleteContact = async (contactId: string): Promise<void> => {
  validateInputs([contactId, ZId]);

  try {
    await prisma.contact.delete({
      where: { id: contactId },
      select: {
        id: true,
        attributes: { select: { attributeKey: { select: { key: true } }, value: true } },
      },
    });
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};
