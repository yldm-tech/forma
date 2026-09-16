import "server-only";
import { cache as reactCache } from "react";
import { z } from "zod";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ZId } from "@forma/types/common";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { TUser, TUserLocale, TUserUpdateInput, ZUserUpdateInput } from "@forma/types/user";
import { deleteUserOrganizationRelationships } from "@/lib/authzed/organization-membership";
import { runPostCommitProjection } from "@/lib/authzed/projection-boundary";
import { deleteUserTeamRelationships } from "@/lib/authzed/team-workspace";
import { deleteOrganization, getOrganizationsWhereUserIsSingleOwner } from "@/lib/organization/service";
import { deleteBrevoCustomerByEmail } from "@/modules/auth/lib/brevo";
import { validateInputs } from "../utils/validate";
import { publicUserSelect } from "./public-user";

// function to retrive basic information about a user's user
export const getUser = reactCache(async (id: string): Promise<TUser | null> => {
  validateInputs([id, ZId]);

  try {
    const user = await prisma.user.findUnique({
      where: {
        id,
      },
      select: publicUserSelect,
    });

    if (!user) {
      return null;
    }
    return user;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
});

export const getUserByEmail = reactCache(async (email: string): Promise<TUser | null> => {
  validateInputs([email, z.email()]);

  try {
    const user = await prisma.user.findFirst({
      where: {
        email,
      },
      select: publicUserSelect,
    });

    return user;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
});

// function to update a user's user
export const updateUser = async (personId: string, data: TUserUpdateInput): Promise<TUser> => {
  validateInputs([personId, ZId], [data, ZUserUpdateInput.partial()]);

  try {
    const updatedUser = await prisma.user.update({
      where: {
        id: personId,
      },
      data: data,
      select: publicUserSelect,
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

const deleteUserById = async (id: string): Promise<TUser> => {
  validateInputs([id, ZId]);

  try {
    const user = await prisma.user.delete({
      where: {
        id,
      },
      select: publicUserSelect,
    });
    return user;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};

// function to delete a user's user including organizations
export const deleteUser = async (id: string): Promise<TUser> => {
  validateInputs([id, ZId]);

  try {
    const organizationsWithSingleOwner = await getOrganizationsWhereUserIsSingleOwner(id);

    for (const organization of organizationsWithSingleOwner) {
      await deleteOrganization(organization.id);
    }

    // tenant-scope-exempt: invites this user created, scoped by creatorId; deleting an account must clear them everywhere
    await prisma.invite.deleteMany({ where: { creatorId: id } });

    const deletedUser = await deleteUserById(id);
    await runPostCommitProjection("user_delete_organization_cleanup", () =>
      deleteUserOrganizationRelationships(id)
    );
    await runPostCommitProjection("user_delete_team_cleanup", () => deleteUserTeamRelationships(id));
    await deleteBrevoCustomerByEmail({ email: deletedUser.email });

    return deletedUser;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};

export const getUsersWithOrganization = async (organizationId: string): Promise<TUser[]> => {
  validateInputs([organizationId, ZId]);

  try {
    const users = await prisma.user.findMany({
      where: {
        memberships: {
          some: {
            organizationId,
          },
        },
      },
      select: publicUserSelect,
    });

    return users;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};

export const getUserLocale = reactCache(async (id: string): Promise<TUserLocale | undefined> => {
  validateInputs([id, ZId]);

  try {
    const user = await prisma.user.findUnique({
      where: {
        id,
      },
      select: publicUserSelect,
    });

    if (!user) {
      return undefined;
    }
    return user.locale;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
});
