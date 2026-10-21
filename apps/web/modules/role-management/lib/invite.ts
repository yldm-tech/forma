import { prisma } from "@forma/database";
import { type Invite, Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ResourceNotFoundError } from "@forma/types/errors";
import { type TInviteUpdateInput } from "@/modules/role-management/types/invites";

/**
 * Reads the role an invite currently carries.
 *
 * Deliberately not `getInvite`: that one omits `role` from its select and is `reactCache`d, so calling it either side of an update returns the same pre-update object. Auditing a role change needs an uncached read of the field that changed.
 */
export const getInviteRole = async (inviteId: string): Promise<Invite["role"] | null> => {
  const invite = await prisma.invite.findUnique({
    where: { id: inviteId },
    select: { role: true },
  });

  return invite?.role ?? null;
};

export const updateInvite = async (inviteId: string, data: TInviteUpdateInput): Promise<boolean> => {
  try {
    const invite = await prisma.invite.update({
      where: { id: inviteId },
      data,
    });

    if (invite === null) {
      throw new ResourceNotFoundError("Invite", inviteId);
    }

    return true;
  } catch (error) {
    if (
      error instanceof Prisma.PrismaClientKnownRequestError &&
      error.code === PrismaErrorType.RecordNotFound
    ) {
      throw new ResourceNotFoundError("Invite", inviteId);
    } else {
      throw error; // Re-throw any other errors
    }
  }
};
