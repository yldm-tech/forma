import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ResourceNotFoundError } from "@forma/types/errors";
import { TUser, TUserUpdateInput } from "@forma/types/user";

// function to update a user's user
export const updateUser = async (personId: string, data: TUserUpdateInput): Promise<TUser> => {
  try {
    const updatedUser = await prisma.user.update({
      where: {
        id: personId,
      },
      data: data,
      select: {
        id: true,
        name: true,
        email: true,
        emailVerified: true,
        createdAt: true,
        updatedAt: true,
        twoFactorEnabled: true,
        identityProvider: true,
        notificationSettings: true,
        locale: true,
        lastLoginAt: true,
        isActive: true,
      },
    });

    return updatedUser;
  } catch (error) {
    if (
      error instanceof Prisma.PrismaClientKnownRequestError &&
      error.code === PrismaErrorType.RecordNotFound
    ) {
      throw new ResourceNotFoundError("User", personId);
    }
    throw error; // Re-throw any other errors
  }
};
