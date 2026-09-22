import { logger } from "@forma/logger";
import { ZUploadPrivateFileRequest } from "@forma/types/storage";
import { parseAndValidateJsonBody } from "@/lib/api/parse-and-validate-json-body";
import { responses } from "@/lib/api/response";
import { THandlerParams, withV1ApiWrapper } from "@/lib/api/with-api-logging";
import { MAX_FILE_UPLOAD_SIZE_BYTES } from "@/lib/constants";
import { getOrganization } from "@/lib/organization/service";
import { getSurvey } from "@/lib/survey/service";
import { getOrganizationIdFromWorkspaceId } from "@/lib/utils/helper";
import { resolveClientApiIds } from "@/lib/utils/resolve-client-id";
import { applyRateLimit } from "@/modules/core/rate-limit/helpers";
import { rateLimitConfigs } from "@/modules/core/rate-limit/rate-limit-configs";
import { getSignedUrlForUpload } from "@/modules/storage/service";
import { getSurveyFileUploadConfigs } from "@/modules/storage/survey-file-upload-elements";
import { getErrorResponseFromStorageError, validateSurveyAllowsFileUpload } from "@/modules/storage/utils";

export const OPTIONS = async (): Promise<Response> => {
  return responses.successResponse(
    {},
    true,
    // Cache CORS preflight responses for 1 hour (conservative approach)
    // Balances performance gains with flexibility for CORS policy changes
    "public, s-maxage=3600, max-age=3600"
  );
};

// api endpoint for getting a s3 signed url for uploading private files
// uploaded files will be private, only the user who has access to the environment can access the file
// uploading private files requires no authentication
// use this to let users upload files to a file upload element response for example

export const POST = withV1ApiWrapper({
  handler: async ({ req, props }: THandlerParams<{ params: Promise<{ workspaceId: string }> }>) => {
    const params = await props.params;

    // Resolve: accepts either an environmentId (old SDK) or a workspaceId (new SDK)
    const resolved = await resolveClientApiIds(params.workspaceId);
    if (!resolved) {
      return {
        response: responses.notFoundResponse("Workspace", params.workspaceId),
      };
    }
    const { workspaceId } = resolved;

    const parsedInputResult = await parseAndValidateJsonBody({
      request: req,
      schema: ZUploadPrivateFileRequest,
      buildInput: (jsonInput) => ({
        ...(jsonInput !== null && typeof jsonInput === "object" ? jsonInput : {}),
        workspaceId,
      }),
    });

    if ("response" in parsedInputResult) {
      if (parsedInputResult.issue === "invalid_json") {
        logger.error({ error: parsedInputResult.details, url: req.url }, "Error parsing JSON input");
      } else {
        logger.error(
          { error: parsedInputResult.details, url: req.url },
          "Fields are missing or incorrectly formatted"
        );
      }

      return {
        response: parsedInputResult.response,
      };
    }

    const { fileName, fileType, surveyId, elementId } = parsedInputResult.data;

    const [survey, organizationId] = await Promise.all([
      getSurvey(surveyId),
      getOrganizationIdFromWorkspaceId(workspaceId),
    ]);
    const organization = await getOrganization(organizationId);

    if (!survey) {
      return {
        response: responses.notFoundResponse("Survey", surveyId),
      };
    }

    if (!organization) {
      return {
        response: responses.notFoundResponse("OrganizationByWorkspaceId", workspaceId),
      };
    }

    if (survey.workspaceId !== workspaceId) {
      return {
        response: responses.badRequestResponse(
          "Survey does not belong to the workspace",
          { surveyId, workspaceId },
          true
        ),
      };
    }

    try {
      await applyRateLimit(rateLimitConfigs.storage.uploadPerWorkspace, workspaceId);
    } catch (error) {
      return {
        response: responses.tooManyRequestsResponse(
          error instanceof Error ? error.message : "Rate limit exceeded",
          true
        ),
      };
    }

    const fileUploadPermission = validateSurveyAllowsFileUpload({
      fileName,
      elementId,
      blocks: survey.blocks,
      questions: survey.questions,
    });

    if (!fileUploadPermission.ok) {
      let responseString: string = "";
      if (fileUploadPermission.reason === "no_file_upload_element") {
        responseString = "Survey does not allow file uploads";
      } else if (fileUploadPermission.reason === "file_upload_element_not_found") {
        responseString = "Element does not allow file uploads";
      } else {
        responseString = "File extension is not allowed for this element";
      }

      return {
        response: responses.badRequestResponse(responseString, undefined),
      };
    }

    // The size the survey advertises to the respondent is the size the server enforces. `maxSizeInMB` is otherwise checked only in the respondent's browser (packages/surveys/src/components/elements/file-upload-element.tsx), so a direct POST to the presigned URL writes up to the ceiling against a survey that shows 5 MB.
    //
    // An element that advertises nothing still gets the ceiling: this is an abuse control on an unauthenticated endpoint, and nothing about it is licensed.
    const advertisedMaxSizeInMB = getSurveyFileUploadConfigs({
      blocks: survey.blocks,
      questions: survey.questions,
    }).find((config) => config.id === elementId)?.maxSizeInMB;

    const maxFileUploadSize =
      typeof advertisedMaxSizeInMB === "number" && advertisedMaxSizeInMB > 0
        ? Math.min(Math.ceil(advertisedMaxSizeInMB * 1024 * 1024), MAX_FILE_UPLOAD_SIZE_BYTES)
        : MAX_FILE_UPLOAD_SIZE_BYTES;

    const signedUrlResponse = await getSignedUrlForUpload(
      fileName,
      workspaceId,
      fileType,
      "private",
      maxFileUploadSize,
      ["surveys", surveyId, "elements", elementId]
    );

    if (!signedUrlResponse.ok) {
      logger.error({ error: signedUrlResponse.error }, "Error getting signed url for upload");
      const errorResponse = getErrorResponseFromStorageError(signedUrlResponse.error, { fileName });
      return errorResponse.status >= 500
        ? {
            response: errorResponse,
            error: signedUrlResponse.error,
          }
        : {
            response: errorResponse,
          };
    }

    return {
      response: responses.successResponse(signedUrlResponse.data),
    };
  },
  customRateLimitConfig: rateLimitConfigs.storage.upload,
});
