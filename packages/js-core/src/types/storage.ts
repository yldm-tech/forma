export interface TUploadFileConfig {
  allowedFileExtensions?: string[];
  surveyId?: string;
  elementId?: string;
}

export interface TUploadFileResponse {
  data: {
    signedUrl: string;
    fileUrl: string;
    signingData: {
      signature: string;
      timestamp: number;
      uuid: string;
    } | null;
    updatedFileName: string;
    presignedFields?: Record<string, string>;
    /** Sent only for a presigned PUT: the file is then PUT to uploadUrl with uploadHeaders instead of POSTed to signedUrl. */
    uploadMethod?: "put";
    uploadUrl?: string;
    uploadHeaders?: Record<string, string>;
  };
}

export interface TFileUploadParams {
  file: { type: string; name: string; base64: string };
  params: TUploadFileConfig;
}
