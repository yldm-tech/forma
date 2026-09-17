import { InvalidInputError, ResourceNotFoundError } from "@forma/types/errors";
import {
  TDisplayCreateInputV2,
  ZDisplayCreateInputV2,
} from "@/app/api/v2/client/[workspaceId]/displays/types/display";
import { reportApiError } from "@/lib/api/api-error-reporter";
import { parseAndValidateJsonBody } from "@/lib/api/parse-and-validate-json-body";
import { responses } from "@/lib/api/response";
import { resolveClientApiIds } from "@/lib/utils/resolve-client-id";
import { createDisplay } from "./lib/display";

interface Context {
  params: Promise<{
    workspaceId: string;
  }>;
}

type TValidatedDisplayInputResult = { displayInputData: TDisplayCreateInputV2 } | { response: Response };

const parseAndValidateDisplayInput = async (
  request: Request,
  workspaceId: string
): Promise<TValidatedDisplayInputResult> => {
  const inputValidation = await parseAndValidateJsonBody({
    request,
    schema: ZDisplayCreateInputV2,
    buildInput: (jsonInput) => ({
      ...(jsonInput !== null && typeof jsonInput === "object" ? jsonInput : {}),
      workspaceId,
    }),
    malformedJsonMessage: "Invalid JSON in request body",
  });

  if ("response" in inputValidation) {
    return inputValidation;
  }

  return { displayInputData: inputValidation.data };
};

export const OPTIONS = async (): Promise<Response> => {
  return responses.successResponse(
    {},
    true,
    // Cache CORS preflight responses for 1 hour (conservative approach)
    // Balances performance gains with flexibility for CORS policy changes
    "public, s-maxage=3600, max-age=3600"
  );
};

export const POST = async (request: Request, context: Context): Promise<Response> => {
  const params = await context.params;
  // Resolve: accepts either an environmentId (old SDK) or a workspaceId (new SDK)
  const resolved = await resolveClientApiIds(params.workspaceId);
  if (!resolved) {
    return responses.notFoundResponse("Workspace", params.workspaceId);
  }
  const { workspaceId } = resolved;

  const validatedInput = await parseAndValidateDisplayInput(request, workspaceId);

  if ("response" in validatedInput) {
    return validatedInput.response;
  }

  const { displayInputData } = validatedInput;

  try {
    const response = await createDisplay(displayInputData);

    return responses.successResponse(response, true);
  } catch (error) {
    if (error instanceof ResourceNotFoundError) {
      return responses.notFoundResponse("Survey", displayInputData.surveyId, true);
    }

    if (error instanceof InvalidInputError) {
      return responses.forbiddenResponse(error.message, true, {
        surveyId: displayInputData.surveyId,
      });
    }

    const response = responses.internalServerErrorResponse("Something went wrong. Please try again.", true);
    reportApiError({
      request,
      status: response.status,
      error,
    });
    return response;
  }
};
