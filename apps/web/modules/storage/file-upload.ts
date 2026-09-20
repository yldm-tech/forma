import { STORAGE_CONFIGURATION_ERROR_CODES, type TStorageApiErrorDetails } from "@forma/types/storage";

export enum FileUploadError {
  NO_FILE = "no_file",
  INVALID_FILE_TYPE = "invalid_file_type",
  FILE_SIZE_EXCEEDED = "file_size_exceeded",
  UPLOAD_FAILED = "upload_failed",
  INVALID_FILE_NAME = "invalid_file_name",
  STORAGE_NOT_CONFIGURED = "storage_not_configured",
  STORAGE_UPLOAD_FAILED = "storage_upload_failed",
}

type UploadApiErrorResponse = {
  details?: TStorageApiErrorDetails;
};

const parseUploadApiError = async (response: Response): Promise<UploadApiErrorResponse | undefined> => {
  try {
    return (await response.json()) as UploadApiErrorResponse;
  } catch {
    return undefined;
  }
};

const getFileUploadErrorFromResponse = async (response: Response): Promise<FileUploadError> => {
  const json = await parseUploadApiError(response);

  if (response.status === 400 && json?.details?.fileName) {
    return FileUploadError.INVALID_FILE_NAME;
  }

  if (
    response.status >= 500 &&
    json?.details?.storage_error_code &&
    STORAGE_CONFIGURATION_ERROR_CODES.has(json.details.storage_error_code)
  ) {
    return FileUploadError.STORAGE_NOT_CONFIGURED;
  }

  return FileUploadError.UPLOAD_FAILED;
};

export const toBase64 = (file: File) =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.readAsDataURL(file);
    reader.onload = () => {
      resolve(reader.result);
    };
    reader.onerror = reject;
  });

export const handleFileUpload = async (
  file: File,
  workspaceId: string,
  allowedFileExtensions?: string[]
): Promise<{
  error?: FileUploadError;
  url: string;
}> => {
  try {
    if (!(file instanceof File)) {
      return {
        error: FileUploadError.NO_FILE,
        url: "",
      };
    }
    // `file.size` is metadata, so the size check no longer reads the file's bytes at all.
    const bufferKB = file.size / 1024;

    const MAX_FILE_SIZE_MB = 5;
    const maxSizeInKB = MAX_FILE_SIZE_MB * 1024;
    if (bufferKB > maxSizeInKB) {
      return {
        error: FileUploadError.FILE_SIZE_EXCEEDED,
        url: "",
      };
    }

    const payload = {
      fileName: file.name,
      fileType: file.type,
      allowedFileExtensions,
      workspaceId,
    };

    const response = await fetch("/api/v1/management/storage", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    if (!response.ok) {
      return {
        error: await getFileUploadErrorFromResponse(response),
        url: "",
      };
    }

    const json = await response.json();
    const { data } = json;

    const { signedUrl, fileUrl, presignedFields } = data as {
      signedUrl: string;
      presignedFields: Record<string, string>;
      fileUrl: string;
    };

    const formDataForS3 = new FormData();

    Object.entries(presignedFields).forEach(([key, value]) => {
      formDataForS3.append(key, value);
    });

    // The `File` goes on the form as-is and `fetch` streams it. Round-tripping it through a base64 data URL, `atob`, and a per-character array first held four copies of a 5 MB upload in memory synchronously — hundreds of MB for the spread alone — and the S3 object's key and content type come from `presignedFields` either way, not from this part.
    formDataForS3.append("file", file);

    let uploadResponse: Response;

    try {
      uploadResponse = await fetch(signedUrl, {
        method: "POST",
        body: formDataForS3,
      });
    } catch (err) {
      console.error("Error in uploading file: ", err);
      return {
        error: FileUploadError.STORAGE_UPLOAD_FAILED,
        url: "",
      };
    }

    if (!uploadResponse.ok) {
      return {
        error: FileUploadError.STORAGE_UPLOAD_FAILED,
        url: "",
      };
    }

    return {
      url: fileUrl,
    };
  } catch (error) {
    console.error("Error in uploading file: ", error);
    return {
      error: FileUploadError.UPLOAD_FAILED,
      url: "",
    };
  }
};
