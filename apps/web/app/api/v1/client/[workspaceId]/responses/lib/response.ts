import "server-only";
import { prisma } from "@forma/database";
import { Prisma } from "@forma/database/prisma";
import { TContactAttributes } from "@forma/types/contact-attribute";
import { type TIngestFlag } from "@forma/types/embedded-data-ingest";
import { ResourceNotFoundError } from "@forma/types/errors";
import { TResponseWithQuotaFull } from "@forma/types/quota";
import { TResponse, TResponseInput, ZResponseInput } from "@forma/types/responses";
import {
  buildClientResponse,
  createResponseWithQuotaEvaluation as createClientResponseWithQuotaEvaluation,
} from "@/app/api/client/[workspaceId]/responses/lib/response";
import { handleClientResponseCreateError } from "@/app/api/client/[workspaceId]/responses/lib/response-error";
import { buildPrismaResponseData } from "@/app/api/v1/lib/utils";
import { assertDisplayOwnership } from "@/lib/display/service";
import { getOrganization } from "@/lib/organization/service";
import { calculateTtcTotal } from "@/lib/response/utils";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { validateInputs } from "@/lib/utils/validate";
import { getContactByUserId } from "./contact";

export const responseSelection = {
  id: true,
  createdAt: true,
  updatedAt: true,
  surveyId: true,
  finished: true,
  data: true,
  meta: true,
  ttc: true,
  variables: true,
  contactAttributes: true,
  singleUseId: true,
  language: true,
  displayId: true,
  endingId: true,
  contact: {
    select: {
      id: true,
      // `userId` is the only attribute anything reads off this relation, so the join stays narrow.
      // Selecting the whole `attributeKey` row dragged all ten of its columns (including the nullable
      // `description` text) along for every attribute of every contact on every response read.
      attributes: {
        where: { attributeKey: { key: "userId" } },
        select: { attributeKey: { select: { key: true } }, value: true },
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
  responseInput: TResponseInput,
  ingestFlags?: readonly TIngestFlag[],
  // Optional caller-owned transaction — see the comment on the client helper this delegates to.
  tx?: Prisma.TransactionClient
): Promise<TResponseWithQuotaFull> => {
  return await createClientResponseWithQuotaEvaluation(responseInput, createResponse, ingestFlags, tx);
};

export const createResponse = async (
  responseInput: TResponseInput,
  tx: Prisma.TransactionClient,
  ingestFlags?: readonly TIngestFlag[]
): Promise<TResponse> => {
  validateInputs([responseInput, ZResponseInput]);

  const { workspaceId, userId, finished, ttc: initialTtc } = responseInput;

  try {
    let contact: { id: string; attributes: TContactAttributes } | null = null;

    const organizationId = await getOrganizationIdFromWorkspaceId(workspaceId);
    const organization = await getOrganization(organizationId);
    if (!organization) {
      throw new ResourceNotFoundError("Organization", organizationId);
    }

    if (userId) {
      contact = await getContactByUserId(workspaceId, userId);
    }

    const ttc = initialTtc ? (finished ? calculateTtcTotal(initialTtc) : initialTtc) : {};

    if (responseInput.displayId) {
      await assertDisplayOwnership(
        responseInput.displayId,
        workspaceId,
        responseInput.surveyId,
        contact?.id ?? null,
        tx
      );
    }

    const prismaData = buildPrismaResponseData(
      { ...responseInput, createdAt: undefined, updatedAt: undefined },
      contact,
      ttc,
      ingestFlags
    );

    const prismaClient = tx ?? prisma;

    const responsePrisma = await prismaClient.response.create({
      data: prismaData,
      select: responseSelection,
    });

    return buildClientResponse(responsePrisma, contact);
  } catch (error) {
    return handleClientResponseCreateError(error, responseInput.displayId);
  }
};
