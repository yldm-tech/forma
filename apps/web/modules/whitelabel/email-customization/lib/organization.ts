import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ZId, ZString } from "@forma/types/common";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { validateInputs } from "@/lib/utils/validate";

export const updateOrganizationEmailLogoUrl = async (
  organizationId: string,
  logoUrl: string
): Promise<boolean> => {
  validateInputs([organizationId, ZId], [logoUrl, ZString]);

  try {
    const organization = await prisma.organization.findUnique({
      where: { id: organizationId },
    });

    if (!organization) {
      throw new ResourceNotFoundError("Organization", organizationId);
    }

    await prisma.organization.update({
      where: { id: organizationId },
      data: {
        whitelabel: {
          ...organization.whitelabel,
          logoUrl,
        },
      },
      select: {
        workspaces: {
          select: {
            id: true,
          },
        },
      },
    });

    return true;
  } catch (error) {
    if (
      error instanceof Prisma.PrismaClientKnownRequestError &&
      error.code === PrismaErrorType.RecordNotFound
    ) {
      throw new ResourceNotFoundError("Organization", organizationId);
    }

    throw error;
  }
};

export const removeOrganizationEmailLogoUrl = async (organizationId: string): Promise<boolean> => {
  validateInputs([organizationId, ZId]);

  try {
    const organization = await prisma.organization.findUnique({
      where: { id: organizationId },
      select: {
        whitelabel: true,
        workspaces: {
          select: {
            id: true,
          },
        },
      },
    });

    if (!organization) {
      throw new ResourceNotFoundError("Organization", organizationId);
    }

    await prisma.organization.update({
      where: { id: organizationId },
      data: {
        whitelabel: {
          ...organization.whitelabel,
          logoUrl: null,
        },
      },
    });

    return true;
  } catch (error) {
    if (
      error instanceof Prisma.PrismaClientKnownRequestError &&
      error.code === PrismaErrorType.RecordNotFound
    ) {
      throw new ResourceNotFoundError("Organization", organizationId);
    }

    throw error;
  }
};

export const getOrganizationLogoUrl = reactCache(async (organizationId: string): Promise<string | null> => {
  validateInputs([organizationId, ZId]);
  try {
    const organization = await prisma.organization.findUnique({
      where: { id: organizationId },
      select: {
        whitelabel: true,
      },
    });
    return organization?.whitelabel?.logoUrl ?? null;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
});
