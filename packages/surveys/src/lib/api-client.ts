import { TDisplayCreateInput } from "@forma/types/displays";
import { Result } from "@forma/types/error-handlers";
import { type ApiErrorResponse, FILE_UPLOAD_ERROR_NAMES } from "@forma/types/errors";
import { TSurveyQuotaAction } from "@forma/types/quota";
import { TResponseInput, TResponseUpdateInput } from "@forma/types/responses";
import type { TUploadFileConfig, TUploadFileResponse } from "@forma/types/storage";
import { STORAGE_CONFIGURATION_ERROR_CODES, type TStorageApiErrorDetails } from "@forma/types/storage-errors";
import { makeRequest } from "@/lib/utils";

type TResponseCreateResponseQuotaFull = {
  quotaFull: true;
  quota: { id: string; action: TSurveyQuotaAction; endingCardId?: string };
};

type TResponseCreateResponseWithoutQuota = {
  quotaFull: false;
};

type TResponseQuota = TResponseCreateResponseQuotaFull | TResponseCreateResponseWithoutQuota;

type TResponseCreateResponse = {
  id: string;
} & TResponseQuota;

type TResponseUpdateResponse = Record<string, unknown> & TResponseQuota;

type TUploadApiErrorResponse = ApiErrorResponse & {
  details?: ApiErrorResponse["details"] & TStorageApiErrorDetails;
};

const parseUploadErrorResponse = async (response: Response): Promise<TUploadApiErrorResponse | undefined> => {
  try {
    return (await response.json()) as TUploadApiErrorResponse;
  } catch {
    return undefined;
  }
};

// Simple API client using fetch
export class ApiClient {
  readonly appUrl: string;
  readonly workspaceId: string;

  constructor({ appUrl, workspaceId }: { appUrl: string; workspaceId: string }) {
    this.appUrl = appUrl;
    this.workspaceId = workspaceId;
  }

  async createDisplay(
    displayInput: Omit<TDisplayCreateInput, "workspaceId"> & { contactId?: string }
  ): Promise<Result<{ id: string }, ApiErrorResponse>> {
    const fromV1 = !!displayInput.userId;

    return makeRequest(
      this.appUrl,
      `/api/${fromV1 ? "v1" : "v2"}/client/${this.workspaceId}/displays`,
      "POST",
      displayInput
    );
  }

  async getResponseIdByDisplayId(
    displayId: string
  ): Promise<Result<{ responseId: string | null }, ApiErrorResponse>> {
    return makeRequest(
      this.appUrl,
      `/api/v1/client/${this.workspaceId}/displays/${displayId}/response`,
      "GET"
    );
  }

  async createResponse(
    responseInput: Omit<TResponseInput, "workspaceId"> & {
      contactId: string | null;
      recaptchaToken?: string;
    }
  ): Promise<Result<TResponseCreateResponse, ApiErrorResponse>> {
    const fromV1 = !!responseInput.userId;

    return makeRequest(
      this.appUrl,
      `/api/${fromV1 ? "v1" : "v2"}/client/${this.workspaceId}/responses`,
      "POST",
      responseInput
    );
  }

  async updateResponse({
    responseId,
    finished,
    endingId,
    data,
    ttc,
    variables,
    language,
    pinAuthToken,
    verifyToken,
  }: TResponseUpdateInput & { responseId: string }): Promise<
    Result<TResponseUpdateResponse, ApiErrorResponse>
  > {
    return makeRequest(this.appUrl, `/api/v1/client/${this.workspaceId}/responses/${responseId}`, "PUT", {
      finished,
      endingId,
      data,
      ttc,
      variables,
      language,
      pinAuthToken,
      // Left out rather than sent as null when there is none, so an update from any other page is the body it always was.
      verifyToken: verifyToken ?? undefined,
    });
  }

