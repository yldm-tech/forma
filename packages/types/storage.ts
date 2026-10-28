import { z } from "zod";
import { ZStorageUrl } from "./common";

// The storage error codes live in storage-errors.ts, free of zod, so the survey runtime reads them without the schema library.
export {
  STORAGE_CONFIGURATION_ERROR_CODES,
  STORAGE_ERROR_CODES,
  type TStorageApiErrorDetails,
  type TStorageConfigurationErrorCode,
} from "./storage-errors";

// Single source of truth for allowed file extensions
const ALLOWED_FILE_EXTENSIONS_TUPLE = [
  "heic",
  "png",
  "jpeg",
  "jpg",
  "webp",
  "ico",
  "pdf",
  "eml",
  "doc",
  "docx",
  "xls",
  "xlsx",
  "ppt",
  "pptx",
  "txt",
  "csv",
  "mp4",
  "mov",
  "avi",
  "mkv",
  "webm",
  "mp3",
  "zip",
  "rar",
  "7z",
  "tar",
] as const;

// Derive zod enum from the tuple
export const ZAllowedFileExtension = z.enum(ALLOWED_FILE_EXTENSIONS_TUPLE);

export type TAllowedFileExtension = z.infer<typeof ZAllowedFileExtension>;

// Export the array derived from the tuple
export const ALLOWED_FILE_EXTENSIONS: TAllowedFileExtension[] = [...ALLOWED_FILE_EXTENSIONS_TUPLE];

/**
 * Image extensions accepted for survey media (question/element/choice `imageUrl`) and offered by the
 * editor's image pickers. Single source of truth so URL validation, uploads, and the picker can't
 * drift apart.
 *
 * Kept a strict subset of the upload allowlist — `satisfies` enforces this at compile time — so any
 * URL that passes image validation is always something the user can actually upload to Forma.
 * `svg` is deliberately excluded: we don't sanitize SVGs, so allowing them would be an XSS vector.
 */
export const IMAGE_FILE_EXTENSIONS = [
  "png",
  "jpeg",
  "jpg",
  "webp",
  "heic",
] as const satisfies readonly TAllowedFileExtension[];

export const mimeTypes: Record<TAllowedFileExtension, string> = {
  heic: "image/heic",
  png: "image/png",
  jpeg: "image/jpeg",
  jpg: "image/jpeg",
  webp: "image/webp",
  ico: "image/x-icon",
  pdf: "application/pdf",
  eml: "message/rfc822",
  doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  txt: "text/plain",
  csv: "text/csv",
  mp4: "video/mp4",
  mov: "video/quicktime",
  avi: "video/x-msvideo",
  mkv: "video/x-matroska",
  webm: "video/webm",
  mp3: "audio/mpeg",
  zip: "application/zip",
  rar: "application/vnd.rar",
  "7z": "application/x-7z-compressed",
  tar: "application/x-tar",
};

export const ZAccessType = z.enum(["public", "private"]);
export type TAccessType = z.infer<typeof ZAccessType>;

export const ZUploadFileConfig = z.object({
  allowedFileExtensions: z.array(z.string()).optional(),
  surveyId: z.string().optional(),
  elementId: z.string().optional(),
});

export type TUploadFileConfig = z.infer<typeof ZUploadFileConfig>;

export const ZUploadPrivateFileRequest = z
  .object({
    fileName: z.string().trim().min(1),
    fileType: z.string().trim().min(1),
    allowedFileExtensions: z.array(ZAllowedFileExtension).optional(),
    // The size in bytes the client will upload. A size over the element's limit is refused before anything is signed, and with STORAGE_UPLOAD_METHOD=put the upload is signed as a presigned PUT of exactly this size. Clients from before it send none.
    fileSize: z.number().int().nonnegative().optional(),
    surveyId: z.cuid2(),
    elementId: z
      .string()
      .trim()
      .min(1)
      .regex(
        /^[a-zA-Z0-9_-]+$/,
        "Element id must contain only alphanumeric characters, hyphens, or underscores"
      ),
    workspaceId: z.cuid2(),
  })
  .superRefine((data, ctx) => {
    refineFileUploadInput({
      data: {
        fileName: data.fileName,
        fileType: data.fileType,
        allowedFileExtensions: data.allowedFileExtensions,
      },
      ctx,
    });
  });

export type TUploadPrivateFileRequest = z.infer<typeof ZUploadPrivateFileRequest>;

export const ZUploadFileResponse = z.object({
  data: z.object({
    signedUrl: z.string(),
    fileUrl: ZStorageUrl,
    signingData: z
      .object({
        signature: z.string(),
        timestamp: z.number(),
        uuid: z.string(),
      })
      .nullable(),
    presignedFields: z.record(z.string(), z.string()).optional(),
    updatedFileName: z.string(),
    // Sent only when the server signs a presigned PUT (STORAGE_UPLOAD_METHOD=put and a fileSize in the request): the file is then PUT to uploadUrl with uploadHeaders, as its whole body, instead of POSTed to signedUrl. Without uploadMethod the upload is the presigned POST.
    uploadMethod: z.literal("put").optional(),
    uploadUrl: z.string().optional(),
    uploadHeaders: z.record(z.string(), z.string()).optional(),
  }),
});

export type TUploadFileResponse = z.infer<typeof ZUploadFileResponse>;

export const ZUploadPublicFileRequest = z
  .object({
    fileName: z.string().trim().min(1),
    fileType: z.string().trim().min(1),
    workspaceId: z.cuid2(),
    allowedFileExtensions: z.array(ZAllowedFileExtension).optional(),
    // As in ZUploadPrivateFileRequest, against the 5 MB limit of a public file.
    fileSize: z.number().int().nonnegative().optional(),
  })
  .superRefine((data, ctx) => {
    refineFileUploadInput({
      data: {
        fileName: data.fileName,
        fileType: data.fileType,
        allowedFileExtensions: data.allowedFileExtensions,
      },
      ctx,
    });
  });

export type TUploadPublicFileRequest = z.infer<typeof ZUploadPublicFileRequest>;

const refineFileUploadInput = ({
  data,
  ctx,
}: {
  data: {
    fileName: string;
    fileType: string;
    allowedFileExtensions?: TAllowedFileExtension[];
  };
  ctx: z.RefinementCtx;
}): void => {
  const fileExtension = data.fileName.split(".").pop()?.toLowerCase() as TAllowedFileExtension | undefined;

  if (!fileExtension || fileExtension.toLowerCase() === data.fileName.toLowerCase()) {
    ctx.addIssue({
      code: "custom",
      message: "File name must have an extension",
      path: ["fileName"],
    });

    return;
  }

  const { success } = ZAllowedFileExtension.safeParse(fileExtension);

  if (!success) {
    ctx.addIssue({
      code: "custom",
      message: "File extension is not allowed for security reasons",
      path: ["fileName"],
    });

    return;
  }

  const normalizedFileType = data.fileType.toLowerCase().split(";")[0]; // removes parameters from fileType like "image/jpeg; charset=binary"
  if (normalizedFileType !== mimeTypes[fileExtension]) {
    ctx.addIssue({
      code: "custom",
      message: "File type doesn't match the file extension",
      path: ["fileType"],
    });

    return;
  }

  if (data.allowedFileExtensions?.length) {
    if (!data.allowedFileExtensions.includes(fileExtension)) {
      ctx.addIssue({
        code: "custom",
        message: `File extension is not allowed, allowed extensions are: ${data.allowedFileExtensions.join(", ")}`,
        path: ["fileName"],
      });
    }
  }
};
