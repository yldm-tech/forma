import "server-only";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { ZId } from "@forma/types/common";
import { DatabaseError } from "@forma/types/errors";
import { TTagsCount, TTagsOnResponses } from "@forma/types/tags";
import { getUniqueConstraintFields, isUniqueConstraintError } from "../utils/prisma-constraint";
import { validateInputs } from "../utils/validate";

const selectTagsOnResponse = {
  tag: {
    select: {
      workspaceId: true,
    },
  },
};

export const addTagToRespone = async (responseId: string, tagId: string): Promise<TTagsOnResponses> => {
  try {
    await prisma.tagsOnResponses.create({
      data: {
        responseId,
        tagId,
      },
      select: selectTagsOnResponse,
    });

    return {
      responseId,
      tagId,
    };
  } catch (error) {
    if (isUniqueConstraintError(error)) {
      const fields = getUniqueConstraintFields(error);
      if (fields.includes("responseId") && fields.includes("tagId")) {
        // Idempotent: the tag is already on the response.
        return {
          responseId,
          tagId,
        };
      }
    }
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};

export const deleteTagOnResponse = async (responseId: string, tagId: string): Promise<TTagsOnResponses> => {
  try {
    await prisma.tagsOnResponses.delete({
      where: {
        responseId_tagId: {
          responseId,
          tagId,
        },
      },
      select: selectTagsOnResponse,
    });

    return {
      tagId,
      responseId,
    };
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
};

/**
 * Counted by tag id, not by walking the response join. A tag belongs to exactly one workspace, so a
 * caller that already holds the workspace's tag ids has an equivalent — and much tighter — scope: the
 * aggregate reads `TagsOnResponses.@@index([tagId])` directly instead of semi-joining Response and
 * Survey, so its cost tracks tagged rows rather than the workspace's total response count.
 *
 * Not `reactCache`d: the cache keys on argument identity, and a fresh `tagIds` array never hits.
 */
export const getTagsOnResponsesCount = async (tagIds: string[]): Promise<TTagsCount> => {
  validateInputs([tagIds, ZId.array()]);

  // Prisma would otherwise be asked for `IN ()`, and a workspace with no tags has nothing to count.
  if (tagIds.length === 0) return [];

  try {
    const tagsCount = await prisma.tagsOnResponses.groupBy({
      by: ["tagId"],
      where: {
        tagId: { in: tagIds },
      },
      _count: {
        _all: true,
      },
    });

    return tagsCount.map((tagCount) => ({ tagId: tagCount.tagId, count: tagCount._count._all }));
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
};

/** Single-tag form of the above, for callers that hold one tag and would otherwise aggregate a set. */
export const getTagOnResponsesCount = async (tagId: string): Promise<number> => {
  validateInputs([tagId, ZId]);

  try {
    return await prisma.tagsOnResponses.count({ where: { tagId } });
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      throw new DatabaseError(error.message);
    }
    throw error;
  }
};
