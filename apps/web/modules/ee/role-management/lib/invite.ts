import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ResourceNotFoundError } from "@forma/types/errors";
import { type TInviteUpdateInput } from "@/modules/ee/role-management/types/invites";

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