  async uploadFile(
    file: {
      type: string;
      name: string;
      base64: string;
    },
    { allowedFileExtensions, surveyId, elementId }: TUploadFileConfig | undefined = {}
  ): Promise<string> {
    if (!file.name || !file.type || !file.base64) {
      throw new Error(`Invalid file object`);
    }

    // The file is decoded before it is signed: the server signs a presigned PUT for its exact size.
    let blob: Blob;
    try {
      const binaryString = atob(file.base64.split(",")[1]);
      const uint8Array = Uint8Array.from([...binaryString].map((char) => char.charCodeAt(0)));
      blob = new Blob([uint8Array], { type: file.type });
    } catch (err) {
      console.error(err);
      throw new Error("Error uploading file");
    }

    const payload = {
      fileName: file.name,
      fileType: file.type,
      fileSize: blob.size,
      allowedFileExtensions,
      surveyId,
      elementId,
    };

    const response = await fetch(`${this.appUrl}/api/v1/client/${this.workspaceId}/storage`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      const json = await parseUploadErrorResponse(response);

      if (response.status === 400 && typeof json?.details?.maxFileSize === "number") {
        const err = new Error("File size exceeds the size limit for your plan");
        err.name = FILE_UPLOAD_ERROR_NAMES.FILE_TOO_LARGE;
        throw err;
      }

      if (response.status === 400 && json?.details?.fileName) {
        const err = new Error("Invalid file name");
        err.name = FILE_UPLOAD_ERROR_NAMES.INVALID_FILE_NAME;
        throw err;
      }

      if (
        response.status >= 500 &&
        json?.details?.storage_error_code &&
        STORAGE_CONFIGURATION_ERROR_CODES.has(json.details.storage_error_code)
      ) {
        const err = new Error("File upload service is not configured");
        err.name = FILE_UPLOAD_ERROR_NAMES.STORAGE_NOT_CONFIGURED;
        throw err;
      }

      throw new Error(`Upload failed with status: ${String(response.status)}`);
    }

    const json = (await response.json()) as TUploadFileResponse;

    const { data } = json;

    const { signedUrl, fileUrl, presignedFields, uploadMethod, uploadUrl, uploadHeaders } = data;

    // A presigned PUT when the server signs one (STORAGE_UPLOAD_METHOD=put, for stores without POST uploads such as Cloudflare R2); otherwise, and always from a server that predates it, the presigned POST.
    let target: string;
    let init: RequestInit;
    if (uploadMethod === "put" && uploadUrl) {
      // The whole body is the file; the store checks it against the signed Content-Type and Content-Length, which the browser sets from the body.
      target = uploadUrl;
      init = { method: "PUT", headers: uploadHeaders, body: blob };
    } else if (signedUrl && presignedFields) {
      const formData = new FormData();
      Object.entries(presignedFields).forEach(([key, value]) => {
        formData.append(key, value);
      });
      formData.append("file", blob);
      target = signedUrl;
      init = { method: "POST", body: formData };
    } else {
      throw new Error("Invalid response");
    }
    if (!fileUrl) {
      throw new Error("Invalid response");
    }

    let uploadResponse: Response;

    try {
      uploadResponse = await fetch(target, init);
    } catch (err) {
      console.error("Error uploading file", err);
      const error = new Error("File upload service is unavailable");
      error.name = FILE_UPLOAD_ERROR_NAMES.STORAGE_UPLOAD_FAILED;
      throw error;
    }

    if (!uploadResponse.ok) {
      const errorText = await uploadResponse.text();

      if (errorText.includes("EntityTooLarge")) {
        const error = new Error("File size exceeds the size limit for your plan");
        error.name = FILE_UPLOAD_ERROR_NAMES.FILE_TOO_LARGE;
        throw error;
      }

      const error = new Error(`Upload failed with status: ${String(uploadResponse.status)}`);
      error.name = FILE_UPLOAD_ERROR_NAMES.STORAGE_UPLOAD_FAILED;
      throw error;
    }

    return fileUrl;
  }
}
