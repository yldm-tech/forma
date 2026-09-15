import { prisma } from "@forma/database";
import { type TIngestFlag } from "@forma/types/embedded-data-ingest";
import { TResponseWithQuotaFull } from "@forma/types/quota";
import { TResponseUpdateInput } from "@forma/types/responses";
import { updateResponse } from "@/lib/response/service";
import { evaluateResponseQuotas } from "@/modules/ee/quotas/lib/evaluation-service";

export const updateResponseWithQuotaEvaluation = async (
  responseId: string,
  responseInput: TResponseUpdateInput,
  ingestFlags?: readonly TIngestFlag[]
): Promise<TResponseWithQuotaFull> => {
  const txResponse = await prisma.$transaction(async (tx) => {
    const response = await updateResponse(responseId, responseInput, tx, ingestFlags);

    const quotaResult = await evaluateResponseQuotas({
      surveyId: response.surveyId,
      responseId: response.id,
      data: response.data,
      variables: response.variables,
      language: response.language || "default",
      responseFinished: response.finished,
      // The row just written, so `reserved` quota operands resolve (ENG-1840).
      response,
      tx,
    });

    return {
      ...response,
      ...(quotaResult.quotaFull && { quotaFull: quotaResult.quotaFull }),
    };
  });

  return txResponse;
};
