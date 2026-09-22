import "server-only";
import { cache as reactCache } from "react";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { PrismaErrorType } from "@forma/database/types/error";
import { ZId, ZOptionalNumber } from "@forma/types/common";
import { TContactAttributes } from "@forma/types/contact-attribute";
import { DatabaseError, ResourceNotFoundError } from "@forma/types/errors";
import { TResponse, TResponseInput, ZResponseInput } from "@forma/types/responses";
import { TTag } from "@forma/types/tags";
import { buildPrismaResponseData } from "@/app/api/v1/lib/utils";
import { RESPONSES_PER_PAGE } from "@/lib/constants";
import { getResponseContact } from "@/lib/response/service";
import { calculateTtcTotal } from "@/lib/response/utils";
import { getSurvey } from "@/lib/survey/service";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { validateInputs } from "@/lib/utils/validate";
import { evaluateResponseQuotas } from "@/modules/quotas/lib/evaluation-service";
import { getContactByUserId } from "./contact";

export const responseSelection = {
  id: true,
  createdAt: true,
  updatedAt: true,
  surveyId: true,
  finished: true,
  endingId: true,
  data: true,
  meta: true,
  ttc: true,
  variables: true,
  contactAttributes: true,
  singleUseId: true,
  language: true,
  displayId: true,
  contact: {
    select: {
      id: true,
      attributes: {
        select: { attributeKey: true, value: true },
      },
    },
  },
  tags: {
    select: {
      tag: {
        select: {
          id: true,
          createdAt: true,
          updatedAt: true,
          name: true,
          workspaceId: true,
        },
      },
    },
  },
} satisfies Prisma.ResponseSelect;

export const createResponseWithQuotaEvaluation = async (
  responseInput: TResponseInput
): Promise<TResponse> => {
  const txResponse = await prisma.$transaction(async (tx) => {
    const response = await createResponse(responseInput, tx);

    // Feed quota evaluation the language actually PERSISTED on the response (createResponse ->
    // buildPrismaResponseData canonicalizes it), so the stored value is the single source of truth and a
    // legacy code from a stale client still matches language-scoped quotas. Mirrors the v2/management path.
    const quotaResult = await evaluateResponseQuotas({
      surveyId: responseInput.surveyId,
      responseId: response.id,
      data: responseInput.data,
      variables: responseInput.variables,
      language: response.language || "default",
      responseFinished: response.finished,
      // The row just written, so `reserved` quota operands resolve (ENG-1840).
      response,
      tx,
    });

    if (quotaResult.shouldEndSurvey && quotaResult.refreshedResponse) {
      return {
        ...quotaResult.refreshedResponse,
        tags: response.tags,
        contact: response.contact,
      };
    }

    return response;
  });

  return txResponse;
};

export const createResponse = async (
  responseInput: TResponseInput,
  tx?: Prisma.TransactionClient
): Promise<TResponse> => {
  validateInputs([responseInput, ZResponseInput]);

  const { workspaceId, surveyId, displayId, userId, finished, ttc: initialTtc } = responseInput;

  try {
    let contact: { id: string; attributes: TContactAttributes } | null = null;

    const organization = await getOrganizationIdFromWorkspaceId(workspaceId);
    if (!organization) {
      throw new ResourceNotFoundError("Organization", null);
    }

    // `displayId` is caller-supplied and was connected with no ownership check at all. Display<->Response is one-to-one, so naming another workspace's display moved that display onto this response, permanently consuming it and corrupting the other tenant's display and completion counts. A display belongs to exactly one survey, and the route has already asserted that survey belongs to this workspace, so matching surveyId is the tightest check available. Mirrors the guard the v2 management path already carries.
    if (displayId) {
      const display = await (tx ?? prisma).display.findUnique({
        where: { id: displayId },
        select: { surveyId: true },
      });

      // Uniform not-found for "does not exist" and "exists but belongs elsewhere": distinguishing the two would confirm that a display id is real, making this endpoint a cross-tenant existence oracle.
      if (display?.surveyId !== surveyId) {
        throw new ResourceNotFoundError("Display", displayId);
      }
    }

    if (userId) {
      contact = await getContactByUserId(workspaceId, userId);
    }

    const ttc = initialTtc ? (finished ? calculateTtcTotal(initialTtc) : initialTtc) : {};

    const prismaData = buildPrismaResponseData(responseInput, contact, ttc);

    const prismaClient = tx ?? prisma;

    const responsePrisma = await prismaClient.response.create({
      data: prismaData,
      select: responseSelection,
    });

    const response: TResponse = {
      ...responsePrisma,
      contact: contact
        ? {
            id: contact.id,
            userId: contact.attributes.userId,
          }
        : null,
      tags: responsePrisma.tags.map((tagPrisma: { tag: TTag }) => tagPrisma.tag),
    };

    return response;
  } catch (error) {
    if (error instanceof Prisma.PrismaClientKnownRequestError) {
      if (error.code === PrismaErrorType.RecordNotFound) {
        throw new DatabaseError("Display ID does not exist");
      }
      throw new DatabaseError(error.message);
    }

    throw error;
  }
};

export const getResponsesByWorkspaceIds = reactCache(
  async (workspaceIds: string[], limit?: number, offset?: number): Promise<TResponse[]> => {
    validateInputs([workspaceIds, ZId.array()], [limit, ZOptionalNumber], [offset, ZOptionalNumber]);
    try {
      const responses = await prisma.response.findMany({
        where: {
          survey: {
            workspaceId: { in: workspaceIds },
          },
        },
        select: responseSelection,
        orderBy: [
          {
            createdAt: "desc",
          },
        ],
        take: limit ? limit : undefined,
        skip: offset ? offset : undefined,
      });

      const transformedResponses: TResponse[] = responses.map((responsePrisma) => ({
        ...responsePrisma,
        contact: getResponseContact(responsePrisma),
        tags: responsePrisma.tags.map((tagPrisma: { tag: TTag }) => tagPrisma.tag),
      }));

      return transformedResponses;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);

export const getResponses = reactCache(
  async (surveyId: string, limit?: number, offset?: number): Promise<TResponse[]> => {
    validateInputs([surveyId, ZId], [limit, ZOptionalNumber], [offset, ZOptionalNumber]);

    limit = limit ?? RESPONSES_PER_PAGE;
    const survey = await getSurvey(surveyId);
    if (!survey) return [];
    try {
      const responses = await prisma.response.findMany({
        where: { surveyId },
        select: responseSelection,
        orderBy: [
          {
            createdAt: "desc",
          },
          {
            id: "desc", // Secondary sort by ID for consistent pagination
          },
        ],
        take: limit,
        skip: offset,
      });

      const transformedResponses: TResponse[] = responses.map((responsePrisma) => ({
        ...responsePrisma,
        contact: getResponseContact(responsePrisma),
        tags: responsePrisma.tags.map((tagPrisma: { tag: TTag }) => tagPrisma.tag),
      }));

      return transformedResponses;
    } catch (error) {
      if (error instanceof Prisma.PrismaClientKnownRequestError) {
        throw new DatabaseError(error.message);
      }

      throw error;
    }
  }
);
